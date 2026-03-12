package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManager_LoadsSkillsFromDir(t *testing.T) {
	dir := t.TempDir()
	skillsDir := filepath.Join(dir, ".bitcode", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write a skill with a heading
	if err := os.WriteFile(filepath.Join(skillsDir, "commit.md"), []byte("# Create a git commit\nAnalyze changes and commit."), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write a skill without a heading
	if err := os.WriteFile(filepath.Join(skillsDir, "review.md"), []byte("Review the code for bugs."), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write a non-md file (should be ignored)
	if err := os.WriteFile(filepath.Join(skillsDir, "notes.txt"), []byte("not a skill"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := &Manager{skills: make(map[string]Skill)}
	m.loadDir(skillsDir, "project")

	if len(m.skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(m.skills))
	}

	commit, ok := m.Get("commit")
	if !ok {
		t.Fatal("expected 'commit' skill to exist")
	}
	if commit.Description != "Create a git commit" {
		t.Errorf("expected description 'Create a git commit', got %q", commit.Description)
	}
	if commit.Source != "project" {
		t.Errorf("expected source 'project', got %q", commit.Source)
	}

	review, ok := m.Get("review")
	if !ok {
		t.Fatal("expected 'review' skill to exist")
	}
	if review.Description != "" {
		t.Errorf("expected empty description, got %q", review.Description)
	}
}

func TestManager_ProjectOverridesUser(t *testing.T) {
	userDir := t.TempDir()
	projDir := t.TempDir()

	userSkills := filepath.Join(userDir, "skills")
	projSkills := filepath.Join(projDir, "skills")
	os.MkdirAll(userSkills, 0o755)
	os.MkdirAll(projSkills, 0o755)

	os.WriteFile(filepath.Join(userSkills, "commit.md"), []byte("# User commit\nuser version"), 0o644)
	os.WriteFile(filepath.Join(projSkills, "commit.md"), []byte("# Project commit\nproject version"), 0o644)

	m := &Manager{skills: make(map[string]Skill)}
	m.loadDir(userSkills, "user")
	m.loadDir(projSkills, "project")

	s, ok := m.Get("commit")
	if !ok {
		t.Fatal("expected 'commit' skill")
	}
	if s.Description != "Project commit" {
		t.Errorf("expected project version to win, got %q", s.Description)
	}
	if s.Source != "project" {
		t.Errorf("expected source 'project', got %q", s.Source)
	}
}

func TestSkill_FormatPrompt(t *testing.T) {
	s := Skill{Prompt: "Review the code."}

	if got := s.FormatPrompt(""); got != "Review the code." {
		t.Errorf("no args: got %q", got)
	}

	if got := s.FormatPrompt("focus on security"); got != "Review the code.\n\nfocus on security" {
		t.Errorf("with args: got %q", got)
	}
}

func TestManager_EmptyDir(t *testing.T) {
	m := &Manager{skills: make(map[string]Skill)}
	m.loadDir("/nonexistent/path", "user")

	if len(m.List()) != 0 {
		t.Error("expected no skills from nonexistent dir")
	}
}
