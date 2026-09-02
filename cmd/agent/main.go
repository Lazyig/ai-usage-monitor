// Command agent scans local AI tool logs on this device and ships usage events to the server.
package main

import (
	"flag"
	"fmt"
	"os"
)

var version = "dev"

func main() {
	fs := flag.NewFlagSet("agent", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `ai-usage-agent %s

usage: agent <command> [flags]

commands:
  scan      read local logs and print a summary table (no server needed)
  run       scan continuously and push to the server
  status    show cursors and pending queue
  install   register as an OS service (systemd / launchd / Windows)
`, version)
	}
	if len(os.Args) < 2 {
		fs.Usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "scan", "run", "status", "install", "uninstall":
		fmt.Fprintf(os.Stderr, "agent %s: not implemented yet (Phase 1)\n", os.Args[1])
		os.Exit(1)
	case "version":
		fmt.Println(version)
	default:
		fs.Usage()
		os.Exit(2)
	}
}
