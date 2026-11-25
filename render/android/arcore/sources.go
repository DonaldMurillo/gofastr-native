// Package arcore supplies Java sources for the opt-in Android AR provider.
package arcore

import "embed"

//go:embed *.java
var Sources embed.FS
