package conversation

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sazid/bitcode/internal/llm"
)

// Metadata holds conversation metadata (stored as first line of JSONL file).
type Metadata struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	WorkDir      string    `json:"work_dir,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	MessageCount int       `json:"message_count"`
}

// Conversation holds a full conversation including its messages.
type Conversation struct {
	Metadata
	Messages []llm.Message `json:"-"` // loaded separately
}

// Manager handles conversation persistence and retrieval.
type Manager struct {
	dir     string
	workDir string
	mu      sync.RWMutex
}

// NewManager creates a new conversation manager.
func NewManager(dir string, workDir string) (*Manager, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create conversations dir: %w", err)
	}
	return &Manager{dir: dir, workDir: workDir}, nil
}

// DefaultDir returns the default conversations directory (~/.bitcode/conversations/).
func DefaultDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".bitcode", "conversations")
}

// Create creates a new conversation with the given title.
func (m *Manager) Create(title string) (*Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	conv := &Conversation{
		Metadata: Metadata{
			ID:        generateID(),
			Title:     truncateTitle(title),
			WorkDir:   m.workDir,
			CreatedAt: now,
			UpdatedAt: now,
		},
		Messages: []llm.Message{},
	}

	if err := m.saveLocked(conv); err != nil {
		return nil, err
	}

	return conv, nil
}

// Load loads a conversation by ID, including all messages.
func (m *Manager) Load(id string) (*Conversation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.loadByIDLocked(id)
}

// LoadMetadata loads only the metadata for a conversation.
func (m *Manager) LoadMetadata(id string) (*Metadata, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	path := filepath.Join(m.dir, id+".jsonl")
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open conversation: %w", err)
	}
	defer file.Close()
	return m.loadMetadataLocked(file)
}

// Save saves a conversation (metadata + messages).
func (m *Manager) Save(conv *Conversation) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	conv.UpdatedAt = time.Now()
	conv.MessageCount = len(conv.Messages)
	return m.saveLocked(conv)
}

// AppendMessage appends a single message to a conversation.
func (m *Manager) AppendMessage(id string, msg llm.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	path := filepath.Join(m.dir, id+".jsonl")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open conversation: %w", err)
	}
	defer file.Close()

	enc := json.NewEncoder(file)
	if err := enc.Encode(msg); err != nil {
		return fmt.Errorf("encode message: %w", err)
	}

	return nil
}

// List returns metadata for all conversations, sorted by updated_at desc.
// If showAll is false, only conversations from the current working directory are returned.
func (m *Manager) List(showAll bool, limit int) ([]Metadata, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return nil, fmt.Errorf("read conversations dir: %w", err)
	}

	var metas []Metadata
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}

		id := strings.TrimSuffix(entry.Name(), ".jsonl")
		path := filepath.Join(m.dir, id+".jsonl")
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		meta, err := m.loadMetadataLocked(file)
		file.Close()
		if err != nil {
			continue
		}
		if !showAll && meta.WorkDir != m.workDir {
			continue
		}
		metas = append(metas, *meta)
	}

	sort.Slice(metas, func(i, j int) bool {
		return metas[i].UpdatedAt.After(metas[j].UpdatedAt)
	})

	if limit > 0 && len(metas) > limit {
		metas = metas[:limit]
	}

	return metas, nil
}

// Search searches all conversations for the given query (case-insensitive).
// Returns conversation IDs that contain the query in any message content.
func (m *Manager) Search(query string, showAll bool, limit int) ([]SearchResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	query = strings.ToLower(query)

	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return nil, fmt.Errorf("read conversations dir: %w", err)
	}

	var results []SearchResult
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}

		id := strings.TrimSuffix(entry.Name(), ".jsonl")
		conv, err := m.loadByIDLocked(id)
		if err != nil {
			continue
		}

		if !showAll && conv.WorkDir != m.workDir {
			continue
		}

		if matches := searchMessages(conv.Messages, query); len(matches) > 0 {
			results = append(results, SearchResult{
				Metadata: conv.Metadata,
				Matches:  matches,
			})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].UpdatedAt.After(results[j].UpdatedAt)
	})

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// SearchResult holds a search result with matched message indices.
type SearchResult struct {
	Metadata
	Matches []int // indices of matched messages
}

// Fork creates a new conversation from an existing one, copying messages up to (but not including) msgIdx.
// If msgIdx is -1 or >= len(messages), all messages are copied.
func (m *Manager) Fork(sourceID string, newTitle string, msgIdx int) (*Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	source, err := m.loadByIDLocked(sourceID)
	if err != nil {
		return nil, err
	}

	if msgIdx < 0 || msgIdx > len(source.Messages) {
		msgIdx = len(source.Messages)
	}

	now := time.Now()
	forked := &Conversation{
		Metadata: Metadata{
			ID:           generateID(),
			Title:        truncateTitle(newTitle),
			WorkDir:      source.WorkDir,
			CreatedAt:    now,
			UpdatedAt:    now,
			MessageCount: msgIdx,
		},
		Messages: make([]llm.Message, msgIdx),
	}
	copy(forked.Messages, source.Messages[:msgIdx])

	if err := m.saveLocked(forked); err != nil {
		return nil, err
	}

	return forked, nil
}

// Truncate rewrites an existing conversation to keep only messages up to msgIdx.
// If msgIdx is -1, all messages are kept. If msgIdx is 0, all messages are removed.
func (m *Manager) Truncate(id string, msgIdx int) (*Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	conv, err := m.loadByIDLocked(id)
	if err != nil {
		return nil, err
	}

	if msgIdx < 0 || msgIdx > len(conv.Messages) {
		msgIdx = len(conv.Messages)
	}

	trimmed := make([]llm.Message, msgIdx)
	copy(trimmed, conv.Messages[:msgIdx])
	conv.Messages = trimmed
	conv.UpdatedAt = time.Now()
	conv.MessageCount = len(trimmed)

	if err := m.saveLocked(conv); err != nil {
		return nil, err
	}

	return conv, nil
}

// Rename updates the title of a conversation.
func (m *Manager) Rename(id string, newTitle string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	conv, err := m.loadByIDLocked(id)
	if err != nil {
		return err
	}

	conv.Title = truncateTitle(newTitle)
	conv.UpdatedAt = time.Now()
	return m.saveLocked(conv)
}

// Delete removes a conversation.
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	path := filepath.Join(m.dir, id+".jsonl")
	return os.Remove(path)
}

// Helper methods (must hold lock when calling)

func (m *Manager) saveLocked(conv *Conversation) error {
	path := filepath.Join(m.dir, conv.ID+".jsonl")
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create conversation file: %w", err)
	}
	defer file.Close()

	// Write metadata as first line
	enc := json.NewEncoder(file)
	if err := enc.Encode(conv.Metadata); err != nil {
		return fmt.Errorf("encode metadata: %w", err)
	}

	// Write messages
	for _, msg := range conv.Messages {
		if err := enc.Encode(msg); err != nil {
			return fmt.Errorf("encode message: %w", err)
		}
	}

	return nil
}

func (m *Manager) loadFromFileLocked(file *os.File) (*Conversation, error) {
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	// First line is metadata
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("read metadata: %w", err)
		}
		return nil, fmt.Errorf("empty conversation file")
	}

	var meta Metadata
	if err := json.Unmarshal(scanner.Bytes(), &meta); err != nil {
		return nil, fmt.Errorf("decode metadata: %w", err)
	}

	// Remaining lines are messages
	var messages []llm.Message
	for scanner.Scan() {
		var msg llm.Message
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			return nil, fmt.Errorf("decode message: %w", err)
		}
		messages = append(messages, msg)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan messages: %w", err)
	}

	meta.MessageCount = len(messages)

	return &Conversation{
		Metadata: meta,
		Messages: messages,
	}, nil
}

func (m *Manager) loadMetadataLocked(file *os.File) (*Metadata, error) {
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	if !scanner.Scan() {
		return nil, fmt.Errorf("empty conversation file")
	}

	var meta Metadata
	if err := json.Unmarshal(scanner.Bytes(), &meta); err != nil {
		return nil, fmt.Errorf("decode metadata: %w", err)
	}

	// Count remaining lines as messages
	count := 0
	for scanner.Scan() {
		count++
	}
	meta.MessageCount = count

	return &meta, nil
}

func (m *Manager) loadByIDLocked(id string) (*Conversation, error) {
	path := filepath.Join(m.dir, id+".jsonl")
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open conversation: %w", err)
	}
	defer file.Close()
	return m.loadFromFileLocked(file)
}

// searchMessages searches messages for query and returns matching indices.
func searchMessages(messages []llm.Message, query string) []int {
	var matches []int
	for i, msg := range messages {
		text := msg.Text()
		if strings.Contains(strings.ToLower(text), query) {
			matches = append(matches, i)
		}
	}
	return matches
}

// generateID creates a short random ID (e.g., "swift-falcon-a7b2c3").
func generateID() string {
	adjectives := []string{"swift", "bright", "calm", "bold", "cool", "keen", "quiet", "grand"}
	nouns := []string{"falcon", "eagle", "hawk", "owl", "wolf", "bear", "lynx", "stag"}

	b := make([]byte, 5)
	rand.Read(b)

	adj := adjectives[int(b[0])%len(adjectives)]
	noun := nouns[int(b[1])%len(nouns)]
	suffix := hex.EncodeToString(b[2:])

	return fmt.Sprintf("%s-%s-%s", adj, noun, suffix)
}

// truncateTitle truncates a title to a reasonable length.
func truncateTitle(title string) string {
	const maxLen = 60
	if len(title) <= maxLen {
		return title
	}
	return title[:maxLen-3] + "..."
}
