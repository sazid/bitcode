package telemetry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Store writes telemetry events as JSONL to ~/.bitcode/telemetry/{date}.jsonl.
type Store struct {
	dir     string
	file    *os.File
	fileDay string // "2006-01-02" of the currently open file
}

// NewStore creates a Store that writes to the given directory.
// The directory is created if it doesn't exist.
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create telemetry dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Write appends an event as a single JSON line.
func (s *Store) Write(ev Event) error {
	day := ev.Timestamp.Format("2006-01-02")

	// Rotate file if day changed
	if s.file == nil || day != s.fileDay {
		if s.file != nil {
			s.file.Close()
		}
		path := filepath.Join(s.dir, day+".jsonl")
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("open telemetry file: %w", err)
		}
		s.file = f
		s.fileDay = day
	}

	data, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	data = append(data, '\n')
	_, err = s.file.Write(data)
	return err
}

// Close closes the underlying file.
func (s *Store) Close() error {
	if s.file != nil {
		return s.file.Close()
	}
	return nil
}

// DefaultDir returns the default telemetry directory (~/.bitcode/telemetry/).
func DefaultDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".bitcode", "telemetry")
}

// now is a package-level function for testability.
var now = time.Now
