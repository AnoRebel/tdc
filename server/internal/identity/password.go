// Package identity implements accounts, credentials and sessions.
package identity

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/alexedwards/argon2id"
)

// passwordParams are the argon2id cost parameters.
//
// These follow the OWASP recommendation for argon2id: 19 MiB of memory, two
// iterations and one degree of parallelism. Memory-hardness is the point — it
// is what makes large-scale offline cracking expensive after a database
// disclosure. The values are deliberately not configurable: an operator
// lowering them would weaken every password on the server, and the cost is
// affordable even on a small VPS.
var passwordParams = &argon2id.Params{
	Memory:      19 * 1024, // KiB
	Iterations:  2,
	Parallelism: 1,
	SaltLength:  16,
	KeyLength:   32,
}

// Password length bounds.
//
// The minimum follows current guidance, which favours length over composition
// rules: forcing a symbol and a digit produces predictable substitutions
// rather than stronger passwords. The maximum exists only to bound the work a
// single request can demand of the server.
const (
	MinPasswordLength = 12
	MaxPasswordLength = 256
)

// ErrWeakPassword reports a password rejected as too weak to accept.
var ErrWeakPassword = errors.New("password is too weak")

// HashPassword returns an argon2id hash of the password, having first checked
// that it is acceptable.
//
// The encoded hash carries its own parameters and salt, so raising the cost
// later does not invalidate existing hashes.
func HashPassword(password string) (string, error) {
	if err := CheckPasswordStrength(password); err != nil {
		return "", err
	}

	hash, err := argon2id.CreateHash(password, passwordParams)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return hash, nil
}

// HashPasscode hashes a short shared secret, such as a call link's passcode,
// the way a password is hashed but without a password's strength rules: it
// guards one link, its guessing is limited where it is used, and the people
// sharing it have to be able to say it aloud.
func HashPasscode(passcode string) (string, error) {
	hash, err := argon2id.CreateHash(passcode, passwordParams)
	if err != nil {
		return "", fmt.Errorf("hash passcode: %w", err)
	}
	return hash, nil
}

// VerifyPassword reports whether the password matches the encoded hash.
//
// It always performs the full comparison, so the time taken does not reveal
// where a mismatch occurred.
func VerifyPassword(password, encodedHash string) (bool, error) {
	match, err := argon2id.ComparePasswordAndHash(password, encodedHash)
	if err != nil {
		return false, fmt.Errorf("verify password: %w", err)
	}
	return match, nil
}

// CheckPasswordStrength reports whether a password may be accepted.
//
// It rejects passwords that are too short, too long, or obviously guessable.
// It deliberately imposes no composition rules.
func CheckPasswordStrength(password string) error {
	if len(password) < MinPasswordLength {
		return fmt.Errorf("%w: must be at least %d characters", ErrWeakPassword, MinPasswordLength)
	}
	if len(password) > MaxPasswordLength {
		return fmt.Errorf("%w: must be at most %d characters", ErrWeakPassword, MaxPasswordLength)
	}

	folded := strings.ToLower(strings.TrimSpace(password))

	if commonPasswords[folded] {
		return fmt.Errorf("%w: this password is among the most commonly used", ErrWeakPassword)
	}
	if isSingleRepeatedCharacter(password) {
		return fmt.Errorf("%w: must not be a single repeated character", ErrWeakPassword)
	}
	if isSequential(folded) {
		return fmt.Errorf("%w: must not be a simple sequence", ErrWeakPassword)
	}

	return nil
}

func isSingleRepeatedCharacter(password string) bool {
	runes := []rune(password)
	if len(runes) == 0 {
		return false
	}
	for _, r := range runes[1:] {
		if r != runes[0] {
			return false
		}
	}
	return true
}

// isSequential reports whether the password is one run of consecutive
// characters, such as "abcdefghijkl" or "123456789012".
func isSequential(password string) bool {
	runes := []rune(password)
	if len(runes) < 2 {
		return false
	}

	ascending, descending := true, true
	for i := 1; i < len(runes); i++ {
		if !unicode.IsLetter(runes[i]) && !unicode.IsDigit(runes[i]) {
			return false
		}
		if runes[i] != runes[i-1]+1 {
			ascending = false
		}
		if runes[i] != runes[i-1]-1 {
			descending = false
		}
	}
	return ascending || descending
}

// commonPasswords are rejected outright.
//
// This is a deliberately small list covering the passwords that appear at the
// top of every breach corpus. A server wanting comprehensive breach checking
// should use a dedicated service; requiring one here would make an external
// dependency mandatory, which the project's constraints forbid.
var commonPasswords = map[string]bool{
	"password":      true,
	"password1":     true,
	"password123":   true,
	"passw0rd":      true,
	"qwertyuiop":    true,
	"qwerty123456":  true,
	"111111111111":  true,
	"123456789012":  true,
	"1234567890":    true,
	"letmein12345":  true,
	"welcome12345":  true,
	"admin1234567":  true,
	"administrator": true,
	"iloveyou1234":  true,
	"monkey123456":  true,
	"dragon123456":  true,
	"football1234":  true,
	"baseball1234":  true,
	"sunshine1234":  true,
	"princess1234":  true,
	"changeme1234":  true,
	"secret123456":  true,
	"tanzania1234":  true,
}
