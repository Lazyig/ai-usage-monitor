package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"ai-usage-monitor/internal/agentcfg"
	"ai-usage-monitor/internal/collector"
	"ai-usage-monitor/internal/event"
)

const rootsUsage = `usage: ai-usage-agent roots list   [flags]
       ai-usage-agent roots add    [flags] PATH...
       ai-usage-agent roots remove [flags] PATH...

Show or change the log folders a source reads. For claude_code, PATH is a
Claude Code config directory (what CLAUDE_CONFIG_DIR points at); its projects
folder is what gets read. ~ and environment variables are expanded now, as you,
and the path is stored absolute. Quote a glob such as '~/.claude-*' to store
the pattern itself: it is expanded on every pass, so folders created later are
read too. A running service is restarted to pick the change up.

flags:
`

// cmdRoots implements `roots list|add|remove`. It edits config.json as stored
// on disk: values that only come from the environment are never written back.
func cmdRoots(args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		fmt.Fprint(os.Stderr, rootsUsage)
		return usagef("roots needs a command: list, add or remove")
	}
	action := args[0]
	switch action {
	case "list", "add", "remove":
	default:
		return usagef("unknown roots command %q (want list, add or remove)", action)
	}

	fs := newFlagSet("roots " + action)
	var (
		source    = fs.String("source", "", "source to show or change (default: claude_code; list shows every multi-folder source)")
		stateDir  = fs.String("state-dir", "", "state directory (default: $AI_USAGE_STATE_DIR or the per-OS config dir)")
		noRestart = fs.Bool("no-restart", false, "do not restart a running service after a change")
	)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), rootsUsage)
		fs.PrintDefaults()
	}
	if err := parseFlags(fs, args[1:]); err != nil {
		return err
	}

	var sources []event.Source
	if *source != "" {
		s := event.Source(*source)
		if _, ok := registry[s]; !ok {
			return usagef("unknown source %q (available: %s)", s, strings.Join(sourceNames(), ", "))
		}
		if !multiRoot(s) {
			return usagef("%s reads a single folder; change sources.%s.root in %s instead", s, s, agentcfg.ConfigPath(resolveStateDir(*stateDir)))
		}
		sources = []event.Source{s}
	}
	dir := resolveStateDir(*stateDir)

	if action == "list" {
		if fs.NArg() > 0 {
			return usagef("roots list takes no paths (got %q)", fs.Arg(0))
		}
		if sources == nil {
			for _, s := range known() {
				if multiRoot(s) {
					sources = append(sources, s)
				}
			}
		}
		cfg, _, err := agentcfg.LoadFile(dir)
		if err != nil {
			return err
		}
		for i, s := range sources {
			if i > 0 {
				fmt.Fprintln(stdout)
			}
			printRoots(stdout, s, *cfg.Source(s))
		}
		if hint := unconfiguredClaudeDirs(cfg); len(hint) > 0 {
			fmt.Fprintln(stdout)
			printClaudeEnvHint(stdout, hint)
		}
		return nil
	}

	if fs.NArg() == 0 {
		return usagef("roots %s needs at least one path", action)
	}
	s := event.SourceClaudeCode
	if sources != nil {
		s = sources[0]
	}
	cfg, _, err := agentcfg.LoadFile(dir)
	if err != nil {
		return err
	}
	sc := cfg.Source(s)
	changed := false
	for _, arg := range fs.Args() {
		if strings.HasPrefix(arg, "-") {
			return usagef("flags go before the paths (got %q after a path)", arg)
		}
	}
	for _, arg := range fs.Args() {
		root, err := agentcfg.NormalizeRoot(s, arg)
		if err != nil {
			return usagef("%v", err)
		}
		if action == "add" {
			wasEnabled := sc.Enabled
			if !sc.AddRoot(root) {
				fmt.Fprintf(os.Stderr, "skip %s: already configured\n", root)
				continue
			}
			if !wasEnabled {
				fmt.Fprintf(os.Stderr, "note: %s was disabled; it is enabled again with only the folders added now\n", s)
			}
			for _, w := range rootWarnings(s, root) {
				fmt.Fprintf(os.Stderr, "warning: %s: %s\n", root, w)
			}
			fmt.Fprintf(stdout, "added %s\n", root)
		} else {
			if !sc.RemoveRoot(root) {
				fmt.Fprintf(os.Stderr, "skip %s: not configured\n", root)
				continue
			}
			fmt.Fprintf(stdout, "removed %s\n", root)
			if !sc.Enabled {
				fmt.Fprintf(stdout, "%s has no folders left and is now disabled\n", s)
			}
		}
		changed = true
	}
	if !changed {
		return errors.New("nothing changed")
	}
	if err := agentcfg.Save(dir, cfg); err != nil {
		return err
	}
	fmt.Fprintln(stdout)
	printRoots(stdout, s, *sc)
	applyConfigChange(dir, *noRestart)
	return nil
}

