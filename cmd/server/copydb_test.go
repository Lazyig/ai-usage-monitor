package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-usage-monitor/internal/store"
	"ai-usage-monitor/internal/store/storetest"
)

func TestCopyDBCommand(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.db")
	s, err := store.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreateDevice(context.Background(), "mac"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	dst := storetest.DSN(t)

	run := func(env map[string]string, args ...string) (int, string, string) {
		var out, errOut bytes.Buffer
		code := runCopyDB(args, &out, &errOut, func(k string) string { return env[k] })
		return code, out.String(), errOut.String()
	}

	if code, _, msg := run(nil); code != 2 || !strings.Contains(msg, "source is required") {
		t.Errorf("no source: %d %s", code, msg)
	}
	if code, _, msg := run(nil, "-from", src, "-to", src); code != 2 || !strings.Contains(msg, "same database") {
		t.Errorf("same database: %d %s", code, msg)
	}
	// The target defaults to the server's own setting.
	if code, _, msg := run(map[string]string{"DB_PATH": src}, "-from", src); code != 2 || !strings.Contains(msg, "same database") {
		t.Errorf("default target = source: %d %s", code, msg)
	}
	if code, _, msg := run(map[string]string{"SRC": src}, "-from", src, "-from-env", "SRC"); code != 2 || !strings.Contains(msg, "not both") {
		t.Errorf("both source forms: %d %s", code, msg)
	}

	code, out, msg := run(map[string]string{"SRC": src, "DATABASE_URL": dst, "DB_PATH": dst}, "-from-env", "SRC")
	if code != 0 || !strings.Contains(out, "copied 1 devices and 0 events") {
		t.Fatalf("copy: %d %s %s", code, out, msg)
	}
	// Once filled, the target is refused.
	if code, _, msg := run(nil, "-from", src, "-to", dst); code != 1 || !strings.Contains(msg, "already holds") {
		t.Errorf("second copy: %d %s", code, msg)
	}
}

// -h shows no value taken from the environment.
func TestCopyDBHelpShowsNoSecrets(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runCopyDB([]string{"-h"}, &out, &errOut, func(string) string { return "postgres://u:secret@db/x" })
	if code != 0 || strings.Contains(errOut.String(), "secret") || !strings.Contains(errOut.String(), "-from-env") {
		t.Errorf("help: %d %s", code, errOut.String())
	}
}

// A source without data (most likely a wrong -from) is refused before the
// target is created.
func TestCopyDBEmptySourceCreatesNothing(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "empty.db")
	s, err := store.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	dst := filepath.Join(dir, "target.db")
	var out, errOut bytes.Buffer
	if code := runCopyDB([]string{"-from", src, "-to", dst}, &out, &errOut, func(string) string { return "" }); code != 1 ||
		!strings.Contains(errOut.String(), "-allow-empty") {
		t.Fatalf("empty source: %d %s", code, errOut.String())
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Errorf("the refused copy created the target: %v", err)
	}
	if code := runCopyDB([]string{"-from", src, "-to", dst, "-allow-empty"}, &out, &errOut, func(string) string { return "" }); code != 0 {
		t.Errorf("-allow-empty: %d %s", code, errOut.String())
	}
}
