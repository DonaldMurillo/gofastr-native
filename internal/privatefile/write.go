package privatefile

import (
	"context"
	"os"
)

// WriteFile atomically replaces target with content. Content goes to a
// private temporary file named by pattern inside dir, is hardened, synced
// and closed, then renamed over target after one final context check, and
// the directory entry is synced. A failed or cancelled write leaves target
// untouched and removes the temporary file.
func WriteFile(ctx context.Context, dir, pattern, target string, content []byte) error {
	temporary, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	defer temporary.Close()
	if err := Harden(temporary.Name(), false); err != nil {
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := Replace(temporary.Name(), target); err != nil {
		return err
	}
	return SyncDirectory(dir)
}
