package capture

import (
	"errors"
	"sync"
)

// Sink receives a running session's media and telemetry, for example a WHIP
// publisher. Each sink gets its own goroutine and queue, so a slow sink
// never stalls the encoder: when its queue fills, video is dropped until
// the next keyframe and a keyframe is requested.
type Sink interface {
	WriteMedia(Media) error
	WriteTelemetry(Telemetry) error
	Close() error
}

const sinkQueueLength = 256

type sinkItem struct {
	media     *Media
	telemetry *Telemetry
}

type sinkQueue struct {
	session *Session
	sink    Sink
	items   chan sinkItem
	done    chan struct{}
	once    sync.Once

	mu         sync.Mutex
	waitForKey bool
	closed     bool
}

// AddSink attaches a sink. Media reaches sinks only when Config.Stream is
// set. A sink that returns an error is closed and detached; Status reports
// the error.
func (s *Session) AddSink(sink Sink) error {
	queue := &sinkQueue{session: s, sink: sink, items: make(chan sinkItem, sinkQueueLength), done: make(chan struct{}), waitForKey: true}
	s.mu.Lock()
	if !s.state.Active() {
		s.mu.Unlock()
		return errors.New("capture: session has ended")
	}
	if len(s.sinks) >= 8 {
		s.mu.Unlock()
		return errors.New("capture: at most 8 sinks per session")
	}
	s.sinks = append(s.sinks, queue)
	s.keyframe = true
	s.mu.Unlock()
	go queue.run()
	return nil
}

func (q *sinkQueue) run() {
	defer close(q.done)
	for item := range q.items {
		var err error
		if item.media != nil {
			err = q.sink.WriteMedia(*item.media)
		} else {
			err = q.sink.WriteTelemetry(*item.telemetry)
		}
		if err != nil {
			q.session.detach(q, err)
			for range q.items {
			}
			return
		}
	}
}

// offer queues without blocking and reports whether the queue overflowed.
// Video waiting for a keyframe is skipped, not counted as an overflow.
func (q *sinkQueue) offer(item sinkItem) (overflow bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return false
	}
	if media := item.media; media != nil && media.Track == Video {
		if q.waitForKey && !media.Key {
			return false
		}
		q.waitForKey = false
	}
	select {
	case q.items <- item:
		return false
	default:
		if item.media != nil && item.media.Track == Video {
			q.waitForKey = true
			q.session.RequestKeyframe()
		}
		return true
	}
}

// shut stops accepting items. It is safe to call more than once.
func (q *sinkQueue) shut() {
	q.mu.Lock()
	if !q.closed {
		q.closed = true
		close(q.items)
	}
	q.mu.Unlock()
}

// close drains the queue, then closes the sink.
func (q *sinkQueue) close() {
	q.shut()
	<-q.done
	q.once.Do(func() { q.sink.Close() })
}

func (s *Session) detach(queue *sinkQueue, err error) {
	s.mu.Lock()
	for index, candidate := range s.sinks {
		if candidate == queue {
			s.sinks = append(s.sinks[:index:index], s.sinks[index+1:]...)
			break
		}
	}
	s.sinkError = err.Error()
	s.mu.Unlock()
	queue.shut()
	queue.once.Do(func() { queue.sink.Close() })
	changed()
}

func (s *Session) publish(item sinkItem) {
	s.mu.Lock()
	sinks := append([]*sinkQueue(nil), s.sinks...)
	s.mu.Unlock()
	dropped := int64(0)
	for _, sink := range sinks {
		if sink.offer(item) {
			dropped++
		}
	}
	if dropped > 0 {
		s.mu.Lock()
		s.dropped += dropped
		s.mu.Unlock()
	}
}
