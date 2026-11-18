// Package whip publishes a capture session live over WebRTC to a WHIP
// endpoint (RFC 9725), such as a media server or a streaming service's
// ingest. Video is H.264 constrained baseline, audio Opus. Telemetry
// (location, camera direction) can travel on a data channel.
//
//	session, _ := capture.Start(capture.Config{Stream: true, Record: true, Location: true, Motion: true})
//	publisher, err := whip.Publish(ctx, session, whip.Options{Endpoint: url, Token: token})
//
// The publisher is a capture.Sink: it ends with the session, and the
// session stops sending to it if the connection fails.
package whip

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"

	"github.com/DonaldMurillo/gofastr-native/extension/capture"
)

// Options configures one WHIP publication.
type Options struct {
	// Endpoint is the WHIP endpoint URL.
	Endpoint string
	// Token, when set, is sent as a bearer token on every request.
	Token string
	// ICEServers are STUN and TURN servers. Without them only host
	// candidates are offered, which reaches servers on the same network or
	// with a public address.
	ICEServers []webrtc.ICEServer
	// Telemetry opens an unordered, unreliable data channel named
	// "telemetry" that carries one capture.Telemetry JSON object per message.
	Telemetry bool
	// Client sends the WHIP requests. Defaults to a client with a 15 s timeout.
	Client *http.Client
	// SettingEngine tunes the WebRTC stack, for example network types.
	SettingEngine *webrtc.SettingEngine
}

// Publisher streams one session. It implements capture.Sink.
type Publisher struct {
	session   *capture.Session
	options   Options
	peer      *webrtc.PeerConnection
	video     *webrtc.TrackLocalStaticSample
	audio     *webrtc.TrackLocalStaticSample
	telemetry *webrtc.DataChannel
	resource  string

	mu       sync.Mutex
	pending  [2]*capture.Media
	failure  error
	closed   bool
	once     sync.Once
	closeErr error
}

// H.264 constrained baseline level 3.1, packetization mode 1: what the
// Android encoder produces and what browsers and media servers decode.
var videoCodec = webrtc.RTPCodecCapability{
	MimeType:    webrtc.MimeTypeH264,
	ClockRate:   90000,
	SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f",
	RTCPFeedback: []webrtc.RTCPFeedback{
		{Type: "goog-remb"}, {Type: "ccm", Parameter: "fir"}, {Type: "nack"}, {Type: "nack", Parameter: "pli"},
	},
}

var audioCodec = webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2, SDPFmtpLine: "minptime=10;useinbandfec=1"}

