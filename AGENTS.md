# gofastr-native

Instructions for coding agents working in this repository. Directories with
their own `AGENTS.md` (`render/ios`, `render/android`, `extension/media`,
`extension/photos`) add rules for that code.

## Before you commit

- Run `GOWORK=off go test ./internal/repocheck`. It also runs inside
  `go test ./...`. Fix every finding; add `repocheck:allow` to a line only
  when the match is a deliberate fixture or placeholder.
- Use `GOWORK=off` for any check you report as passing. The local `go.work`
  may point at a gofastr checkout that a fresh clone does not have.
- Run `gofmt -l .` in each module you touched (`.`, `tui`, `examples/chordharp`).

## Writing docs, notes and comments

Everything committed here is public and read by people who were not there
when you made the change.

- No paths from your machine. Write `GOCACHE="$(mktemp -d)"`, not the
  directory one run produced. Write `/path/to/gofastr`, not your checkout.
- Evidence is what was checked and what it showed. A `/tmp` screenshot or log
  is not evidence anyone else can open: describe it, or commit it under
  `testdata/`.
- Name no editor, agent or harness (the tool you are running in included).
  Write "the UI automation tool could not attach", not the product name.
- No chat phrasing: "this continuation", "this session", "the user asked".
  State the fact.
- Say NOT RUN for checks that did not run. Do not promote a build, a launch
  screenshot or a simulator run to a hardware or accessibility pass.
- Keep `docs/design/status.md` true when you change what works.

## Dependencies and assets

- New Go dependencies must be MIT, BSD, Apache-2.0 or ISC.
  `go run ./internal/release check -skip-tests vX.Y.Z` lists every
  dependency's license.
- New images, fonts, sounds or data files from elsewhere: confirm the license
  allows redistribution, keep their notice, and credit their path in the
  README's License section. repocheck fails on an uncredited copyright notice.
- Never commit keystores, provisioning profiles, `.env` files or `go.work`.

## Releases

Follow [RELEASING.md](RELEASING.md). Do not tag or push tags unless asked.
