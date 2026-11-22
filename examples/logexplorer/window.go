package logexplorer

import (
	"context"
	"fmt"
	"strconv"

	"github.com/DonaldMurillo/gofastr-native/render/desktop"
)

var filters = append([]string{"All levels"}, levels...)
var columns = []desktop.Column{
	{Title: "UTC time", Width: 115},
	{Title: "Level", Width: 85},
	{Title: "Source", Width: 95},
	{Title: "Message", Width: 270},
}

// Window holds the filter, live-follow setting and selected ID for one view.
// The renderer retains the controls and the viewport across View calls.
type Window struct {
	ctx      context.Context
	replay   *Replay
	filter   int
	selected int
	follow   bool
}

func NewWindow(ctx context.Context, replay *Replay) *Window {
	return &Window{ctx: ctx, replay: replay, follow: true}
}

func (w *Window) View() desktop.View {
	rows, running := w.replay.Snapshot(w.filter - 1)
	selected := rows.Index(w.selected)
	followLabel := "Resume live follow"
	if w.follow {
		followLabel = "Pause live follow"
	}
	replayLabel := "Replay 50,000 entries"
	if running {
		replayLabel = "Replaying entries"
	}
	detail := desktop.Detail{Title: "Entry details", Text: "Select a log entry to see its details."}
	if e, ok := rows.At(selected); ok {
		detail.Text = fmt.Sprintf("Entry %d\r\n%s\r\n%s  /  %s\r\n\r\n%s",
			e.ID, e.Time.Format("2006-01-02 15:04:05.000 UTC"), e.Level, e.Source, e.Message)
		detail.Copy = e.Message
	}
	view := desktop.View{
		Title: "Log Explorer",
		Buttons: []desktop.Button{
			{Label: followLabel, Press: func() { w.follow = !w.follow }},
			{Label: replayLabel, Disabled: running, Press: func() { w.replay.Start(w.ctx) }},
		},
		Filter: desktop.Choice{Options: filters, Selected: w.filter, Change: w.setFilter},
		Table: desktop.Table{
			Key: strconv.Itoa(w.filter), Columns: columns, Count: rows.Len(), Follow: w.follow, Selected: selected, Copy: detail.Copy,
			Cell: func(row, column int) string { return cell(rows, row, column) },
			Select: func(row int) {
				if row < 0 {
					w.selected = 0
				} else if e, ok := rows.At(row); ok {
					w.selected = e.ID
					w.follow = false
				}
			},
		},
		Detail: detail,
		Status: fmt.Sprintf("%d matching entries · %d total", rows.Len(), rows.Total),
	}
	view.Content = desktop.VStack(6,
		desktop.Heading(view.Title),
		desktop.HStack(8,
			desktop.SelectBox("severity", view.Filter),
			desktop.PushButton("follow", view.Buttons[0]),
			desktop.PushButton("replay", view.Buttons[1]),
		).Height(40),
		desktop.Split("logs-and-details", 68,
			desktop.VirtualTable("logs", view.Table),
			desktop.Details("details", view.Detail),
		),
		desktop.Status(view.Status),
	).Padding(12)
	return view
}

func (w *Window) setFilter(filter int) {
	if filter < 0 || filter >= len(filters) {
		return
	}
	w.filter = filter
	rows, _ := w.replay.Snapshot(filter - 1)
	if rows.Index(w.selected) < 0 {
		w.selected = 0
	}
}

func cell(rows Rows, row, column int) string {
	e, ok := rows.At(row)
	if !ok {
		return ""
	}
	switch column {
	case 0:
		return e.Time.Format("15:04:05.000")
	case 1:
		return e.Level
	case 2:
		return e.Source
	case 3:
		return e.Message
	}
	return ""
}
