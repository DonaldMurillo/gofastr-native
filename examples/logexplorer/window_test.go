package logexplorer

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestPausedSelectionSurvivesReplay(t *testing.T) {
	replay := NewReplay()
	window := NewWindow(t.Context(), replay)
	first := window.View()
	first.Buttons[0].Press()
	const selected = InitialEntries - 22
	window.View().Table.Select(selected)
	paused := window.View()
	if paused.Table.Follow || !strings.Contains(paused.Detail.Text, "Entry 999979\r\n") {
		t.Fatalf("selected view = %#v", paused.Detail)
	}
	for elapsed := replayInterval; elapsed <= ReplayDuration; elapsed += replayInterval {
		replay.advance(InitialEntries, elapsed)
		next := window.View()
		if next.Table.Follow || next.Table.Key != paused.Table.Key || next.Table.Selected != selected {
			t.Fatalf("at %v, paused table moved or lost its selection", elapsed)
		}
		if next.Detail != paused.Detail || next.Table.Cell(selected, 3) != paused.Detail.Copy {
			t.Fatalf("at %v, selected record changed", elapsed)
		}
	}
	last := window.View()
	if want := InitialEntries + ReplayRate*10; last.Table.Count != want {
		t.Fatalf("row count = %d, want %d", last.Table.Count, want)
	}
	last.Buttons[0].Press()
	if resumed := window.View(); !resumed.Table.Follow || resumed.Detail != paused.Detail {
		t.Fatal("resuming live follow changed the selected entry")
	}
}

func TestFilterRemapsStableSelection(t *testing.T) {
	window := NewWindow(t.Context(), NewReplay())
	window.View().Table.Select(3)
	before := window.View()
	before.Filter.Change(4) // Error
	filtered := window.View()
	if filtered.Table.Count != InitialEntries/4 || filtered.Table.Selected != 0 || filtered.Detail != before.Detail {
		t.Fatalf("matching filter lost selection: %#v", filtered.Detail)
	}
	filtered.Filter.Change(3) // Warning
	if next := window.View(); next.Table.Selected != -1 || next.Detail.Copy != "" {
		t.Fatal("a hidden row remained selected or copyable")
	}
}

func TestFixtureOrdersTimestampTiesByID(t *testing.T) {
	rows, _ := NewReplay().Snapshot(-1)
	first, _ := rows.At(0)
	for i := 1; i < 5; i++ {
		next, ok := rows.At(i)
		if !ok || next.Time != first.Time || next.ID != first.ID+i {
			t.Fatalf("unexpected tied entry at %d: %#v", i, next)
		}
	}
}

func TestReplayWorkerPublishesAndStops(t *testing.T) {
	replay := NewReplay()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	replay.Start(ctx)
	waitFor := func(condition func(Rows, bool) bool) {
		t.Helper()
		timeout := time.NewTimer(2 * time.Second)
		defer timeout.Stop()
		for {
			select {
			case <-replay.Updates():
				if condition(replay.Snapshot(-1)) {
					return
				}
			case <-timeout.C:
				t.Fatal("timed out waiting for a replay update")
			}
		}
	}
	waitFor(func(rows Rows, running bool) bool { return running && rows.Total > InitialEntries })
	cancel()
	waitFor(func(_ Rows, running bool) bool { return !running })
}

func BenchmarkReplayViewport(b *testing.B) {
	replay := NewReplay()
	window := NewWindow(b.Context(), replay)
	window.View().Table.Select(InitialEntries - 20)
	b.ReportAllocs()
	for b.Loop() {
		replay.advance(InitialEntries, 5*time.Second)
		view := window.View()
		for row := InitialEntries - 25; row < InitialEntries; row++ {
			for column := range view.Table.Columns {
				_ = view.Table.Cell(row, column)
			}
		}
	}
}