// Publish negotiates with the endpoint and attaches the publisher to the
// session. The session must have Config.Stream set. Media flows once the
// device starts capture and the connection is up; a keyframe is requested
// then, so viewers start with a full picture.
func Publish(ctx context.Context, session *capture.Session, options Options) (*Publisher, error) {
	if session == nil || !session.Config().Stream {
		return nil, errors.New("whip: the session must be started with Config.Stream")
	}
	endpoint, err := url.Parse(options.Endpoint)
	if err != nil || (endpoint.Scheme != "https" && endpoint.Scheme != "http") || endpoint.Host == "" {
		return nil, errors.New("whip: endpoint must be an http or https URL")
	}
	if options.Client == nil {
		options.Client = &http.Client{Timeout: 15 * time.Second}
	}

	engine := &webrtc.MediaEngine{}
	if err := engine.RegisterCodec(webrtc.RTPCodecParameters{RTPCodecCapability: videoCodec, PayloadType: 102}, webrtc.RTPCodecTypeVideo); err != nil {
		return nil, err
	}
	if err := engine.RegisterCodec(webrtc.RTPCodecParameters{RTPCodecCapability: audioCodec, PayloadType: 111}, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, err
	}
	registry := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(engine, registry); err != nil {
		return nil, err
	}
	apiOptions := []func(*webrtc.API){webrtc.WithMediaEngine(engine), webrtc.WithInterceptorRegistry(registry)}
	if options.SettingEngine != nil {
		apiOptions = append(apiOptions, webrtc.WithSettingEngine(*options.SettingEngine))
	}
	peer, err := webrtc.NewAPI(apiOptions...).NewPeerConnection(webrtc.Configuration{ICEServers: options.ICEServers})
	if err != nil {
		return nil, err
	}
	p := &Publisher{session: session, options: options, peer: peer}
	fail := func(err error) (*Publisher, error) {
		peer.Close()
		return nil, err
	}

	p.video, err = webrtc.NewTrackLocalStaticSample(videoCodec, "video", "gofastr-capture")
	if err != nil {
		return fail(err)
	}
	sender, err := peer.AddTransceiverFromTrack(p.video, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly})
	if err != nil {
		return fail(err)
	}
	go p.readFeedback(sender.Sender())
	if session.Config().Audio {
		p.audio, err = webrtc.NewTrackLocalStaticSample(audioCodec, "audio", "gofastr-capture")
		if err != nil {
			return fail(err)
		}
		audio, err := peer.AddTransceiverFromTrack(p.audio, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly})
		if err != nil {
			return fail(err)
		}
		go p.drain(audio.Sender())
	}
	if options.Telemetry {
		ordered, retransmits := false, uint16(0)
		p.telemetry, err = peer.CreateDataChannel("telemetry", &webrtc.DataChannelInit{Ordered: &ordered, MaxRetransmits: &retransmits})
		if err != nil {
			return fail(err)
		}
	}
	peer.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		switch state {
		case webrtc.PeerConnectionStateConnected:
			session.RequestKeyframe()
		case webrtc.PeerConnectionStateFailed:
			p.setFailure(errors.New("whip: connection failed"))
		case webrtc.PeerConnectionStateClosed:
			p.setFailure(errors.New("whip: connection closed"))
		}
	})

	offer, err := peer.CreateOffer(nil)
	if err != nil {
		return fail(err)
	}
	gathered := webrtc.GatheringCompletePromise(peer)
	if err := peer.SetLocalDescription(offer); err != nil {
		return fail(err)
	}
	select {
	case <-gathered:
	case <-ctx.Done():
		return fail(ctx.Err())
	}
	answer, resource, err := p.post(ctx, endpoint, peer.LocalDescription().SDP)
	if err != nil {
		return fail(err)
	}
	p.resource = resource
	if err := peer.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: answer}); err != nil {
		p.hangUp()
		return fail(fmt.Errorf("whip: endpoint answer: %w", err))
	}
	if err := session.AddSink(p); err != nil {
		p.hangUp()
		return fail(err)
	}
	return p, nil
}

func (p *Publisher) post(ctx context.Context, endpoint *url.URL, offer string) (answer, resource string, err error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewBufferString(offer))
	if err != nil {
		return "", "", err
	}
	request.Header.Set("Content-Type", "application/sdp")
	p.authorize(request)
	response, err := p.options.Client.Do(request)
	if err != nil {
		return "", "", fmt.Errorf("whip: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return "", "", fmt.Errorf("whip: %w", err)
	}
	if response.StatusCode != http.StatusCreated {
		return "", "", fmt.Errorf("whip: endpoint answered %s", response.Status)
	}
	if location := response.Header.Get("Location"); location != "" {
		// Resolve against the URL that answered: a redirected POST's session
		// lives on the server it was redirected to.
		base := endpoint
		if response.Request != nil && response.Request.URL != nil {
			base = response.Request.URL
		}
		reference, err := url.Parse(location)
		if err != nil {
			return "", "", fmt.Errorf("whip: unreadable session URL: %w", err)
		}
		resolved := base.ResolveReference(reference)
		// The DELETE carries the token, so it never goes out in clear text
		// when the endpoint was HTTPS.
		if endpoint.Scheme == "https" && resolved.Scheme != "https" {
			return "", "", errors.New("whip: the endpoint gave a session URL that is not HTTPS")
		}
		resource = resolved.String()
	}
	return string(body), resource, nil
}

func (p *Publisher) authorize(request *http.Request) {
	if p.options.Token != "" {
		request.Header.Set("Authorization", "Bearer "+p.options.Token)
	}
}

