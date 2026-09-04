package attachments

import (
	"strings"

	"github.com/gabriel-vasile/mimetype"
)

// detectionWindow is how much of a file is read to determine its type.
//
// Every format worth recognising declares itself in its first bytes, so this is
// generous rather than necessary. Bounded because the check has to happen
// before the file is stored, which means holding this much in memory per
// upload in flight.
const detectionWindow = 3072

// DetectType determines a file's media type from its content.
//
// The client's declared type and filename are deliberately not arguments. A
// name is chosen by whoever uploads the file and can say anything; a declared
// type is a header they also control. Deciding what a file is from either would
// make every restriction an operator configures a matter of politeness.
//
// The returned type never carries parameters, so callers compare it directly.
func DetectType(head []byte) string {
	if len(head) == 0 {
		// An empty file is not text and not anything else. Naming it the
		// generic binary type means it is treated as inert, which is right.
		return "application/octet-stream"
	}

	detected := mimetype.Detect(head).String()

	// "text/plain; charset=utf-8" and "text/plain" are the same decision.
	mediaType, _, _ := strings.Cut(detected, ";")
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	if mediaType == "" {
		return "application/octet-stream"
	}

	return mediaType
}

// ServingType is the type an attachment is served as.
//
// Not always the type it was detected as. A browser asked to render SVG or HTML
// will execute script inside it, in the origin of whatever page linked to it —
// so those are handed back as a type no browser will act on, whatever they
// really are. The stored type is unchanged; only what is claimed on the way out
// differs, because that is what the browser acts on.
//
// This works together with the inert serving headers rather than instead of
// them: either alone would be enough for a well-behaved browser, and neither
// alone is enough for all of them.
func ServingType(detected string) string {
	switch strings.ToLower(detected) {
	case "image/svg+xml",
		"text/html",
		"application/xhtml+xml",
		"text/xml",
		"application/xml",
		"application/javascript",
		"text/javascript",
		"application/x-javascript",
		"text/vtt":
		return "application/octet-stream"
	}

	return detected
}

// IsRenderable reports whether a client can be expected to display this inline.
//
// Used to decide whether a thumbnail is worth generating, not to decide
// anything about safety.
func IsRenderable(detected string) bool {
	switch {
	case detected == "image/svg+xml":
		// Renderable in principle, and never rendered here: see ServingType.
		return false
	case strings.HasPrefix(detected, "image/"),
		strings.HasPrefix(detected, "video/"),
		strings.HasPrefix(detected, "audio/"):
		return true
	case detected == "application/pdf":
		return true
	default:
		return false
	}
}