// applyConfigChange restarts a running service so it reads the new config;
// collectors are built once at startup.
func applyConfigChange(dir string, noRestart bool) {
	svc, running := runningService(dir)
	switch {
	case running && noRestart:
		fmt.Fprintf(stdout, "takes effect after: %s restart\n", exeName())
	case running:
		if err := svc.Restart(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not restart the service: %v\n", err)
			fmt.Fprintf(stdout, "takes effect after: %s restart\n", exeName())
			return
		}
		fmt.Fprintln(stdout, "service restarted")
	default:
		fmt.Fprintln(stdout, "takes effect the next time the agent starts")
	}
}

// printRoots shows a source's configured folders, what each pattern currently
// expands to and whether anything is there.
func printRoots(w io.Writer, s event.Source, sc agentcfg.SourceConfig) {
	state := ""
	if !sc.Enabled {
		state = " (disabled)"
	}
	fmt.Fprintf(w, "%s%s\n", s, state)
	for _, r := range usedRoots(s, sc) {
		if !collector.IsGlob(r) {
			fmt.Fprintf(w, "  %s  %s\n", r, describeDir(s, r))
			continue
		}
		dirs := collector.ExpandRoots([]string{r})
		fmt.Fprintf(w, "  %s  pattern, %d folder(s)\n", r, len(dirs))
		for _, d := range dirs {
			fmt.Fprintf(w, "    %s  %s\n", d, describeDir(s, d))
		}
	}
}

// describeDir is the short state of one folder for printRoots.
func describeDir(s event.Source, dir string) string {
	if !dirExists(dir) {
		return "missing"
	}
	n, err := countLogs(s, dir)
	if err != nil {
		return "unreadable (" + err.Error() + ")"
	}
	return fmt.Sprintf("ok, %d log file(s)", n)
}

// logExt is the file type a source's logs are stored in.
func logExt(s event.Source) string {
	if s == event.SourceAntigravity {
		return ".db"
	}
	return ".jsonl"
}

// countLogs counts the source's log files below dir.
func countLogs(s event.Source, dir string) (int, error) {
	if _, err := os.ReadDir(dir); err != nil {
		return 0, err
	}
	n := 0
	ext := logExt(s)
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.EqualFold(filepath.Ext(d.Name()), ext) {
			n++
		}
		return nil
	})
	return n, nil
}

// rootWarnings are the reasons a just added folder may not yield anything. They
// do not stop the change: Claude Code may simply not have run with that config
// directory yet.
func rootWarnings(s event.Source, root string) []string {
	if collector.IsGlob(root) {
		if len(collector.ExpandRoots([]string{root})) == 0 {
			return []string{"the pattern matches no folder yet"}
		}
		return nil
	}
	if !dirExists(root) {
		return []string{"folder does not exist (yet)"}
	}
	n, err := countLogs(s, root)
	switch {
	case err != nil:
		return []string{"not readable by you: " + err.Error()}
	case n == 0:
		return []string{"no " + logExt(s) + " files in it yet"}
	}
	return nil
}

// unconfiguredClaudeDirs lists the $CLAUDE_CONFIG_DIR entries whose projects
// folder this config does not read, so a user who exported it in their shell
// learns the service will not see it.
func unconfiguredClaudeDirs(cfg agentcfg.Config) []string {
	sc := cfg.Source(event.SourceClaudeCode)
	if !sc.Enabled {
		return nil
	}
	read := collector.ExpandRoots(sc.Roots())
	var out []string
	for _, d := range agentcfg.ClaudeConfigDirsFromEnv() {
		root, err := agentcfg.NormalizeRoot(event.SourceClaudeCode, d)
		if err != nil {
			continue
		}
		covered := false
		for _, r := range read {
			if agentcfg.SameRoot(r, root) {
				covered = true
				break
			}
		}
		if !covered {
			out = append(out, d)
		}
	}
	return out
}

func printClaudeEnvHint(w io.Writer, dirs []string) {
	fmt.Fprintf(w, "note: %s is set, but the agent does not read %s\n", agentcfg.EnvClaudeConfigDir, strings.Join(dirs, ", "))
	fmt.Fprintf(w, "      (a service never sees your shell's environment). Add it with:\n")
	for _, d := range dirs {
		fmt.Fprintf(w, "        %s roots add %s\n", exeName(), shellQuote(d))
	}
}

// shellQuote quotes p for a POSIX shell (or PowerShell, whose single quotes
// work the same way) when it needs it.
func shellQuote(p string) string {
	if p != "" && !strings.ContainsAny(p, " \t'\"$`\\*?[]{}()&;|<>!#~") {
		return p
	}
	return "'" + strings.ReplaceAll(p, "'", `'\''`) + "'"
}

// stringList is a flag that may be given several times.
type stringList []string

func (l *stringList) String() string { return strings.Join(*l, ", ") }
func (l *stringList) Set(v string) error {
	*l = append(*l, v)
	return nil
}
