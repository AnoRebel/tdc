package identity_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/AnoRebel/tdc/server/internal/identity"
)

const goodPassword = "correct horse battery staple"

func TestHashAndVerifyRoundTrip(t *testing.T) {
	hash, err := identity.HashPassword(goodPassword)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	match, err := identity.VerifyPassword(goodPassword, hash)
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if !match {
		t.Error("the correct password did not verify")
	}
}

func TestVerifyRejectsWrongPassword(t *testing.T) {
	hash, err := identity.HashPassword(goodPassword)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	match, err := identity.VerifyPassword("not the password at all", hash)
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if match {
		t.Error("an incorrect password verified")
	}
}

// The stored hash must never contain the password, so a database disclosure
// does not hand over credentials.
func TestHashDoesNotContainPassword(t *testing.T) {
	hash, err := identity.HashPassword(goodPassword)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if strings.Contains(hash, goodPassword) {
		t.Fatal("the encoded hash contains the password")
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Errorf("hash is not argon2id: %q", hash)
	}
}

// Two hashes of one password must differ, or a database disclosure would
// reveal which accounts share a password.
func TestHashesAreSalted(t *testing.T) {
	first, err := identity.HashPassword(goodPassword)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	second, err := identity.HashPassword(goodPassword)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if first == second {
		t.Fatal("two hashes of the same password are identical, so they are unsalted")
	}
}

func TestWeakPasswordsAreRejected(t *testing.T) {
	cases := map[string]string{
		"too short":           "short",
		"commonly used":       "password123",
		"repeated character":  "aaaaaaaaaaaaaa",
		"ascending sequence":  "abcdefghijklm",
		"descending sequence": "zyxwvutsrqpon",
	}

	for name, password := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := identity.HashPassword(password); !errors.Is(err, identity.ErrWeakPassword) {
				t.Errorf("HashPassword(%q) error = %v, want ErrWeakPassword", password, err)
			}
		})
	}
}

// Composition rules are deliberately not imposed: a long passphrase of only
// lowercase letters is stronger than a short one with a symbol.
func TestPassphraseWithoutSymbolsIsAccepted(t *testing.T) {
	if err := identity.CheckPasswordStrength("the quick brown fox jumps"); err != nil {
		t.Errorf("a long lowercase passphrase was rejected: %v", err)
	}
}

// An over-long password must be refused rather than hashed, to bound the work
// one request can demand.
func TestOverlongPasswordIsRejected(t *testing.T) {
	if err := identity.CheckPasswordStrength(strings.Repeat("a", identity.MaxPasswordLength+1)); err == nil {
		t.Error("an over-long password was accepted")
	}
}
