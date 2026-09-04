// Package attachments accepts, stores and serves files attached to messages.
//
// Everything an operator can restrict is checked here, on the server, against
// the file's actual content — never against what the client said about it. A
// client's own validation is a convenience for the person using it, not a
// control: anything that matters is decided again on this side.
package attachments

import (
	"fmt"
	"strings"
)

// Limits are the operator's restrictions on what may be uploaded.
type Limits struct {
	// MaxFileSize is the largest single file, in bytes.
	MaxFileSize int64

	// MaxMessageSize is the largest total across one message's attachments.
	MaxMessageSize int64

	// MaxPerMessage is how many files one message may carry.
	MaxPerMessage int

	// AllowedTypes restricts uploads to these media types when non-empty. An
	// entry may name a type exactly ("image/png") or a whole group
	// ("image/*"). Empty means every type the server does not refuse outright
	// is permitted.
	AllowedTypes []string

	// BlockedTypes is refused whichever way AllowedTypes is set, so that an
	// operator who opens up a group can still exclude parts of it.
	BlockedTypes []string
}

// DefaultLimits are what a server allows before an operator changes anything.
//
// Chosen to be useful rather than generous: a community sharing screenshots,
// documents and short clips is served by this, and an operator who wants more
// can say so. Defaults that are too permissive cost disk on every deployment
// that never thought about it.
func DefaultLimits() Limits {
	return Limits{
		MaxFileSize:    25 << 20,  // 25 MiB
		MaxMessageSize: 100 << 20, // 100 MiB
		MaxPerMessage:  10,
		AllowedTypes:   nil,
		// Refused by default because a browser or an operating system may act
		// on them rather than merely show them. An operator can remove these,
		// but they should have to decide to.
		BlockedTypes: []string{
			"application/x-msdownload",
			"application/x-msdos-program",
			"application/x-executable",
			"application/x-sharedlib",
			"application/vnd.microsoft.portable-executable",
			"application/x-mach-binary",
			"application/x-elf",
		},
	}
}

// LimitError says which limit an upload exceeded.
//
// A refusal has to be specific enough to act on. "Too large" leaves someone
// guessing whether to compress the file or split the message; naming the limit
// and the value tells them what to do.
type LimitError struct {
	// Limit names what was exceeded, e.g. "file size" or "attachment count".
	Limit string
	// Allowed is the configured limit, in the units of the limit.
	Allowed int64
	// Actual is what the upload supplied.
	Actual int64
	// Detail carries a type name for type refusals, where a number says nothing.
	Detail string
}

func (e *LimitError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("attachments: %s: %s", e.Limit, e.Detail)
	}
	return fmt.Sprintf("attachments: %s limit is %d, supplied %d", e.Limit, e.Allowed, e.Actual)
}

// CheckSize reports whether one file is within the size limit.
func (l Limits) CheckSize(size int64) error {
	if l.MaxFileSize > 0 && size > l.MaxFileSize {
		return &LimitError{Limit: "file size", Allowed: l.MaxFileSize, Actual: size}
	}
	return nil
}

// CheckCount reports whether a message carries too many attachments.
func (l Limits) CheckCount(count int) error {
	if l.MaxPerMessage > 0 && count > l.MaxPerMessage {
		return &LimitError{
			Limit:   "attachment count",
			Allowed: int64(l.MaxPerMessage),
			Actual:  int64(count),
		}
	}
	return nil
}

// CheckTotal reports whether a message's attachments are too large together.
func (l Limits) CheckTotal(total int64) error {
	if l.MaxMessageSize > 0 && total > l.MaxMessageSize {
		return &LimitError{Limit: "total message size", Allowed: l.MaxMessageSize, Actual: total}
	}
	return nil
}

// CheckType reports whether a detected media type may be stored.
//
// The type given here must be the one detected from the file's content. Passing
// a client's declared type would make the whole check meaningless, since
// renaming a file would be enough to defeat it.
func (l Limits) CheckType(detected string) error {
	// Parameters such as "; charset=utf-8" are not part of the decision.
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(detected, ";")[0]))
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}

	// Blocked wins, so an operator who allows a whole group can still carve
	// pieces out of it.
	for _, blocked := range l.BlockedTypes {
		if matchesType(mediaType, blocked) {
			return &LimitError{
				Limit:  "type not permitted",
				Detail: mediaType,
			}
		}
	}

	if len(l.AllowedTypes) == 0 {
		return nil
	}

	for _, allowed := range l.AllowedTypes {
		if matchesType(mediaType, allowed) {
			return nil
		}
	}

	return &LimitError{Limit: "type not permitted", Detail: mediaType}
}

// matchesType reports whether a media type matches a pattern, which is either
// an exact type or a group wildcard such as "image/*".
func matchesType(mediaType, pattern string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	if pattern == "" {
		return false
	}
	if pattern == "*" || pattern == "*/*" {
		return true
	}

	if group, ok := strings.CutSuffix(pattern, "/*"); ok {
		prefix, _, found := strings.Cut(mediaType, "/")
		return found && prefix == group
	}

	return mediaType == pattern
}
