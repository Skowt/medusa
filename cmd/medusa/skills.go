package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"github.com/Skowt/medusa/internal/config"
	"github.com/Skowt/medusa/internal/skillstats"
)

// skillsCommandName is the subcommand that serves or prints skill-usage stats.
// The same dashboard is reachable from the TUI's [U] toolbar button; this exists
// for headless use — a scan from a script, or a report without a browser.
const skillsCommandName = "skills"

// runSkillsCommand implements `medusa skills`. It owns its own FlagSet so the
// subcommand's flags stay independent of the TUI's argument handling.
func runSkillsCommand(args []string) error {
	fs := flag.NewFlagSet("medusa "+skillsCommandName, flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:7788", "listen address for the dashboard")
	scanOnly := fs.Bool("scan", false, "scan transcripts, print a summary, and exit")
	report := fs.Bool("report", false, "print a text report to stdout and exit")
	gran := fs.String("gran", "day", "report granularity: hour, day, or week")
	profile := fs.String("profile", "", "limit the report to one Medusa profile")
	noClaude := fs.Bool("no-claude-dir", false, "ignore ~/.claude/projects (non-Medusa sessions)")
	fs.Usage = func() {
		// fs.Output() is a plain io.Writer, so unlike the os.Stderr calls below
		// errcheck does not treat the error as safe to drop implicitly.
		_, _ = fmt.Fprintf(fs.Output(), "Usage: medusa %s [flags]\n\n"+
			"Reports which Claude Code skills were invoked, grouped by plugin.\n"+
			"With no flags, serves the dashboard until interrupted.\n\nFlags:\n", skillsCommandName)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	paths, err := config.DefaultPaths()
	if err != nil {
		return fmt.Errorf("resolve medusa paths: %w", err)
	}
	claudeProjects := ""
	if !*noClaude {
		if home, err := os.UserHomeDir(); err == nil {
			claudeProjects = filepath.Join(home, ".claude", "projects")
		}
	}

	storeDir := filepath.Join(paths.Home, "skill-usage")
	store, err := skillstats.Open(storeDir, paths.ProfilesRoot, claudeProjects)
	if err != nil {
		return err
	}

	result := store.Scan()
	switch {
	case *scanOnly:
		printSkillScan(storeDir, result, store)
		return nil
	case *report:
		printSkillReport(store, *gran, *profile)
		return nil
	}

	fmt.Printf("scanned %d transcripts (%d read) in %s, %d new invocations, %d total\n",
		result.FilesConsidered, result.FilesRead, result.Duration.Round(time.Millisecond),
		result.NewEvents, len(store.Events()))
	return serveSkills(*addr, store)
}

// serveSkills starts the dashboard and blocks until interrupted.
func serveSkills(addr string, store *skillstats.Store) error {
	srv := &http.Server{
		Handler:           skillstats.NewServer(store).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	fmt.Printf("dashboard: http://%s\n", ln.Addr().String())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

// printSkillScan summarizes a scan for the -scan flag.
func printSkillScan(storeDir string, r skillstats.ScanResult, store *skillstats.Store) {
	fmt.Printf("store:       %s\n", storeDir)
	fmt.Printf("transcripts: %d considered, %d read\n", r.FilesConsidered, r.FilesRead)
	fmt.Printf("events:      +%d new, -%d pruned (older than %d months), %d total\n",
		r.NewEvents, r.PrunedEvents, skillstats.RetentionMonths, len(store.Events()))
	fmt.Printf("duration:    %s\n", r.Duration.Round(time.Millisecond))
	for _, e := range r.Errors {
		fmt.Fprintln(os.Stderr, "warn:", e)
	}
	if profiles := store.Profiles(); len(profiles) > 0 {
		fmt.Printf("profiles:    %v\n", profiles)
	}
}

// printSkillReport renders the same aggregation the dashboard shows, as text,
// for a quick look without a browser.
func printSkillReport(store *skillstats.Store, gran, profile string) {
	g := skillstats.ParseGranularity(gran)
	stats := skillstats.Compute(store.Events(), skillstats.Query{Gran: g, Profile: profile})

	scope := "all profiles"
	if profile != "" {
		scope = profile
	}
	fmt.Printf("%s buckets · %s → %s · %s\n", g,
		stats.From.Format("2006-01-02 15:04"), stats.To.Format("2006-01-02 15:04"), scope)
	fmt.Printf("%d invocations (%d by subagents)\n\n", stats.Total, stats.Subagent)

	for _, p := range stats.Plugins {
		fmt.Printf("%-34s %5d   last %s\n", p.Plugin, p.Total, p.Last.Format("Jan 02 15:04"))
		for _, sk := range p.Skills {
			fmt.Printf("  %-32s %5d   last %s\n", sk.Skill, sk.Total, sk.Last.Format("Jan 02 15:04"))
		}
	}

	busiest := append([]skillstats.Bucket(nil), stats.Buckets...)
	sort.Slice(busiest, func(i, j int) bool { return busiest[i].Total > busiest[j].Total })
	if len(busiest) > 0 && busiest[0].Total > 0 {
		fmt.Printf("\nbusiest %s: %s (%d)\n", g, busiest[0].Label, busiest[0].Total)
	}
}
