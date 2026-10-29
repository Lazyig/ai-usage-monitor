//go:build windows

package agentcfg

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// machineSD makes Administrators the owner and grants full control to SYSTEM
// and Administrators only, with inheritance from the parent cut: %ProgramData%
// lets every user create and write inside new folders, and the LocalSystem
// service reads its config, and writes its log, here. The owner matters as much
// as the DACL: an owner may rewrite the DACL whatever it says.
const machineSD = "O:BAD:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"

// trustedInstallerSID is NT SERVICE\TrustedInstaller, which owns folders
// Windows itself creates.
const trustedInstallerSID = "S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464"

// SecureStateDir locks the default (machine-wide) state directory down to
// SYSTEM and Administrators. A directory chosen with --state-dir or
// AI_USAGE_STATE_DIR is left as its owner set it up.
//
// It refuses a directory another account could have prepared before the
// install: a junction or symlink would send the service's writes elsewhere, and
// a folder owned by an ordinary user stays under that user's control.
func SecureStateDir(dir string) error {
	if !isMachineStateDir(dir) {
		return nil
	}
	if err := checkPrepared(dir); err != nil {
		return err
	}
	sd, err := windows.SecurityDescriptorFromString(machineSD)
	if err != nil {
		return fmt.Errorf("agentcfg: state dir ACL: %w", err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return fmt.Errorf("agentcfg: state dir owner: %w", err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("agentcfg: state dir ACL: %w", err)
	}
	err = windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		owner, nil, dacl, nil)
	if err != nil {
		return fmt.Errorf("agentcfg: restrict %s to SYSTEM and Administrators (run as administrator): %w", dir, err)
	}
	return nil
}

func isMachineStateDir(dir string) bool {
	return strings.EqualFold(filepath.Clean(dir), filepath.Clean(defaultMachineStateDir()))
}

// checkPrepared rejects a state directory that is a reparse point or is owned
// by anyone but SYSTEM, Administrators, TrustedInstaller or the current user.
func checkPrepared(dir string) error {
	if isReparsePoint(dir) {
		return fmt.Errorf("agentcfg: %s is a junction or symlink; delete it and install again", dir)
	}
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("agentcfg: read the owner of %s: %w", dir, err)
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil {
		return fmt.Errorf("agentcfg: read the owner of %s: %v", dir, err)
	}
	if trustedOwner(owner) {
		return nil
	}
	name, domain, _, _ := owner.LookupAccount("")
	return fmt.Errorf("agentcfg: %s is owned by %s\\%s, not by SYSTEM or Administrators; another account may have prepared it, so delete it and install again", dir, domain, name)
}

func trustedOwner(owner *windows.SID) bool {
	if owner.IsWellKnown(windows.WinLocalSystemSid) || owner.IsWellKnown(windows.WinBuiltinAdministratorsSid) {
		return true
	}
	if ti, err := windows.StringToSid(trustedInstallerSID); err == nil && owner.Equals(ti) {
		return true
	}
	// An administrator whose objects default to "object creator" ownership.
	if u, err := windows.GetCurrentProcessToken().GetTokenUser(); err == nil && owner.Equals(u.User.Sid) {
		return true
	}
	return false
}

func isReparsePoint(path string) bool {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	attrs, err := windows.GetFileAttributes(p)
	return err == nil && attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
}

func defaultMachineStateDir() string {
	if pd := os.Getenv("ProgramData"); pd != "" {
		return filepath.Join(pd, AppName)
	}
	return filepath.Join(`C:\ProgramData`, AppName)
}

// LegacyWindowsStateDir is where agents before the move to %ProgramData% kept
// their state: the installing user's roaming profile.
func LegacyWindowsStateDir() string {
	if ad := os.Getenv("APPDATA"); ad != "" {
		return filepath.Join(ad, AppName)
	}
	return filepath.Join(homeDir(), "AppData", "Roaming", AppName)
}

// MigrateLegacyState moves config.json, cursors.json and queue/ from the old
// per-user state directory into dir, the default machine one, when dir has no
// config yet. It reports what it moved from, or "" when there was nothing to
// do. The old directory is removed once everything is across.
func MigrateLegacyState(dir string) (string, error) {
	if !isMachineStateDir(dir) {
		return "", nil
	}
	old := LegacyWindowsStateDir()
	if _, err := os.Stat(ConfigPath(dir)); err == nil {
		return "", nil // already migrated, or a fresh install that kept going
	}
	if _, err := os.Stat(ConfigPath(old)); err != nil {
		return "", nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("agentcfg: create %s: %w", dir, err)
	}
	if err := SecureStateDir(dir); err != nil {
		return "", err
	}
	// Copied, not renamed: a rename on the same volume keeps the old per-user
	// permissions (the user as owner, with full control), while a copy inherits
	// the locked-down ones above. Config last: its presence marks the migration
	// done, so an interrupted one is simply repeated.
	for _, name := range []string{"cursors.json", "queue", "config.json"} {
		if err := copyTree(filepath.Join(old, name), filepath.Join(dir, name)); err != nil {
			return "", fmt.Errorf("agentcfg: migrate %s: %w", name, err)
		}
	}
	_ = os.RemoveAll(old) // leftovers (agent.log) are not worth failing over
	return old, nil
}

// copyTree copies src to dst; the new files take their permissions from dst's
// parent. A missing src is fine, and reparse points are skipped rather than
// followed: the old folder was writable by the user.
func copyTree(src, dst string) error {
	fi, err := os.Lstat(src)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if isReparsePoint(src) || fi.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	_ = os.RemoveAll(dst)
	if fi.IsDir() {
		if err := os.MkdirAll(dst, 0o700); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := copyTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	if !fi.Mode().IsRegular() {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
