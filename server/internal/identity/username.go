package identity

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// Username length bounds. The minimum keeps addresses meaningful; the maximum
// keeps them displayable and bounds index size.
const (
	MinUsernameLength = 2
	MaxUsernameLength = 32
)

// ErrInvalidUsername reports a username that cannot be accepted.
var ErrInvalidUsername = errors.New("invalid username")

// reservedUsernames cannot be registered.
//
// These would either impersonate the server itself or collide with routes and
// addresses the platform needs. Reserving them now costs nothing; reclaiming
// one later would mean renaming a real account.
var reservedUsernames = map[string]bool{
	"admin": true, "administrator": true, "root": true, "system": true,
	"support": true, "help": true, "security": true, "abuse": true,
	"postmaster": true, "webmaster": true, "hostmaster": true,
	"tdc": true, "server": true, "api": true, "www": true,
	"everyone": true, "here": true, "channel": true, "all": true,
	"me": true, "self": true, "anonymous": true, "deleted": true,
	"bot": true, "moderator": true, "mod": true, "staff": true,
}

// NormaliseUsername returns the comparison form of a username.
//
// Lower-casing is what makes "Ano" and "ano" the same account. It is applied
// consistently at registration, at sign-in and in rate limiting, so that
// varying case cannot be used to register a lookalike account or to sidestep
// a per-account limit.
func NormaliseUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

// ValidateUsername reports whether a username may be registered.
//
// The permitted set is deliberately narrow: letters, digits, underscore, dot
// and hyphen. A wider set invites homoglyph impersonation, and these
// identifiers have to survive being embedded in an address of the form
// user@server.
func ValidateUsername(username string) error {
	trimmed := strings.TrimSpace(username)

	if length := len([]rune(trimmed)); length < MinUsernameLength || length > MaxUsernameLength {
		return fmt.Errorf("%w: must be between %d and %d characters",
			ErrInvalidUsername, MinUsernameLength, MaxUsernameLength)
	}

	for _, r := range trimmed {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			unicode.IsDigit(r),
			r == '_', r == '.', r == '-':
		default:
			return fmt.Errorf("%w: may contain only letters, digits, underscore, dot and hyphen",
				ErrInvalidUsername)
		}
	}

	// A leading or trailing separator reads as a typo and makes addresses
	// ambiguous to parse.
	if strings.ContainsAny(trimmed[:1], "_.-") || strings.ContainsAny(trimmed[len(trimmed)-1:], "_.-") {
		return fmt.Errorf("%w: must start and end with a letter or digit", ErrInvalidUsername)
	}

	if reservedUsernames[NormaliseUsername(trimmed)] {
		return fmt.Errorf("%w: this name is reserved", ErrInvalidUsername)
	}

	return nil
}

// NormaliseEmail returns the comparison form of an email address.
//
// Only the domain is lower-cased along with the local part here; the local
// part is technically case-sensitive per RFC 5321, but treating it as
// case-insensitive is what every mail provider does in practice, and doing
// otherwise would let two accounts claim what users would read as one address.
func NormaliseEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// ValidateEmail performs the shallow check that is actually useful.
//
// Deep syntactic validation rejects addresses that work and accepts ones that
// do not; only sending mail proves an address is real, which is what
// verification is for.
func ValidateEmail(email string) error {
	trimmed := strings.TrimSpace(email)

	at := strings.LastIndex(trimmed, "@")
	if at <= 0 || at == len(trimmed)-1 {
		return errors.New("email address must contain a local part and a domain")
	}
	if strings.ContainsAny(trimmed, " \t\r\n") {
		return errors.New("email address must not contain whitespace")
	}
	if !strings.Contains(trimmed[at+1:], ".") {
		return errors.New("email domain must contain a dot")
	}
	if len(trimmed) > 254 {
		return errors.New("email address is too long")
	}

	return nil
}
