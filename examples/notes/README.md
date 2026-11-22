# One Notes application

Shared Go behavior, owner-scoped persistence, and Go-authored mobile screens;
native UI/device implementations belong to the renderer libraries. Android
native is deferred. No application-specific Swift or JavaScript is required.

- [Mobile services, action IDs, paging, audio metadata and acceptance fixtures](docs/mobile-services.md)
- [Implementation lessons and regression pointers](docs/agent-notes.md)

Validate with `go test ./examples/notes/...`,
`go test -race ./examples/notes/...`, and `go vet ./examples/notes/...`.
Also run the relevant packages with `GOWORK=off` to check the pinned dependency.

## Windows desktop editor

Run from the repository root in PowerShell.

```powershell
$env:GOWORK = 'off'
$env:CGO_ENABLED = '0'
go run ./examples/notes/cmd/notes-desktop
```

The desktop view uses the same owner-scoped Notes service and SQLite entity.
Create a note and enter a title and body in **Write**. Use **Pin note** in
**Details** to set its pin.
**Save** or Ctrl+Enter in the body saves the current text. You can keep typing
while the save runs. Use an empty title to see inline validation.
To reset unsaved edits, choose **Discard draft** from **Note actions** and
confirm. **Keep editing** closes the confirmation and leaves the draft open.

The app uses a separate local installation by default. Pass `-data <directory>`
to choose its directory. Each directory can be open in one desktop window at a
time. Drafts are stored on normal window close or when you
open another note. A new-note draft reopens at startup. To resume a draft for an
existing note, select that note in the list. The body uses explicit newlines
and scrolls long lines horizontally.
