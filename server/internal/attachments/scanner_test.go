package attachments_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/AnoRebel/tdc/server/internal/attachments"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeClamd answers INSTREAM requests with a fixed reply, so the protocol and
// the policy can be tested without a real scanner.
func fakeClamd(t *testing.T, reply string) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}
	t.Cleanup(func() { listener.Close() })

	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}

			go func(c net.Conn) {
				defer c.Close()

				// Drain until the zero-length terminator, as clamd does.
				buffer := make([]byte, 4096)
				for {
					if _, err := c.Read(buffer); err != nil {
						break
					}
					// The reply is sent as soon as anything arrives; a real
					// clamd waits for the terminator, and the difference does
					// not matter for what is under test here.
					break
				}

				c.Write([]byte(reply))
			}(connection)
		}
	}()

	return listener.Addr().String()
}

// TestScannerAcceptsCleanFile covers the ordinary path.
func TestScannerAcceptsCleanFile(t *testing.T) {
	scanner := attachments.NewClamAVScanner(fakeClamd(t, "stream: OK\x00"), 5*time.Second)

	if err := scanner.Scan(context.Background(), strings.NewReader("harmless"), 8); err != nil {
		t.Errorf("a clean file was rejected: %v", err)
	}
}

// TestScannerRejectsInfectedFile checks that a detection is reported as one.
func TestScannerRejectsInfectedFile(t *testing.T) {
	scanner := attachments.NewClamAVScanner(
		fakeClamd(t, "stream: Eicar-Test-Signature FOUND\x00"), 5*time.Second)

	err := scanner.Scan(context.Background(), strings.NewReader("x"), 1)
	if !errors.Is(err, attachments.ErrInfected) {
		t.Errorf("gave %v, expected ErrInfected", err)
	}
}

// TestUnreachableScannerIsDistinguishable checks that "cannot decide" is not
// reported as "the file is bad".
//
// The two mean different things to the person uploading, and the fail-open
// policy turns on exactly this distinction.
func TestUnreachableScannerIsDistinguishable(t *testing.T) {
	// A port nothing is listening on.
	scanner := attachments.NewClamAVScanner("127.0.0.1:1", time.Second)

	err := scanner.Scan(context.Background(), strings.NewReader("x"), 1)
	if !errors.Is(err, attachments.ErrScannerUnavailable) {
		t.Errorf("gave %v, expected ErrScannerUnavailable", err)
	}
	if errors.Is(err, attachments.ErrInfected) {
		t.Error("an unreachable scanner was reported as a detection")
	}
}

// TestUnexpectedReplyFailsClosed checks that a reply the server does not
// understand is treated as "cannot decide" rather than as "clean".
//
// Reading an unknown answer as success is how a scanner silently stops
// protecting anything.
func TestUnexpectedReplyFailsClosed(t *testing.T) {
	for _, reply := range []string{
		"stream: ERROR\x00",
		"INSTREAM size limit exceeded\x00",
		"\x00",
		"",
	} {
		scanner := attachments.NewClamAVScanner(fakeClamd(t, reply), 3*time.Second)

		err := scanner.Scan(context.Background(), strings.NewReader("x"), 1)
		if err == nil {
			t.Errorf("the reply %q was treated as clean", reply)
		}
	}
}

// TestScannerRespectsTimeout checks that a scanner which accepts a connection
// and then says nothing cannot hold an upload open.
func TestScannerRespectsTimeout(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}
	defer listener.Close()

	go func() {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		// Accepted and then ignored, which is the worst case for a caller
		// without a deadline.
		defer connection.Close()
		time.Sleep(10 * time.Second)
	}()

	scanner := attachments.NewClamAVScanner(listener.Addr().String(), 500*time.Millisecond)

	started := time.Now()
	err = scanner.Scan(context.Background(), strings.NewReader("x"), 1)
	elapsed := time.Since(started)

	if err == nil {
		t.Fatal("a silent scanner was treated as clean")
	}
	if elapsed > 3*time.Second {
		t.Errorf("the scan took %v; the timeout did not apply", elapsed)
	}
}
