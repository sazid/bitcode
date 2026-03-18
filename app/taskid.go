package main

import (
	"fmt"
	"math/rand"
	"time"
)

var taskAdjectives = []string{
	"swift", "bold", "calm", "dark", "keen",
	"warm", "cool", "wild", "soft", "sharp",
	"bright", "quiet", "rapid", "steady", "vivid",
	"amber", "coral", "ivory", "azure", "rustic",
	"agile", "brave", "crisp", "deft", "eager",
	"fleet", "grand", "hazy", "jade", "lucid",
	"noble", "prime", "royal", "solar", "tidal",
	"ultra", "vital", "witty", "zesty", "misty",
	"dusty", "frosty", "golden", "hidden", "inner",
	"lunar", "mossy", "neon", "opal", "polar",
}

var taskNouns = []string{
	"falcon", "ember", "river", "storm", "nexus",
	"pulse", "forge", "atlas", "prism", "lotus",
	"cedar", "flint", "quartz", "raven", "surge",
	"drift", "glyph", "haven", "knoll", "marsh",
	"orbit", "ridge", "spark", "trail", "vault",
	"bloom", "crest", "delta", "frost", "grove",
	"helix", "inlet", "jewel", "ledge", "maple",
	"north", "omega", "pixel", "quest", "relay",
	"sigma", "torch", "unity", "viper", "wraith",
	"zenith", "anchor", "beacon", "cipher", "dune",
}

const alphanumChars = "abcdefghijklmnopqrstuvwxyz0123456789"

// GenerateTaskID returns a human-readable task ID like "swift-falcon-a7".
func GenerateTaskID() string {
	adj := taskAdjectives[rand.Intn(len(taskAdjectives))]
	noun := taskNouns[rand.Intn(len(taskNouns))]
	suffix := make([]byte, 2)
	for i := range suffix {
		suffix[i] = alphanumChars[rand.Intn(len(alphanumChars))]
	}
	return fmt.Sprintf("%s-%s-%s", adj, noun, string(suffix))
}

// formatDuration formats a duration as "5s", "1m 23s", "2m", etc.
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	if s == 0 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%dm %ds", m, s)
}
