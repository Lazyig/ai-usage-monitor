package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"ai-usage-monitor/internal/agentcfg"
	"ai-usage-monitor/internal/event"
)

// cmdService implements install, uninstall, start, stop and restart.
//
// install is the only one that writes configuration: it stores the server URL
// and the device token, then registers `<this binary> run` with the platform's
// service manager and starts it. Failing to register is deliberately NOT fatal
// (containers without systemd are a normal case here): the config is kept and
// the user is told how to run the agent manually.
func cmdService(cmd string, args []string) error {
	flags := newFlagSet(cmd)
	var (
		server    = flags.String("server", "", "server base URL, e.g. https://usage.example.com")
		token     = flags.String("token", "", "device token issued by the server")
		name      = flags.String("name", "", "device name (default: this machine's hostname)")
		interval  = flags.Int("interval", 0, "seconds between collection passes (default 60)")
		stateDir  = flags.String("state-dir", "", stateDirUsage)
		claude    stringList
		allowHTTP bool
	)
	if cmd == "install" {
		flags.BoolVar(&allowHTTP, "allow-http", false, "permit (and remember) a plain http:// server that is not on this machine: the token then crosses the network unencrypted")
		flags.Var(&claude, "claude-dir", "extra Claude Code config directory to read, repeatable (default: $"+agentcfg.EnvClaudeConfigDir+", comma separated)")
	}
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "usage: ai-usage-agent %s [flags]\n\n", cmd)
		if cmd == "install" {
			fmt.Fprint(flags.Output(), "Write config.json and register the OS service.\n\nflags:\n")
			flags.PrintDefaults()
			return
		}
		fmt.Fprintf(flags.Output(), "%s the %s OS service.\n\nflags:\n", cmd, serviceName)
		flags.PrintDefaults()
	}
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	if err := noArgs(flags, cmd); err != nil {
		return err
	}
	dir := resolveStateDir(*stateDir)

	if cmd == "install" {
		return install(dir, *server, *token, *name, *interval, claude, allowHTTP)
	}

	svc, err := newService(dir)
	if err != nil {
		return fmt.Errorf("%s: %w", cmd, err)
	}
	switch cmd {
	case "uninstall":
		err = svc.Uninstall()
	case "start":
		err = svc.Start()
	case "stop":
		err = svc.Stop()
	case "restart":
		err = svc.Restart()
	}
	if err != nil {
		return fmt.Errorf("%s %s: %w", cmd, serviceName, err)
	}
	fmt.Fprintf(stdout, "%s: %s ok\n", serviceName, cmd)
	return nil
}

