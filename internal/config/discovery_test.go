package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# instructions\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_NOSYSTEM=1",
			"HOME="+dir,
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("commit", "--allow-empty", "-m", "init")
}

// --- WalkDir fallback tests (non-git dirs) ---

func TestDiscoverWalk_RootOnly(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "CLAUDE.md"))

	files := discoverProjectFilesWalk(dir)

	if len(files) != 1 || files[0] != "CLAUDE.md" {
		t.Errorf("expected [CLAUDE.md], got %v", files)
	}
}

func TestDiscoverWalk_Nested(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "CLAUDE.md"))
	writeFile(t, filepath.Join(dir, "service1", "CLAUDE.md"))
	writeFile(t, filepath.Join(dir, "service2", "CLAUDE.md"))
	writeFile(t, filepath.Join(dir, "service2", "big-feature", "CLAUDE.md"))

	result := DiscoverInstructionFiles(dir)

	expected := []string{
		"CLAUDE.md",
		"service1/CLAUDE.md",
		"service2/CLAUDE.md",
		"service2/big-feature/CLAUDE.md",
	}
	if len(result.ProjectFiles) != len(expected) {
		t.Fatalf("expected %d files, got %d: %v", len(expected), len(result.ProjectFiles), result.ProjectFiles)
	}
	for i, e := range expected {
		if result.ProjectFiles[i] != e {
			t.Errorf("index %d: expected %q, got %q", i, e, result.ProjectFiles[i])
		}
	}
}

func TestDiscoverWalk_SkippedDirs(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "CLAUDE.md"))
	writeFile(t, filepath.Join(dir, "node_modules", "CLAUDE.md"))
	writeFile(t, filepath.Join(dir, "vendor", "AGENTS.md"))

	files := discoverProjectFilesWalk(dir)

	if len(files) != 1 || files[0] != "CLAUDE.md" {
		t.Errorf("expected [CLAUDE.md], got %v", files)
	}
}

func TestDiscoverWalk_BothTypes(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "CLAUDE.md"))
	writeFile(t, filepath.Join(dir, "AGENTS.md"))
	writeFile(t, filepath.Join(dir, "api", "AGENTS.md"))

	result := DiscoverInstructionFiles(dir)

	expected := []string{"AGENTS.md", "CLAUDE.md", "api/AGENTS.md"}
	if len(result.ProjectFiles) != len(expected) {
		t.Fatalf("expected %d files, got %d: %v", len(expected), len(result.ProjectFiles), result.ProjectFiles)
	}
	for i, e := range expected {
		if result.ProjectFiles[i] != e {
			t.Errorf("index %d: expected %q, got %q", i, e, result.ProjectFiles[i])
		}
	}
}

func TestDiscoverWalk_Empty(t *testing.T) {
	dir := t.TempDir()

	result := DiscoverInstructionFiles(dir)

	if len(result.ProjectFiles) != 0 {
		t.Errorf("expected empty, got %v", result.ProjectFiles)
	}
	if len(result.UserFiles) != 0 {
		t.Errorf("expected empty user files, got %v", result.UserFiles)
	}
}

// --- Git-based discovery tests ---

func TestDiscoverGit_RespectsGitignore(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	writeFile(t, filepath.Join(dir, "CLAUDE.md"))
	writeFile(t, filepath.Join(dir, "service1", "CLAUDE.md"))
	writeFile(t, filepath.Join(dir, "ignored-dir", "CLAUDE.md"))

	// Add .gitignore that ignores the ignored-dir
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored-dir/\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := DiscoverInstructionFiles(dir)

	expected := []string{"CLAUDE.md", "service1/CLAUDE.md"}
	if len(result.ProjectFiles) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, result.ProjectFiles)
	}
	for i, e := range expected {
		if result.ProjectFiles[i] != e {
			t.Errorf("index %d: expected %q, got %q", i, e, result.ProjectFiles[i])
		}
	}
}

func TestDiscoverGit_GitignorePattern(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	writeFile(t, filepath.Join(dir, "CLAUDE.md"))
	writeFile(t, filepath.Join(dir, "AGENTS.md"))
	writeFile(t, filepath.Join(dir, "build", "CLAUDE.md"))
	writeFile(t, filepath.Join(dir, "tmp", "AGENTS.md"))

	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("build/\ntmp/\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := DiscoverInstructionFiles(dir)

	expected := []string{"AGENTS.md", "CLAUDE.md"}
	if len(result.ProjectFiles) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, result.ProjectFiles)
	}
	for i, e := range expected {
		if result.ProjectFiles[i] != e {
			t.Errorf("index %d: expected %q, got %q", i, e, result.ProjectFiles[i])
		}
	}
}

func TestDiscoverGit_NestedGitignore(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	writeFile(t, filepath.Join(dir, "CLAUDE.md"))
	writeFile(t, filepath.Join(dir, "service", "CLAUDE.md"))
	writeFile(t, filepath.Join(dir, "service", "generated", "AGENTS.md"))

	// Nested .gitignore inside service/
	if err := os.MkdirAll(filepath.Join(dir, "service"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "service", ".gitignore"), []byte("generated/\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := DiscoverInstructionFiles(dir)

	expected := []string{"CLAUDE.md", "service/CLAUDE.md"}
	if len(result.ProjectFiles) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, result.ProjectFiles)
	}
}
