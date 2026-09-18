/*
Package mail sends the few messages this server has to send.

SMTP rather than a provider's API, because recovery has to work against a mail
server the operator runs themselves and every self-hosted one speaks SMTP. There
is no account to open and nothing to pay for.

Off by default. A server with no mail configured runs perfectly well; what it
loses is the ability to offer recovery, which it then does not offer rather than
offering and failing.
*/
package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/AnoRebel/tdc/server/internal/config"
)

// ErrNotConfigured reports a server with no mail server to send through.
// Callers treat it as "this cannot be offered" rather than as a failure.
var ErrNotConfigured = errors.New("this server does not send mail")

// Message is one message to one person.
type Message struct {
	To      string
	Subject string
	// Body is plain text. No HTML: a recovery mail is a sentence and a link,
	// HTML is where mail clients disagree with each other, and plain text is
	// what a spam filter is least suspicious of.
	Body string
}

// Sender delivers a message, or says it cannot.
//
// An interface so that a test can watch what would have been sent without a
// mail server, and so a caller can be written against "mail may be off" rather
// than against SMTP.
type Sender interface {
	Send(ctx context.Context, message Message) error
	// Configured says whether there is anywhere to send. Callers ask before
	// offering something that depends on mail.
	Configured() bool
}

// Service sends over SMTP.
type Service struct {
	settings config.MailConfig
	log      *slog.Logger
	// serverName is what appears beside the sender address when the operator
	// has not chosen something else.
	serverName string
}

// NewService builds a sender from the operator's configuration.
func NewService(settings config.MailConfig, serverName string, log *slog.Logger) *Service {
	return &Service{settings: settings, serverName: serverName, log: log}
}

// Configured reports whether mail can be sent at all.
func (s *Service) Configured() bool {
	return s != nil && strings.TrimSpace(s.settings.Host) != ""
}

/*
Send delivers one message.

Synchronous, deliberately. The caller decides whether to wait: recovery does not
— it answers immediately and sends in the background, because a response that
took as long as the mail server did would tell somebody whether their address
was known by how long it took.
*/
func (s *Service) Send(ctx context.Context, message Message) error {
	if !s.Configured() {
		return ErrNotConfigured
	}

	to := strings.TrimSpace(message.To)
	if to == "" || !strings.Contains(to, "@") {
		return fmt.Errorf("%q is not an address to send to", message.To)
	}

	address := net.JoinHostPort(s.settings.Host, strconv.Itoa(int(s.settings.Port)))

	client, err := s.connect(ctx, address)
	if err != nil {
		return err
	}
	defer func() {
		// Quit says goodbye properly; Close is the fallback for a connection
		// that is already unhappy.
		if err := client.Quit(); err != nil {
			_ = client.Close()
		}
	}()

	if err := s.authenticate(client); err != nil {
		return err
	}

	if err := client.Mail(s.settings.From); err != nil {
		return fmt.Errorf("the mail server refused the sender: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("the mail server refused the recipient: %w", err)
	}

	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("the mail server refused the message: %w", err)
	}
	if _, err := writer.Write([]byte(s.compose(to, message))); err != nil {
		return fmt.Errorf("writing the message: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("the mail server did not accept the message: %w", err)
	}

	return nil
}

// connect opens the connection, encrypted the way the operator configured.
func (s *Service) connect(ctx context.Context, address string) (*smtp.Client, error) {
	dialer := &net.Dialer{Timeout: s.settings.Timeout}

	if s.settings.TLS == "tls" {
		// TLS from the first byte, which is what port 465 does.
		conn, err := tls.DialWithDialer(dialer, "tcp", address,
			&tls.Config{ServerName: s.settings.Host, MinVersion: tls.VersionTLS12})
		if err != nil {
			return nil, fmt.Errorf("connecting to the mail server: %w", err)
		}
		client, err := smtp.NewClient(conn, s.settings.Host)
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("starting the session: %w", err)
		}
		return client, nil
	}

	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("connecting to the mail server: %w", err)
	}

	client, err := smtp.NewClient(conn, s.settings.Host)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("starting the session: %w", err)
	}

	if s.settings.TLS == "starttls" {
		/*
			Upgraded, and refused if it cannot be.

			A server that advertises no STARTTLS when the operator asked for it
			is either misconfigured or being impersonated, and continuing in the
			clear would send the password to whichever it is. The configuration
			refuses "none" off this machine for the same reason.
		*/
		if ok, _ := client.Extension("STARTTLS"); !ok {
			_ = client.Close()
			return nil, errors.New("the mail server does not offer STARTTLS, and mail.tls asks for it")
		}
		if err := client.StartTLS(&tls.Config{
			ServerName: s.settings.Host,
			MinVersion: tls.VersionTLS12,
		}); err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("encrypting the connection: %w", err)
		}
	}

	return client, nil
}

// authenticate signs in, where the server wants it.
func (s *Service) authenticate(client *smtp.Client) error {
	if s.settings.Username == "" {
		return nil
	}

	ok, _ := client.Extension("AUTH")
	if !ok {
		return errors.New("the mail server does not accept credentials, but a username is configured")
	}

	auth := smtp.PlainAuth("", s.settings.Username, s.settings.Password, s.settings.Host)
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("the mail server refused the credentials: %w", err)
	}
	return nil
}

/*
compose builds the message as it goes on the wire.

Assembled here rather than with a library because what is sent is a subject and
some plain text: headers, a blank line, and the body. The one subtlety is the
subject, which may contain anything somebody's display name does and so is
encoded rather than pasted in.
*/
func (s *Service) compose(to string, message Message) string {
	name := s.settings.FromName
	if name == "" {
		name = s.serverName
	}

	from := s.settings.From
	if name != "" {
		from = fmt.Sprintf("%s <%s>", mime.QEncoding.Encode("utf-8", name), s.settings.From)
	}

	var builder strings.Builder
	builder.WriteString("From: " + from + "\r\n")
	builder.WriteString("To: " + to + "\r\n")
	builder.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", message.Subject) + "\r\n")
	builder.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	builder.WriteString("MIME-Version: 1.0\r\n")
	builder.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	/*
		Asking not to be replied to automatically.

		A recovery message reaching an out-of-office responder produces a reply
		to this server's own address, and enough of them produce a mailbox
		nobody reads full of mail nobody sent.
	*/
	builder.WriteString("Auto-Submitted: auto-generated\r\n")
	builder.WriteString("\r\n")
	builder.WriteString(normaliseLineEndings(message.Body))

	return builder.String()
}

/*
normaliseLineEndings makes every break CRLF, and escapes a line that would end
the message early.

A line consisting of one full stop terminates the data in SMTP. A body
containing one would be truncated there, and the rest would be interpreted as
commands — which is worth preventing whether or not anything in this server can
currently produce such a body.
*/
func normaliseLineEndings(body string) string {
	normalised := strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\r", "\n")

	lines := strings.Split(normalised, "\n")
	for index, line := range lines {
		if strings.HasPrefix(line, ".") {
			lines[index] = "." + line
		}
	}
	return strings.Join(lines, "\r\n")
}
