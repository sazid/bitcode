package sse

import (
	"io"
	"strings"
	"testing"
)

func TestReader_BasicEvents(t *testing.T) {
	input := "event: message\ndata: hello world\n\nevent: done\ndata: bye\n\n"
	r := NewReader(strings.NewReader(input))

	ev, err := r.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev.Type != "message" || ev.Data != "hello world" {
		t.Fatalf("unexpected event: %+v", ev)
	}

	ev, err = r.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev.Type != "done" || ev.Data != "bye" {
		t.Fatalf("unexpected event: %+v", ev)
	}

	_, err = r.Next()
	if err != io.EOF {
		t.Fatalf("expected EOF, got: %v", err)
	}
}

func TestReader_MultiLineData(t *testing.T) {
	input := "data: line1\ndata: line2\ndata: line3\n\n"
	r := NewReader(strings.NewReader(input))

	ev, err := r.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev.Data != "line1\nline2\nline3" {
		t.Fatalf("unexpected data: %q", ev.Data)
	}
}

func TestReader_Comments(t *testing.T) {
	input := ": this is a comment\nevent: test\ndata: value\n\n"
	r := NewReader(strings.NewReader(input))

	ev, err := r.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev.Type != "test" || ev.Data != "value" {
		t.Fatalf("unexpected event: %+v", ev)
	}
}

func TestReader_IDField(t *testing.T) {
	input := "id: 42\nevent: update\ndata: payload\n\n"
	r := NewReader(strings.NewReader(input))

	ev, err := r.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev.ID != "42" || ev.Type != "update" || ev.Data != "payload" {
		t.Fatalf("unexpected event: %+v", ev)
	}
}

func TestReader_NoEventType(t *testing.T) {
	input := "data: just data\n\n"
	r := NewReader(strings.NewReader(input))

	ev, err := r.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev.Type != "" || ev.Data != "just data" {
		t.Fatalf("unexpected event: %+v", ev)
	}
}

func TestReader_EmptyStream(t *testing.T) {
	r := NewReader(strings.NewReader(""))
	_, err := r.Next()
	if err != io.EOF {
		t.Fatalf("expected EOF, got: %v", err)
	}
}

func TestReader_TrailingDataWithoutBlankLine(t *testing.T) {
	// Stream ends without trailing blank line — should still return the event
	input := "data: final"
	r := NewReader(strings.NewReader(input))

	ev, err := r.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev.Data != "final" {
		t.Fatalf("unexpected data: %q", ev.Data)
	}
}

func TestReader_SkipEmptyLinesBetweenEvents(t *testing.T) {
	input := "\n\ndata: first\n\n\n\ndata: second\n\n"
	r := NewReader(strings.NewReader(input))

	ev, err := r.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev.Data != "first" {
		t.Fatalf("unexpected data: %q", ev.Data)
	}

	ev, err = r.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev.Data != "second" {
		t.Fatalf("unexpected data: %q", ev.Data)
	}
}
