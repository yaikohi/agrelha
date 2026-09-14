// Package build carries information stamped in at compile time and at startup,
// so any layer may read it without depending on config.
package build

var Version = "dev"

// Commit and Date identify which source revision a running binary came from.
// A version alone cannot: "dev" is every local build, and even a released
// version can be rebuilt from a tree that has moved on.
var (
	Commit = "unknown"
	Date   = "unknown"
)

var SourceURL = "https://codeberg.org/ykhi/agrelha"
