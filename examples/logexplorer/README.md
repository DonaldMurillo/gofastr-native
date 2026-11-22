# Log Explorer

Log Explorer composes a Go-authored desktop view with live data. A GoFastr
host runs the app in-process. The renderer draws the table, filter, and detail
pane on a DirectX surface and retains scroll and keyboard focus by key as
the view changes. It schedules a draw on input or a worker update.

Run from the repository root on Windows:

```powershell
$env:GOWORK = 'off'
$env:CGO_ENABLED = '0'
go run ./examples/logexplorer/cmd/logexplorer
```

The window starts at the tail of a deterministic fixture with one million
entries. Select an entry or click **Pause live follow**, then click
**Replay 50,000 entries**. The replay publishes changes for ten seconds
at 5,000 entries per second. While paused, the visible rows, selection, and
detail stay in place. **Resume live follow** jumps to the newest matching
entry. Use the level filter to change which entries the table shows.
**Copy message** or Ctrl+C in the table copies the selected message.

The fixture generates each entry by ID when requested. The synthetic replay
isolates the cost of updating the UI from disk, parsing, and indexing.
[The example view](window.go) builds a column with a toolbar row and a
resizable split pane. It supplies Go callbacks and a row snapshot. The
[node API](../../render/desktop/node.go) also supports a clipped paint node
for custom drawing.

Run the model benchmark and Windows graphics checks:

```powershell
$env:GOWORK = 'off'
$env:CGO_ENABLED = '0'
go test ./examples/logexplorer -bench BenchmarkReplayViewport -benchmem
$env:GOFASTR_DESKTOP_GPU_TEST = '1'
go test ./render/desktop -count=1 -v
```

The benchmark measures Go view construction and data access for a 25-row
viewport. The graphics check opens a DirectX window, checks the drawn
selection and a custom paint node, and verifies that the paused viewport
survives replay and reordering.

## DirectX benchmark and acceptance

`logbench` measures the example on its real DirectX path, also with cgo
disabled. It has six modes: `suite` (default; startup, replay, idle),
`startup`, `replay`, `idle`, `steady`, and `interactive`:

```powershell
$env:GOWORK = 'off'
$env:CGO_ENABLED = '0'
go build -o logbench.exe ./examples/logexplorer/cmd/logbench
1..3 | ForEach-Object { ./logbench.exe -mode startup }   # fresh processes
./logbench.exe -mode suite -json report.json             # replay + idle
./logbench.exe -mode steady -seconds 5                   # redraw throughput
```

By default the window runs hidden: no window appears, nothing takes the
foreground, and the run does not disturb other GUI work. The DirectX
device, scene build, text layout, and draw-command submission all run;
only presentation to a display is skipped. The suite checks the graphics
library, replay completion at 1,050,000 rows, that Update+Draw p99 fits a
60 Hz frame, the event-to-drawn-frame latency, that an idle window draws
no frames and stays near 0% CPU, and that the back buffer content changes
across the replay. Every run prints `RESULT: PASS` or `RESULT: FAIL` with
the measured value and limit per check; `-json` also writes the full
report.

Timing sources are distinct in the report: Update/Draw and frame periods
are CPU timestamps of the game-loop callbacks; event-to-draw runs from the
fixture worker's data notification (a synthetic command, not keyboard or
mouse) to the frame that rendered it; back-buffer readbacks are
GPU-to-CPU syncs, timed separately. Presentation and display scanout
happens inside the graphics driver after Draw and is not directly
observable. `-mode steady -fps-mode vsync -visible` approximates the
present-and-vsync wait as the frame period beyond Update+Draw.
`-mode interactive -visible` measures time from the focused window polling
keyboard or mouse input to the end of Draw. Both modes open a visible
window. Physical input-to-display latency is NOT RUN.

Screen-reader acceptance for the GPU renderer is NOT RUN. True cold start
after a reboot is NOT RUN; `startup` reports per-run process-create to
first-draw times from repeated fresh processes instead.
