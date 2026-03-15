package version

import "fmt"

// These variables are injected at build time via ldflags
var (
	Version   = "dev"
	Commit    = "unknown"
	Date      = "unknown"
	BuildType = "development"
)

// String returns a formatted version string.
func String() string {
	if BuildType == "release" {
		return fmt.Sprintf("%s (%s, built %s)", Version, Commit[:min(7, len(Commit))], Date)
	}
	return fmt.Sprintf("%s (dev, %s)", Version, Commit[:min(7, len(Commit))])
}
