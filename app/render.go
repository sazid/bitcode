package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/sazid/bitcode/internal"
	"github.com/sazid/bitcode/internal/tools"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// renderMarkdown renders markdown text for terminal output using glamour.
func renderMarkdown(w io.Writer, t *Theme, text string) {
	rendered, err := glamour.Render(text, t.GlamourStyle)
	if err != nil {
		fmt.Fprintln(w, text)
		return
	}
	fmt.Fprint(w, strings.TrimRight(rendered, "\n")+"\n")
}

// Spinner shows a simple last-line animation while the agent is working.
type Spinner struct {
	w    io.Writer
	stop chan struct{}
	done chan struct{}
}

func StartSpinner(w io.Writer, t *Theme) *Spinner {
	s := &Spinner{w: w, stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()

		frame := 0
		for {
			select {
			case <-s.stop:
				fmt.Fprintf(w, "\r\033[K")
				return
			case <-ticker.C:
				glyph := spinnerFrames[frame%len(spinnerFrames)]
				frame++
				fmt.Fprintf(w, "\r\033[K  %s%s %sWorking…%s", t.ANSIDim(), glyph, t.ANSIDim(), t.ANSIReset())
			}
		}
	}()
	return s
}

func (s *Spinner) Stop() {
	close(s.stop)
	<-s.done
}

// eventBullet returns a colored bullet for normal events, or red for errors.
func eventBullet(t *Theme, isError bool) string {
	if isError {
		return t.ANSI(t.Error) + "●" + t.ANSIReset()
	}
	return t.ANSI(t.Primary) + "●" + t.ANSIReset()
}

func renderEvent(w io.Writer, t *Theme, e internal.Event) {
	switch e.Name {
	case "TodoWrite":
		renderTodoEvent(w, t, e, "Update Todos")
		return
	case "TodoRead":
		renderTodoEvent(w, t, e, "Read Todos")
		return
	case "Bash", "PowerShell":
		renderShellEvent(w, t, e)
		return
	case "Read", "Edit", "Write":
		renderFileEvent(w, t, e)
		return
	}

	if e.PreviewType == internal.PreviewGuard {
		renderGuardEvent(w, t, e)
		return
	}
	if e.PreviewType == internal.PreviewBash {
		renderShellEvent(w, t, e)
		return
	}

	title := e.Name
	if args := strings.TrimSpace(strings.Join(e.Args, " ")); args != "" {
		title = fmt.Sprintf("%s %s", title, args)
	}
	renderEventHeader(w, t, e, title)
	lines := formatPreviewLines(t, e.PreviewType, e.Preview)
	if shouldRenderEventMessage(e) {
		lines = append([]string{e.Message}, lines...)
	}
	renderEventLines(w, lines...)
}

func renderGuardEvent(w io.Writer, t *Theme, e internal.Event) {
	tool := firstArg(e.Args)
	title := "Guard"
	if tool != "" {
		title = fmt.Sprintf("Guard %s", tool)
	}
	renderEventHeader(w, t, e, title)
	renderEventLines(w, t.ANSI(t.Warning)+e.Message+t.ANSIReset())
}

func renderShellEvent(w io.Writer, t *Theme, e internal.Event) {
	command := ""
	if len(e.Args) > 1 {
		command = e.Args[1]
	}
	shellPath := tools.GetShellInfo().Path
	title := fmt.Sprintf("Execute [%s]", shellPath)
	if command != "" {
		title = fmt.Sprintf("%s %s", title, command)
	}
	renderEventHeader(w, t, e, title)
	lines := formatPreviewLines(t, e.PreviewType, e.Preview)
	if e.IsError && e.Message != "" {
		lines = append([]string{t.ANSI(t.Error) + e.Message + t.ANSIReset()}, lines...)
	} else if shouldRenderEventMessage(e) {
		lines = append([]string{e.Message}, lines...)
	}
	renderEventLines(w, lines...)
}

func renderFileEvent(w io.Writer, t *Theme, e internal.Event) {
	target := displayPath(firstArg(e.Args))
	title := e.Name
	if e.Name == "Read" {
		title = buildReadTitle(target, readRangeArg(e.Args))
	} else if e.Name == "Edit" && target != "" {
		title = fmt.Sprintf("Update %s", target)
	} else if target != "" {
		title = fmt.Sprintf("%s %s", e.Name, target)
	}
	renderEventHeader(w, t, e, title)
	lines := formatPreviewLines(t, e.PreviewType, e.Preview)
	if shouldRenderEventMessage(e) {
		lines = append([]string{e.Message}, lines...)
	}
	renderEventLines(w, lines...)
}

