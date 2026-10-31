// Package saturnv holds the files a saturn job runs with: the launchers and the
// saturn_db() helpers in lib/, built into the saturn binary.
package saturnv

import "embed"

//go:embed lib
var Lib embed.FS
