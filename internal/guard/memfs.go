package guard

import (
	"io"
	"io/fs"
	"strings"
	"time"
)

// memFS implements fs.FS backed by a flat map of filename → content.
// Used to expose embedded skill byte slices as an fs.FS for skills.NewManager.
type memFS map[string][]byte

func newMemFS(files map[string][]byte) memFS {
	return memFS(files)
}

func (m memFS) Open(name string) (fs.File, error) {
	// Strip leading "./" if present
	name = strings.TrimPrefix(name, "./")

	if name == "." {
		return &memDir{files: m}, nil
	}

	data, ok := m[name]
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &memFile{name: name, reader: strings.NewReader(string(data)), size: int64(len(data))}, nil
}

// ReadDir implements fs.ReadDirFS so that fs.ReadDir works on memFS.
func (m memFS) ReadDir(name string) ([]fs.DirEntry, error) {
	name = strings.TrimPrefix(name, "./")
	if name != "." && name != "" {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	entries := make([]fs.DirEntry, 0, len(m))
	for fname, data := range m {
		entries = append(entries, &memDirEntry{name: fname, size: int64(len(data))})
	}
	return entries, nil
}

// memFile is a single in-memory file.
type memFile struct {
	name   string
	reader *strings.Reader
	size   int64
}

func (f *memFile) Stat() (fs.FileInfo, error) {
	return &memFileInfo{name: f.name, size: f.size}, nil
}
func (f *memFile) Read(b []byte) (int, error) { return f.reader.Read(b) }
func (f *memFile) Close() error               { return nil }

// memDir represents the root directory of a memFS.
type memDir struct {
	files memFS
	pos   int
	names []string
}

func (d *memDir) Stat() (fs.FileInfo, error) {
	return &memFileInfo{name: ".", isDir: true}, nil
}
func (d *memDir) Read([]byte) (int, error) { return 0, io.EOF }
func (d *memDir) Close() error             { return nil }

// memFileInfo implements fs.FileInfo.
type memFileInfo struct {
	name  string
	size  int64
	isDir bool
}

func (fi *memFileInfo) Name() string      { return fi.name }
func (fi *memFileInfo) Size() int64       { return fi.size }
func (fi *memFileInfo) Mode() fs.FileMode { return 0o444 }
func (fi *memFileInfo) ModTime() time.Time { return time.Time{} }
func (fi *memFileInfo) IsDir() bool       { return fi.isDir }
func (fi *memFileInfo) Sys() any          { return nil }

// memDirEntry implements fs.DirEntry.
type memDirEntry struct {
	name string
	size int64
}

func (e *memDirEntry) Name() string               { return e.name }
func (e *memDirEntry) IsDir() bool                { return false }
func (e *memDirEntry) Type() fs.FileMode          { return 0 }
func (e *memDirEntry) Info() (fs.FileInfo, error) { return &memFileInfo{name: e.name, size: e.size}, nil }
