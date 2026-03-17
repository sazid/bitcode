package skills

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/sazid/bitcode/internal/plugin"
)

func TestParseFrontmatter(t *testing.T) {
	t.Run("with valid frontmatter", func(t *testing.T) {
		content := "---\nname: deploy\ndescription: Deploy the app\ntrigger: When the user asks to deploy\n---\nDo the deployment."
		fm, body := plugin.ParseFrontmatter(content)

		if fm["name"] != "deploy" {
			t.Errorf("expected name 'deploy', got %q", fm["name"])
		}
		if fm["description"] != "Deploy the app" {
			t.Errorf("expected description 'Deploy the app', got %q", fm["description"])
		}
		if fm["trigger"] != "When the user asks to deploy" {
			t.Errorf("expected trigger, got %q", fm["trigger"])
		}
		if body != "Do the deployment." {
			t.Errorf("expected body 'Do the deployment.', got %q", body)
		}
	})

	t.Run("without frontmatter", func(t *testing.T) {
		content := "# Simple Skill\nJust a prompt."
		fm, body := plugin.ParseFrontmatter(content)

		if fm != nil {
			t.Errorf("expected nil frontmatter, got %+v", fm)
		}
		if body != content {
			t.Errorf("expected original content, got %q", body)
		}
	})

	t.Run("malformed YAML", func(t *testing.T) {
		content := "---\n: invalid: yaml: [broken\n---\nBody here."
		fm, body := plugin.ParseFrontmatter(content)

		if fm != nil {
			t.Errorf("expected nil frontmatter on malformed YAML, got %+v", fm)
		}
		if body != content {
			t.Errorf("expected original content on malformed YAML, got %q", body)
		}
	})

	t.Run("no closing delimiter", func(t *testing.T) {
		content := "---\nname: test\nThis is not closed."
		fm, body := plugin.ParseFrontmatter(content)

		if fm != nil {
			t.Errorf("expected nil frontmatter, got %+v", fm)
		}
		if body != content {
			t.Errorf("expected original content, got %q", body)
		}
	})

	t.Run("partial frontmatter fields", func(t *testing.T) {
		content := "---\ndescription: Only a description\n---\nBody."
		fm, body := plugin.ParseFrontmatter(content)

		if fm["name"] != nil {
			t.Errorf("expected empty name, got %q", fm["name"])
		}
		if fm["description"] != "Only a description" {
			t.Errorf("expected 'Only a description', got %q", fm["description"])
		}
		if body != "Body." {
			t.Errorf("expected 'Body.', got %q", body)
		}
	})

	t.Run("extra metadata fields", func(t *testing.T) {
		content := "---\nname: bash-guard\nlanguage: bash\nauto_invoke: true\n---\nBody."
		fm, _ := plugin.ParseFrontmatter(content)

		if fm["language"] != "bash" {
			t.Errorf("expected language 'bash', got %q", fm["language"])
		}
		if fm["auto_invoke"] != true {
			t.Errorf("expected auto_invoke true, got %v", fm["auto_invoke"])
		}
	})
}

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
	m.loadDirRecursive(skillsDir, "project", "")

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
	m.loadDirRecursive(userSkills, "user", "")
	m.loadDirRecursive(projSkills, "project", "")

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
	m.loadDirRecursive("/nonexistent/path", "user", "")

	if len(m.List()) != 0 {
		t.Error("expected no skills from nonexistent dir")
	}
}

func TestManager_FrontmatterOverridesHeading(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: custom-name\ndescription: From frontmatter\ntrigger: When user asks to deploy\n---\n# Heading description\nBody of the skill."
	os.WriteFile(filepath.Join(dir, "deploy.md"), []byte(content), 0o644)

	m := &Manager{skills: make(map[string]Skill)}
	m.loadDirRecursive(dir, "project", "")

	// Frontmatter name should override filename
	s, ok := m.Get("custom-name")
	if !ok {
		t.Fatal("expected skill registered as 'custom-name' from frontmatter name")
	}

	// Frontmatter description should override heading
	if s.Description != "From frontmatter" {
		t.Errorf("expected 'From frontmatter', got %q", s.Description)
	}

	if s.Trigger != "When user asks to deploy" {
		t.Errorf("expected trigger, got %q", s.Trigger)
	}

	// Should NOT be registered under filename
	if _, ok := m.Get("deploy"); ok {
		t.Error("skill should not be registered under filename when frontmatter provides name")
	}
}

func TestManager_NestedSkills(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "git")
	os.MkdirAll(subDir, 0o755)

	os.WriteFile(filepath.Join(dir, "review.md"), []byte("# Review code\nReview."), 0o644)
	os.WriteFile(filepath.Join(subDir, "commit.md"), []byte("# Git commit\nCommit changes."), 0o644)

	m := &Manager{skills: make(map[string]Skill)}
	m.loadDirRecursive(dir, "project", "")

	// Top-level skill
	if _, ok := m.Get("review"); !ok {
		t.Error("expected top-level 'review' skill")
	}

	// Nested skill with namespace
	s, ok := m.Get("git:commit")
	if !ok {
		t.Fatal("expected namespaced 'git:commit' skill")
	}
	if s.Name != "git:commit" {
		t.Errorf("expected name 'git:commit', got %q", s.Name)
	}
	if s.Description != "Git commit" {
		t.Errorf("expected 'Git commit', got %q", s.Description)
	}
}

