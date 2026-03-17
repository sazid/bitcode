package llm

import (
	"bufio"
	"encoding/json"
	"io"
)

// SaveConversation writes messages as JSONL (one JSON object per line).
func SaveConversation(messages []Message, w io.Writer) error {
	enc := json.NewEncoder(w)
	for _, m := range messages {
		if err := enc.Encode(m); err != nil {
			return err
		}
	}
	return nil
}

// LoadConversation reads messages from a JSONL reader.
func LoadConversation(r io.Reader) ([]Message, error) {
	var messages []Message
	scanner := bufio.NewScanner(r)
	// Allow up to 10MB per line for large tool results
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		var m Message
		if err := json.Unmarshal(scanner.Bytes(), &m); err != nil {
			return nil, err
		}
		messages = append(messages, m)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return messages, nil
}
