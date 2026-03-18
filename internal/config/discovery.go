package config

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sazid/bitcode/internal/plugin"
)

// InstructionFiles holds discovered CLAUDE.md and AGENTS.md file paths.
type InstructionFiles struct {
	// ProjectFiles are paths relative to the project root directory.
	ProjectFiles []string
	// UserFiles are paths relative to the user's home directory (e.g. ".bitcode/AGENTS.md").
	UserFiles []string
}

// instructionFileNames lists the filenames we look for.
var instructionFileNames = map[string]bool{
	"CLAUDE.md": true,
	"AGENTS.md": true,
}

// defaultIgnoreDirs lists directories to skip during recursive project discovery.
// Used as fallback when git is not available.
var defaultIgnoreDirs = map[string]bool{
	".git":          true,
	"node_modules":  true,
	"vendor":        true,
	".venv":         true,
	"venv":          true,
	"__pycache__":   true,
	".tox":          true,
	".mypy_cache":   true,
	".pytest_cache": true,
	"dist":          true,
	"build":         true,
	".next":         true,
	".nuxt":         true,
	".gradle":       true,
	".idea":         true,
	".vscode":       true,
	".terraform":    true,
	".bundle":       true,
	".cache":        true,
}

// DiscoverInstructionFiles finds all CLAUDE.md and AGENTS.md files in projectDir
// and user-level config directories.
// In git repos, it uses "git ls-files" to respect .gitignore rules.
// Falls back to a filesystem walk with hardcoded ignore list for non-git directories.
func DiscoverInstructionFiles(projectDir string) InstructionFiles {
	var result InstructionFiles

	// Try git-based discovery first (respects .gitignore)
	if files, ok := discoverProjectFilesGit(projectDir); ok {
		result.ProjectFiles = files
	} else {
		result.ProjectFiles = discoverProjectFilesWalk(projectDir)
	}

	// User-level: check known config dirs (non-recursive, top-level only)
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		for _, dir := range plugin.BaseDirs {
			for name := range instructionFileNames {
				rel := filepath.Join(dir, name)
				abs := filepath.Join(home, rel)
				if _, err := os.Stat(abs); err == nil {
					result.UserFiles = append(result.UserFiles, rel)
				}
			}
		}
	}

	sort.Strings(result.ProjectFiles)
	sort.Strings(result.UserFiles)
	return result
}

// discoverProjectFilesGit uses "git ls-files" to find instruction files,
// automatically respecting .gitignore, .git/info/exclude, and global gitignore.
// Returns the file list and true on success, or nil and false if git is unavailable
// or the directory is not a git repository.
func discoverProjectFilesGit(projectDir string) ([]string, bool) {
	// git ls-files --cached --others --exclude-standard lists:
	//   --cached: tracked files
	//   --others: untracked files
	//   --exclude-standard: apply .gitignore, .git/info/exclude, global gitignore
	cmd := exec.Command("git", "ls-files", "--cached", "--others", "--exclude-standard")
	cmd.Dir = projectDir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = nil

	if err := cmd.Run(); err != nil {
		return nil, false
	}

	var files []string
	for line := range strings.SplitSeq(out.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		base := filepath.Base(line)
		if instructionFileNames[base] {
			files = append(files, line)
		}
	}
	return files, true
}

// discoverProjectFilesWalk uses filepath.WalkDir as a fallback for non-git directories.
func discoverProjectFilesWalk(projectDir string) []string {
	var files []string

	filepath.WalkDir(projectDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if defaultIgnoreDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if instructionFileNames[d.Name()] {
			rel, err := filepath.Rel(projectDir, p)
			if err != nil {
				return nil
			}
			files = append(files, rel)
		}
		return nil
	})

	return files
}
