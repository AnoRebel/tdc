package attachments_test

import (
	"strings"
	"testing"

	"github.com/AnoRebel/tdc/server/internal/attachments"
)

// Small but genuine file headers. Real magic bytes rather than invented ones,
// because the point of these tests is that detection matches what a real file
// would produce.
var (
	pngHeader  = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 13, 'I', 'H', 'D', 'R'}
	jpegHeader = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0x10, 'J', 'F', 'I', 'F', 0}
	gifHeader  = []byte("GIF89a\x01\x00\x01\x00")
	pdfHeader  = []byte("%PDF-1.7\n1 0 obj\n")
	zipHeader  = []byte{'P', 'K', 0x03, 0x04, 0x14, 0, 0, 0}
	elfHeader  = []byte{0x7F, 'E', 'L', 'F', 2, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2, 0, 0x3E, 0}
	htmlBody   = []byte("<!DOCTYPE html><html><body><script>alert(1)</script></body></html>")
	svgBody    = []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	textBody   = []byte("habari ya asubuhi, hii ni maandishi ya kawaida\n")
)

func peHeader() []byte {
	// "MZ" then enough of a DOS stub for the detector to be sure.
	header := make([]byte, 64)
	copy(header, []byte{'M', 'Z', 0x90, 0x00, 0x03, 0x00, 0x00, 0x00, 0x04, 0x00})
	return header
}

// TestDetectionUsesContent checks that a file is identified by what is in it.
func TestDetectionUsesContent(t *testing.T) {
	for _, tc := range []struct {
		name     string
		content  []byte
		expected string
	}{
		{"png", pngHeader, "image/png"},
		{"jpeg", jpegHeader, "image/jpeg"},
		{"gif", gifHeader, "image/gif"},
		{"pdf", pdfHeader, "application/pdf"},
		{"zip", zipHeader, "application/zip"},
		{"elf", elfHeader, "application/x-executable"},
		{"pe", peHeader(), "application/vnd.microsoft.portable-executable"},
		{"html", htmlBody, "text/html"},
		{"svg", svgBody, "image/svg+xml"},
		{"text", textBody, "text/plain"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			detected := attachments.DetectType(tc.content)
			if detected != tc.expected {
				t.Errorf("detected %q, expected %q", detected, tc.expected)
			}
			// A type carrying parameters cannot be compared directly, so the
			// detector must have stripped them.
			if strings.Contains(detected, ";") {
				t.Errorf("the detected type carries parameters: %q", detected)
			}
		})
	}
}

// TestDeclaredTypeIsIrrelevant is the requirement stated as a test.
//
// A client controls the filename and the declared type; if either could decide
// what a file is, every restriction an operator configures would be a matter of
// politeness. Detection takes only the content, so there is nowhere for a lie
// to enter.
func TestDeclaredTypeIsIrrelevant(t *testing.T) {
	// An executable that a client would very much like to call an image.
	if detected := attachments.DetectType(elfHeader); detected != "application/x-executable" {
		t.Errorf("an ELF binary was detected as %q", detected)
	}

	// The defaults must then refuse it, whatever it was called.
	if err := attachments.DefaultLimits().CheckType(attachments.DetectType(elfHeader)); err == nil {
		t.Error("an executable passed the default limits")
	}

	// And an actual image is still an image.
	if detected := attachments.DetectType(pngHeader); detected != "image/png" {
		t.Errorf("a PNG was detected as %q", detected)
	}
}

// TestEmptyFileIsInert checks that a file with no content is treated as binary
// rather than as text, which a browser might render.
func TestEmptyFileIsInert(t *testing.T) {
	if detected := attachments.DetectType(nil); detected != "application/octet-stream" {
		t.Errorf("an empty file was detected as %q", detected)
	}
}

