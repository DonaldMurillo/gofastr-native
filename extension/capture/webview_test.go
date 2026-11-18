package capture

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A 64x48 constrained-baseline clip from x264: two GOPs of an IDR and one
// P frame, as Annex B access units with parameter sets before each IDR.
func tinyH264(t *testing.T) [][]byte {
	t.Helper()
	unit := func(text string) []byte {
		data, err := hex.DecodeString(text)
		if err != nil {
			t.Fatal(err)
		}
		return append([]byte{0, 0, 0, 1}, data...)
	}
	sps, pps := unit("6742c01fd9047b011000000300100000030140f1832480"), unit("68cb83cb20")
	join := func(parts ...[]byte) []byte { return bytes.Join(parts, nil) }
	return [][]byte{
		join(sps, pps, unit("6588840ff2628000803c9c9c9d75d75d75d75e")),
		unit("419a381de360"),
		join(sps, pps, unit("658882010f2628000806c9c9c9d75d75d75d75e0")),
		unit("419a381be360"),
	}
}

type reportItem struct {
	Event   string `json:"event,omitempty"`
	Payload string `json:"payload,omitempty"`
	Track   int    `json:"track"`
	Flags   int    `json:"flags"`
	Clock   int64  `json:"clock"`
	Size    int    `json:"size"`
	data    []byte
}

// report encodes a batch the way capture.js does.
func report(t *testing.T, session string, items ...reportItem) []byte {
	t.Helper()
	header, err := json.Marshal(map[string]any{"session": session, "items": items})
	if err != nil {
		t.Fatal(err)
	}
	body := binary.BigEndian.AppendUint32(nil, uint32(len(header)))
	body = append(body, header...)
	for _, item := range items {
		body = append(body, item.data...)
	}
	return body
}

func event(kind string, payload any) reportItem {
	content, _ := json.Marshal(payload)
	return reportItem{Event: kind, Payload: string(content)}
}

func sample(track Track, key bool, clock int64, data []byte) reportItem {
	flags := 0
	if key {
		flags = 1
	}
	return reportItem{Track: int(track), Flags: flags, Clock: clock, Size: len(data), data: data}
}

func post(t *testing.T, body []byte) (int, string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	WebViewHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, WebViewPath, bytes.NewReader(body)))
	return recorder.Code, recorder.Body.String()
}

