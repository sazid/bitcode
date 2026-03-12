package internal

type PreviewType string

const (
	PreviewPlain    PreviewType = ""
	PreviewDiff     PreviewType = "diff"
	PreviewCode     PreviewType = "code"
	PreviewFileList PreviewType = "filelist"
	PreviewBash     PreviewType = "bash"
)

type Event struct {
	Name        string
	Args        []string
	Message     string
	Preview     []string    // optional preview lines (raw, no indentation)
	PreviewType PreviewType // how to render Preview (default: plain)
	IsError     bool        // if true, render ⏺ in red instead of green
}
