// Command agent scans local AI tool logs on this device and ships usage events
// to the server.
//
// Subcommands:
//
//	scan     read local logs and print a summary table (no server needed)
//	run      scan continuously and push to the server
//	status   show config, cursors, queue and server health
//	install  write the config and register an OS service
//	uninstall / start / stop / restart   manage that service
//	version
//
// Exit codes: 0 ok, 1 runtime error, 2 usage error.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

// stdout is where the reporting commands write. It is a variable so tests can
// capture a command's output without going through a pipe.
var stdout io.Writer = os.Stdout

// usageError makes a command exit 2 instead of 1.
type usageError struct{ err error }

func (e *usageError) Error() string { return e.err.Error() }
func (e *usageError) Unwrap() error { return e.err }

func usagef(format string, args ...any) error {
	return &usageError{err: fmt.Errorf(format, args...)}
}

const usageText = `ai-usage-agent %s

usage: ai-usage-agent <command> [flags]

commands:
  scan       read local logs and print a usage table (works with no config)
  run        collect and push to the server on a timer
  status     show state dir, config, cursors, queue and server health
  install    write config.json and register the OS service
  uninstall  remove the OS service
  start      start the OS service
  stop       stop the OS service
  restart    restart the OS service
  version    print the agent version

environment:
  AI_USAGE_SERVER      server base URL (overridden by --server)
  AI_USAGE_TOKEN       device token   (overridden by --token)
  AI_USAGE_STATE_DIR   state directory (default: per-OS config dir)

run "ai-usage-agent <command> -h" for a command's flags.
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, usageText, version)
		return 2
	}

	var err error
	switch cmd := args[0]; cmd {
	case "scan":
		err = cmdScan(args[1:])
	case "run":
		err = cmdRun(args[1:])
	case "status":
		err = cmdStatus(args[1:])
	case "install", "uninstall", "start", "stop", "restart":
		err = cmdService(cmd, args[1:])
	case "version":
		fmt.Println(version)
	case "-h", "--help", "help":
		fmt.Fprintf(os.Stdout, usageText, version)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		fmt.Fprintf(os.Stderr, usageText, version)
		return 2
	}

	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 2
		}
		fmt.Fprintf(os.Stderr, "ai-usage-agent: %v\n", err)
		var ue *usageError
		if errors.As(err, &ue) {
			return 2
		}
		return 1
	}
	return 0
}

// newFlagSet builds a per-subcommand FlagSet that reports errors to the caller
// instead of calling os.Exit, so main owns the exit codes.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
}

// parseFlags wraps flag parsing so a bad flag becomes exit code 2.
func parseFlags(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		return &usageError{err: err}
	}
	return nil
}
