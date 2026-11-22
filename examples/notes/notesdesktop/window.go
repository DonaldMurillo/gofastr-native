// Package notesdesktop composes the shared Notes service into a desktop editor.
package notesdesktop

import (
	"context"
	"crypto/rand"
	"errors"
	"strconv"
	"time"

	"github.com/DonaldMurillo/gofastr-native/behavior"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notes"
	"github.com/DonaldMurillo/gofastr-native/render/desktop"
)

type Window struct {
	ctx          context.Context
	service      *notes.Service
	drafts       *behavior.FileDraftStore
	editor       *behavior.Editor
	state        behavior.Snapshot
	note         notes.Note
	list         []notes.Note
	listRevision int
	stored       *behavior.DraftRevision
	status       string
	tab          int
	confirming   bool
	results      chan saveResult
	updates      chan struct{}
}

type saveResult struct {
	note         notes.Note
	list         []notes.Note
	listErr, err error
	submission   behavior.Submission
}

// New loads the note list and restores an unsaved new note. Call it after
// GoFastr starts the service, with the installation's owner in ctx.
func New(ctx context.Context, service *notes.Service, drafts *behavior.FileDraftStore) (*Window, error) {
	list, err := service.List(ctx, "")
	if err != nil {
		return nil, err
	}
	window := &Window{
		ctx: ctx, service: service, drafts: drafts, list: list,
		results: make(chan saveResult, 1), updates: make(chan struct{}, 1),
	}
	if err := window.open(notes.Note{}); err != nil {
		return nil, err
	}
	return window, nil
}

func (w *Window) Updates() <-chan struct{} { return w.updates }

func draftKey(id string) string { return "desktop-notes/" + id }

func (w *Window) open(note notes.Note) error {
	ctx := context.WithoutCancel(w.ctx)
	if err := w.flush(ctx); err != nil {
		return err
	}
	draft, found, err := w.drafts.Load(ctx, draftKey(note.ID))
	if err != nil {
		return err
	}
	baseline := notes.EditorValues(note)
	if found {
		baseline = draft.Baseline
	}
	editor, err := behavior.NewEditor(rand.Text(), draftKey(note.ID), baseline)
	if err != nil {
		return err
	}
	var stored *behavior.DraftRevision
	if found {
		if err := editor.Restore(draft); err != nil {
			return err
		}
		revision := draft.DraftRevision()
		stored = &revision
	}
	w.editor, w.state, w.note, w.stored = editor, editor.Snapshot(), note, stored
	w.tab, w.confirming = 0, false
	w.status = ""
	if found {
		w.status = "Draft restored"
	}
	return nil
}

func (w *Window) flush(ctx context.Context) error {
	if w.editor == nil {
		return nil
	}
	key := draftKey(w.note.ID)
	if w.state.Dirty {
		if err := w.drafts.PutIfRevision(ctx, key, w.state, w.stored); err != nil {
			return err
		}
		revision := w.state.DraftRevision()
		w.stored = &revision
	} else if w.stored != nil {
		if _, err := w.drafts.ClearIfRevision(ctx, key, *w.stored); err != nil {
			return err
		}
		w.stored = nil
	}
	return nil
}

func (w *Window) Close() error {
	if w.state.Pending {
		w.complete(<-w.results)
	}
	return w.flush(context.WithoutCancel(w.ctx))
}

func (w *Window) choose(note notes.Note) {
	if w.state.Pending || note.ID == w.note.ID {
		return
	}
	if err := w.open(note); err != nil {
		w.status = "Could not open that note or keep the current draft."
	}
}

// discardDraft resets the open editor after the user confirms. Re-read a
// saved note so an explicit discard can also recover from a stale baseline.
func (w *Window) discardDraft() {
	if w.state.Pending || !w.state.Dirty {
		return
	}
	ctx := context.WithoutCancel(w.ctx)
	note := w.note
	if note.ID != "" {
		current, err := w.service.Get(ctx, note.ID)
		if err != nil {
			w.status = "Could not reload the saved note. Your draft is still open."
			return
		}
		note = current
	}
	editor, err := behavior.NewEditor(rand.Text(), draftKey(note.ID), notes.EditorValues(note))
	if err != nil {
		w.status = "Could not reset the editor. Your draft is still open."
		return
	}
	if w.stored != nil {
		cleared, err := w.drafts.ClearIfRevision(ctx, draftKey(note.ID), *w.stored)
		if err != nil || !cleared {
			w.status = "Could not remove the stored draft. Your draft is still open."
			return
		}
	}
	w.editor, w.state, w.note, w.stored = editor, editor.Snapshot(), note, nil
	w.tab = 0
	w.status = "Draft discarded"
}

func (w *Window) change(key, value string) {
	values := w.state.Current
	values[key] = value
	w.state = w.editor.Checkpoint(values)
	w.status = ""
}

func (w *Window) save() {
	decision := w.editor.Begin(w.state.Current)
	w.state = decision.Snapshot
	if !decision.Admitted {
		return
	}
	submission := *decision.Submission
	id, details, baseline := w.note.ID, w.note.Details, w.state.Baseline
	ctx := context.WithoutCancel(w.ctx)
	go func() {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		details.Pinned = submission.Values["pinned"] == "true"
		note, err := w.service.SaveDetailsIfUnchanged(ctx, id, submission.Values["title"], submission.Values["body"], details, baseline)
		result := saveResult{note: note, err: err, submission: submission}
		if err == nil {
			result.list, result.listErr = w.service.List(ctx, "")
		}
		w.results <- result
		select {
		case w.updates <- struct{}{}:
		default:
		}
	}()
}

