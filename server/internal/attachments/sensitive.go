package attachments

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

/*
Marking media as sensitive, so it arrives obscured.

Three parties can decide, and which of them decided is what governs who may
change it. An uploader marking their own file is the ordinary case and the one
that must work on a server with nothing else configured — no classifier, no
external service, no account anywhere. A moderator marking somebody else's is
the case that has to stick. A classifier, where an operator has configured one,
is a third source with no more authority than the uploader.
*/

// SensitiveSource says who decided a file is sensitive.
type SensitiveSource string

const (
	// SensitiveByUploader is somebody marking their own file, which is the
	// case that must work with nothing configured.
	SensitiveByUploader SensitiveSource = "uploader"
	// SensitiveByModerator is a decision the uploader cannot undo.
	SensitiveByModerator SensitiveSource = "moderator"
	// SensitiveByClassifier is an optional automated decision, carrying no
	// more authority than the uploader's own. An operator who configures one
	// is adding a suggestion, not an adjudicator.
	SensitiveByClassifier SensitiveSource = "automatic"
)

// ErrNotSensitiveSource is returned for a source this server does not define.
var ErrNotSensitiveSource = errors.New("that is not a way media can be marked")

// ErrMarkNotYours is returned when somebody tries to lift a mark that is not
// theirs to lift.
var ErrMarkNotYours = errors.New("that mark was made by a moderator")

func knownSensitiveSource(source SensitiveSource) bool {
	switch source {
	case SensitiveByUploader, SensitiveByModerator, SensitiveByClassifier:
		return true
	}
	return false
}

/*
MarkSensitive marks an attachment so viewers see it obscured.

A moderator's mark replaces an uploader's; an uploader's does not replace a
moderator's. The rule lives in the query rather than here, so it holds for
every caller and so two people marking at once cannot interleave into the
weaker outcome.
*/
func (s *Service) MarkSensitive(
	ctx context.Context,
	id uuid.UUID,
	by string,
	source SensitiveSource,
) error {
	if !knownSensitiveSource(source) {
		return fmt.Errorf("%w: %q", ErrNotSensitiveSource, source)
	}

	marker := by
	sourceName := string(source)

	changed, err := s.queries.MarkAttachmentSensitive(ctx, dbgen.MarkAttachmentSensitiveParams{
		ID:              id,
		SensitiveBy:     &marker,
		SensitiveSource: &sourceName,
	})
	if err != nil {
		return fmt.Errorf("attachments: marking sensitive: %w", err)
	}
	if changed == 0 {
		/*
			Either the attachment does not exist, or a moderator's mark is
			already there and this is a weaker one.

			Not an error in the second case: the caller asked for the file to
			be treated as sensitive and it is. Reporting a failure would invite
			a client to retry something that is already true.
		*/
		if _, err := s.queries.GetAttachment(ctx, id); err != nil {
			return ErrNotFound
		}
	}

	return nil
}

/*
UnmarkSensitive lifts a mark.

asModerator is what separates lifting your own decision from overriding
somebody else's. An uploader may undo their own mark, and an automatic one on
their own file; a moderator's stands until a moderator lifts it.
*/
func (s *Service) UnmarkSensitive(
	ctx context.Context,
	id uuid.UUID,
	actor uuid.UUID,
	asModerator bool,
) error {
	changed, err := s.queries.UnmarkAttachmentSensitive(ctx, dbgen.UnmarkAttachmentSensitiveParams{
		ID:          id,
		AsModerator: asModerator,
		ActorID:     actor,
	})
	if err != nil {
		return fmt.Errorf("attachments: unmarking sensitive: %w", err)
	}

	if changed == 0 {
		// Distinguish "no such file" from "not yours to lift", because the
		// second is something the caller can act on and the first is not.
		row, err := s.queries.GetAttachment(ctx, id)
		if err != nil {
			return ErrNotFound
		}
		if row.SensitiveSource != nil {
			return ErrMarkNotYours
		}
		// Already unmarked, which is what the caller wanted.
	}

	return nil
}
