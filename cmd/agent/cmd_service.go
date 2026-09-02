package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"ai-usage-monitor/internal/agentcfg"
)

// cmdService implements install, uninstall, start, stop and restart.
//
// install is the only one that writes configuration: it stores the server URL
// and the device token, then registers `<this binary> run` with the platform's
// service manager and starts it. Failing to register is deliberately NOT fatal
// (containers without systemd are a normal case here): the config is kept and
// the user is told how to run the agent manually.
func cmdService(cmd string, args []string) error {
	fs := newFlagSet(cmd)
	var (
		server   = fs.String("server", "", "server base URL, e.g. https://usage.example.com")
		token    = fs.String("token", "", "device token issued by the server")
		name     = fs.String("name", "", "device name (default: this machine's hostname)")
		interval = fs.Int("interval", 0, "seconds between collection passes (default 60)")
		stateDir = fs.String("state-dir", "", "state directory (default: $AI_USAGE_STATE_DIR or the per-OS config dir)")
	)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: ai-usage-agent %s [flags]\n\n", cmd)
		if cmd == "install" {
			fmt.Fprint(fs.Output(), "Write config.json and register the OS service.\n\nflags:\n")
			fs.PrintDefaults()
			return
		}
		fmt.Fprintf(fs.Output(), "%s the %s OS service.\n\nflags:\n", cmd, serviceName)
		fs.PrintDefaults()
	}
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return usagef("%s takes no positional arguments (got %q)", cmd, fs.Arg(0))
	}
	dir := resolveStateDir(*stateDir)

	if cmd == "install" {
		return install(dir, *server, *token, *name, *interval)
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
	fmt.Printf("%s: %s ok\n", serviceName, cmd)
	return nil
}

func install(dir, server, token, name string, interval int) error {
	cfg, _, err := agentcfg.Load(dir)
	if err != nil {
		return err
	}
	if server != "" {
		cfg.ServerURL = server
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
	if err := agentcfg.Save(dir, cfg); err != nil {
		return err
	}
	syncDeviceID(dir, &cfg, func(format string, args ...any) { fmt.Printf(format+"\n", args...) })
	// Never print the token, not even partially.
	fmt.Printf("wrote %s (device %s, name %q, interval %ds)\n",
		agentcfg.ConfigPath(dir), cfg.DeviceID, cfg.DeviceName, cfg.Interval())

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
	fmt.Printf("registered the %s service (%s)\n", serviceName, svc.Platform())

	if err := svc.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not start the service: %v\n", err)
		fmt.Printf("start it later with: %s start\n", exeName())
		return nil
	}
	fmt.Println("service started")
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
