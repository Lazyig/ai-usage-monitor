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

// machineDACL grants full control to SYSTEM and Administrators only, with
// inheritance from the parent cut: %ProgramData% lets every user create and
// write inside new folders, and the LocalSystem service reads its config, and
// writes its log, here.
const machineDACL = "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"

// SecureStateDir locks the default (machine-wide) state directory down to
// SYSTEM and Administrators. A directory chosen with --state-dir or
// AI_USAGE_STATE_DIR is left as its owner set it up.
func SecureStateDir(dir string) error {
	if !strings.EqualFold(filepath.Clean(dir), filepath.Clean(defaultMachineStateDir())) {
		return nil
	}
	sd, err := windows.SecurityDescriptorFromString(machineDACL)
	if err != nil {
		return fmt.Errorf("agentcfg: state dir ACL: %w", err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("agentcfg: state dir ACL: %w", err)
	}
	err = windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
	if err != nil {
		return fmt.Errorf("agentcfg: restrict %s to SYSTEM and Administrators: %w", dir, err)
	}
	return nil
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
	if !strings.EqualFold(filepath.Clean(dir), filepath.Clean(defaultMachineStateDir())) {
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
	// Config last: its presence marks the migration done, so an interrupted
	// one is simply repeated.
	for _, name := range []string{"cursors.json", "queue", "config.json"} {
		if err := moveTree(filepath.Join(old, name), filepath.Join(dir, name)); err != nil {
			return "", fmt.Errorf("agentcfg: migrate %s: %w", name, err)
		}
	}
	_ = os.RemoveAll(old) // leftovers (agent.log) are not worth failing over
	return old, nil
}

// moveTree renames src to dst, copying when the rename crosses volumes. A
// missing src is fine.
func moveTree(src, dst string) error {
	fi, err := os.Stat(src)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	_ = os.RemoveAll(dst)
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if fi.IsDir() {
		if err := os.MkdirAll(dst, 0o700); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := moveTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		return os.RemoveAll(src)
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
	if err := out.Close(); err != nil {
		return err
	}
	in.Close()
	return os.Remove(src)
}
