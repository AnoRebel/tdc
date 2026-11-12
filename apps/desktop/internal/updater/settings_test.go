package updater

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

/*
When this looks for a newer version.

Checking says something about a machine to a third party — that TDC is
installed, and which version — so how often is somebody's choice rather than
this application's assumption.
*/

/*
The defaults are quiet, and never install anything.

Somebody who has not opened these settings has not agreed to have their
application replaced while they were not looking.
*/
func TestTheDefaultsInstallNothing(t *testing.T) {
	defaults := Defaults()

	if defaults.Download {
		t.Error("the defaults fetch an update without being asked")
	}
	if !defaults.OnLaunch {
		t.Error("the defaults never check, so nobody hears about a security fix")
	}
	if defaults.Every != Daily {
		t.Errorf("the default interval is %q", defaults.Every)
	}
}

func TestAnIntervalThisDoesNotUnderstandIsRefused(t *testing.T) {
	/*
		Several values rather than one, because the failure worth catching is
		the accepted set quietly widening — a single example passes even when
		everything else is admitted alongside it.
	*/
	for _, every := range []Interval{
		"hourly-ish", "anything", "", "DAILY", "1h", "always", "0",
	} {
		settings := Defaults()
		settings.Every = every

		if err := settings.Valid(); err == nil {
			t.Errorf("%q was accepted as how often to check", every)
		}
	}

	// And the three that are real still are, or this would pass by refusing
	// everything.
	for _, every := range []Interval{NeverCheck, Daily, Weekly} {
		settings := Defaults()
		settings.Every = every

		if err := settings.Valid(); err != nil {
			t.Errorf("%q is a real interval and was refused: %v", every, err)
		}
	}
}

/* Never means never, not "very rarely". */
func TestNeverMeansNoScheduledCheck(t *testing.T) {
	settings := Settings{Every: NeverCheck, LastChecked: time.Time{}}

	if settings.Due(time.Now()) {
		t.Error("a schedule of never came due")
	}
}

func TestACheckComesDueAfterItsInterval(t *testing.T) {
	now := time.Now()

	daily := Settings{Every: Daily, LastChecked: now.Add(-25 * time.Hour)}
	if !daily.Due(now) {
		t.Error("a daily check did not come due after twenty-five hours")
	}

	recent := Settings{Every: Daily, LastChecked: now.Add(-time.Hour)}
	if recent.Due(now) {
		t.Error("a daily check came due after an hour")
	}

	weekly := Settings{Every: Weekly, LastChecked: now.Add(-3 * 24 * time.Hour)}
	if weekly.Due(now) {
		t.Error("a weekly check came due after three days")
	}
}

/*
Never having checked is due, whatever the interval.

Otherwise a fresh installation would wait a day before its first check, which
is a day of not knowing about a fix that already exists.
*/
func TestAFreshInstallationIsDueImmediately(t *testing.T) {
	if !(Settings{Every: Daily}).Due(time.Now()) {
		t.Error("an installation that has never checked was not due")
	}
}

func TestSettingsSurviveARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "updates.json")

	store := OpenStore(path)
	if err := store.Save(Settings{OnLaunch: false, Every: Weekly, Download: true}); err != nil {
		t.Fatalf("saving: %v", err)
	}

	// A second store, as the next launch would build.
	reopened := OpenStore(path)
	settings := reopened.Settings()

	if settings.OnLaunch || settings.Every != Weekly || !settings.Download {
		t.Errorf("what came back is %+v", settings)
	}
}

/*
An unreadable file resets rather than refusing to start.

Losing somebody's preferences is a small harm; losing the updater because a
file was truncated is a larger one.
*/
func TestAnUnreadableFileFallsBackToTheDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "updates.json")
	if err := os.WriteFile(path, []byte("{ this is not json"), 0o644); err != nil {
		t.Fatalf("writing a broken file: %v", err)
	}

	if store := OpenStore(path); store.Settings() != Defaults() {
		t.Errorf("a broken file gave %+v", store.Settings())
	}
}

/*
And a file holding an interval this does not understand.

Written by an older version, or by hand. Reading it would make "hourly-ish"
the setting everything else consults.
*/
func TestAFileWithAnUnknownIntervalFallsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "updates.json")
	if err := os.WriteFile(path, []byte(`{"onLaunch":true,"every":"hourly-ish"}`), 0o644); err != nil {
		t.Fatalf("writing: %v", err)
	}

	if store := OpenStore(path); store.Settings().Every != Daily {
		t.Errorf("an unknown interval survived as %q", store.Settings().Every)
	}
}

func TestSavingRefusesSettingsItCannotUse(t *testing.T) {
	store := OpenStore(filepath.Join(t.TempDir(), "updates.json"))

	if err := store.Save(Settings{Every: "constantly"}); err == nil {
		t.Error("an interval nobody defined was saved")
	}
	if store.Settings().Every != Daily {
		t.Error("a refused save changed the settings anyway")
	}
}

/*
When the last check happened is this package's to keep.

Somebody sending settings should not be able to say they checked five minutes
ago and thereby stop it checking.
*/
func TestTheInterfaceCannotRewriteWhenItLastChecked(t *testing.T) {
	store := OpenStore(filepath.Join(t.TempDir(), "updates.json"))

	realCheck := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
	store.MarkChecked(realCheck)

	// Settings arriving from the interface, claiming a check that never was.
	_ = store.Save(Settings{Every: Daily, LastChecked: time.Now()})

	if got := store.Settings().LastChecked; !got.Equal(realCheck) {
		t.Errorf("when it last checked became %s, want %s", got, realCheck)
	}
}
