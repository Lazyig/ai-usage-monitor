package main

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"

	"ai-usage-monitor/internal/agentcfg"
	"ai-usage-monitor/internal/cursor"
	"ai-usage-monitor/internal/transport"
)

// cmdStatus reports what this device is configured to do and how far it got.
// It is read-only: it never creates a config or advances a cursor.
func cmdStatus(args []string) error {
	flags := newFlagSet("status")
	var (
		stateDir = flags.String("state-dir", "", stateDirUsage)
		timeout  = flags.Duration("timeout", 5*time.Second, "server health check timeout")
	)
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "usage: ai-usage-agent status [flags]\n\nShow the configuration, cursors, queue and server health.\n\nflags:\n")
		flags.PrintDefaults()
	}
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	if err := noArgs(flags, "status"); err != nil {
		return err
	}

	dir := resolveStateDir(*stateDir)
	cfg, existed, err := agentcfg.Load(dir)
	if err != nil {
		return stateAccessError(dir, err)
	}
	out := stdout

	fmt.Fprintf(out, "agent      %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(out, "state dir  %s\n", dir)
	if !existed {
		fmt.Fprintf(out, "config     %s (missing - showing defaults; run `install` to create it)\n", agentcfg.ConfigPath(dir))
	} else {
		fmt.Fprintf(out, "config     %s\n", agentcfg.ConfigPath(dir))
	}
	fmt.Fprintf(out, "service    %s\n\n", serviceStatus(dir))

	red := cfg.Redacted()
	fmt.Fprintf(out, "server     %s\n", orNone(red.ServerURL))
	if agentcfg.CheckServerURL(red.ServerURL, false) != nil {
		fmt.Fprintln(out, "           warning: plain http to another machine, the device token is sent unencrypted; reinstall with an https:// server")
	}
	fmt.Fprintf(out, "token      %s\n", orNone(red.DeviceToken))
	fmt.Fprintf(out, "device     %s  %q\n", red.DeviceID, red.DeviceName)
	fmt.Fprintf(out, "interval   %ds\n\n", red.Interval())

	// Sources and their cursors.
	cur, err := cursor.Open(agentcfg.CursorPath(dir))
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprint(tw, "SOURCE\tENABLED\tROOT\tPRESENT\tCURSOR\n")
	more := false
	for _, s := range agentcfg.Sources {
		sc := cfg.Source(s)
		enabled := "no"
		if sc.Enabled {
			enabled = "yes"
		}
		if _, compiled := registry[s]; !compiled {
			enabled = "no (collector not in this build)"
		}
		roots := usedRoots(s, *sc)
		present := "no"
		if anyDirExists(roots) {
			present = "yes"
		}
		state := "none (will scan from the beginning)"
		if n := cur.Size(s); n > 0 {
			state = fmt.Sprintf("%s bytes of state", num(int64(n)))
		}
		shown := roots[0]
		if len(roots) > 1 {
			shown += fmt.Sprintf(" (+%d more)", len(roots)-1)
			more = true
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", s, enabled, shown, present, state)
	}
	tw.Flush()
	if hint := unconfiguredClaudeDirs(cfg); len(hint) > 0 {
		printClaudeEnvHint(out, hint)
	}
	if more {
		fmt.Fprintf(out, "every folder: %s roots list\n", exeName())
	}
	fmt.Fprintln(out)

	// Offline queue.
	q, err := transport.OpenQueue(agentcfg.QueueDir(dir), transport.DefaultQueueMaxBytes)
	if err != nil {
		return err
	}
	n, bytes, err := q.Stats()
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "queue      %s in %d batch(es), %s bytes pending (cap %s bytes)\n",
		q.Dir(), n, num(bytes), num(transport.DefaultQueueMaxBytes))

	// Server health.
	if strings.TrimSpace(cfg.ServerURL) == "" {
		fmt.Fprintln(out, "health     no server configured")
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	client := &transport.Client{
		BaseURL:    cfg.ServerURL,
		HTTPClient: &http.Client{Timeout: *timeout},
		DeviceInfo: transport.Device{AgentVersion: version},
	}
	h, err := client.Health(ctx)
	if err != nil {
		fmt.Fprintf(out, "health     unreachable: %v\n", err)
		return nil
	}
	fmt.Fprintf(out, "health     ok=%v version=%s events=%s\n", h.OK, orNone(h.Version), num(h.Events))
	return nil
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(not set)"
	}
	return s
}
