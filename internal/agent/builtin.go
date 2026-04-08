package agent

import (
	"embed"
	"io/fs"
	"strings"
)

//go:embed agents/*.md
var builtinFS embed.FS

// BuiltinDefinitions returns the built-in agent definitions embedded in
// the binary. These have the lowest precedence — disk definitions with the
// same name will overwrite them.
func BuiltinDefinitions() []Definition {
	entries, err := fs.ReadDir(builtinFS, "agents")
	if err != nil {
		return nil
	}

	var defs []Definition
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		data, err := fs.ReadFile(builtinFS, "agents/"+entry.Name())
		if err != nil {
			continue
		}

		def := parseDefinition(data, entry.Name(), "builtin")
		defs = append(defs, def)
	}
	return defs
}
