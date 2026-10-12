/*
Package markup turns a message body into plain text.

A message body is the Markdown its author typed, and clients render it. Some
things the server sends cannot show formatting — a notification preview, a push
payload on a lock screen, the text the search index holds and highlights — and
for those the markers come off and the words stay: code keeps its contents, a
link keeps its address, a quote and a heading lose only their marker.

The rules mirror the web client's (packages/ui/app/lib/formatting.ts), and both
are tested against the same cases in packages/ui/app/lib/plaintext.cases.json,
so a notification and the pinned list cannot disagree about what was said. The
desktop application carries a copy of these rules for its banners; it is tested
against the same file.
*/
package markup

import (
	"regexp"
	"strings"
)

var (
	// fence opens or closes a code block: three or more backticks or tildes.
	fence = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})(.*)$")

	quote   = regexp.MustCompile(`^ {0,3}(?:> ?)+`)
	heading = regexp.MustCompile(`^ {0,3}#{1,6}[ \t]+(.*?)(?:[ \t]+#+)?[ \t]*$`)
	bullet  = regexp.MustCompile(`^(\s*)[*+][ \t]+`)

	link          = regexp.MustCompile(`!?\[([^\]]*)\]\(\s*<?([^\s)>]*)>?(?:\s+"[^"]*")?\s*\)`)
	strong        = regexp.MustCompile(`\*\*(\S(?:.*?\S)??)\*\*`)
	strongUnder   = regexp.MustCompile(`__(\S(?:.*?\S)??)__`)
	strike        = regexp.MustCompile(`~~(\S(?:.*?\S)??)~~`)
	emphasis      = regexp.MustCompile(`\*(\S(?:.*?\S)??)\*`)
	emphasisUnder = regexp.MustCompile(`(^|[^A-Za-z0-9_])_(\S(?:.*?\S)??)_([^A-Za-z0-9_]|$)`)
	escaped       = regexp.MustCompile("\\\\([!-/:-@\\[-`{-~])")
)

// park is where an escaped character waits while the emphasis rules run, so
// "\*not italic\*" cannot be taken for markers.
const park = 0xe000

// PlainText returns a message body with its formatting taken off.
func PlainText(source string) string {
	var out []string
	open := ""

	for _, line := range strings.Split(source, "\n") {
		if open != "" {
			if closes(line, open) {
				open = ""
				continue
			}
			out = append(out, line)
			continue
		}
		if marker, ok := opens(line); ok {
			open = marker
			continue
		}

		line = quote.ReplaceAllString(line, "")
		line = heading.ReplaceAllString(line, "$1")
		line = bullet.ReplaceAllString(line, "$1- ")

		var b strings.Builder
		for _, part := range splitCode(line) {
			if part.code {
				b.WriteString(part.text)
			} else {
				b.WriteString(plainInline(part.text))
			}
		}
		out = append(out, b.String())
	}

	return strings.Join(out, "\n")
}

// WithoutCode returns a body with its code spans and blocks removed, for
// anything looking for what people meant rather than what they quoted — an
// @name inside a code sample is not somebody being addressed.
func WithoutCode(source string) string {
	var out []string
	open := ""

	for _, line := range strings.Split(source, "\n") {
		if open != "" {
			if closes(line, open) {
				open = ""
			}
			continue
		}
		if marker, ok := opens(line); ok {
			open = marker
			continue
		}
		var kept []string
		for _, part := range splitCode(line) {
			if !part.code {
				kept = append(kept, part.text)
			}
		}
		out = append(out, strings.Join(kept, " "))
	}

	return strings.Join(out, "\n")
}

func opens(line string) (string, bool) {
	match := fence.FindStringSubmatch(line)
	if match == nil {
		return "", false
	}
	// A backtick fence's label may not hold a backtick: "```a``` b" is inline code.
	if match[1][0] == '`' && strings.Contains(match[2], "`") {
		return "", false
	}
	return match[1], true
}

func closes(line, open string) bool {
	match := fence.FindStringSubmatch(line)
	return match != nil && match[1][0] == open[0] && len(match[1]) >= len(open) &&
		strings.TrimSpace(match[2]) == ""
}

type segment struct {
	code bool
	text string
}

// splitCode separates a line into code spans and the text between them. A
// scan rather than a pattern, because a span closes on a run of exactly as
// many backticks as opened it, which needs a backreference RE2 does not have.
func splitCode(line string) []segment {
	var parts []segment
	cursor, index := 0, 0

	runAt := func(at int) int {
		n := 0
		for at+n < len(line) && line[at+n] == '`' {
			n++
		}
		return n
	}

	for index < len(line) {
		if line[index] != '`' {
			index++
			continue
		}
		run := runAt(index)

		closeAt := -1
		for search := index + run; search < len(line); {
			if line[search] != '`' {
				search++
				continue
			}
			length := runAt(search)
			if length == run {
				closeAt = search
				break
			}
			search += length
		}

		if closeAt == -1 {
			index += run
			continue
		}

		if index > cursor {
			parts = append(parts, segment{text: line[cursor:index]})
		}
		inner := line[index+run : closeAt]
		// One space of padding either side is how a span holds a backtick.
		if len(inner) >= 2 && inner[0] == ' ' && inner[len(inner)-1] == ' ' && strings.TrimSpace(inner) != "" {
			inner = inner[1 : len(inner)-1]
		}
		parts = append(parts, segment{code: true, text: inner})
		index = closeAt + run
		cursor = index
	}

	if cursor < len(line) {
		parts = append(parts, segment{text: line[cursor:]})
	}
	return parts
}

func plainInline(text string) string {
	text = escaped.ReplaceAllStringFunc(text, func(match string) string {
		return string(rune(park + int(match[1])))
	})

	text = link.ReplaceAllStringFunc(text, func(match string) string {
		parts := link.FindStringSubmatch(match)
		label, url := parts[1], parts[2]
		if label == "" || label == url {
			return url
		}
		return label + " (" + url + ")"
	})
	text = strong.ReplaceAllString(text, "$1")
	text = strongUnder.ReplaceAllString(text, "$1")
	text = strike.ReplaceAllString(text, "$1")
	text = emphasis.ReplaceAllString(text, "$1")
	// Again until nothing changes: each match consumes the character after
	// it, which is the one the next match needs before it ("_a_ _b_").
	for next := emphasisUnder.ReplaceAllString(text, "$1$2$3"); next != text; next = emphasisUnder.ReplaceAllString(text, "$1$2$3") {
		text = next
	}

	return strings.Map(func(r rune) rune {
		if r > park+0x20 && r < park+0x7f {
			return r - park
		}
		return r
	}, text)
}
