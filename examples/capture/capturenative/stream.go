package capturenative

import (
	"context"
	"time"

	"github.com/DonaldMurillo/gofastr-native/extension/capture"
	"github.com/DonaldMurillo/gofastr-native/extension/capture/whip"
)

// publish negotiates WHIP before the camera starts, so a bad endpoint or
// token fails the Start action instead of a running session. Add STUN or
// TURN servers to Options.ICEServers for endpoints behind NAT.
func publish(session *capture.Session, endpoint, token string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, err := whip.Publish(ctx, session, whip.Options{
		Endpoint:  endpoint,
		Token:     token,
		Telemetry: true,
	})
	return err
}