func (w *Window) complete(reply saveResult) {
	result := behavior.Result{
		InstanceID: reply.submission.InstanceID, Token: reply.submission.Token, Revision: reply.submission.Revision,
		Outcome: behavior.SaveSucceeded, Values: notes.EditorValues(reply.note), SavedRoute: draftKey(reply.note.ID),
	}
	if reply.err != nil {
		result.Outcome = behavior.SaveFailed
		result.Message = "Could not save the note."
		var validation *notes.ValidationError
		switch {
		case errors.As(reply.err, &validation):
			field := "body"
			if notes.ValidateText(reply.submission.Values["title"], "") != nil {
				field = "title"
			}
			result.FieldErrors = behavior.FieldErrors{field: validation.Message}
			result.Message = "Check the highlighted field."
		case errors.Is(reply.err, notes.ErrChanged):
			result.Message = "The saved note changed while this draft was open. Your edits are still here."
		}
	}
	done := w.editor.Complete(result, w.state.Current)
	if !done.Applied {
		return
	}
	w.state = done.Snapshot
	w.status = result.Message
	if result.Outcome != behavior.SaveSucceeded {
		if len(w.state.FieldErrors) != 0 {
			w.tab = 0
		}
		return
	}
	w.status = "Note saved"
	if w.stored != nil {
		if _, err := w.drafts.ClearIfRevision(context.WithoutCancel(w.ctx), draftKey(w.note.ID), *w.stored); err != nil {
			w.status = "Note saved. Could not clear the previous draft."
		}
	}
	w.stored = nil
	w.note = reply.note
	if reply.listErr == nil {
		w.list = reply.list
	} else {
		updated := false
		for i, note := range w.list {
			if note.ID == w.note.ID {
				w.list[i], updated = w.note, true
				break
			}
		}
		if !updated {
			w.list = append([]notes.Note{w.note}, w.list...)
		}
	}
	w.listRevision++
}

func (w *Window) View() desktop.View {
	select {
	case result := <-w.results:
		w.complete(result)
	default:
	}
	state := w.state
	status := w.status
	switch {
	case state.Pending:
		status = "Saving note…"
	case status == "" && state.Dirty:
		status = "Unsaved changes"
	}
	list := w.list
	selected := -1
	for i, note := range list {
		if note.ID == w.note.ID {
			selected = i
			break
		}
	}
	field := func(name, label, placeholder string) desktop.Field {
		return desktop.Field{
			Label: label, Value: state.Current[name], Placeholder: placeholder, Error: state.FieldErrors[name],
			Change: func(value string) { w.change(name, value) }, Submit: w.save,
		}
	}
	details := desktop.Checkbox("pin", desktop.Toggle{
		Label: "Pin note", Checked: state.Current["pinned"] == "true",
		Change: func(checked bool) { w.change("pinned", strconv.FormatBool(checked)) },
	})
	page := details.Height(30)
	if w.tab == 0 {
		page = desktop.VStack(8,
			desktop.TextInput("title-"+state.InstanceID, field("title", "Title", "Give this note a title")),
			desktop.TextArea("body-"+state.InstanceID, field("body", "Note", "Write your note")),
		)
	}
	editor := desktop.TabStrip("note-tabs", desktop.Tabs{
		Labels: []string{"Write", "Details"}, Selected: w.tab, Change: func(index int) { w.tab = index },
	}, page)
	content := desktop.VStack(8,
		desktop.Heading("Notes"),
		desktop.HStack(8,
			desktop.PushButton("new", desktop.Button{Label: "New note", Disabled: state.Pending, Press: func() { w.choose(notes.Note{}) }}),
			desktop.PushButton("save", desktop.Button{Label: "Save", Disabled: state.Pending, Press: w.save}),
			desktop.MenuButton("note-actions", desktop.Menu{Label: "Note actions", Items: []desktop.MenuItem{{
				Label: "Discard draft…", Disabled: state.Pending || !state.Dirty,
				Action: func() { w.confirming = true },
			}}}),
		).Height(38),
		desktop.Split("notes-and-editor", 30,
			desktop.VirtualTable("notes", desktop.Table{
				Key: strconv.Itoa(w.listRevision), Count: len(list), Selected: selected,
				Columns: []desktop.Column{{Title: "Title"}},
				Cell:    func(row, _ int) string { return list[row].Title },
				Select: func(row int) {
					if row >= 0 && row < len(list) {
						w.choose(list[row])
					}
				},
			}),
			editor,
		),
		desktop.Status(status),
	).Padding(12)
	if w.confirming {
		message := "Restore the last saved version of this note."
		if w.note.ID == "" {
			message = "Clear this unsaved note."
		}
		content = desktop.DialogLayer("discard-draft", desktop.Dialog{
			Title: "Discard this draft?", Content: desktop.Text(message),
			Confirm: "Discard draft", Cancel: "Keep editing",
			OnConfirm: func() { w.confirming = false; w.discardDraft() },
			OnCancel:  func() { w.confirming = false },
		}, content)
	}
	return desktop.View{Title: "Notes", Content: content}
}
