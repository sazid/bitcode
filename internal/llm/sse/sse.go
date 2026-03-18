// Package sse provides a minimal Server-Sent Events (SSE) parser.
package sse

import (
	"bufio"
	"io"
	"strings"
)

// Event represents a single SSE event.
type Event struct {
	Type string // "event:" field (empty string if not specified)
	Data string // "data:" field (may span multiple lines, joined with \n)
	ID   string // "id:" field
}

// Reader reads SSE events from an io.Reader.
type Reader struct {
	scanner *bufio.Scanner
}

// NewReader creates a new SSE reader.
func NewReader(r io.Reader) *Reader {
	return &Reader{scanner: bufio.NewScanner(r)}
}

// Next returns the next SSE event. Returns io.EOF when the stream ends.
func (r *Reader) Next() (Event, error) {
	var ev Event
	var dataParts []string
	hasData := false

	for r.scanner.Scan() {
		line := r.scanner.Text()

		// Blank line marks end of an event
		if line == "" {
			if hasData {
				ev.Data = strings.Join(dataParts, "\n")
				return ev, nil
			}
			// No data accumulated yet; skip empty lines between events
			continue
		}

		// Lines starting with : are comments
		if strings.HasPrefix(line, ":") {
			continue
		}

		field, value, _ := strings.Cut(line, ":")
		// Per SSE spec: if value starts with a space, strip one leading space
		value = strings.TrimPrefix(value, " ")

		switch field {
		case "event":
			ev.Type = value
		case "data":
			dataParts = append(dataParts, value)
			hasData = true
		case "id":
			ev.ID = value
		}
	}

	if err := r.scanner.Err(); err != nil {
		return Event{}, err
	}

	// If we have accumulated data when the stream ends, return it
	if hasData {
		ev.Data = strings.Join(dataParts, "\n")
		return ev, nil
	}

	return Event{}, io.EOF
}