func install(dir, server, token, name string, interval int, claudeDirs []string, allowHTTP bool) error {
	// Windows agents before 0.3 kept their state in the installing user's
	// %APPDATA%, which the LocalSystem service must not trust: carry it over.
	if from, err := agentcfg.MigrateLegacyState(dir); err != nil {
		return err
	} else if from != "" {
		fmt.Fprintf(stdout, "moved the agent state from %s to %s\n", from, dir)
	}
	cfg, _, err := agentcfg.Load(dir)
	if err != nil {
		return stateAccessError(dir, err)
	}
	if server != "" {
		cfg.ServerURL = server
		// A new address is judged on its own: an http allowance kept for the
		// previous one must not carry over to it.
		cfg.AllowHTTP = false
	}
	if token != "" {
		cfg.DeviceToken = token
	}
	if name != "" {
		cfg.DeviceName = name
	}
	if interval > 0 {
		cfg.IntervalSeconds = interval
	}
	if strings.TrimSpace(cfg.ServerURL) == "" {
		return usagef("--server is required (or set %s)", agentcfg.EnvServer)
	}
	if strings.TrimSpace(cfg.DeviceToken) == "" {
		return usagef("--token is required (or set %s); get one with `POST /api/v1/devices` on the server", agentcfg.EnvToken)
	}
	if allowHTTP {
		cfg.AllowHTTP = true
	}
	if err := agentcfg.CheckServerURL(cfg.ServerURL, cfg.AllowHTTP); err != nil {
		return &usageError{err: err}
	}
	if cfg.AllowHTTP && agentcfg.CheckServerURL(cfg.ServerURL, false) != nil {
		fmt.Fprintf(os.Stderr, "warning: %s is plain http: the device token crosses the network unencrypted\n", cfg.ServerURL)
	}
	if err := addClaudeDirs(&cfg, claudeDirs); err != nil {
		return err
	}
	if err := agentcfg.Save(dir, cfg); err != nil {
		return err
	}
	syncDeviceID(dir, &cfg, func(format string, args ...any) { fmt.Fprintf(stdout, format+"\n", args...) })
	// Never print the token, not even partially.
	fmt.Fprintf(stdout, "wrote %s (device %s, name %q, interval %ds)\n",
		agentcfg.ConfigPath(dir), cfg.DeviceID, cfg.DeviceName, cfg.Interval())
	fmt.Fprintln(stdout)
	printRoots(stdout, event.SourceClaudeCode, *cfg.Source(event.SourceClaudeCode))
	fmt.Fprintf(stdout, "add another Claude Code config dir with: %s roots add <dir>\n\n", exeName())

	svc, err := newService(dir)
	if err != nil {
		manualHint(dir, err)
		return nil
	}
	// Re-running the installer (e.g. after downloading a newer binary) replaces
	// the existing registration: stop + uninstall, then install again. The SCM
	// deletes a Windows service asynchronously, hence the short retry.
	_ = svc.Stop()
	_ = svc.Uninstall()
	var ierr error
	for attempt := 0; attempt < 5; attempt++ {
		if ierr = svc.Install(); ierr == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if ierr != nil {
		manualHint(dir, ierr)
		return nil
	}
	fmt.Fprintf(stdout, "registered the %s service (%s)\n", serviceName, svc.Platform())

	if err := svc.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not start the service: %v\n", err)
		fmt.Fprintf(stdout, "start it later with: %s start\n", exeName())
		return nil
	}
	fmt.Fprintln(stdout, "service started")
	return nil
}

// addClaudeDirs adds the Claude Code config directories given to install, or
// those in $CLAUDE_CONFIG_DIR when none were, to cfg. Folders already
// configured (a re-install, an upgrade) are kept; install only ever adds.
func addClaudeDirs(cfg *agentcfg.Config, dirs []string) error {
	fromEnv := false
	if len(dirs) == 0 {
		dirs, fromEnv = agentcfg.ClaudeConfigDirsFromEnv(), true
	}
	sc := cfg.Source(event.SourceClaudeCode)
	if fromEnv && len(dirs) > 0 && !sc.Enabled {
		// Only an explicit --claude-dir turns a disabled source back on.
		fmt.Fprintf(stdout, "%s is disabled; ignoring $%s\n", event.SourceClaudeCode, agentcfg.EnvClaudeConfigDir)
		return nil
	}
	for _, d := range dirs {
		root, err := agentcfg.NormalizeRoot(event.SourceClaudeCode, d)
		if err != nil {
			return usagef("--claude-dir: %v", err)
		}
		if !sc.AddRoot(root) {
			continue
		}
		for _, w := range rootWarnings(event.SourceClaudeCode, root) {
			fmt.Fprintf(os.Stderr, "warning: %s: %s\n", root, w)
		}
	}
	return nil
}

// manualHint explains how to keep going when no service manager is available,
// which is the normal case inside a container. Exit code stays 0: the device is
// configured, only the autostart is missing.
func manualHint(dir string, err error) {
	fmt.Fprintf(os.Stderr, "warning: could not register an OS service: %v\n", err)
	fmt.Fprintf(os.Stderr, "the configuration is saved; run the agent yourself with:\n")
	fmt.Fprintf(os.Stderr, "    %s\n", strings.Join(append([]string{exeName()}, runArgs(dir)...), " "))
	fmt.Fprintf(os.Stderr, "(inside a container, add it to the image's start script or supervisor)\n")
}

func exeName() string {
	exe, err := os.Executable()
	if err != nil {
		return serviceName
	}
	return exe
}
