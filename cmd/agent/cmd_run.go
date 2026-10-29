package main

import (
	"fmt"

	"github.com/kardianos/service"

	"ai-usage-monitor/internal/agentcfg"
)

// cmdRun collects and pushes on a timer until it is asked to stop.
func cmdRun(args []string) error {
	flags := newFlagSet("run")
	var (
		once      = flags.Bool("once", false, "do a single collect+push pass and exit")
		interval  = flags.Int("interval", 0, "seconds between passes (default: the configured interval)")
		server    = flags.String("server", "", "server base URL (overrides the config and $AI_USAGE_SERVER)")
		token     = flags.String("token", "", "device token (overrides the config and $AI_USAGE_TOKEN)")
		sources   = flags.String("source", "", "comma separated sources to collect (default: all enabled)")
		stateDir  = flags.String("state-dir", "", stateDirUsage)
		allowHTTP = flags.Bool("allow-http", false, "permit a plain http:// server that is not on this machine, for this run (or set "+agentcfg.EnvAllowHTTP+"=1)")
	)
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "usage: ai-usage-agent run [flags]\n\nCollect local usage and push it to the server.\n\nflags:\n")
		flags.PrintDefaults()
	}
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	if err := noArgs(flags, "run"); err != nil {
		return err
	}

	sel, err := parseSources(*sources)
	if err != nil {
		return &usageError{err: err}
	}
	dir := resolveStateDir(*stateDir)
	ov := runOverrides{server: *server, token: *token, interval: *interval, allowHTTP: *allowHTTP}

	// Under a service manager the process must reach svc.Run() quickly (Windows
	// gives it 30s), so every slow step happens after that, inside the service.
	if !*once && !service.Interactive() {
		return runAsService(dir, sel, ov)
	}

	ag, err := bootstrap(dir, sel, ov)
	if err != nil {
		return err
	}

	ctx, stop := signalContext()
	defer stop()
	if *once {
		return ag.tick(ctx)
	}
	return ag.loop(ctx)
}
