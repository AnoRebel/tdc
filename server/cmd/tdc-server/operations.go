package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
	"uuid"

	"github.com/AnoRebel/tdc/server/internal/config"
	"github.com/AnoRebel/tdc/server/internal/db"
	"github.com/AnoRebel/tdc/server/internal/httpapi"
	"github.com/AnoRebel/tdc/server/internal/settings"
)

/*
Reloading and restarting this process.

The configuration a process started with is plumbing, and most of it cannot
change without a new process: the database it holds a pool for, the address it
listens on. A few values are read where they are used and can change in place;
a reload applies those and names the rest. A restart replaces the process, and
checks first that its replacement would start.
*/

// restartExitCode is what a process exits with to be started again by its
// supervisor: EX_TEMPFAIL, "try again".
const restartExitCode = 75

// restartOperationVariable tells a replacement process which restart it
// completes.
const restartOperationVariable = "TDC_RESTART_OPERATION"

// hotKeys are the settings a reload applies in place.
var hotKeys = []string{"log.level", "calls.turn_secret"}

// controller holds what reload and restart change.
type controller struct {
	configPath string
	level      *slog.LevelVar
	log        *slog.Logger
	server     *httpapi.Server
	settings   *settings.Service
	stop       context.CancelFunc

	mu      sync.Mutex
	current config.Config
	restart uuid.UUID
}

func (c *controller) reload(ctx context.Context) (httpapi.ReloadReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	next, err := config.Load(c.configPath)
	if err != nil {
		var invalid *config.InvalidError
		if errors.As(err, &invalid) {
			report := httpapi.ReloadReport{}
			for _, problem := range invalid.Problems {
				report.Problems = append(report.Problems, problem.String())
			}
			return report, nil
		}
		return httpapi.ReloadReport{Problems: []string{err.Error()}}, nil
	}

	report := httpapi.ReloadReport{Applied: []string{}, NeedsRestart: []string{}}
	if next.Log.Level != c.current.Log.Level {
		c.level.Set(parseLevel(next.Log.Level))
		report.Applied = append(report.Applied, "log.level")
	}
	if next.Calls.TURNSecret != c.current.Calls.TURNSecret {
		report.Applied = append(report.Applied, "calls.turn_secret")
	}
	c.server.Reconfigure(next.Calls.TURNSecret, next.Describe(c.configPath), next.Warnings())

	if reloaded, err := c.server.ReloadWebShell(); err != nil {
		return report, fmt.Errorf("reading the web application again: %w", err)
	} else if reloaded {
		report.Applied = append(report.Applied, "web application")
	}
	if c.settings != nil {
		c.settings.Invalidate()
		report.Applied = append(report.Applied, "settings")
	}

	for _, key := range config.Changed(c.current, next) {
		if !slices.Contains(hotKeys, key) {
			report.NeedsRestart = append(report.NeedsRestart, key)
		}
	}
	c.current.Log.Level = next.Log.Level
	c.current.Calls.TURNSecret = next.Calls.TURNSecret
	return report, nil
}

// supervisor names what will start this process again, if anything visibly will.
func supervisor() string {
	if os.Getenv("INVOCATION_ID") != "" || os.Getenv("NOTIFY_SOCKET") != "" {
		return "systemd"
	}
	return ""
}

func inContainer() bool {
	_, err := os.Stat("/.dockerenv")
	return err == nil
}

/*
describe is how a restart would happen, from the configuration alone.

Under exec the process replaces itself and nothing outside is needed; if the
replacement then fails to start, only a supervisor brings it back, which the
plan says so the administrator reads it before confirming.
*/
func (c *controller) describe() httpapi.RestartPlan {
	method := c.current.Server.RestartMethod
	found := supervisor()

	switch method {
	case "off":
		return httpapi.RestartPlan{Refusal: "restarting from the interface is switched off (server.restart_method = off)"}
	case "supervisor":
		plan := httpapi.RestartPlan{Method: "supervisor", Supervisor: found, Recovers: true}
		if found == "" {
			plan.Supervisor = "the supervisor server.restart_method names"
		}
		return plan
	case "auto":
		if found != "" {
			return httpapi.RestartPlan{Method: "supervisor", Supervisor: found, Recovers: true}
		}
	}

	if runtime.GOOS == "windows" {
		return httpapi.RestartPlan{Refusal: "this platform cannot replace a running process; " +
			"run the server under a supervisor and set server.restart_method = supervisor"}
	}
	plan := httpapi.RestartPlan{Method: "exec", Supervisor: found, Recovers: found != ""}
	if !plan.Recovers && inContainer() {
		plan.Supervisor = "the container's restart policy, if it has one"
	}
	return plan
}

