package translation

import (
	"regexp"
	"strings"
	"unicode"
)

/*
Segments: what of a message is sent for translation, and what is kept.

A message is Markdown its author typed, and a translation service rewrites
everything it is given. Code, a mention, a link's address or an email address
rewritten is a message that now says something else — a command that no longer
runs, a mention of nobody — so those are cut out before anything is sent and
put back exactly as written.

The same rules are implemented in packages/ui/app/lib/translation-segments.ts,
for translation on the reader's own device, and both are tested against
packages/ui/app/lib/translation.cases.json. Two implementations that disagreed
would translate the same message differently depending on where it ran.
*/

// Segment is one piece of a message: either sent for translation or kept.
type Segment struct {
	Text      string
	Translate bool
}

var (
	// A fence opens a code block: three or more backticks or tildes, after at
	// most three spaces.
	fenceOpen = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")

	// What starts a line and is structure rather than words: indentation,
	// quote markers, a list marker, a heading marker.
	lineLead = regexp.MustCompile(`^[ \t]*(?:>[ \t]*)*(?:(?:[-*+]|[0-9]{1,9}[.)])[ \t]+)?(?:#{1,6}[ \t]+)?`)

	/*
		What is kept within a line, leftmost first:

		inline code, double then single backticks; a Markdown link's target,
		"](…)", so its label is still translated; a bare address; an email
		address; a mention or channel reference, including user@server; an
		emoji shortcode.

		Whitespace is spelled as space and tab rather than \s, which means
		different things in Go and JavaScript.
	*/
	kept = regexp.MustCompile("``[^\n]+?``" +
		"|`[^`\n]+`" +
		`|\]\([^) \t]*\)` +
		`|https?://[^ \t<>()]+` +
		`|www\.[^ \t<>()]+` +
		`|[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)+` +
		`|[@#][A-Za-z0-9_](?:[A-Za-z0-9_.:@-]*[A-Za-z0-9_])?` +
		`|:[a-z0-9_+-]+:`)
)

// Split cuts a message body into segments.
func Split(body string) []Segment {
	var out segmenter

	fence := ""
	for index, line := range strings.Split(body, "\n") {
		if index > 0 {
			out.keep("\n")
		}

		if fence != "" {
			out.keep(line)
			if closesFence(line, fence) {
				fence = ""
			}
			continue
		}
		if match := fenceOpen.FindStringSubmatch(line); match != nil {
			fence = match[1]
			out.keep(line)
			continue
		}

		lead := lineLead.FindString(line)
		out.keep(lead)
		rest := line[len(lead):]

		position := 0
		for _, found := range kept.FindAllStringIndex(rest, -1) {
			start, end := found[0], found[1]
			// "@" or "#" inside a word is not a mention: the end of an
			// address someone typed without a scheme, a C# or an F#.
			if (rest[start] == '@' || rest[start] == '#') && start > 0 && isWordByte(rest[start-1]) {
				continue
			}
			out.text(rest[position:start])
			out.keep(rest[start:end])
			position = end
		}
		out.text(rest[position:])
	}

	return out.segments
}

// Join puts segments back together, in order.
func Join(segments []Segment) string {
	var builder strings.Builder
	for _, segment := range segments {
		builder.WriteString(segment.Text)
	}
	return builder.String()
}

// Translatable returns the text of every segment to be sent, in order.
func Translatable(segments []Segment) []string {
	var texts []string
	for _, segment := range segments {
		if segment.Translate {
			texts = append(texts, segment.Text)
		}
	}
	return texts
}

// Replace puts translations back in place of the segments that were sent.
// The caller passes exactly as many as Translatable returned.
func Replace(segments []Segment, translated []string) string {
	var builder strings.Builder
	next := 0
	for _, segment := range segments {
		if segment.Translate && next < len(translated) {
			builder.WriteString(translated[next])
			next++
			continue
		}
		builder.WriteString(segment.Text)
	}
	return builder.String()
}

func closesFence(line, fence string) bool {
	trimmed := strings.TrimRight(strings.TrimLeft(line, " "), " \t\r")
	if len(line)-len(strings.TrimLeft(line, " ")) > 3 || len(trimmed) < len(fence) {
		return false
	}
	return strings.Trim(trimmed, fence[:1]) == ""
}

func isWordByte(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z'
}

type segmenter struct {
	segments []Segment
}

func (s *segmenter) add(text string, translate bool) {
	if text == "" {
		return
	}
	if last := len(s.segments) - 1; last >= 0 && s.segments[last].Translate == translate {
		s.segments[last].Text += text
		return
	}
	s.segments = append(s.segments, Segment{Text: text, Translate: translate})
}

func (s *segmenter) keep(text string) { s.add(text, false) }

// text adds words: whitespace at either edge is kept, so a service that trims
// what it returns cannot run two pieces together, and a run with no letters
// in it — punctuation, numbers, an emoji — is kept whole.
func (s *segmenter) text(chunk string) {
	core := strings.TrimLeft(chunk, " \t\r")
	s.keep(chunk[:len(chunk)-len(core)])
	trimmed := strings.TrimRight(core, " \t\r")
	if strings.IndexFunc(trimmed, unicode.IsLetter) >= 0 {
		s.add(trimmed, true)
	} else {
		s.keep(trimmed)
	}
	s.keep(core[len(trimmed):])
}
