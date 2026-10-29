package main

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A file name with a newline in it (anyone who can write a log folder picks
// those) must not start a line of its own in agent.log.
func TestLogEntriesStayOnOneLine(t *testing.T) {
	var buf bytes.Buffer
	lg := log.New(oneLine{&buf}, "", 0)
	lg.Printf("claudecode: read %s: denied", "x\n* * * * * root touch /tmp/PWNED\r\n.jsonl")
	lg.Printf("plain\tentry")
	got := buf.String()
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d: %q", len(lines), got)
	}
	if !strings.Contains(lines[0], `x\n* * * * * root touch /tmp/PWNED\r\n.jsonl`) {
		t.Errorf("newlines not escaped: %q", lines[0])
	}
	if lines[1] != "plain\tentry" {
		t.Errorf("an ordinary entry changed: %q", lines[1])
	}
}

// agent.log swapped for a symlink (to /etc/crontab, a shell profile) is not
// written through.
func TestLogFileRefusesASymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "crontab")
	if err := os.WriteFile(target, []byte("original\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "agent.log")); err != nil {
		t.Fatal(err)
	}
	if f, err := openLog(filepath.Join(dir, "agent.log")); err == nil {
		f.Close()
		t.Fatal("openLog followed a symlink")
	}
	if b, _ := os.ReadFile(target); string(b) != "original\n" {
		t.Errorf("target changed: %q", b)
	}
}