// hangUp deletes the WHIP resource, ending the publication on the server.
func (p *Publisher) hangUp() error {
	if p.resource == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, p.resource, nil)
	if err != nil {
		return err
	}
	p.authorize(request)
	response, err := p.options.Client.Do(request)
	if err != nil {
		return fmt.Errorf("whip: %w", err)
	}
	response.Body.Close()
	return nil
}

// readFeedback turns viewers' picture-loss and bandwidth reports into
// encoder requests.
func (p *Publisher) readFeedback(sender *webrtc.RTPSender) {
	target := p.session.Config().Bitrate
	applied := target
	var last time.Time
	for {
		packets, _, err := sender.ReadRTCP()
		if err != nil {
			return
		}
		for _, packet := range packets {
			switch packet := packet.(type) {
			case *rtcp.PictureLossIndication, *rtcp.FullIntraRequest:
				p.session.RequestKeyframe()
			case *rtcp.ReceiverEstimatedMaximumBitrate:
				// Follow the estimate down promptly and back up slowly,
				// never above the configured bitrate.
				estimate := min(int(packet.Bitrate*0.9), target)
				if estimate != applied && (estimate < applied || time.Since(last) > 2*time.Second) {
					p.session.SetBitrate(estimate)
					applied, last = estimate, time.Now()
				}
			}
		}
	}
}

func (p *Publisher) drain(sender *webrtc.RTPSender) {
	for {
		if _, _, err := sender.ReadRTCP(); err != nil {
			return
		}
	}
}

func (p *Publisher) setFailure(err error) {
	p.mu.Lock()
	if p.failure == nil && !p.closed {
		p.failure = err
	}
	p.mu.Unlock()
}

// WriteMedia sends one access unit. Each is held until the next on its
// track arrives, because RTP needs a sample's duration: one frame of delay.
func (p *Publisher) WriteMedia(sample capture.Media) error {
	p.mu.Lock()
	if p.failure != nil {
		err := p.failure
		p.mu.Unlock()
		return err
	}
	if sample.Track != capture.Video && sample.Track != capture.Audio {
		p.mu.Unlock()
		return nil
	}
	previous := p.pending[sample.Track]
	p.pending[sample.Track] = &sample
	p.mu.Unlock()
	if previous == nil {
		return nil
	}
	duration := time.Duration(sample.Clock - previous.Clock)
	if duration <= 0 || duration > time.Second {
		duration = 33 * time.Millisecond
		if sample.Track == capture.Audio {
			duration = 20 * time.Millisecond
		}
	}
	track := p.video
	if sample.Track == capture.Audio {
		track = p.audio
	}
	if track == nil {
		return nil
	}
	return track.WriteSample(media.Sample{Data: previous.Data, Duration: duration})
}

// WriteTelemetry sends a sample on the telemetry channel once it is open.
func (p *Publisher) WriteTelemetry(sample capture.Telemetry) error {
	if p.telemetry == nil || p.telemetry.ReadyState() != webrtc.DataChannelStateOpen {
		return nil
	}
	message, err := json.Marshal(sample)
	if err != nil {
		return nil
	}
	// A full channel buffer drops telemetry rather than stalling media.
	if p.telemetry.BufferedAmount() > 1<<20 {
		return nil
	}
	if err := p.telemetry.SendText(string(message)); err != nil && p.telemetry.ReadyState() == webrtc.DataChannelStateOpen {
		return fmt.Errorf("whip: telemetry: %w", err)
	}
	return nil
}

// Close ends the publication: it deletes the WHIP resource and closes the
// connection. The session calls it when capture ends; calling it earlier
// stops streaming while capture continues.
func (p *Publisher) Close() error {
	p.once.Do(func() {
		p.mu.Lock()
		p.closed = true
		if p.failure == nil {
			p.failure = errors.New("whip: publisher closed")
		}
		p.mu.Unlock()
		hangUp := p.hangUp()
		p.closeErr = errors.Join(hangUp, p.peer.Close())
	})
	return p.closeErr
}

// ConnectionState reports the WebRTC connection state.
func (p *Publisher) ConnectionState() webrtc.PeerConnectionState {
	return p.peer.ConnectionState()
}
