// This file holds the service lifecycle: registering the agent with the
// platform's service manager (systemd, launchd, the Windows SCM), running under
// it, and reporting its state.

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

	"github.com/kardianos/service"
)

// serviceName is the systemd unit / launchd label / Windows service name.
const serviceName = "ai-usage-agent"

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
	// setup builds the agent (see bootstrap). It runs inside Start's goroutine so
	// the service manager gets its "running" signal immediately.
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
	closeLog := setupLogFile(dir)
	defer closeLog()

	prg := &program{setup: func(context.Context) (*agent, error) { return bootstrap(dir, sel, ov) }}
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

// setupLogFile sends the standard logger to <dir>/agent.log (appending) and,
// best effort, to stdout for service managers that capture it (systemd/launchd).
// Writes to a dead stdout are ignored so they cannot break file logging.
func setupLogFile(dir string) (closeFn func()) {
	_ = os.MkdirAll(dir, 0o700)
	f, err := openLog(filepath.Join(dir, "agent.log"))
	if err != nil {
		log.SetOutput(oneLine{os.Stderr})
		log.Printf("warn: not logging to a file: %v", err)
		return func() {}
	}
	// Tee to stdout only when it is something other than a plain file: systemd and
	// launchd capture it, but `nohup agent run >> agent.log` would log twice.
	if st, err := os.Stdout.Stat(); err == nil && st.Mode().IsRegular() {
		log.SetOutput(oneLine{f})
	} else {
		log.SetOutput(oneLine{io.MultiWriter(f, ignoreErrors{os.Stdout})})
	}
	return func() { _ = f.Close() }
}

// openLog opens path for appending, refusing anything but a regular file: a
// symlink or device planted there would make the service write wherever it
// points, with the service's privileges.
func openLog(path string) (*os.File, error) {
	if fi, err := os.Lstat(path); err == nil && !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY|noFollow, 0o600)
}

// oneLine keeps every log entry on one line. Entries quote file paths and
// server replies, which anyone who can name a file in a log folder controls;
// an embedded newline would let them forge whole lines (a crontab entry, a
// fake "ok") in a log that other tools trust. The log package writes each
// entry in one call, ending in its own newline, which is kept.
type oneLine struct{ w io.Writer }

func (o oneLine) Write(p []byte) (int, error) {
	body, nl := p, false
	if n := len(body); n > 0 && body[n-1] == '\n' {
		body, nl = body[:n-1], true
	}
	if !needsEscape(body) {
		return o.w.Write(p)
	}
	var b strings.Builder
	for _, r := range string(body) {
		switch {
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x20 && r != '\t' || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		default:
			b.WriteRune(r)
		}
	}
	if nl {
		b.WriteByte('\n')
	}
	if _, err := io.WriteString(o.w, b.String()); err != nil {
		return 0, err
	}
	return len(p), nil
}

func needsEscape(b []byte) bool {
	for _, c := range b {
		if c < 0x20 && c != '\t' || c == 0x7f {
			return true
		}
	}
	return false
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

// runningService returns the service handle only when one is installed and
// actually running. Every way a platform can say "nothing installed here" -
// no service manager at all, ErrNotInstalled, an unreadable status - means the
// agent is not being managed, and rescan just clears the files.
func runningService(dir string) (service.Service, bool) {
	svc, err := newService(dir)
	if err != nil {
		return nil, false
	}
	st, err := svc.Status()
	if err != nil {
		return svc, false
	}
	return svc, st == service.StatusRunning
}
