package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/kardianos/service"

	"ai-usage-monitor/internal/agentcfg"
)

// serviceName is the systemd unit / launchd label / Windows service name.
const serviceName = "ai-usage-agent"

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

// runArgs is the argv the service (or the operator) needs to start this agent.
// The state directory is passed explicitly whenever it is not the plain per-OS
// default, so the service does not depend on the installing shell's environment.
// runArgs always pins the state directory: a service runs under another account
// (Windows LocalSystem, systemd system unit) whose default state dir is not the
// installing user's, and without the flag the service would find no config.
func runArgs(dir string) []string {
	return []string{"run", "--state-dir", dir}
}

// serviceConfig describes the unit to register.
func serviceConfig(dir string) *service.Config {
	cfg := &service.Config{
		Name:        serviceName,
		DisplayName: "AI Usage Monitor agent",
		Description: "Collects local AI CLI token usage and reports it to the AI Usage Monitor server.",
		Arguments:   runArgs(dir),
		Option:      service.KeyValue{},
	}
	// Without root there is no system-wide unit to write, so fall back to a
	// per-user service (systemd --user, ~/Library/LaunchAgents). Windows has no
	// such split and always needs an elevated install.
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		cfg.Option["UserService"] = true
	}
	if runtime.GOOS == "linux" {
		cfg.Dependencies = []string{"After=network-online.target"}
	}
	return cfg
}

func newService(dir string) (service.Service, error) {
	return service.New(&program{}, serviceConfig(dir))
}

// program adapts the run loop to the service manager lifecycle. It matters most
// on Windows, where a service process must answer the SCM; on systemd and
// launchd the same code simply runs in the foreground.
type program struct {
	// setup builds the agent (config, whoami, collectors). It runs inside Start's
	// goroutine so the service manager gets its "running" signal immediately.
	setup  func(ctx context.Context) (*agent, error)
	syslog service.Logger

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

func (p *program) Start(service.Service) error {
	ctx, cancel := context.WithCancel(context.Background())
	p.mu.Lock()
	p.cancel = cancel
	p.done = make(chan struct{})
	p.mu.Unlock()

	go func() {
		defer close(p.done)
		if p.setup == nil {
			return
		}
		ag, err := p.setup(ctx)
		if err != nil {
			p.err = err
			log.Printf("fatal: %v", err)
			if p.syslog != nil {
				_ = p.syslog.Errorf("ai-usage-agent: %v", err)
			}
			// A service that cannot start must fail visibly rather than idle.
			os.Exit(1)
		}
		p.err = ag.loop(ctx)
	}()
	return nil
}

func (p *program) Stop(service.Service) error {
	p.mu.Lock()
	cancel, done := p.cancel, p.done
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	return p.err
}

// runAsService runs under the service manager: reach svc.Run() first, then do
// the slow setup (config, whoami, collectors) inside Start. Logs go to
// <state dir>/agent.log because a service has no usable stdout (Windows) or
// the operator wants a file next to the state anyway.
func runAsService(dir string, sel selected, ov runOverrides) error {
	closeLog := setupServiceLog(dir)
	defer closeLog()

	prg := &program{setup: func(ctx context.Context) (*agent, error) {
		cfg, err := loadRunConfig(dir, ov)
		if err != nil {
			return nil, err
		}
		syncDeviceID(dir, &cfg, log.Printf)
		return newAgent(cfg, dir, sel)
	}}
	svc, err := service.New(prg, serviceConfig(dir))
	if err != nil {
		if errors.Is(err, service.ErrNoServiceSystemDetected) {
			ctx, stop := signalContext()
			defer stop()
			ag, err := prg.setup(ctx)
			if err != nil {
				return err
			}
			return ag.loop(ctx)
		}
		return err
	}
	if lg, err := svc.Logger(nil); err == nil {
		prg.syslog = lg
	}
	if err := svc.Run(); err != nil {
		return err
	}
	return prg.err
}

// setupServiceLog sends the standard logger to <dir>/agent.log (appending) and,
// best effort, to stdout for service managers that capture it (systemd/launchd).
// Writes to a dead stdout are ignored so they cannot break file logging.
func setupServiceLog(dir string) (closeFn func()) {
	_ = os.MkdirAll(dir, 0o700)
	f, err := os.OpenFile(filepath.Join(dir, "agent.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return func() {}
	}
	log.SetOutput(io.MultiWriter(f, ignoreErrors{os.Stdout}))
	return func() { _ = f.Close() }
}

// ignoreErrors makes a writer that never fails (a service's stdout may be closed).
type ignoreErrors struct{ w io.Writer }

func (i ignoreErrors) Write(p []byte) (int, error) { _, _ = i.w.Write(p); return len(p), nil }

// serviceStatus renders the service state for `status`, tolerating every way a
// platform can say "there is nothing installed here".
func serviceStatus(dir string) string {
	svc, err := newService(dir)
	if err != nil {
		return "unavailable (" + err.Error() + ")"
	}
	st, err := svc.Status()
	switch {
	case errors.Is(err, service.ErrNotInstalled):
		return "not installed"
	case err != nil:
		return "unknown (" + err.Error() + ")"
	}
	switch st {
	case service.StatusRunning:
		return "running"
	case service.StatusStopped:
		return "stopped"
	default:
		return "unknown"
	}
}
