package attachments

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// ErrInfected means a scanner rejected a file.
//
// Distinct from a scanner failure, because the two mean different things to the
// person uploading: one says the file is a problem, the other says the server
// could not tell.
var ErrInfected = errors.New("attachments: the file was rejected by the malware scanner")

// ErrScannerUnavailable means the scanner could not be reached or did not
// answer in time.
var ErrScannerUnavailable = errors.New("attachments: the malware scanner is unavailable")

// Scanner inspects a file before it is made available.
type Scanner interface {
	// Scan reports whether a file is acceptable. It returns ErrInfected for a
	// rejection and ErrScannerUnavailable when it could not decide.
	Scan(ctx context.Context, content io.Reader, size int64) error
}

// ScannerConfig is how an operator configures scanning.
type ScannerConfig struct {
	// Address is the scanner's host and port. Empty disables scanning
	// entirely, which is the default: this is optional, and a server with no
	// scanner configured must work without one.
	Address string

	// Timeout bounds a scan. A scanner that hangs must not hold an upload
	// open indefinitely.
	Timeout time.Duration

	/*
		FailOpen accepts a file when the scanner cannot be reached.

		Off by default, and deliberately awkward to turn on. An operator who
		configures a scanner has said uploads should be inspected; silently
		accepting them when the scanner is down would mean the protection
		disappears exactly when something is wrong — which is the moment it is
		most likely to matter. Turning this on is a legitimate choice for a
		small community that would rather stay usable, but it should be a
		choice, not a default nobody noticed.
	*/
	FailOpen bool
}

// DefaultScannerConfig is scanning switched off.
func DefaultScannerConfig() ScannerConfig {
	return ScannerConfig{
		Address:  "",
		Timeout:  30 * time.Second,
		FailOpen: false,
	}
}

/*
ClamAVScanner talks to clamd over its INSTREAM protocol.

ClamAV because it is the only malware scanner that is free, open source, and
self-hostable — which the zero-cost constraint requires. The protocol is simple
enough to speak directly: a command, length-prefixed chunks, a zero-length
terminator, and one line of reply. A library for this would be a dependency
carrying more than is needed.
*/
type ClamAVScanner struct {
	address string
	timeout time.Duration
}

// NewClamAVScanner connects to clamd at the given address on each scan.
//
// No pooling: clamd closes the connection after every INSTREAM, so a pool would
// hold sockets that are already gone.
func NewClamAVScanner(address string, timeout time.Duration) *ClamAVScanner {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &ClamAVScanner{address: address, timeout: timeout}
}

// chunkSize is how much is sent per INSTREAM frame.
//
// clamd's default StreamMaxLength rejects frames above 25 MB; this is well
// inside that and large enough that a big file is not thousands of frames.
const chunkSize = 64 << 10

func (c *ClamAVScanner) Scan(ctx context.Context, content io.Reader, size int64) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	var dialer net.Dialer
	connection, err := dialer.DialContext(ctx, "tcp", c.address)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrScannerUnavailable, err)
	}
	defer connection.Close()

	// The deadline covers the whole exchange, so a scanner that accepts the
	// connection and then stops responding is still bounded.
	if deadline, ok := ctx.Deadline(); ok {
		_ = connection.SetDeadline(deadline)
	}

	if _, err := connection.Write([]byte("zINSTREAM\x00")); err != nil {
		return fmt.Errorf("%w: %w", ErrScannerUnavailable, err)
	}

	buffer := make([]byte, chunkSize)
	for {
		read, readErr := content.Read(buffer)
		if read > 0 {
			// Each frame is a four-byte big-endian length then the bytes.
			header := []byte{
				byte(read >> 24), byte(read >> 16), byte(read >> 8), byte(read),
			}
			if _, err := connection.Write(header); err != nil {
				return fmt.Errorf("%w: %w", ErrScannerUnavailable, err)
			}
			if _, err := connection.Write(buffer[:read]); err != nil {
				return fmt.Errorf("%w: %w", ErrScannerUnavailable, err)
			}
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("attachments: reading the file to scan: %w", readErr)
		}
	}

	// A zero-length frame ends the stream.
	if _, err := connection.Write([]byte{0, 0, 0, 0}); err != nil {
		return fmt.Errorf("%w: %w", ErrScannerUnavailable, err)
	}

	reply := make([]byte, 512)
	read, err := connection.Read(reply)
	// errors.Is rather than ==: a wrapped EOF is a complete reply, and treating
	// it as a failure would refuse an upload the scanner actually approved.
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: %w", ErrScannerUnavailable, err)
	}

	return interpretClamAVReply(string(reply[:read]))
}

// interpretClamAVReply turns clamd's one-line answer into a decision.
//
// "stream: OK" is clean, "stream: <name> FOUND" is a rejection, and anything
// else — including "ERROR" and a truncated reply — is treated as the scanner
// being unable to decide, which fails closed by default.
func interpretClamAVReply(reply string) error {
	switch {
	case reply == "":
		return fmt.Errorf("%w: the scanner returned nothing", ErrScannerUnavailable)
	case strings.Contains(reply, "FOUND"):
		return ErrInfected
	case strings.Contains(reply, "OK"):
		return nil
	default:
		return fmt.Errorf("%w: the scanner said %q", ErrScannerUnavailable, trimReply(reply))
	}
}

// trimReply keeps a scanner's message short enough for a log line.
func trimReply(reply string) string {
	cleaned := make([]rune, 0, 80)
	for _, r := range reply {
		if r == '\n' || r == '\r' || r == 0 {
			continue
		}
		cleaned = append(cleaned, r)
		if len(cleaned) == 80 {
			break
		}
	}
	return string(cleaned)
}

/*
scan applies the operator's scanning policy to a spooled upload.

Returns nil when the file may be stored. The fail-open decision lives here
rather than in the Scanner, so that a scanner implementation only has to report
what it found and the policy is in one readable place.
*/
func (s *Service) scan(ctx context.Context, content io.ReadSeeker, size int64) error {
	if s.scanner == nil {
		// No scanner configured, which is the default and a perfectly ordinary
		// way to run this server.
		return nil
	}

	if _, err := content.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("attachments: rewinding to scan: %w", err)
	}

	err := s.scanner.Scan(ctx, content, size)
	switch {
	case err == nil:
		return nil

	case errors.Is(err, ErrInfected):
		// A rejection is always a rejection, whatever the fail-open setting
		// says: that setting is about not being able to decide, not about
		// ignoring an answer.
		return err

	case errors.Is(err, ErrScannerUnavailable):
		if s.scannerFailOpen {
			// The operator has chosen availability over inspection. Logged so
			// that a scanner which has been down for a week is visible rather
			// than merely quiet.
			s.log.Warn("accepting an upload without scanning it", "error", err)
			return nil
		}
		return err

	default:
		return err
	}
}
