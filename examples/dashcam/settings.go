package dashcam

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr-native/extension/capture"
)

// Settings are the dash cam's choices. They persist as JSON in the app's
// data directory.
type Settings struct {
	// Quality is "720" or "1080".
	Quality string `json:"quality"`
	// Segment is each file's length in seconds.
	Segment int `json:"segment"`
	// Budget is the loop recording size in megabytes.
	Budget int `json:"budget"`
	// Impact is "off", "low", "medium" or "high" sensitivity.
	Impact    string `json:"impact"`
	Audio     bool   `json:"audio"`
	Location  bool   `json:"location"`
	AutoStart bool   `json:"autoStart"`
}

// Defaults: 1080p in one-minute files, a 4 GB loop, medium impact
// sensitivity and no sound. The microphone records conversations in the
// car, so it stays off until chosen.
func defaults() Settings {
	return Settings{Quality: "1080", Segment: 60, Budget: 4096, Impact: "medium", Location: true}
}

// impactThresholds maps sensitivity to the acceleration, in g, that locks
// footage. A pothole jolts a phone mount by about 1 g.
var impactThresholds = map[string]float64{"low": 3, "medium": 2, "high": 1.5}

// Config turns settings into a capture configuration.
func (s Settings) Config() capture.Config {
	config := capture.Config{
		Record:   true,
		Audio:    s.Audio,
		Location: s.Location,
		Motion:   true,
		Impact:   impactThresholds[s.Impact],
		Segment:  time.Duration(s.Segment) * time.Second,
		Budget:   int64(s.Budget) << 20,
	}
	if s.Quality == "1080" {
		config.Width, config.Height, config.Bitrate = 1920, 1080, 8_000_000
	} else {
		config.Width, config.Height, config.Bitrate = 1280, 720, 4_000_000
	}
	return config
}

// Summary says what Start records.
func (s Settings) Summary() string {
	impact := "impact lock off"
	if g, ok := impactThresholds[s.Impact]; ok {
		impact = fmt.Sprintf("locks on impacts over %g g", g)
	}
	sound := "no sound"
	if s.Audio {
		sound = "with sound"
	}
	return fmt.Sprintf("%sp in %s files, %s loop, %s, %s.", s.Quality, seconds(s.Segment), megabytes(s.Budget), impact, sound)
}

func seconds(value int) string {
	if value%60 == 0 {
		if value == 60 {
			return "1-minute"
		}
		return strconv.Itoa(value/60) + "-minute"
	}
	return strconv.Itoa(value) + "-second"
}

func megabytes(value int) string {
	if value >= 1024 && value%1024 == 0 {
		return strconv.Itoa(value/1024) + " GB"
	}
	return strconv.Itoa(value) + " MB"
}

// valid reports whether the stored values make a usable configuration.
func (s Settings) valid() error {
	if s.Quality != "720" && s.Quality != "1080" {
		return errors.New("quality must be 720 or 1080")
	}
	if s.Impact != "off" {
		if _, ok := impactThresholds[s.Impact]; !ok {
			return errors.New("unknown impact sensitivity")
		}
	}
	config := s.Config()
	// A loop smaller than a file deletes every unlocked file as it closes.
	file := int64(config.Bitrate) / 8 * int64(s.Segment)
	if int64(s.Budget)<<20 < 2*file {
		return errors.New("the loop must hold at least two files")
	}
	return config.Validate()
}

// store keeps settings in memory and, with a path, on disk.
type store struct {
	mu       sync.Mutex
	path     string
	settings Settings
}

// openStore reads saved settings. A missing or unreadable file gives the
// defaults; values it lacks keep theirs.
func openStore(dir string) *store {
	s := &store{settings: defaults()}
	if dir == "" {
		return s
	}
	s.path = filepath.Join(dir, "dashcam.json")
	content, err := os.ReadFile(s.path)
	if err != nil {
		return s
	}
	loaded := defaults()
	if json.Unmarshal(content, &loaded) == nil && loaded.valid() == nil {
		s.settings = loaded
	}
	return s
}

func (s *store) get() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings
}

// set validates and saves settings.
func (s *store) set(settings Settings) error {
	if err := settings.valid(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings = settings
	if s.path == "" {
		return nil
	}
	content, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	temporary := s.path + ".tmp"
	if err := os.WriteFile(temporary, content, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, s.path)
}
