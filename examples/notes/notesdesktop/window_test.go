package notesdesktop

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/localapp"
	"github.com/DonaldMurillo/gofastr/framework/owner"

	"github.com/DonaldMurillo/gofastr-native/behavior"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notes"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notesapp/application"
)

func TestDesktopNotesValidatesAndRecoversNewerEdits(t *testing.T) {
	dir := t.TempDir()
	shared, err := application.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	old := owner.GetExtractor()
	localapp.Install()
	t.Cleanup(func() { owner.SetExtractor(old) })
	ctx, cancel := context.WithCancel(t.Context())
	finished := make(chan error, 1)
	go func() { finished <- shared.App.Run(ctx, framework.ServicesHost()) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-finished:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(6 * time.Second):
			t.Error("GoFastr did not stop")
		}
	})
	select {
	case <-shared.App.ServicesReady():
	case <-time.After(5 * time.Second):
		t.Fatal("GoFastr did not start")
	}
	ctx = handler.SetUser(ctx, shared.Data.User)
	drafts, err := behavior.NewFileDraftStore(filepath.Join(dir, "drafts"))
	if err != nil {
		t.Fatal(err)
	}
	w, err := New(ctx, shared.Service, drafts)
	if err != nil {
		t.Fatal(err)
	}
	complete := func() {
		t.Helper()
		select {
		case <-w.Updates():
			w.View()
		case <-time.After(6 * time.Second):
			t.Fatal("save did not return")
		}
	}
	w.change("body", "first line\nsecond line")
	w.save()
	complete()
	if w.state.FieldErrors["title"] == "" || w.note.ID != "" {
		t.Fatal("a missing title did not stop the save")
	}
	w.change("title", "Design notes")
	w.change("pinned", "true")
	w.save()
	w.change("body", "typed during the save")
	complete()
	if w.note.ID == "" || !w.note.Pinned || w.note.Body != "first line\nsecond line" ||
		w.state.Current["body"] != "typed during the save" || !w.state.Dirty {
		t.Fatalf("save or draft state was lost: %#v", w.state)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := New(ctx, shared.Service, drafts)
	if err != nil {
		t.Fatal(err)
	}
	next.choose(w.note)
	if next.state.Current["body"] != "typed during the save" || next.status != "Draft restored" {
		t.Fatal("the saved note's newer draft did not reopen")
	}
	next.discardDraft()
	if next.state.Dirty || next.state.Current["body"] != "first line\nsecond line" || next.status != "Draft discarded" {
		t.Fatal("discard did not reload the saved text")
	}
	if _, found, err := drafts.Load(ctx, draftKey(w.note.ID)); err != nil || found {
		t.Fatalf("discard left a stored draft: found=%t, err=%v", found, err)
	}

	next.change("body", "draft in this window")
	if err := next.flush(ctx); err != nil {
		t.Fatal(err)
	}
	key := draftKey(w.note.ID)
	baseline := notes.EditorValues(w.note)
	other, err := behavior.NewEditor("other-window", key, baseline)
	if err != nil {
		t.Fatal(err)
	}
	baseline["body"] = "draft saved elsewhere"
	replacement := other.Checkpoint(baseline)
	if err := drafts.Put(ctx, key, replacement); err != nil {
		t.Fatal(err)
	}
	next.discardDraft()
	if !next.state.Dirty || next.state.Current["body"] != "draft in this window" {
		t.Fatal("a conflicting stored draft caused the current editor to lose its changes")
	}
	if err := next.Close(); !errors.Is(err, behavior.ErrDraftConflict) {
		t.Fatalf("a stale editor could overwrite the newer draft on close: %v", err)
	}
	persisted, found, err := drafts.Load(ctx, key)
	if err != nil || !found || !replacement.DraftRevision().Matches(persisted) {
		t.Fatalf("discard removed a newer stored draft: found=%t, err=%v", found, err)
	}
}
