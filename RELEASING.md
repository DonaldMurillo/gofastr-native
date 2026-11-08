# Releasing gofastr-native

This is about tagging versions of the Go modules in this repository. For
signing an app built with gofastr-native, see [docs/release.md](docs/release.md).

## Versions and tags

Two modules get tags, from the same commit:

| Module | Directory | Tag |
|---|---|---|
| `github.com/DonaldMurillo/gofastr-native` | `.` | `vX.Y.Z` |
| `github.com/DonaldMurillo/gofastr-native/tui` | `tui/` | `tui/vX.Y.Z` |

Modules under `examples/` are not tagged. The list lives in
`repocheck.ReleasedModules`; a new `go.mod` anywhere else fails the hygiene
test until you add it there or move it under `examples/`.

Before 1.0, bump the minor version for API changes and the patch version for
fixes.

## Checks that run all the time

`internal/repocheck` runs with every `go test ./...`. It fails on:

- paths from one machine: home directories, `mktemp` suffixes, `/var/folders`,
  and `/tmp` screenshots or logs cited as evidence in Markdown
- names of the editor or agent tools a change was made with, and chat-style
  phrasing ("this continuation", "the user asked") in Markdown <!-- repocheck:allow -->
- real email addresses, private keys, access tokens, keystores, provisioning
  profiles and `.env` files
- claims that gofastr-native has no published version
- a missing `LICENSE`, `CHANGELOG.md` or README License section, a tracked
  `go.work`, a `replace` in a released module, and a module path that does not
  match its directory
- a file carrying someone else's copyright notice that the README's License
  section does not credit by path

A line that must keep a match, such as a test fixture, can carry
`repocheck:allow`.

To run it before every commit:

```sh
git config core.hooksPath scripts/hooks
```

The hook runs `GOWORK=off go test ./internal/repocheck`, which takes a few
seconds. `git commit --no-verify` skips it.

## Cutting a release

1. Write the `## vX.Y.Z (YYYY-MM-DD)` section of [CHANGELOG.md](CHANGELOG.md).
   Read `git log <previous tag>..HEAD` so nothing user-visible is missing, and
   say what was and was not run on real hardware.
2. Update anything that names the current version: the README, getting
   started, and [docs/design/status.md](docs/design/status.md).
3. Commit on a branch, then run:

   ```sh
   go run ./internal/release check vX.Y.Z
   ```

   It changes nothing. It checks the changelog entry, a clean working tree,
   that the tags are unused locally and on `origin`, and the hygiene rules.
   Then it clones `HEAD` into a temporary directory and, with `GOWORK=off`,
   runs `go mod tidy -diff`, `gofmt -l`, `go vet` and `go test` in every
   module, `node --test` on the WebView tests, and classifies the license of
   every dependency module that is built, tested or used as a `go tool`.
   Allowed licenses: MIT, BSD-2-Clause, BSD-3-Clause, Apache-2.0, ISC and
   Unicode-3.0. Anything else fails until someone reviews it.

   `-skip-tests` skips `go test` and `node --test` for a quick pass. Run the
   full check at least once on the release commit.
4. Open the PR and merge it once CI passes.
5. Publish from `main`:

   ```sh
   git switch main && git pull --ff-only
   go run ./internal/release publish -dry-run vX.Y.Z   # shows the plan, changes nothing
   go run ./internal/release publish vX.Y.Z
   ```

   publish refuses to run unless `HEAD` is `origin/main` and the CI run for
   that commit's push to `main` succeeded. It repeats the checks (CI already
   ran the tests), then asks you to type the version. After that it:

   - creates an annotated tag per released module at `HEAD`
     (`vX.Y.Z`, `tui/vX.Y.Z`)
   - pushes both tags in one `git push --atomic`; if the push fails, it
     deletes the local tags, so nothing is half-published
   - creates the GitHub release from the version's CHANGELOG section, with
     `--verify-tag` so gh never makes a tag of its own
   - asks proxy.golang.org for each module at the new version, retrying for
     about two and a half minutes while the proxy fetches the tag

   `-yes` skips the prompt. A failure after the push says what is already
   public and what to finish by hand.

## What the checks cannot see

Read for these yourself before tagging:

- Docs that promise behavior the code does not have, or say something was
  verified on hardware when it was not.
- New images, fonts, sounds or data files: where they came from and whether
  their license allows redistribution. Credit third-party ones in the README's
  License section.
- Code adapted from another project, even without a copyright header.
- Identifiers that only mean something to one person: ticket numbers from a
  private tracker, internal repository IDs, names of local branches.
