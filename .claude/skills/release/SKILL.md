---
name: release
description: Prepare and verify a gofastr-native version (vX.Y.Z and tui/vX.Y.Z). Use when asked to cut, prepare, check or tag a release, bump the version, write the changelog, or audit the repository for things that should not ship (local paths, tool names, secrets, licenses).
---

# Release gofastr-native

RELEASING.md is the procedure. This skill adds the order to work in and the
review that scripts cannot do. Never tag, push tags or create a GitHub release
unless the user asks for that step in this conversation.

## 1. Find the range

```sh
git describe --tags --abbrev=0 --match 'v*' 2>/dev/null || echo "no tag yet"
git log --oneline <previous tag>..HEAD
```

With no previous tag, the range is the whole history.

## 2. Run the automatic checks first

```sh
GOWORK=off go test -count=1 ./internal/repocheck
go run ./internal/release check -skip-tests vX.Y.Z
```

Fix every repocheck finding at its source. Add `repocheck:allow` only to a
deliberate fixture or placeholder, and say which lines you marked. If a rule
misfires on legitimate text, narrow the rule in `internal/repocheck` and add
a case to `TestRules` rather than marking many lines.

If you found a new kind of problem by hand in step 3, add a rule for it with
a test case, so the next release catches it automatically.

## 3. Review what the checks cannot see

Read the diff for the range, not only the file list.

- Claims: grep the docs for "verified", "tested on", "works on", "PASS". Each
  must match what was actually run. Hardware, store upload, screen reader and
  IME checks stay NOT RUN unless someone ran them.
- Version mentions: README, `docs/getting-started.md`, `docs/design/status.md`,
  CLI help and scaffold templates (`cmd/gofastr-native/scaffolds`). Anything
  that says "unreleased", names the previous version, or will be wrong once
  the tag exists.
- Provenance: new files that are not Go, Swift, Java or JS source (images,
  fonts, sounds, data tables, vendored code). Where did each come from, and
  does its license allow redistribution? Credit third-party files in the
  README's License section.
- Private identifiers: ticket numbers from a private tracker, repository IDs,
  local branch or worktree names, machine names, people's names other than
  the author's.
- Design notes and agent notes (`docs/design/`, `*/docs/agent-notes.md`):
  they collect session residue. Read new entries in the range.

## 4. Write the changelog

Add `## vX.Y.Z (YYYY-MM-DD)` to CHANGELOG.md with today's date, split by
module (phone apps, `tui`). List user-visible changes from the commit range,
then one short paragraph on what was and was not tested on real hardware.
Follow the repository's writing rules in AGENTS.md: plain words, no puffery.

## 5. Full check on the release commit

Commit on a branch, then run the full check (it takes several minutes):

```sh
go run ./internal/release check vX.Y.Z
```

Report every line that is not PASS. A NOT RUN for the origin tag lookup or
node needs a reason in your report. Once the PR is open, its CI run must pass
too: CI covers Linux, the check covers the machine you ran it on.

## 6. Hand back

Tell the user: the PR, the check result, the CI result, what you changed and
what you could not verify. Do not merge without being asked.

## 7. Publish, only when asked

After the PR merges and CI passes on main:

```sh
git switch main && git pull --ff-only
go run ./internal/release publish -dry-run vX.Y.Z
```

Show the user the dry-run plan. Run `publish -yes vX.Y.Z` only after they
confirm in this conversation; the prompt it skips is their confirmation, not
yours. Report every line, including the module proxy results.
