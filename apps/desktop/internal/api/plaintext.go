/*
The plain text of a message, for a desktop banner.

A body is the Markdown its author typed. A banner shows text, so the markers
come off and the words stay. This is a copy of the server's rules
(server/internal/markup), because this module does not depend on the server's;
both are tested against packages/ui/app/lib/plaintext.cases.json, which is what
keeps the copies equal.
*/
package api

import (
	"regexp"
	"strings"
)

var (
	// mdFence opens or closes a code block: three or more backticks or tildes.
	mdFence = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})(.*)$")

	mdQuote   = regexp.MustCompile(`^ {0,3}(?:> ?)+`)
	mdHeading = regexp.MustCompile(`^ {0,3}#{1,6}[ \t]+(.*?)(?:[ \t]+#+)?[ \t]*$`)
	mdBullet  = regexp.MustCompile(`^(\s*)[*+][ \t]+`)

	mdLink          = regexp.MustCompile(`!?\[([^\]]*)\]\(\s*<?([^\s)>]*)>?(?:\s+"[^"]*")?\s*\)`)
	mdStrong        = regexp.MustCompile(`\*\*(\S(?:.*?\S)??)\*\*`)
	mdStrongUnder   = regexp.MustCompile(`__(\S(?:.*?\S)??)__`)
	mdStrike        = regexp.MustCompile(`~~(\S(?:.*?\S)??)~~`)
	mdEmphasis      = regexp.MustCompile(`\*(\S(?:.*?\S)??)\*`)
	mdEmphasisUnder = regexp.MustCompile(`(^|[^A-Za-z0-9_])_(\S(?:.*?\S)??)_([^A-Za-z0-9_]|$)`)
	mdEscaped       = regexp.MustCompile("\\\\([!-/:-@\\[-`{-~])")
)

// mdPark is where an escaped character waits while the emphasis rules run, so
// "\*not italic\*" cannot be taken for markers.
const mdPark = 0xe000

// plainText returns a message body with its formatting taken off.
func plainText(source string) string {
	var out []string
	open := ""

	for _, line := range strings.Split(source, "\n") {
		if open != "" {
			if mdCloses(line, open) {
				open = ""
				continue
			}
			out = append(out, line)
			continue
		}
		if marker, ok := mdOpens(line); ok {
			open = marker
			continue
		}

		line = mdQuote.ReplaceAllString(line, "")
		line = mdHeading.ReplaceAllString(line, "$1")
		line = mdBullet.ReplaceAllString(line, "$1- ")

		var b strings.Builder
		for _, part := range mdSplitCode(line) {
			if part.code {
				b.WriteString(part.text)
			} else {
				b.WriteString(mdPlainInline(part.text))
			}
		}
		out = append(out, b.String())
	}

	return strings.Join(out, "\n")
}

func mdOpens(line string) (string, bool) {
	match := mdFence.FindStringSubmatch(line)
	if match == nil {
		return "", false
	}
	// A backtick fence's label may not hold a backtick: "```a``` b" is inline code.
	if match[1][0] == '`' && strings.Contains(match[2], "`") {
		return "", false
	}
	return match[1], true
}

func mdCloses(line, open string) bool {
	match := mdFence.FindStringSubmatch(line)
	return match != nil && match[1][0] == open[0] && len(match[1]) >= len(open) &&
		strings.TrimSpace(match[2]) == ""
}

type mdSegment struct {
	code bool
	text string
}

// mdSplitCode separates a line into code spans and the text between them. A
// scan rather than a pattern, because a span closes on a run of exactly as
// many backticks as opened it, which needs a backreference RE2 does not have.
func mdSplitCode(line string) []mdSegment {
	var parts []mdSegment
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
			parts = append(parts, mdSegment{text: line[cursor:index]})
		}
		inner := line[index+run : closeAt]
		// One space of padding either side is how a span holds a backtick.
		if len(inner) >= 2 && inner[0] == ' ' && inner[len(inner)-1] == ' ' && strings.TrimSpace(inner) != "" {
			inner = inner[1 : len(inner)-1]
		}
		parts = append(parts, mdSegment{code: true, text: inner})
		index = closeAt + run
		cursor = index
	}

	if cursor < len(line) {
		parts = append(parts, mdSegment{text: line[cursor:]})
	}
	return parts
}

func mdPlainInline(text string) string {
	text = mdEscaped.ReplaceAllStringFunc(text, func(match string) string {
		return string(rune(mdPark + int(match[1])))
	})

	text = mdLink.ReplaceAllStringFunc(text, func(match string) string {
		parts := mdLink.FindStringSubmatch(match)
		label, url := parts[1], parts[2]
		if label == "" || label == url {
			return url
		}
		return label + " (" + url + ")"
	})
	text = mdStrong.ReplaceAllString(text, "$1")
	text = mdStrongUnder.ReplaceAllString(text, "$1")
	text = mdStrike.ReplaceAllString(text, "$1")
	text = mdEmphasis.ReplaceAllString(text, "$1")
	// Again until nothing changes: each match consumes the character after
	// it, which is the one the next match needs before it ("_a_ _b_").
	for next := mdEmphasisUnder.ReplaceAllString(text, "$1$2$3"); next != text; next = mdEmphasisUnder.ReplaceAllString(text, "$1$2$3") {
		text = next
	}

	return strings.Map(func(r rune) rune {
		if r > mdPark+0x20 && r < mdPark+0x7f {
			return r - mdPark
		}
		return r
	}, text)
}
