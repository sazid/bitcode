package main

import (
	"fmt"
	"io"
	"math/rand"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/sazid/bitcode/internal"
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
	rendered, err := glamour.Render(text, "dark")
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

func StartSpinner(w io.Writer) *Spinner {
	s := &Spinner{w: w, stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(s.done)
		frames := [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		msg := spinnerMessages[rand.Intn(len(spinnerMessages))]
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()
		i := 0
		nextSwap := 40 + rand.Intn(30) // swap message every ~3-5s
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
				fmt.Fprintf(w, "\r\033[K\033[2m  %s %s\033[0m", frames[i%len(frames)], msg)
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
	if isError {
		return "\033[31m⏺\033[0m" // red
	}
	return "\033[32m⏺\033[0m" // green
}

func renderEvent(w io.Writer, e internal.Event) {
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

func renderBashEvent(w io.Writer, e internal.Event) {
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
	fmt.Fprintf(w, "  \033[2m$ %s\033[0m\n", command)
	fmt.Fprintf(w, "⎿  %s\n", e.Message)

	for _, line := range e.Preview {
		fmt.Fprintf(w, "   %s\n", renderPreviewLine(e.PreviewType, line))
	}
}

func renderPreviewLine(pt internal.PreviewType, line string) string {
	switch pt {
	case internal.PreviewDiff:
		if strings.HasPrefix(line, "+") {
			return fmt.Sprintf("\033[32m%s\033[0m", line)
		} else if strings.HasPrefix(line, "-") {
			return fmt.Sprintf("\033[31m%s\033[0m", line)
		}
		return line
	case internal.PreviewBash:
		if strings.HasPrefix(line, "stderr:") {
			return fmt.Sprintf("\033[31m%s\033[0m", strings.TrimPrefix(line, "stderr:"))
		}
		return fmt.Sprintf("\033[2m%s\033[0m", line)
	case internal.PreviewCode:
		return fmt.Sprintf("\033[2m%s\033[0m", line)
	case internal.PreviewFileList:
		return fmt.Sprintf("\033[36m%s\033[0m", line)
	default:
		return line
	}
}
