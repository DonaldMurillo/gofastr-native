// Package logexplorer is the desktop renderer's live-data example.
package logexplorer

import (
	"context"
	"sync"
	"time"
)

const (
	InitialEntries = 1_000_000
	ReplayRate     = 5_000
	ReplayDuration = 10 * time.Second
	replayInterval = 20 * time.Millisecond
)

const severityCount = 4

var levels = []string{"Debug", "Info", "Warning", "Error"}
var sources = []string{"scheduler", "http", "storage", "worker", "cache"}
var messages = [severityCount][]string{
	{"Cache lookup completed", "Worker heartbeat received"},
	{"Request completed", "Job finished", "Snapshot loaded"},
	{"Queue lag exceeded threshold", "Request retry scheduled"},
	{"Upstream request timed out", "Job failed"},
}
var startTime = time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)

// Entry IDs are ascending, unique, and independent of filter and scroll state.
type Entry struct {
	ID      int
	Time    time.Time
	Level   string
	Source  string
	Message string
}

func entry(id int) Entry {
	index := id - 1
	level := index % severityCount
	group := messages[level]
	return Entry{
		ID: id, Time: startTime.Add(time.Duration(index/5) * time.Millisecond),
		Level: levels[level], Source: sources[index%len(sources)],
		Message: group[(index/severityCount)%len(group)],
	}
}

// Rows is a stable snapshot of the deterministic fixture. Its index can
// resolve a viewport without building or scanning the entire result set.
type Rows struct {
	Total int
	level int // -1 means all levels
}

func (r Rows) Len() int {
	if r.level < 0 {
		return r.Total
	}
	return (r.Total + severityCount - 1 - r.level) / severityCount
}

func (r Rows) At(index int) (Entry, bool) {
	if index < 0 || index >= r.Len() {
		return Entry{}, false
	}
	if r.level >= 0 {
		index = index*severityCount + r.level
	}
	return entry(index + 1), true
}

func (r Rows) Index(id int) int {
	if id < 1 || id > r.Total {
		return -1
	}
	index := id - 1
	if r.level < 0 {
		return index
	}
	if index%severityCount != r.level {
		return -1
	}
	return index / severityCount
}

// Replay publishes the high-water mark of the fixture from a worker. All
// data up to that mark can be read by ID. No row changes or is removed.
type Replay struct {
	mu      sync.Mutex
	total   int
	running bool
	updates chan struct{}
}

func NewReplay() *Replay {
	return &Replay{total: InitialEntries, updates: make(chan struct{}, 1)}
}

func (r *Replay) Updates() <-chan struct{} { return r.updates }

func (r *Replay) Snapshot(level int) (Rows, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return Rows{Total: r.total, level: level}, r.running
}

func (r *Replay) Start(ctx context.Context) {
	r.mu.Lock()
	if r.running || ctx.Err() != nil {
		r.mu.Unlock()
		return
	}
	first := r.total
	r.running = true
	r.mu.Unlock()
	r.notify()
	go func() {
		start := time.Now()
		ticker := time.NewTicker(replayInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				r.mu.Lock()
				r.running = false
				r.mu.Unlock()
				r.notify()
				return
			case <-ticker.C:
				if r.advance(first, time.Since(start)) {
					return
				}
			}
		}
	}()
}

func (r *Replay) advance(first int, elapsed time.Duration) (done bool) {
	elapsed = min(elapsed, ReplayDuration)
	total := first + int(elapsed*ReplayRate/time.Second)
	done = elapsed == ReplayDuration
	r.mu.Lock()
	r.total = total
	r.running = !done
	r.mu.Unlock()
	r.notify()
	return done
}

func (r *Replay) notify() {
	select {
	case r.updates <- struct{}{}:
	default:
	}
}
