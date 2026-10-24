package rooms

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

/*
FileRef describes one file a message carries, inside the message's own content.

In the content rather than only in the attachments table because the content is
what is signed and what crosses to other servers. The digest is the part that
matters: a server copying the bytes from anywhere checks them against what the
author's server vouched for. The name and type are the author's claims, shown
until the bytes arrive and decided again from the bytes once they do.
*/
type FileRef struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
	Caption     string `json:"caption,omitempty"`
}

// MaxFilesPerMessage bounds the descriptors one message may carry, which is
// also the attachments limit on what one message may bind.
const MaxFilesPerMessage = 10

// ErrBadFile reports a file description that cannot be trusted to mean anything.
var ErrBadFile = errors.New("a file in that message is not described properly")

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

/*
Check refuses a description another server sent that this one cannot use.

A digest that is not one would become a storage key, and a negative size would
pass every limit; neither is ever produced by a server telling the truth.
*/
func (f FileRef) Check() error {
	if !digestPattern.MatchString(f.SHA256) {
		return fmt.Errorf("%w: %q is not a SHA-256 digest", ErrBadFile, f.SHA256)
	}
	if f.Size < 0 {
		return fmt.Errorf("%w: a size cannot be negative", ErrBadFile)
	}
	if f.ID == "" {
		return fmt.Errorf("%w: it has no identifier", ErrBadFile)
	}
	return nil
}

// FilesIn reads the files a message's content describes, or none.
func FilesIn(content json.RawMessage) []FileRef {
	var message struct {
		Attachments []FileRef `json:"attachments"`
	}
	if json.Unmarshal(content, &message) != nil {
		return nil
	}
	return message.Attachments
}
