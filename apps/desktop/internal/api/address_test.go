package api

import (
	"errors"
	"strings"
	"testing"
)

/*
Which servers this client will talk to.

The rule is that a server reached over plain HTTP must be on this machine.
Everything below is either that rule or the ways somebody might express an
address that gets around it.
*/

func TestAnEncryptedAddressIsAccepted(t *testing.T) {
	for _, typed := range []string{
		"https://tdc.or.tz",
		"https://tdc.or.tz/",
		"HTTPS://TDC.OR.TZ",
		"https://tdc.or.tz:8443",
	} {
		if _, err := ParseAddress(typed); err != nil {
			t.Errorf("%q was refused: %v", typed, err)
		}
	}
}

/*
A bare host is read as HTTPS.

Somebody typing the name of their server has not mistyped it by leaving off
the scheme, and the safe reading is the one that is right nearly always.
*/
func TestABareHostBecomesEncrypted(t *testing.T) {
	address, err := ParseAddress("tdc.or.tz")
	if err != nil {
		t.Fatalf("a bare host was refused: %v", err)
	}

	if address.URL != "https://tdc.or.tz" {
		t.Errorf("a bare host became %q", address.URL)
	}
}

/*
The refusal this whole file exists for.

Plain HTTP to somewhere that is not this machine sends the session cookie and
every message in the clear, where anybody on the same network can read them.
*/
func TestAnUnencryptedRemoteAddressIsRefused(t *testing.T) {
	for _, typed := range []string{
		"http://tdc.or.tz",
		"http://192.168.1.10:8080",
		"http://10.0.0.5",
		"http://[2001:db8::1]:8080",
	} {
		_, err := ParseAddress(typed)

		if err == nil {
			t.Errorf("%q was accepted; a password would travel in the clear", typed)
			continue
		}
		if !errors.Is(err, ErrInsecure) {
			t.Errorf("%q was refused for the wrong reason: %v", typed, err)
		}
	}
}

/*
And the explanation, which is the difference between a dead end and a next
step.

Somebody meeting this is usually running their own server and has not yet
arranged a certificate, so the message has to name both ways forward rather
than only saying no.
*/
func TestTheRefusalSaysWhatToDoInstead(t *testing.T) {
	_, err := ParseAddress("http://tdc.or.tz")
	if err == nil {
		t.Fatal("an unencrypted remote address was accepted")
	}

	message := err.Error()
	for _, expected := range []string{"https://", "this machine"} {
		if !strings.Contains(message, expected) {
			t.Errorf("the refusal does not mention %q: %s", expected, message)
		}
	}
}

/*
Loopback over plain HTTP, which is the exemption.

There is no wire for anybody to listen on, and refusing it would make running a
server on your own machine impossible without a certificate — which is what
somebody trying this out does first.
*/
func TestAnUnencryptedLoopbackAddressIsAccepted(t *testing.T) {
	for _, typed := range []string{
		"http://localhost:8080",
		"http://127.0.0.1:8080",
		"http://[::1]:8080",
		"http://tdc.localhost:8080",
	} {
		address, err := ParseAddress(typed)
		if err != nil {
			t.Errorf("%q was refused: %v", typed, err)
			continue
		}
		if !address.Loopback {
			t.Errorf("%q was not recognised as this machine", typed)
		}
	}
}

/*
An IPv4 address written as IPv6 is still this machine.

::ffff:127.0.0.1 is loopback, and reading it as anything else would refuse a
local server written in a form somebody's tooling produced.

This does not distinguish unmapping from not: Go's IsLoopback unmaps
internally, so both readings agree here. It is a check that the form is
accepted at all, which is what somebody meeting it cares about.

The mapping in the other direction — a remote address dressed as IPv6 — is
covered by the refusal test, which includes an IPv6 literal.
*/
func TestAMappedLoopbackAddressIsRecognised(t *testing.T) {
	address, err := ParseAddress("http://[::ffff:127.0.0.1]:8080")
	if err != nil {
		t.Fatalf("a mapped loopback address was refused: %v", err)
	}

	if !address.Loopback {
		t.Error("a mapped loopback address was not recognised as this machine")
	}
}

/*
A remote address written as mapped IPv6 is still remote.

The direction that matters for safety: if a mapped form were read as loopback
by its shape rather than its value, ::ffff:203.0.113.5 would look local and
plain HTTP to it would be allowed.
*/
func TestAMappedRemoteAddressIsStillRefused(t *testing.T) {
	if _, err := ParseAddress("http://[::ffff:203.0.113.5]:8080"); err == nil {
		t.Error("a mapped remote address was accepted over plain HTTP")
	}
}

/*
A name that is not a literal is treated as remote.

Resolving it would make this depend on DNS at the moment somebody types an
address, and a name resolving to loopback today may not tomorrow — so anything
but the conventional names is judged remote, which is the safe reading.
*/
func TestANameIsNotAssumedLocal(t *testing.T) {
	if _, err := ParseAddress("http://localhost.attacker.example"); err == nil {
		t.Error("a name merely beginning with localhost was treated as this machine")
	}
}

/*
Only the origin is kept.

A path would be appended to every request this client builds, producing
addresses nobody meant; credentials in the URL would be stored with the server
rather than in the keychain.
*/
func TestOnlyTheOriginIsKept(t *testing.T) {
	address, err := ParseAddress("https://tdc.or.tz/some/path?token=secret#fragment")
	if err != nil {
		t.Fatalf("an address with a path was refused: %v", err)
	}

	if address.URL != "https://tdc.or.tz" {
		t.Errorf("the origin is %q; a path or query survived", address.URL)
	}
}

func TestCredentialsInTheAddressAreDiscarded(t *testing.T) {
	address, err := ParseAddress("https://someone:hunter2@tdc.or.tz")
	if err != nil {
		t.Fatalf("an address with credentials was refused: %v", err)
	}

	if strings.Contains(address.URL, "hunter2") || strings.Contains(address.URL, "someone") {
		t.Errorf("credentials were kept in the address: %q", address.URL)
	}
}

// Something that is not an address at all, refused in words rather than by
// failing later as an opaque connection error.
func TestSomethingThatIsNotAnAddressIsRefused(t *testing.T) {
	for _, typed := range []string{"", "   ", "ftp://tdc.or.tz", "file:///etc/passwd", "https://"} {
		if _, err := ParseAddress(typed); err == nil {
			t.Errorf("%q was accepted as a server address", typed)
		}
	}
}
