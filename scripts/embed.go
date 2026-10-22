// Package scripts embeds the same installer shipped by CI. The HTTP fallback
// must not drift into a second implementation with weaker verification.
package scripts

import (
	_ "embed"
	"strings"
)

//go:embed install.sh
var unixInstaller string

// UnixInstaller sets a default URL while retaining explicit --server overrides.
// Shell quoting prevents request host/forwarding headers from becoming code.
func UnixInstaller(server string) string {
	quoted := "'" + strings.ReplaceAll(server, "'", "'\"'\"'") + "'"
	return strings.Replace(unixInstaller, `SERVER=""`, "SERVER="+quoted, 1)
}
