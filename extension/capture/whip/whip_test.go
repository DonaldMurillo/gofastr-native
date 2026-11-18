package whip

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"

	"github.com/DonaldMurillo/gofastr-native/extension/capture"
)

func loopback() *webrtc.SettingEngine {
	engine := &webrtc.SettingEngine{}
	engine.SetIncludeLoopbackCandidate(true)
	engine.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	engine.SetICEMulticastDNSMode(0)
	return engine
}

// server is a WHIP endpoint backed by a receive-only pion peer.
type server struct {
	t        *testing.T
	mu       sync.Mutex
	peer     *webrtc.PeerConnection
	tracks   map[string]int // mime type -> RTP packets
	messages []string
	deleted  bool
	auth     string
	video    chan uint32 // SSRC of the first video packet
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/whip":
		s.mu.Lock()
		s.auth = r.Header.Get("Authorization")
		s.mu.Unlock()
		if r.Header.Get("Content-Type") != "application/sdp" {
			http.Error(w, "want application/sdp", http.StatusUnsupportedMediaType)
			return
		}
		offer, _ := io.ReadAll(r.Body)
		engine := &webrtc.MediaEngine{}
		if err := engine.RegisterDefaultCodecs(); err != nil {
			s.t.Error(err)
		}
		api := webrtc.NewAPI(webrtc.WithMediaEngine(engine), webrtc.WithSettingEngine(*loopback()))
		peer, err := api.NewPeerConnection(webrtc.Configuration{})
		if err != nil {
			s.t.Error(err)
			return
		}
		peer.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
			first := true
			for {
				packet, _, err := track.ReadRTP()
				if err != nil {
					return
				}
				s.mu.Lock()
				s.tracks[track.Codec().MimeType]++
				s.mu.Unlock()
				if first && track.Kind() == webrtc.RTPCodecTypeVideo {
					first = false
					s.video <- packet.SSRC
				}
			}
		})
		peer.OnDataChannel(func(channel *webrtc.DataChannel) {
			channel.OnMessage(func(message webrtc.DataChannelMessage) {
				s.mu.Lock()
				s.messages = append(s.messages, string(message.Data))
				s.mu.Unlock()
			})
		})
		if err := peer.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: string(offer)}); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		answer, err := peer.CreateAnswer(nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		gathered := webrtc.GatheringCompletePromise(peer)
		peer.SetLocalDescription(answer)
		<-gathered
		s.mu.Lock()
		s.peer = peer
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/sdp")
		w.Header().Set("Location", "/whip/resource/1")
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, peer.LocalDescription().SDP)
	case r.Method == http.MethodDelete && r.URL.Path == "/whip/resource/1":
		s.mu.Lock()
		s.deleted = true
		s.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	default:
		http.NotFound(w, r)
	}
}

func (s *server) count(mime string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tracks[mime]
}