// TestActiveContentIsServedInertly is the second half of the same protection.
//
// An attachment is served from the application's own origin, so a browser that
// renders uploaded HTML or SVG runs script with the application's cookies
// available. The stored type is unchanged; what is claimed on the way out is
// something no browser will act on.
func TestActiveContentIsServedInertly(t *testing.T) {
	for _, active := range []string{
		"text/html",
		"image/svg+xml",
		"application/xhtml+xml",
		"text/xml",
		"application/xml",
		"application/javascript",
		"text/javascript",
	} {
		if served := attachments.ServingType(active); served != "application/octet-stream" {
			t.Errorf("%q is served as %q; a browser would act on it", active, served)
		}
	}

	// Inert types keep their own, so images still display and PDFs still open.
	for _, inert := range []string{
		"image/png", "image/jpeg", "image/gif", "image/webp",
		"application/pdf", "video/mp4", "audio/mpeg", "text/plain",
	} {
		if served := attachments.ServingType(inert); served != inert {
			t.Errorf("%q is served as %q, losing its type for no reason", inert, served)
		}
	}
}

// TestRenderableExcludesSVG checks that SVG is never treated as displayable.
//
// It is an image by type and a document by capability. Since it is served as an
// opaque download, telling a client it can be rendered inline would only
// produce a broken preview.
func TestRenderableExcludesSVG(t *testing.T) {
	if attachments.IsRenderable("image/svg+xml") {
		t.Error("SVG is reported as renderable, but it is served as a download")
	}

	for _, renderable := range []string{"image/png", "video/mp4", "audio/mpeg", "application/pdf"} {
		if !attachments.IsRenderable(renderable) {
			t.Errorf("%q is not reported as renderable", renderable)
		}
	}

	// Text is viewable now that there is a viewer for it; these are the types
	// with nothing to show but their name and size.
	for _, not := range []string{"application/zip", "application/octet-stream"} {
		if attachments.IsRenderable(not) {
			t.Errorf("%q is reported as renderable", not)
		}
	}
}

// TestTextualTypesAreViewable covers the files a developer community actually
// passes around — logs, configuration, patches — which are worth reading in
// place rather than downloading to see three lines.
func TestTextualTypesAreViewable(t *testing.T) {
	for _, textual := range []string{
		"text/plain",
		"text/markdown",
		"text/csv",
		"application/json",
		"application/yaml",
		"application/toml",
	} {
		if !attachments.IsTextual(textual) {
			t.Errorf("%q is not treated as text", textual)
		}
		if !attachments.IsRenderable(textual) {
			t.Errorf("%q is not viewable, so it would only be downloadable", textual)
		}
	}
}

// TestActiveTextTypesStayUnviewable is the important half of the same rule.
//
// text/html is text, and rendering it is exactly what must never happen: it
// would run in the application's origin with the reader's session available.
// The type test must not accidentally let it through.
func TestActiveTextTypesStayUnviewable(t *testing.T) {
	for _, active := range []string{
		"text/html",
		"application/xhtml+xml",
		"text/xml",
		"application/xml",
		"image/svg+xml",
		"application/javascript",
		"text/javascript",
	} {
		if attachments.IsTextual(active) {
			t.Errorf("%q is treated as ordinary text; it must not be", active)
		}
		if attachments.IsRenderable(active) {
			t.Errorf("%q is reported as viewable; a client might then render it", active)
		}
	}
}

// TestMarkdownIsRecognisedByName checks the case detection cannot cover.
//
// Markdown has no magic bytes: a document opening with a heading is
// indistinguishable from one opening with a hash, so every detector reports it
// as text/plain. The filename is the only evidence there is.
func TestMarkdownIsRecognisedByName(t *testing.T) {
	for _, name := range []string{"README.md", "notes.markdown", "CHANGELOG.MD", "guide.mdown"} {
		if !attachments.IsMarkdown("text/plain", name) {
			t.Errorf("%q is not recognised as markdown", name)
		}
	}

	for _, name := range []string{"notes.txt", "server.log", "data.json", "mdfile", "a.md.txt"} {
		if attachments.IsMarkdown("text/plain", name) {
			t.Errorf("%q is treated as markdown", name)
		}
	}

	// A declared type settles it regardless of the name.
	if !attachments.IsMarkdown("text/markdown", "no-extension") {
		t.Error("a declared markdown type was not honoured")
	}
}
