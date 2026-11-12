package mail

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/AnoRebel/tdc/server/internal/config"
)

/*
What goes on the wire.

Composing a message is the part of SMTP that looks trivial and is not: a line
that happens to begin with a full stop ends the message early, and a subject
carrying anything but ASCII has to be encoded or it arrives as mojibake. Both
are invisible until somebody's name has an accent in it.
*/

func testService() *Service {
	return NewService(config.MailConfig{
		Host: "mail.example.test",
		From: "noreply@example.test",
	}, "Test Server", slog.New(slog.NewTextHandler(io.Discard, nil)))
}

/*
TestALineOfOneFullStopDoesNotEndTheMessage.

In SMTP a line containing only "." terminates the data. A body with one would be
truncated there, and everything after it read as commands — so it is doubled,
which the receiving server undoes.
*/
func TestALineOfOneFullStopDoesNotEndTheMessage(t *testing.T) {
	composed := testService().compose("somebody@example.test", Message{
		Subject: "Anything",
		Body:    "before\n.\nafter",
	})

	_, body, found := strings.Cut(composed, "\r\n\r\n")
	if !found {
		t.Fatal("no blank line between the headers and the body")
	}

	if strings.Contains(body, "\r\n.\r\n") {
		t.Error("a line of one full stop would end the message early")
	}
	if !strings.Contains(body, "\r\n..\r\n") {
		t.Errorf("the full stop was not doubled:\n%q", body)
	}
	// And the rest is still there.
	if !strings.Contains(body, "after") {
		t.Error("everything after the full stop was lost")
	}
}

// TestEveryLineEndsWithCRLF, which is what the protocol requires and what a
// body written with plain newlines does not do.
func TestEveryLineEndsWithCRLF(t *testing.T) {
	composed := testService().compose("somebody@example.test", Message{
		Subject: "Anything",
		Body:    "one\ntwo\nthree",
	})

	// A newline not preceded by a carriage return is a bare one.
	for index, character := range composed {
		if character == '\n' && (index == 0 || composed[index-1] != '\r') {
			t.Fatalf("a bare newline at %d:\n%q", index, composed)
		}
	}
}

/*
TestASubjectIsEncoded.

A subject is ASCII on the wire. Anything else — a name with an accent, a
Kiswahili word, an em dash — has to be encoded, or it arrives as the wrong
characters in whichever client guesses differently.
*/
func TestASubjectIsEncoded(t *testing.T) {
	composed := testService().compose("somebody@example.test", Message{
		Subject: "Rudisha nenosiri lako — Fatuma",
		Body:    "anything",
	})

	subject := headerOf(t, composed, "Subject")
	for _, character := range subject {
		if character > 127 {
			t.Errorf("the subject went out unencoded: %q", subject)
			break
		}
	}
	if !strings.Contains(subject, "=?utf-8?") {
		t.Errorf("the subject is not encoded at all: %q", subject)
	}
}

// TestTheSenderCarriesAName, so a message arrives from something recognisable
// rather than a bare address.
func TestTheSenderCarriesAName(t *testing.T) {
	from := headerOf(t, testService().compose("somebody@example.test", Message{
		Subject: "Anything", Body: "anything",
	}), "From")

	if !strings.Contains(from, "noreply@example.test") {
		t.Errorf("From = %q, want it to carry the configured address", from)
	}
	if !strings.Contains(from, "Test Server") {
		t.Errorf("From = %q, want it to name the server", from)
	}
}

/*
TestAMessageAsksNotToBeRepliedToAutomatically.

A recovery mail reaching an out-of-office responder produces a reply to this
server's own address. Enough of them produce a mailbox nobody reads, full of
mail nobody sent.
*/
func TestAMessageAsksNotToBeRepliedToAutomatically(t *testing.T) {
	composed := testService().compose("somebody@example.test", Message{
		Subject: "Anything", Body: "anything",
	})

	if headerOf(t, composed, "Auto-Submitted") == "" {
		t.Error("nothing tells a responder not to reply")
	}
}

// TestMailIsOffWithNoHost: the default, and a working state.
func TestMailIsOffWithNoHost(t *testing.T) {
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))

	if NewService(config.MailConfig{}, "Test Server", quiet).Configured() {
		t.Error("a server with no mail host says it can send mail")
	}
}

// headerOf reads one header out of a composed message.
func headerOf(t *testing.T, composed, name string) string {
	t.Helper()

	headers, _, _ := strings.Cut(composed, "\r\n\r\n")
	for _, line := range strings.Split(headers, "\r\n") {
		if after, found := strings.CutPrefix(line, name+": "); found {
			return after
		}
	}
	return ""
}