/*
plan checks a restart would succeed by asking the next process.

The same binary, arguments and environment, run with -check: it loads the
configuration, connects to the database and checks the schema is one it can
run against. Whatever it prints on failure is what the administrator reads.
*/
func (c *controller) plan(ctx context.Context) httpapi.RestartPlan {
	plan := c.describe()
	if plan.Refusal != "" {
		return plan
	}

	executable, err := os.Executable()
	if err != nil {
		plan.Refusal = "this process cannot find its own binary to restart with: " + err.Error()
		return plan
	}
	if _, err := os.Stat(executable); err != nil {
		plan.Refusal = "the binary this process was started from is gone (" + executable + ")"
		return plan
	}

	checkCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	command := exec.CommandContext(checkCtx, executable, append(withoutCheck(os.Args[1:]), "-check")...)
	command.Env = os.Environ()
	output, err := command.CombinedOutput()
	if err != nil {
		for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				plan.Problems = append(plan.Problems, line)
			}
		}
		if len(plan.Problems) == 0 {
			plan.Problems = []string{"the check of the next process failed: " + err.Error()}
		}
	}
	return plan
}

func withoutCheck(args []string) []string {
	var kept []string
	for _, arg := range args {
		if arg != "-check" && arg != "--check" {
			kept = append(kept, arg)
		}
	}
	return kept
}

// begin starts shutting down to come back.
func (c *controller) begin(operation uuid.UUID) {
	c.mu.Lock()
	c.restart = operation
	c.mu.Unlock()
	c.stop()
}

func (c *controller) restarting() uuid.UUID {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.restart
}

/*
comeBack replaces this process, or exits for its supervisor, once it has shut
down cleanly. Everything the old process held has been closed by then, and Go
opens every descriptor close-on-exec, so the replacement starts with nothing
but its arguments and environment.
*/
func comeBack(method string, operation uuid.UUID) error {
	if method == "supervisor" {
		fmt.Fprintf(os.Stderr, "tdc-server: restarting (operation %s); exiting for the supervisor\n", operation)
		os.Exit(restartExitCode)
	}

	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("restart: %w", err)
	}
	environment := []string{}
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, restartOperationVariable+"=") {
			environment = append(environment, value)
		}
	}
	environment = append(environment, restartOperationVariable+"="+operation.String())
	fmt.Fprintf(os.Stderr, "tdc-server: restarting (operation %s) in place\n", operation)
	return syscall.Exec(executable, os.Args, environment)
}

/*
check is what -check does: everything a start needs short of serving.

Nothing is changed — no migration is applied — so asking does no harm to the
server that asked.
*/
func check(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := db.Open(ctx, db.Config{
		URL: cfg.Database.URL, MaxConnections: 1, MinConnections: 1,
		ConnectTimeout: cfg.Database.ConnectTimeout, MaxConnLifetime: time.Minute, MaxConnIdleTime: time.Minute,
	}, slog.New(slog.DiscardHandler))
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer pool.Close()

	var version int64
	var dirty bool
	if err := pool.QueryRow(ctx, "SELECT version, dirty FROM schema_migrations LIMIT 1").Scan(&version, &dirty); err != nil {
		return fmt.Errorf("database: reading the schema version: %w", err)
	}
	if dirty {
		return fmt.Errorf("database: the schema is marked dirty at version %d; a migration failed part way", version)
	}
	if latest := db.LatestSchemaVersion(); uint(version) > latest {
		return fmt.Errorf("database: the schema is at version %d, newer than this binary understands (%d)", version, latest)
	}
	if uint(version) < db.LatestSchemaVersion() && !cfg.Database.AutoMigrate {
		return fmt.Errorf("database: the schema is at version %d and needs migrating, and database.auto_migrate is off", version)
	}
	return nil
}