func eventually(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestPublishLoopback(t *testing.T) {
	if err := capture.Attach(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	session, err := capture.Start(capture.Config{Stream: true, Audio: true, Motion: true})
	if err != nil {
		t.Fatal(err)
	}
	defer capture.Event(session.ID(), "stopped", `{}`)
	capture.Event(session.ID(), "started", `{"width":1280,"height":720,"audio":true,"motion":true,"clock":0,"wall":"2026-01-01T00:00:00Z"}`)

	receiver := &server{t: t, tracks: map[string]int{}, video: make(chan uint32, 1)}
	endpoint := httptest.NewServer(receiver)
	defer endpoint.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	publisher, err := Publish(ctx, session, Options{Endpoint: endpoint.URL + "/whip", Token: "secret", Telemetry: true, SettingEngine: loopback()})
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "connection", func() bool { return publisher.ConnectionState() == webrtc.PeerConnectionStateConnected })
	if receiver.auth != "Bearer secret" {
		t.Fatalf("Authorization = %q", receiver.auth)
	}
	keyframe := false
	for _, directive := range strings.Split(capture.Event(session.ID(), "heartbeat", ""), ",") {
		keyframe = keyframe || directive == "keyframe"
	}
	if !keyframe {
		t.Fatal("connecting must request a keyframe")
	}

	// Annex B access units: SPS, PPS and IDR, then non-IDR slices.
	idr := []byte{0, 0, 0, 1, 0x67, 0x42, 0xe0, 0x1f, 0xda, 0, 0, 0, 1, 0x68, 0xce, 0x3c, 0x80, 0, 0, 0, 1, 0x65, 0x88, 0x84, 0x00, 0x33}
	slice := []byte{0, 0, 0, 1, 0x41, 0x9a, 0x02, 0x03}
	opus := []byte{0xfc, 0xff, 0xfe}
	stop := make(chan struct{})
	done := make(chan struct{})
	// Directives go to whichever device call comes next, so the generator
	// reports keyframe requests it receives.
	var requested atomic.Bool
	note := func(directives string) {
		if strings.Contains(directives, "keyframe") {
			requested.Store(true)
		}
	}
	go func() {
		defer close(done)
		clock := int64(0)
		for frame := 0; ; frame++ {
			select {
			case <-stop:
				return
			case <-time.After(20 * time.Millisecond):
			}
			clock += int64(20 * time.Millisecond)
			data, flags := slice, 0
			if frame%30 == 0 {
				data, flags = idr, 1
			}
			note(capture.Sample(session.ID(), int(capture.Video), flags, clock, data))
			note(capture.Sample(session.ID(), int(capture.Audio), 0, clock, opus))
			if frame%5 == 0 {
				note(capture.Event(session.ID(), "orientation", `{"clock":1,"azimuth":90,"pitch":0,"roll":0,"quaternion":[0,0,0,1]}`))
			}
		}
	}()
	halt := sync.OnceFunc(func() {
		close(stop)
		<-done
	})
	defer halt()

	eventually(t, "video packets", func() bool { return receiver.count(webrtc.MimeTypeH264) > 5 })
	eventually(t, "audio packets", func() bool { return receiver.count(webrtc.MimeTypeOpus) > 5 })
	eventually(t, "telemetry", func() bool {
		receiver.mu.Lock()
		defer receiver.mu.Unlock()
		if len(receiver.messages) == 0 {
			return false
		}
		var sample capture.Telemetry
		return json.Unmarshal([]byte(receiver.messages[0]), &sample) == nil && sample.Orientation != nil && sample.Orientation.Azimuth == 90
	})

	// A viewer's picture-loss report becomes a keyframe request.
	ssrc := <-receiver.video
	capture.Event(session.ID(), "heartbeat", "")
	requested.Store(false)
	receiver.mu.Lock()
	peer := receiver.peer
	receiver.mu.Unlock()
	if err := peer.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: ssrc}}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "keyframe request after PLI", func() bool {
		note(capture.Event(session.ID(), "heartbeat", ""))
		return requested.Load()
	})

	halt()
	capture.Event(session.ID(), "stopped", `{}`)
	eventually(t, "DELETE of the WHIP resource", func() bool {
		receiver.mu.Lock()
		defer receiver.mu.Unlock()
		return receiver.deleted
	})
	if state := publisher.ConnectionState(); state != webrtc.PeerConnectionStateClosed {
		t.Fatalf("connection state after the session ended = %s", state)
	}
	receiver.mu.Lock()
	receiver.peer.Close()
	receiver.mu.Unlock()
}

func TestPublishRejections(t *testing.T) {
	if err := capture.Attach(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	recordOnly, err := capture.Start(capture.Config{Record: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Publish(context.Background(), recordOnly, Options{Endpoint: "https://example.com/whip"}); err == nil {
		t.Fatal("published a session without Stream")
	}
	recordOnly.Stop()

	session, err := capture.Start(capture.Config{Stream: true})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Stop()
	if _, err := Publish(context.Background(), session, Options{Endpoint: "ftp://example.com"}); err == nil {
		t.Fatal("accepted a non-HTTP endpoint")
	}
	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer refusing.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := Publish(ctx, session, Options{Endpoint: refusing.URL, SettingEngine: loopback()}); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v", err)
	}
}

// The session URL resolves against the server that answered, and an HTTPS
// endpoint may not hand out a plain HTTP one: its DELETE carries the token.
func TestSessionURL(t *testing.T) {
	answer := func(location string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", location)
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, "v=0")
		}
	}
	regional := httptest.NewServer(answer("/whip/session/7"))
	defer regional.Close()
	balancer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, regional.URL+"/whip", http.StatusTemporaryRedirect)
	}))
	defer balancer.Close()
	endpoint, _ := url.Parse(balancer.URL + "/whip")
	p := &Publisher{options: Options{Client: http.DefaultClient}}
	if _, resource, err := p.post(context.Background(), endpoint, "offer"); err != nil || resource != regional.URL+"/whip/session/7" {
		t.Fatalf("after a redirect, resource = %q, %v", resource, err)
	}

	secure := httptest.NewTLSServer(answer("http://example.com/whip/session/7"))
	defer secure.Close()
	endpoint, _ = url.Parse(secure.URL + "/whip")
	p = &Publisher{options: Options{Client: secure.Client(), Token: "secret"}}
	if _, resource, err := p.post(context.Background(), endpoint, "offer"); err == nil {
		t.Fatalf("an HTTPS endpoint handed out %q", resource)
	}
}
