//go:build windows

package agentcfg

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestTrustedOwner(t *testing.T) {
	for _, s := range []string{"S-1-5-18", "S-1-5-32-544", trustedInstallerSID} {
		sid, err := windows.StringToSid(s)
		if err != nil {
			t.Fatal(err)
		}
		if !trustedOwner(sid) {
			t.Errorf("%s should be a trusted owner", s)
		}
	}
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	if !trustedOwner(u.User.Sid) {
		t.Error("the current user should be trusted (objects it created itself)")
	}
	users, _ := windows.StringToSid("S-1-5-32-545") // BUILTIN\Users
	if trustedOwner(users) {
		t.Error(`BUILTIN\Users must not be a trusted owner`)
	}
}

// junction makes a directory junction, which needs no privilege on Windows.
func junction(t *testing.T, link, target string) {
	t.Helper()
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("mklink /J: %v %s", err, out)
	}
}

func TestCheckPreparedRejectsAJunction(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "elsewhere")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "state")
	junction(t, link, target)
	if err := checkPrepared(link); err == nil || !strings.Contains(err.Error(), "junction") {
		t.Fatalf("checkPrepared(junction) = %v, want a refusal", err)
	}
	if err := checkPrepared(target); err != nil {
		t.Fatalf("a folder this user created was refused: %v", err)
	}
}

// Migration copies the old state, so new files inherit the locked-down folder's
// permissions, and it never follows a reparse point the user left behind.
func TestCopyTreeCopiesFilesAndSkipsJunctions(t *testing.T) {
	base := t.TempDir()
	src, dst, secret := filepath.Join(base, "old"), filepath.Join(base, "new"), filepath.Join(base, "secret")
	for _, d := range []string{filepath.Join(src, "queue"), secret} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(src, "queue", "batch-1.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secret, "key.txt"), []byte("k"), 0o600); err != nil {
		t.Fatal(err)
	}
	junction(t, filepath.Join(src, "queue", "link"), secret)
	if err := copyTree(filepath.Join(src, "queue"), filepath.Join(dst, "queue")); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dst, "queue", "batch-1.json")); err != nil || string(b) != "{}" {
		t.Fatalf("batch not copied: %q %v", b, err)
	}
	if _, err := os.Lstat(filepath.Join(dst, "queue", "link")); !os.IsNotExist(err) {
		t.Fatalf("the junction was copied or followed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(src, "queue", "batch-1.json")); err != nil {
		t.Fatalf("copyTree must leave the source for MigrateLegacyState to remove: %v", err)
	}
}
