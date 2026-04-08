package agent

import "testing"

func TestConfigZeroValue(t *testing.T) {
	var cfg Config
	if cfg.MaxTurns != 0 {
		t.Error("expected zero value for MaxTurns")
	}
	if cfg.Name != "" {
		t.Error("expected empty name")
	}
}

func TestResultConstruction(t *testing.T) {
	r := Result{Output: "hello"}
	if r.Output != "hello" {
		t.Errorf("expected 'hello', got %q", r.Output)
	}
}

func TestDefaultMaxTurns(t *testing.T) {
	if DefaultMaxTurns != 200 {
		t.Errorf("expected 200, got %d", DefaultMaxTurns)
	}
}
