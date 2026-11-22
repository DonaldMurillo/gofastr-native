package main

import (
	"testing"

	"golang.org/x/sys/windows"
)

func TestEditorInstallationLockIsExclusive(t *testing.T) {
	dir := t.TempDir()
	first, err := lockInstallation(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, blocked := lockInstallation(dir)
	if err := windows.CloseHandle(first); err != nil {
		t.Fatal(err)
	}
	if blocked == nil {
		windows.CloseHandle(second)
		t.Fatal("a second editor acquired the same installation while it was open")
	}
	second, err = lockInstallation(dir)
	if err != nil {
		t.Fatal(err)
	}
	windows.CloseHandle(second)
}