func TestManager_MultipleDirectoryPrecedence(t *testing.T) {
	base := t.TempDir()

	// Simulate .agents/skills and .bitcode/skills at same level
	agentsDir := filepath.Join(base, ".agents", "skills")
	claudeDir := filepath.Join(base, ".claude", "skills")
	bitcodeDir := filepath.Join(base, ".bitcode", "skills")
	os.MkdirAll(agentsDir, 0o755)
	os.MkdirAll(claudeDir, 0o755)
	os.MkdirAll(bitcodeDir, 0o755)

	os.WriteFile(filepath.Join(agentsDir, "deploy.md"), []byte("# Agents deploy\nfrom agents"), 0o644)
	os.WriteFile(filepath.Join(claudeDir, "deploy.md"), []byte("# Claude deploy\nfrom claude"), 0o644)
	os.WriteFile(filepath.Join(bitcodeDir, "deploy.md"), []byte("# Bitcode deploy\nfrom bitcode"), 0o644)

	// Also put a skill only in .agents
	os.WriteFile(filepath.Join(agentsDir, "unique.md"), []byte("# Unique\nonly in agents"), 0o644)

	m := &Manager{skills: make(map[string]Skill)}
	// Load in precedence order: .agents < .claude < .bitcode
	m.loadDirRecursive(agentsDir, "project", "")
	m.loadDirRecursive(claudeDir, "project", "")
	m.loadDirRecursive(bitcodeDir, "project", "")

	// .bitcode should win for "deploy"
	s, ok := m.Get("deploy")
	if !ok {
		t.Fatal("expected 'deploy' skill")
	}
	if s.Description != "Bitcode deploy" {
		t.Errorf("expected .bitcode to win, got %q", s.Description)
	}

	// Unique skill from .agents should still be present
	if _, ok := m.Get("unique"); !ok {
		t.Error("expected 'unique' skill from .agents")
	}
}

func TestManager_EmbeddedFS(t *testing.T) {
	// Embedded FS has flat paths (no subdirectory prefix) — the loader starts at ".".
	embeddedFS := fstest.MapFS{
		"bash.md": &fstest.MapFile{
			Data: []byte("---\nname: Bash Security Expert\ndescription: Bash security patterns\nlanguage: bash\nauto_invoke: true\n---\n# Bash Security\nPatterns here."),
		},
		"simulate.md": &fstest.MapFile{
			Data: []byte("---\nname: simulate\ndescription: Code simulation protocol\n---\nSimulate code."),
		},
	}

	m := NewManager(Config{
		SubDir:         "skills",
		Embedded:       embeddedFS,
		EmbeddedSource: "builtin",
	})

	bash, ok := m.Get("Bash Security Expert")
	if !ok {
		t.Fatal("expected 'Bash Security Expert' skill from embedded FS")
	}
	if bash.Source != "builtin" {
		t.Errorf("expected source 'builtin', got %q", bash.Source)
	}
	if bash.Metadata["language"] != "bash" {
		t.Errorf("expected language 'bash' in metadata, got %v", bash.Metadata["language"])
	}
	if bash.Metadata["auto_invoke"] != true {
		t.Errorf("expected auto_invoke true in metadata, got %v", bash.Metadata["auto_invoke"])
	}

	sim, ok := m.Get("simulate")
	if !ok {
		t.Fatal("expected 'simulate' skill from embedded FS")
	}
	if sim.Description != "Code simulation protocol" {
		t.Errorf("expected description, got %q", sim.Description)
	}
}

func TestManager_DiskOverridesEmbedded(t *testing.T) {
	embeddedFS := fstest.MapFS{
		"bash.md": &fstest.MapFile{
			Data: []byte("---\nname: bash-expert\ndescription: Embedded version\n---\nEmbedded body."),
		},
	}

	// Create a disk override in a temp directory
	dir := t.TempDir()
	bitcodeDir := filepath.Join(dir, ".bitcode", "guard-skills")
	os.MkdirAll(bitcodeDir, 0o755)
	os.WriteFile(filepath.Join(bitcodeDir, "bash.md"), []byte("---\nname: bash-expert\ndescription: Disk version\n---\nDisk body."), 0o644)

	// Use a custom Manager creation that loads embedded then the specific disk dir
	m := &Manager{skills: make(map[string]Skill), cfg: Config{SubDir: "guard-skills"}}
	m.loadEmbeddedDir(embeddedFS, ".", "builtin", "")
	m.loadDirRecursive(bitcodeDir, "project", "")

	s, ok := m.Get("bash-expert")
	if !ok {
		t.Fatal("expected 'bash-expert' skill")
	}
	if s.Description != "Disk version" {
		t.Errorf("expected disk to override embedded, got %q", s.Description)
	}
}
