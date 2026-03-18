package main

import (
	"math/rand"
	"strings"
)

type spinnerAnimKind int

const (
	animPlain spinnerAnimKind = iota
	animGlimmer
	animWave
	animScramble
	animGlitch
	animTypewriter
	animFadeIn
	animCount
)

var glitchChars = []rune{'░', '▒', '▓', '█', '▄', '▀', '▌', '▐'}

func randomSpinnerAnim() spinnerAnimKind {
	// Skip animPlain — always use a real animation
	return spinnerAnimKind(1 + rand.Intn(int(animCount)-1))
}

func renderAnimatedMsg(t *Theme, msg string, anim spinnerAnimKind, frame int) string {
	runes := []rune(msg)
	if len(runes) == 0 {
		return ""
	}
	switch anim {
	case animGlimmer:
		return renderGlimmer(t, runes, frame)
	case animWave:
		return renderWave(t, runes, frame)
	case animScramble:
		return renderScramble(t, runes, frame)
	case animGlitch:
		return renderGlitch(t, runes, frame)
	case animTypewriter:
		return renderTypewriter(t, runes, frame)
	case animFadeIn:
		return renderFadeIn(t, runes, frame)
	default:
		return t.ANSIDim() + msg + t.ANSIReset()
	}
}

// renderGlimmer highlights 1-2 random characters with the primary color each frame.
func renderGlimmer(t *Theme, runes []rune, _ int) string {
	pos1 := rand.Intn(len(runes))
	pos2 := -1
	if len(runes) > 3 && rand.Float64() < 0.4 {
		pos2 = rand.Intn(len(runes))
	}
	var sb strings.Builder
	sb.WriteString(t.ANSIDim())
	for i, r := range runes {
		if i == pos1 || i == pos2 {
			sb.WriteString(t.ANSIReset())
			sb.WriteString(t.ANSI(t.Primary))
			sb.WriteRune(r)
			sb.WriteString(t.ANSIReset())
			sb.WriteString(t.ANSIDim())
		} else {
			sb.WriteRune(r)
		}
	}
	sb.WriteString(t.ANSIReset())
	return sb.String()
}

// renderWave sweeps a bright spot across the text, looping.
func renderWave(t *Theme, runes []rune, frame int) string {
	wavePos := (frame/2)%(len(runes)+6) - 3
	var sb strings.Builder
	sb.WriteString(t.ANSIDim())
	for i, r := range runes {
		dist := i - wavePos
		if dist < 0 {
			dist = -dist
		}
		switch {
		case dist == 0:
			sb.WriteString(t.ANSIReset())
			sb.WriteString(t.ANSI(t.Primary))
			sb.WriteRune(r)
			sb.WriteString(t.ANSIReset())
			sb.WriteString(t.ANSIDim())
		case dist <= 2:
			sb.WriteString(t.ANSIReset())
			sb.WriteRune(r)
			sb.WriteString(t.ANSIDim())
		default:
			sb.WriteRune(r)
		}
	}
	sb.WriteString(t.ANSIReset())
	return sb.String()
}

// renderScramble starts with random characters and gradually reveals the real text.
func renderScramble(t *Theme, runes []rune, frame int) string {
	const resolveOver = 40
	var sb strings.Builder
	sb.WriteString(t.ANSIDim())
	for i, r := range runes {
		resolveAt := ((i*7 + 3) % len(runes)) * resolveOver / len(runes)
		if frame >= resolveAt || r == ' ' {
			sb.WriteRune(r)
		} else {
			sb.WriteString(t.ANSIReset())
			sb.WriteString(t.ANSI(t.Primary))
			sb.WriteRune(rune('a' + rand.Intn(26)))
			sb.WriteString(t.ANSIReset())
			sb.WriteString(t.ANSIDim())
		}
	}
	sb.WriteString(t.ANSIReset())
	return sb.String()
}

// renderGlitch randomly replaces characters with block elements.
func renderGlitch(t *Theme, runes []rune, _ int) string {
	var sb strings.Builder
	sb.WriteString(t.ANSIDim())
	for _, r := range runes {
		if r != ' ' && rand.Float64() < 0.07 {
			sb.WriteString(t.ANSIReset())
			sb.WriteString(t.ANSI(t.Primary))
			sb.WriteRune(glitchChars[rand.Intn(len(glitchChars))])
			sb.WriteString(t.ANSIReset())
			sb.WriteString(t.ANSIDim())
		} else {
			sb.WriteRune(r)
		}
	}
	sb.WriteString(t.ANSIReset())
	return sb.String()
}

// renderTypewriter reveals characters left to right with a cursor.
func renderTypewriter(t *Theme, runes []rune, frame int) string {
	revealCount := frame * len(runes) / 30
	if revealCount > len(runes) {
		revealCount = len(runes)
	}
	var sb strings.Builder
	sb.WriteString(t.ANSIDim())
	for i, r := range runes {
		if i < revealCount {
			sb.WriteRune(r)
		} else if i == revealCount {
			sb.WriteString(t.ANSIReset())
			sb.WriteString(t.ANSI(t.Primary))
			sb.WriteRune('▌')
			sb.WriteString(t.ANSIReset())
			sb.WriteString(t.ANSIDim())
		} else {
			sb.WriteRune(' ')
		}
	}
	sb.WriteString(t.ANSIReset())
	return sb.String()
}

// renderFadeIn reveals characters in pseudo-random order from spaces.
func renderFadeIn(t *Theme, runes []rune, frame int) string {
	var sb strings.Builder
	sb.WriteString(t.ANSIDim())
	for i, r := range runes {
		threshold := (i*13 + 5) % len(runes)
		revealAt := threshold * 35 / len(runes)
		if frame >= revealAt || r == ' ' {
			sb.WriteRune(r)
		} else {
			sb.WriteRune(' ')
		}
	}
	sb.WriteString(t.ANSIReset())
	return sb.String()
}
