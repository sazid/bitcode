package main

import (
	"fmt"
	"io"
	"math/rand"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/sazid/bitcode/internal"
	"github.com/sazid/bitcode/internal/tools"
)

var spinnerMessages = []string{
	"Thinking…",
	"Pondering…",
	"Reasoning…",
	"Cogitating…",
	"Ruminating…",
	"Contemplating…",
	"Brainstorming…",
	"Tokenizing…",
	"Crunching…",
	"Compiling…",
	"Parsing…",
	"Decoding…",
	"Diffing…",
	"Rebasing…",
	"Merging…",
	"Unwrapping…",
	"Dereferencing…",
	"Allocating…",
	"Defragmenting…",
	"Reticulating…",
	"Untangling…",
	"Refactoring…",
	"Brewing…",
	"Distilling…",
	"Fermenting…",
	"Marinating…",
	"Simmering…",
	"Whisking…",
	"Purring…",
	"Napping…",
	"Judging…",
	"Calibrating…",
	"Overclocking…",
	"Downloading…",
	"Consulting…",
	"Summoning…",
	"Manifesting…",
	"Speedrunning…",
	"Buffering…",
	"Hallucinating…",
	"Daydreaming…",
	"Scheming…",
	"Plotting…",
	"Conjuring…",
	"Synthesizing…",
	"Percolating…",
	"Meditating…",
	"Vibing…",
	"Yearning…",
	"Spiraling…",
}

// renderMarkdown renders markdown text for terminal output using glamour.
func renderMarkdown(w io.Writer, t *Theme, text string) {
	rendered, err := glamour.Render(text, t.GlamourStyle)
	if err != nil {
		fmt.Fprintln(w, text)
		return
	}
	fmt.Fprint(w, strings.TrimRight(rendered, "\n")+"\n")
}

// Spinner shows a binary digits animation while the LLM is thinking.
type Spinner struct {
	w    io.Writer
	stop chan struct{}
	done chan struct{}
}

// randomBinary returns a string of n random '0' and '1' characters.
func randomBinary(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = '0' + byte(rand.Intn(2))
	}
	return string(b)
}

func StartSpinner(w io.Writer, t *Theme, todos []tools.TodoItem) *Spinner {
	s := &Spinner{w: w, stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()

		_ = todos

		for {
			select {
			case <-s.stop:
				fmt.Fprintf(w, "\r\033[K")
				return
			case <-ticker.C:
				bits := randomBinary(6)
				fmt.Fprintf(w, "\r\033[K  %s%s%s %sWorking…%s", t.ANSI(t.Primary), bits, t.ANSIReset(), t.ANSIDim(), t.ANSIReset())
			}
		}
	}()
	return s
}

func (s *Spinner) Stop() {
	close(s.stop)
	<-s.done
}

// coloredBullet returns a bullet in green (success) or red (error).
func coloredBullet(t *Theme, isError bool) string {
	if isError {
		return t.ANSI(t.Error) + "⏺" + t.ANSIReset()
	}
	return t.ANSI(t.Success) + "⏺" + t.ANSIReset()
}

func renderEvent(w io.Writer, t *Theme, e internal.Event) {
	if e.PreviewType == internal.PreviewGuard {
		renderGuardEvent(w, t, e)
		return
	}
	if e.PreviewType == internal.PreviewBash {
		renderBashEvent(w, t, e)
		return
	}

	args := strings.Join(e.Args, ", ")
	if len(args) > 0 {
		args = fmt.Sprintf("(%s)", args)
	}
	fmt.Fprintf(w, "\n%s %s%s\n", coloredBullet(t, e.IsError), e.Name, args)
	fmt.Fprintf(w, "⎿  %s\n", e.Message)

	for _, line := range e.Preview {
		fmt.Fprintf(w, "   %s\n", renderPreviewLine(t, e.PreviewType, line))
	}
}

func renderGuardEvent(w io.Writer, t *Theme, e internal.Event) {
	tool := ""
	if len(e.Args) > 0 {
		tool = e.Args[0]
	}
	fmt.Fprintf(w, "\n%s⏺ Guard(%s)%s\n", t.ANSI(t.Warning), tool, t.ANSIReset())
	fmt.Fprintf(w, "⎿  %s%s%s\n", t.ANSI(t.Warning), e.Message, t.ANSIReset())
}

func renderBashEvent(w io.Writer, t *Theme, e internal.Event) {
	description := ""
	command := ""
	if len(e.Args) > 0 {
		description = e.Args[0]
	}
	if len(e.Args) > 1 {
		command = e.Args[1]
	}

	if description != "" {
		fmt.Fprintf(w, "\n%s %s(%s)\n", coloredBullet(t, e.IsError), e.Name, description)
	} else {
		fmt.Fprintf(w, "\n%s %s\n", coloredBullet(t, e.IsError), e.Name)
	}
	fmt.Fprintf(w, "  %s$ %s%s\n", t.ANSIDim(), command, t.ANSIReset())
	fmt.Fprintf(w, "⎿  %s\n", e.Message)

	for _, line := range e.Preview {
		fmt.Fprintf(w, "   %s\n", renderPreviewLine(t, e.PreviewType, line))
	}
}

func renderPreviewLine(t *Theme, pt internal.PreviewType, line string) string {
	switch pt {
	case internal.PreviewDiff:
		if strings.HasPrefix(line, "+") {
			return t.ANSI(t.Success) + line + t.ANSIReset()
		} else if strings.HasPrefix(line, "-") {
			return t.ANSI(t.Error) + line + t.ANSIReset()
		}
		return line
	case internal.PreviewBash:
		if strings.HasPrefix(line, "stderr:") {
			return t.ANSI(t.Error) + strings.TrimPrefix(line, "stderr:") + t.ANSIReset()
		}
		return t.ANSIDim() + line + t.ANSIReset()
	case internal.PreviewCode:
		return t.ANSIDim() + line + t.ANSIReset()
	case internal.PreviewFileList:
		return t.ANSI(t.Primary) + line + t.ANSIReset()
	default:
		return line
	}
}