func renderTodoEvent(w io.Writer, t *Theme, e internal.Event, action string) {
	title := action
	if count := firstArg(e.Args); count != "" {
		title = fmt.Sprintf("%s %s", action, count)
	} else if count := countTodoPreviewItems(e.Preview); count > 0 {
		title = fmt.Sprintf("%s %d item(s)", action, count)
	}
	renderTodoEventHeader(w, t, title)
	lines := make([]string, 0, len(e.Preview))
	for _, line := range e.Preview {
		lines = append(lines, renderTodoPreviewLine(t, line))
	}
	if len(lines) == 0 && e.Message != "" {
		lines = append(lines, t.ANSI(t.Primary)+e.Message+t.ANSIReset())
	}
	renderEventLines(w, lines...)
}

func renderTodoEventHeader(w io.Writer, t *Theme, title string) {
	timestamp := fmt.Sprintf("%s[%s]%s", t.ANSIDim(), time.Now().Format("15:04:05"), t.ANSIReset())
	fmt.Fprintf(w, "%s %s %s%s%s\n", t.ANSI(t.Primary)+"●"+t.ANSIReset(), timestamp, t.ANSI(t.Primary), title, t.ANSIReset())
}

func renderEventHeader(w io.Writer, t *Theme, e internal.Event, title string) {
	timestamp := fmt.Sprintf("%s[%s]%s", t.ANSIDim(), time.Now().Format("15:04:05"), t.ANSIReset())
	titleText := t.ANSIDim() + title + t.ANSIReset()
	if e.IsError {
		titleText = t.ANSI(t.Error) + title + t.ANSIReset()
	}
	fmt.Fprintf(w, "%s %s %s\n", eventBullet(t, e.IsError), timestamp, titleText)
}

func renderEventLines(w io.Writer, lines ...string) {
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fmt.Fprintf(w, "  %s\n", line)
	}
}

func formatPreviewLines(t *Theme, pt internal.PreviewType, lines []string) []string {
	formatted := make([]string, 0, len(lines))
	for _, line := range lines {
		formatted = append(formatted, renderPreviewLine(t, pt, line))
	}
	return formatted
}

func renderTodoPreviewLine(t *Theme, line string) string {
	switch {
	case strings.HasPrefix(line, "[✓] "):
		return fmt.Sprintf("%s󰄵%s %s", t.ANSI(t.Success), t.ANSIReset(), strings.TrimPrefix(line, "[✓] "))
	case strings.HasPrefix(line, "[~] "):
		return fmt.Sprintf("%s󰄗%s %s", t.ANSI(t.Primary), t.ANSIReset(), strings.TrimPrefix(line, "[~] "))
	case strings.HasPrefix(line, "[ ] "):
		return fmt.Sprintf("%s󰄌%s %s", t.ANSI(t.Secondary), t.ANSIReset(), strings.TrimPrefix(line, "[ ] "))
	default:
		return t.ANSI(t.Primary) + line + t.ANSIReset()
	}
}

func countTodoPreviewItems(lines []string) int {
	count := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "...") {
			continue
		}
		count++
	}
	return count
}

func firstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

func readRangeArg(args []string) string {
	if len(args) < 2 {
		return ""
	}
	return args[1]
}

func buildReadTitle(path, lineRange string) string {
	title := "Read"
	if path == "" {
		return title
	}
	if lineRange == "" {
		return fmt.Sprintf("%s %s", title, path)
	}
	if lineRange == "empty" {
		return fmt.Sprintf("%s %s (empty)", title, path)
	}
	return fmt.Sprintf("%s %s:%s", title, path, lineRange)
}

func displayPath(path string) string {
	if path == "" {
		return ""
	}
	cwd, err := os.Getwd()
	if err == nil {
		if rel, relErr := filepath.Rel(cwd, path); relErr == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(path)
}

func shouldRenderEventMessage(e internal.Event) bool {
	if strings.TrimSpace(e.Message) == "" {
		return false
	}
	if (e.Name == "Bash" || e.Name == "PowerShell") && !e.IsError && strings.HasPrefix(e.Message, "Exit code: 0") {
		return false
	}
	if e.Name == "Read" && strings.HasPrefix(e.Message, "Read ") && strings.HasSuffix(e.Message, " lines") {
		return false
	}
	if e.Name == "Edit" && e.PreviewType == internal.PreviewDiff && strings.HasPrefix(e.Message, "Replaced ") {
		return false
	}
	return true
}

func renderPreviewLine(t *Theme, pt internal.PreviewType, line string) string {
	switch pt {
	case internal.PreviewDiff:
		switch {
		case strings.HasPrefix(line, "@@"), strings.HasPrefix(line, "--- "), strings.HasPrefix(line, "+++ "), line == "...":
			return line
		case strings.HasPrefix(line, "+"):
			return t.ANSI(t.Success) + line + t.ANSIReset()
		case strings.HasPrefix(line, "-"):
			return t.ANSI(t.Error) + line + t.ANSIReset()
		default:
			return line
		}
	case internal.PreviewBash:
		if strings.HasPrefix(line, "stderr:") {
			return t.ANSI(t.Error) + strings.TrimPrefix(line, "stderr:") + t.ANSIReset()
		}
		return line
	case internal.PreviewCode:
		return line
	case internal.PreviewFileList:
		return line
	default:
		return line
	}
}
