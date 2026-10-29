//go:build !windows

package agentcfg

// SecureStateDir is a no-op outside Windows: the state directory is created
// 0700 and its files 0600.
func SecureStateDir(string) error { return nil }

// MigrateLegacyState only has work to do on Windows, where the state moved
// from %APPDATA% to %ProgramData%.
func MigrateLegacyState(string) (string, error) { return "", nil }
