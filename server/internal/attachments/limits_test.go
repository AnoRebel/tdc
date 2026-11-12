package attachments_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/AnoRebel/tdc/server/internal/attachments"
)

// TestSizeLimitNamesWhatWasExceeded checks that a refusal is actionable.
//
// "Too large" leaves someone guessing whether to compress the file or send
// fewer of them; the limit and the actual value say what to do.
func TestSizeLimitNamesWhatWasExceeded(t *testing.T) {
	limits := attachments.Limits{MaxFileSize: 1000}

	if err := limits.CheckSize(1000); err != nil {
		t.Errorf("a file exactly at the limit was refused: %v", err)
	}

	err := limits.CheckSize(1001)
	if err == nil {
		t.Fatal("a file over the limit was accepted")
	}

	var limitErr *attachments.LimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("the error is %T, not a LimitError a caller can act on", err)
	}
	if limitErr.Allowed != 1000 || limitErr.Actual != 1001 {
		t.Errorf("the error reports allowed=%d actual=%d, expected 1000 and 1001",
			limitErr.Allowed, limitErr.Actual)
	}
	if !strings.Contains(limitErr.Error(), "file size") {
		t.Errorf("the message does not say which limit: %q", limitErr.Error())
	}
}

// TestCountAndTotalLimits covers the two message-wide limits.
func TestCountAndTotalLimits(t *testing.T) {
	limits := attachments.Limits{MaxPerMessage: 3, MaxMessageSize: 500}

	if err := limits.CheckCount(3); err != nil {
		t.Errorf("exactly the permitted count was refused: %v", err)
	}
	if err := limits.CheckCount(4); err == nil {
		t.Error("one attachment over the limit was accepted")
	}

	if err := limits.CheckTotal(500); err != nil {
		t.Errorf("a message exactly at the total limit was refused: %v", err)
	}

	err := limits.CheckTotal(501)
	if err == nil {
		t.Fatal("a message over the total limit was accepted")
	}
	if !strings.Contains(err.Error(), "total message size") {
		t.Errorf("the message does not name the total limit: %q", err.Error())
	}
}

// TestUnsetLimitsAllowAnything checks that a zero means "no limit" rather than
// "nothing is permitted", which would refuse every upload on a server whose
// operator left a setting blank.
func TestUnsetLimitsAllowAnything(t *testing.T) {
	var none attachments.Limits

	if err := none.CheckSize(1 << 40); err != nil {
		t.Errorf("an unset size limit refused a file: %v", err)
	}
	if err := none.CheckCount(1000); err != nil {
		t.Errorf("an unset count limit refused a message: %v", err)
	}
	if err := none.CheckTotal(1 << 40); err != nil {
		t.Errorf("an unset total limit refused a message: %v", err)
	}
	if err := none.CheckType("image/png"); err != nil {
		t.Errorf("an unset type list refused a type: %v", err)
	}
}

// TestAllowedTypes covers exact names and group wildcards.
func TestAllowedTypes(t *testing.T) {
	limits := attachments.Limits{
		AllowedTypes: []string{"image/*", "application/pdf"},
	}

	for _, permitted := range []string{
		"image/png",
		"image/jpeg",
		"image/webp",
		"application/pdf",
		// Parameters are not part of the decision.
		"image/png; charset=binary",
		// Neither is case.
		"IMAGE/PNG",
	} {
		if err := limits.CheckType(permitted); err != nil {
			t.Errorf("%q was refused by an allowlist that covers it: %v", permitted, err)
		}
	}

	for _, refused := range []string{
		"video/mp4",
		"text/html",
		"application/zip",
		"application/octet-stream",
	} {
		if err := limits.CheckType(refused); err == nil {
			t.Errorf("%q was accepted although the allowlist does not cover it", refused)
		}
	}
}

// TestBlockedTypesWinOverAllowed checks that an operator who opens a whole
// group can still carve pieces out of it.
func TestBlockedTypesWinOverAllowed(t *testing.T) {
	limits := attachments.Limits{
		AllowedTypes: []string{"*/*"},
		BlockedTypes: []string{"application/x-executable", "image/svg+xml"},
	}

	if err := limits.CheckType("image/png"); err != nil {
		t.Errorf("an ordinary image was refused: %v", err)
	}

	for _, blocked := range []string{"application/x-executable", "image/svg+xml"} {
		err := limits.CheckType(blocked)
		if err == nil {
			t.Errorf("%q was accepted although it is blocked", blocked)
			continue
		}
		if !strings.Contains(err.Error(), blocked) {
			t.Errorf("the refusal does not name the type: %q", err.Error())
		}
	}
}

// TestDefaultsRefuseExecutables checks the out-of-the-box behaviour.
//
// An operator who has configured nothing should not be hosting binaries that a
// browser or operating system may act on. They can allow them, but it should be
// a decision.
func TestDefaultsRefuseExecutables(t *testing.T) {
	limits := attachments.DefaultLimits()

	for _, blocked := range []string{
		"application/x-executable",
		"application/vnd.microsoft.portable-executable",
		"application/x-mach-binary",
	} {
		if err := limits.CheckType(blocked); err == nil {
			t.Errorf("the defaults accept %q", blocked)
		}
	}

	// The ordinary cases still work, or the defaults are useless.
	for _, permitted := range []string{
		"image/png", "image/jpeg", "application/pdf",
		"text/plain", "video/mp4", "application/zip",
	} {
		if err := limits.CheckType(permitted); err != nil {
			t.Errorf("the defaults refuse %q: %v", permitted, err)
		}
	}
}