func TestWebViewReportsRecordAnMP4(t *testing.T) {
	reset(t)
	session, err := Start(Config{Record: true, Audio: true, Location: true})
	if err != nil {
		t.Fatal(err)
	}
	const second = int64(1e9)
	start := 10 * second
	started := map[string]any{"width": 64, "height": 48, "rotation": 0, "audio": true, "location": true, "motion": false,
		"clock": start, "wall": "2026-01-02T03:04:05Z", "recorder": "go"}
	items := []reportItem{event("started", started)}
	// Audio begins 100 ms after video; a P frame before the first IDR is
	// dropped.
	items = append(items, sample(Video, false, start-second/10, tinyH264(t)[1]))
	for i, frame := range tinyH264(t) {
		items = append(items, sample(Video, i%2 == 0, start+int64(i)*second/10, frame))
	}
	opus := []byte{0xfc, 0xff, 0xfe}
	for i := range 10 {
		items = append(items, sample(Audio, true, start+second/10+int64(i)*second/50, opus))
	}
	items = append(items, event("location", map[string]any{"clock": start, "latitude": 1.5, "longitude": 2.5, "accuracy": 3}))
	code, directives := post(t, report(t, session.ID(), items...))
	if code != http.StatusOK || directives != "" {
		t.Fatalf("report = %d %q", code, directives)
	}
	status := session.Status()
	if status.State != Running || status.Frames != 5 || status.Location == nil || status.Location.Latitude != 1.5 {
		t.Fatalf("status = %+v", status)
	}

	session.RequestKeyframe()
	if code, directives := post(t, report(t, session.ID())); code != http.StatusOK || directives != "keyframe" {
		t.Fatalf("heartbeat = %d %q", code, directives)
	}
	session.Stop()
	if code, directives := post(t, report(t, session.ID(), event("stopped", map[string]string{"reason": "requested"}))); code != http.StatusOK || directives != "stop" {
		t.Fatalf("stop report = %d %q", code, directives)
	}
	recordings, err := Recordings()
	if err != nil || len(recordings) != 1 || !recordings[0].Complete() || recordings[0].Info.Width != 64 {
		t.Fatalf("recordings = %+v, %v", recordings, err)
	}
	file, err := recordings[0].Segments[0].OpenVideo()
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(file)
	file.Close()
	if err != nil {
		t.Fatal(err)
	}

	boxes := mp4Boxes(t, content)
	for _, want := range []string{"ftyp", "mdat", "moov", "moov/mvhd", "moov/trak/tkhd", "moov/trak/mdia/minf/stbl/stsd/avc1/avcC", "moov/trak/mdia/minf/stbl/stsd/Opus/dOps", "moov/trak/edts/elst"} {
		if _, ok := boxes[want]; !ok {
			t.Errorf("no %s box; have %v", want, keys(boxes))
		}
	}
	if count := len(boxes["moov/trak"]); count != 2 {
		t.Fatalf("%d tracks", count)
	}
	stsz := boxes["moov/trak/mdia/minf/stbl/stsz"]
	if binary.BigEndian.Uint32(stsz[0][8:]) != 4 || binary.BigEndian.Uint32(stsz[1][8:]) != 10 {
		t.Fatalf("sample counts: video %d audio %d", binary.BigEndian.Uint32(stsz[0][8:]), binary.BigEndian.Uint32(stsz[1][8:]))
	}
	stss := boxes["moov/trak/mdia/minf/stbl/stss"][0]
	if binary.BigEndian.Uint32(stss[4:]) != 2 || binary.BigEndian.Uint32(stss[8:]) != 1 || binary.BigEndian.Uint32(stss[12:]) != 3 {
		t.Fatalf("sync samples = %x", stss)
	}
	mdat := boxes["mdat"][0]
	if bytes.Contains(mdat, []byte{0, 0, 0, 1, 0x67}) || !bytes.Contains(mdat, []byte{0, 0, 0, 19, 0x65}) {
		t.Fatal("mdat must hold length-prefixed slices without parameter sets")
	}

	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Log("ffprobe not on PATH; container check only")
		return
	}
	path := filepath.Join(t.TempDir(), "video.mp4")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(ffprobe, "-v", "error", "-count_frames", "-show_entries", "stream=codec_name,width,height,nb_read_frames,nb_frames", "-of", "json", path).CombinedOutput()
	if err != nil {
		t.Fatalf("ffprobe: %v\n%s", err, out)
	}
	var probe struct {
		Streams []struct {
			Codec  string `json:"codec_name"`
			Width  int    `json:"width"`
			Frames string `json:"nb_frames"`
			Read   string `json:"nb_read_frames"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &probe); err != nil || len(probe.Streams) != 2 ||
		probe.Streams[0].Codec != "h264" || probe.Streams[0].Width != 64 || probe.Streams[0].Read != "4" ||
		probe.Streams[1].Codec != "opus" || probe.Streams[1].Frames != "10" {
		t.Fatalf("ffprobe = %s", out)
	}
}

func TestWebViewHandlerRejectsBadReports(t *testing.T) {
	reset(t)
	session, err := Start(Config{Stream: true})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Stop()
	recorder := httptest.NewRecorder()
	WebViewHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, WebViewPath, nil))
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET = %d", recorder.Code)
	}
	good := report(t, session.ID(), sample(Video, true, 1, []byte{0, 0, 0, 1, 0x65}))
	for name, body := range map[string][]byte{
		"empty":             nil,
		"header too long":   binary.BigEndian.AppendUint32(nil, 100),
		"not json":          append(binary.BigEndian.AppendUint32(nil, 3), "abc"...),
		"missing bytes":     good[:len(good)-1],
		"extra bytes":       append(append([]byte(nil), good...), 0),
		"bad session":       report(t, "not-a-session"),
		"sized event":       report(t, session.ID(), reportItem{Event: "heartbeat", Size: 1, data: []byte{0}}),
		"negative size":     report(t, session.ID(), reportItem{Size: -1}),
		"overflowing sizes": report(t, session.ID(), reportItem{Size: math.MaxInt, data: []byte{0, 0, 0, 1, 0x65}}, reportItem{Size: math.MaxInt}, reportItem{Size: 3}),
		"oversized report":  append(binary.BigEndian.AppendUint32(nil, 2), make([]byte, maxWebViewBatch)...),
	} {
		if code, _ := post(t, body); code != http.StatusBadRequest {
			t.Errorf("%s: status %d", name, code)
		}
	}
	if code, _ := post(t, good); code != http.StatusOK {
		t.Fatalf("good report = %d", code)
	}
	other := strings.Repeat("ab", 16)
	if code, directives := post(t, report(t, other)); code != http.StatusOK || directives != "stop" {
		t.Fatalf("unknown session = %d %q", code, directives)
	}
}

func TestSecondStartReportInterruptsTheSession(t *testing.T) {
	reset(t)
	session, err := Start(Config{Record: true})
	if err != nil {
		t.Fatal(err)
	}
	started(t, session)
	if directives := Event(session.ID(), "started", `{"width":64,"height":48,"recorder":"go"}`); directives != "stop" {
		t.Fatalf("directives = %q", directives)
	}
	if status := session.Status(); status.State != Stopped || status.Reason != "interrupted" {
		t.Fatalf("status = %+v", status)
	}
}

// mp4Boxes indexes an MP4's boxes by path. Container boxes are descended;
// sample entries skip their fixed fields first.
func mp4Boxes(t *testing.T, content []byte) map[string][][]byte {
	t.Helper()
	boxes := map[string][][]byte{}
	containers := map[string]int{"moov": 0, "trak": 0, "mdia": 0, "minf": 0, "stbl": 0, "edts": 0, "dinf": 0, "stsd": 8, "avc1": 78, "Opus": 28}
	var walk func(prefix string, data []byte)
	walk = func(prefix string, data []byte) {
		for len(data) >= 8 {
			size := int(binary.BigEndian.Uint32(data))
			kind := string(data[4:8])
			header := 8
			if size == 1 {
				size, header = int(binary.BigEndian.Uint64(data[8:])), 16
			}
			if size < header || size > len(data) {
				t.Fatalf("box %s%s has size %d of %d", prefix, kind, size, len(data))
			}
			path := prefix + kind
			body := data[header:size]
			boxes[path] = append(boxes[path], body)
			if skip, ok := containers[kind]; ok {
				walk(path+"/", body[skip:])
			}
			data = data[size:]
		}
		if len(data) != 0 {
			t.Fatalf("%d trailing bytes under %s", len(data), prefix)
		}
	}
	walk("", content)
	return boxes
}

func keys(boxes map[string][][]byte) []string {
	var out []string
	for key := range boxes {
		out = append(out, key)
	}
	return out
}
