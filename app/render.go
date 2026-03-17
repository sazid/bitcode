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
	"Thinking...",
	"Hmm, let me think...",
	"Crunching tokens...",
	"Consulting the matrix...",
	"Brewing code...",
	"Summoning the LLM birds...",
	"Cat spilled coffee over my keyboard!",
	"Hold my semicolons...",
	"Reticulating splines...",
	"Compiling thoughts...",
	"Asking the rubber duck...",
	"Untangling spaghetti...",
	"Overthinking this...",
	"Reading the docs (jk)...",
	"sudo think harder...",
	"It's not a bug, it's a feature...",
	"Have you tried turning it off and on?...",
}

// renderMarkdown renders markdown text for terminal output using glamour.
func renderMarkdown(w io.Writer, text string) {
	rendered, err := glamour.Render(text, ActiveTheme().GlamourStyle)
	if err != nil {
		fmt.Fprintln(w, text)
		return
	}
	fmt.Fprint(w, strings.TrimRight(rendered, "\n")+"\n")
}

// Spinner shows a braille animation while the LLM is thinking.
type Spinner struct {
	w    io.Writer
	stop chan struct{}
	done chan struct{}
}

func StartSpinner(w io.Writer, todos []tools.TodoItem) *Spinner {
	s := &Spinner{w: w, stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(s.done)
		frames := [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		msg := spinnerMessages[rand.Intn(len(spinnerMessages))]
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()
		i := 0
		nextSwap := 40 + rand.Intn(30) // swap message every ~3-5s

		// Print todo status once at the start
		if ts := RenderTodoStatus(todos); ts != "" {
			fmt.Fprintf(w, "%s", ts)
		}

		for {
			select {
			case <-s.stop:
				fmt.Fprintf(w, "\r\033[K")
				return
			case <-ticker.C:
				if i == nextSwap {
					msg = spinnerMessages[rand.Intn(len(spinnerMessages))]
					nextSwap = i + 40 + rand.Intn(30)
				}
				t := ActiveTheme()
				fmt.Fprintf(w, "\r\033[K%s  %s %s%s", t.ANSIDim(), frames[i%len(frames)], msg, t.ANSIReset())
				i++
			}
		}
	}()
	return s
}

func (s *Spinner) Stop() {
	close(s.stop)
	<-s.done
}

// coloredBullet returns ⏺ in green (success) or red (error).
func coloredBullet(isError bool) string {
	t := ActiveTheme()
	if isError {
		return t.ANSI(t.Error) + "⏺" + t.ANSIReset()
	}
	return t.ANSI(t.Success) + "⏺" + t.ANSIReset()
}

func renderEvent(w io.Writer, e internal.Event) {
	if e.PreviewType == internal.PreviewGuard {
		renderGuardEvent(w, e)
		return
	}
	if e.PreviewType == internal.PreviewBash {
		renderBashEvent(w, e)
		return
	}

	args := strings.Join(e.Args, ", ")
	if len(args) > 0 {
		args = fmt.Sprintf("(%s)", args)
	}
	fmt.Fprintf(w, "\n%s %s%s\n", coloredBullet(e.IsError), e.Name, args)
	fmt.Fprintf(w, "⎿  %s\n", e.Message)

	for _, line := range e.Preview {
		fmt.Fprintf(w, "   %s\n", renderPreviewLine(e.PreviewType, line))
	}
}

func renderGuardEvent(w io.Writer, e internal.Event) {
	t := ActiveTheme()
	tool := ""
	if len(e.Args) > 0 {
		tool = e.Args[0]
	}
	fmt.Fprintf(w, "\n%s⏺ Guard(%s)%s\n", t.ANSI(t.Warning), tool, t.ANSIReset())
	fmt.Fprintf(w, "⎿  %s%s%s\n", t.ANSI(t.Warning), e.Message, t.ANSIReset())
}

func renderBashEvent(w io.Writer, e internal.Event) {
	t := ActiveTheme()
	description := ""
	command := ""
	if len(e.Args) > 0 {
		description = e.Args[0]
	}
	if len(e.Args) > 1 {
		command = e.Args[1]
	}

	if description != "" {
		fmt.Fprintf(w, "\n%s %s(%s)\n", coloredBullet(e.IsError), e.Name, description)
	} else {
		fmt.Fprintf(w, "\n%s %s\n", coloredBullet(e.IsError), e.Name)
	}
	fmt.Fprintf(w, "  %s$ %s%s\n", t.ANSIDim(), command, t.ANSIReset())
	fmt.Fprintf(w, "⎿  %s\n", e.Message)

	for _, line := range e.Preview {
		fmt.Fprintf(w, "   %s\n", renderPreviewLine(e.PreviewType, line))
	}
}

func renderPreviewLine(pt internal.PreviewType, line string) string {
	t := ActiveTheme()
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
