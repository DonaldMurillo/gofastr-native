//go:build windows

package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/localapp"
	"golang.org/x/sys/windows"

	"github.com/DonaldMurillo/gofastr-native/behavior"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notesapp/application"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notesdesktop"
	"github.com/DonaldMurillo/gofastr-native/render/desktop"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	config, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	directory := flag.String("data", filepath.Join(config, "gofastr-notes-desktop"), "Notes data directory")
	flag.Parse()
	dataDir, err := filepath.Abs(*directory)
	if err != nil {
		return err
	}
	lock, err := lockInstallation(dataDir)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(lock)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	shared, err := application.Open(dataDir)
	if err != nil {
		return err
	}
	if !localapp.Install() {
		shared.App.NoteAuthenticator()
	}
	shared.App.Config.DisableSignalHandling = true
	drafts, err := behavior.NewFileDraftStore(filepath.Join(dataDir, "drafts"))
	if err != nil {
		_ = shared.App.Shutdown(context.Background())
		return err
	}
	var window *notesdesktop.Window
	host := &notesHost{Host: desktop.Host{View: func() desktop.View { return window.View() }}}
	shared.App.OnStart(func(appCtx context.Context) error {
		var err error
		window, err = notesdesktop.New(handler.SetUser(appCtx, shared.Data.User), shared.Service, drafts)
		if err != nil {
			return err
		}
		host.Updates = window.Updates()
		return nil
	})
	shared.App.OnStop(func() error {
		if window != nil {
			return window.Close()
		}
		return nil
	})
	return shared.App.Run(ctx, host)
}

func lockInstallation(dir string) (windows.Handle, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return 0, err
	}
	path, err := windows.UTF16PtrFromString(filepath.Join(dir, "desktop.lock"))
	if err != nil {
		return 0, err
	}
	// Deny sharing while the editor is open. The kernel releases the lock
	// when the process exits, including after a crash.
	lock, err := windows.CreateFile(path, windows.GENERIC_READ|windows.GENERIC_WRITE,
		0, nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		return 0, errors.New("another Desktop Notes window is using this data directory")
	}
	return lock, err
}

// Wait for the UI thread before the stop hooks write its final draft.
type notesHost struct {
	desktop.Host
	mu      sync.Mutex
	done    chan struct{}
	stopped bool
}

func (h *notesHost) Run(ctx context.Context, session *framework.Session) error {
	h.mu.Lock()
	if h.stopped {
		h.mu.Unlock()
		return nil
	}
	done := make(chan struct{})
	h.done = done
	h.mu.Unlock()
	defer close(done)
	return h.Host.Run(ctx, session)
}

func (h *notesHost) Stop(ctx context.Context) error {
	h.mu.Lock()
	h.stopped = true
	done := h.done
	h.mu.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
