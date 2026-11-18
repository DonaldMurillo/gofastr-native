// Package capturebind is bound by gomobile beside the app package so the
// capture library's Java can call its Go side. Apps never import it.
package capturebind

import "github.com/DonaldMurillo/gofastr-native/extension/capture"

// Attach sets the private directory recordings are written under.
func Attach(root string) error { return capture.Attach(root) }

// Event delivers a lifecycle event or telemetry sample and returns directives.
func Event(session, kind, payload string) string { return capture.Event(session, kind, payload) }

// Sample delivers one encoded access unit and returns directives.
func Sample(session string, track, flags int, clock int64, data []byte) string {
	return capture.Sample(session, track, flags, clock, data)
}
