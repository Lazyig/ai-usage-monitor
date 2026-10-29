// Command agent scans local AI tool logs on this device and ships usage events
// to the server.
//
// Subcommands:
//
//	scan     read local logs and print a summary table (no server needed)
//	run      scan continuously and push to the server
//	status   show config, cursors, queue and server health
//	rescan   drop the cursors and the queue so the next pass re-reads everything
//	roots    list, add or remove the log folders a source reads
//	install  write the config and register an OS service
//	uninstall / start / stop / restart   manage that service
//	version
//
// Exit codes: 0 ok, 1 runtime error, 2 usage error.
//
// Source layout:
//
//	main.go        flag plumbing and the subcommand dispatch
//	cmd_*.go       one file per subcommand: flags in, work handed off
//	bootstrap.go   config + device identity + migrations -> a ready agent
//	agent.go       the run loop: scan, push, persist cursors, resync
//	service.go     OS service registration and lifecycle
//	collectors.go  which collectors this binary has and which ones to run
//	report.go      the tables `scan` prints
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"ai-usage-monitor/internal/agentcfg"
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
  rescan     forget the cursors and the queue, so the next pass starts from zero
  roots      list, add or remove log folders (e.g. extra Claude Code config dirs)
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
  CLAUDE_CONFIG_DIR    read by install only: extra Claude Code config dir(s),
                       comma separated (overridden by --claude-dir)

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
	case "rescan":
		err = cmdRescan(args[1:])
	case "roots":
		err = cmdRoots(args[1:])
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

// stringList is a flag that may be given several times.
type stringList []string

func (l *stringList) String() string { return strings.Join(*l, ", ") }
func (l *stringList) Set(v string) error {
	*l = append(*l, v)
	return nil
}

// parseFlags wraps flag parsing so a bad flag becomes exit code 2.
func parseFlags(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		return &usageError{err: err}
	}
	return nil
}

// noArgs rejects positional arguments for a command that takes none.
func noArgs(flags *flag.FlagSet, name string) error {
	if flags.NArg() > 0 {
		return usagef("%s takes no positional arguments (got %q)", name, flags.Arg(0))
	}
	return nil
}

// stateDirUsage is the --state-dir help text every command shares.
const stateDirUsage = "state directory (default: $AI_USAGE_STATE_DIR or the per-OS config dir)"

// resolveStateDir lets --state-dir win over $AI_USAGE_STATE_DIR and the per-OS
// default, matching the "flags override env" rule.
func resolveStateDir(flagValue string) string {
	if strings.TrimSpace(flagValue) != "" {
		return flagValue
	}
	return agentcfg.StateDir()
}

// stateAccessError explains a state directory the current user may not read:
// on Windows the service's settings are readable by SYSTEM and Administrators
// only, so a command run from an ordinary terminal is refused.
func stateAccessError(dir string, err error) error {
	if err == nil || !errors.Is(err, fs.ErrPermission) {
		return err
	}
	return fmt.Errorf("%w\n%s holds the service's settings and only administrators may read it: run this in an administrator terminal, or pass --state-dir", err, dir)
}

// signalContext cancels on SIGINT/SIGTERM so a run in a terminal, a systemd
// stop and a launchd shutdown all unwind the same way.
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
