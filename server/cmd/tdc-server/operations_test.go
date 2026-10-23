package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AnoRebel/tdc/server/internal/config"
)

/*
The check a restart runs refuses a configuration the next process could not
start with, and names what is wrong, so the restart is refused rather than
taking the server down.
*/
func TestCheckRefusesAnInvalidConfiguration(t *testing.T) {
	t.Setenv("TDC_DATABASE_URL", "postgres://nobody@127.0.0.1:1/none")
	path := filepath.Join(t.TempDir(), "tdc.conf")
	if err := os.WriteFile(path, []byte("storage.backend = floppy\nlog.level = loud\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := check(path)
	if err == nil {
		t.Fatal("an invalid configuration passed the check")
	}
	for _, named := range []string{"storage.backend", "log.level"} {
		if !strings.Contains(err.Error(), named) {
			t.Errorf("the check did not name %s: %v", named, err)
		}
	}
}

// A database the next process cannot reach refuses the restart too.
func TestCheckRefusesAnUnreachableDatabase(t *testing.T) {
	t.Setenv("TDC_DATABASE_URL", "postgres://nobody@127.0.0.1:1/none?connect_timeout=1")
	t.Setenv("TDC_DATABASE_CONNECT_TIMEOUT", "1s")
	if err := check(""); err == nil || !strings.Contains(err.Error(), "database") {
		t.Errorf("an unreachable database: %v", err)
	}
}

// Restarting for a supervisor that is not there would only stop the server.
func TestRestartMethods(t *testing.T) {
	t.Setenv("INVOCATION_ID", "")
	t.Setenv("NOTIFY_SOCKET", "")

	plan := func(method string) (string, string, bool) {
		c := &controller{current: config.Default()}
		c.current.Server.RestartMethod = method
		p := c.describe()
		return p.Method, p.Refusal, p.Recovers
	}

	if method, refusal, _ := plan("auto"); method != "exec" || refusal != "" {
		t.Errorf("auto without a supervisor: %q %q", method, refusal)
	}
	if _, refusal, _ := plan("off"); refusal == "" {
		t.Error("off was not refused")
	}
	if method, _, recovers := plan("supervisor"); method != "supervisor" || !recovers {
		t.Errorf("a declared supervisor: %q recovers=%v", method, recovers)
	}

	t.Setenv("INVOCATION_ID", "systemd-unit")
	if method, _, recovers := plan("auto"); method != "supervisor" || !recovers {
		t.Errorf("auto under systemd: %q recovers=%v", method, recovers)
	}
}
