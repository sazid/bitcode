package plugin

import (
	"os"
	"path/filepath"
)

// BaseDirs are the directory prefixes to scan for plugins, in order of
// increasing precedence (.bitcode wins over .claude wins over .agents).
var BaseDirs = []string{".agents", ".claude", ".bitcode"}

// ScanDirs returns directories to scan for a given subDir in precedence order.
// Home directories (lower precedence) come first, project directories (higher
// precedence) come last.
func ScanDirs(subDir string) []string {
	var dirs []string

	home, _ := os.UserHomeDir()
	wd, _ := os.Getwd()

	if home != "" {
		for _, d := range BaseDirs {
			dirs = append(dirs, filepath.Join(home, d, subDir))
		}
	}

	for _, d := range BaseDirs {
		dirs = append(dirs, filepath.Join(wd, d, subDir))
	}

	return dirs
}
