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
	case IsTextual(detected):
		return true
	default:
		return false
	}
}

// IsTextual reports whether an attachment is text a person can read.
//
// A shared log, patch or configuration file is something people in a developer
// community pass around constantly, and downloading one to look at three lines
// is a poor experience. These are still served inertly — the client fetches the
// bytes and renders them as text it escapes itself, never as markup a browser
// interprets.
//
// Detection reports most source files as text/plain, so the specific types here
// are the ones that carry their own identity.
func IsTextual(detected string) bool {
	switch detected {
	case "text/plain",
		"text/markdown",
		"text/csv",
		"text/tab-separated-values",
		"application/json",
		"application/x-ndjson",
		"application/toml",
		"application/yaml",
		"text/x-yaml",
		"text/x-log":
		return true
	}

	// Anything else claiming to be text is still text, but the guard above
	// keeps types like text/html — which must never be rendered — out.
	return false
}

// IsMarkdown reports whether an attachment should be rendered as markdown
// rather than shown as plain text.
//
// Decided from the type where one is declared, and otherwise from the file's
// name, because detection reports a .md file as text/plain: markdown has no
// magic bytes, and a document that opens with a heading is indistinguishable
// from one that opens with a hash.
func IsMarkdown(detected, filename string) bool {
	if detected == "text/markdown" {
		return true
	}

	lower := strings.ToLower(filename)
	return strings.HasSuffix(lower, ".md") ||
		strings.HasSuffix(lower, ".markdown") ||
		strings.HasSuffix(lower, ".mdown")
}
