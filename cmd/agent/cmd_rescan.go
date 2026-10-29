package main

import (
	"fmt"

	"ai-usage-monitor/internal/agentcfg"
)

// cmdRescan throws away this device's collection progress so the next pass
// reads every log from the beginning.
//
// It is the local half of the server's `DELETE /api/v1/devices/{id}/events`:
// normally the server asks for the resync itself and the running agent does
// this on its own, and this command is for the times it cannot be asked - a
// device whose data looks wrong, a token that was rotated while the agent was
// offline. Re-sending is safe because an EventID does not depend on the device
// or on which run produced it, so the server drops whatever it already has.
//
// The offline queue goes with the cursors: its batches only hold events that
// the very next scan produces again.
func cmdRescan(args []string) error {
	flags := newFlagSet("rescan")
	stateDir := flags.String("state-dir", "", stateDirUsage)
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "usage: ai-usage-agent rescan [flags]\n\n"+
			"Forget every collector cursor and the offline queue, so the agent re-reads\n"+
			"all local logs from the beginning. A running service is stopped and started\n"+
			"again around it.\n\nflags:\n")
		flags.PrintDefaults()
	}
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	if err := noArgs(flags, "rescan"); err != nil {
		return err
	}
	dir := resolveStateDir(*stateDir)

	// Stop first: a running agent holds the cursors in memory and would write
	// them straight back over the deleted file at the end of its next tick.
	svc, running := runningService(dir)
	if running {
		if err := svc.Stop(); err != nil {
			return fmt.Errorf("stop %s: %w", serviceName, err)
		}
		fmt.Fprintf(stdout, "stopped the %s service\n", serviceName)
	}

	if err := clearProgress(dir); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "cleared %s and %s\n", agentcfg.CursorPath(dir), agentcfg.QueueDir(dir))

	if running {
		if err := svc.Start(); err != nil {
			return fmt.Errorf("start %s: %w", serviceName, err)
		}
		fmt.Fprintf(stdout, "started the %s service again; it is rescanning from the beginning\n", serviceName)
		return nil
	}
	fmt.Fprintln(stdout, "the next `run` will scan every log from the beginning")
	return nil
}
