package notify

import (
	"encoding/json"
	"slices"
	"testing"
)

// A notification preview shows the words of a message, not its Markdown.
func TestAPreviewIsPlainText(t *testing.T) {
	content, _ := json.Marshal(map[string]string{
		"body": "**Deploy** is _done_, see [the log](https://ci.example/1)\n```\nok\n```",
	})

	if got, want := previewOf(content), "Deploy is done, see the log (https://ci.example/1) ok"; got != want {
		t.Errorf("previewOf = %q, want %q", got, want)
	}
}

// Somebody named in a code sample is being quoted, not addressed.
func TestAMentionInsideCodeNotifiesNobody(t *testing.T) {
	got := MentionsIn("@amina have a look:\n```\n@Component\nclass X {}\n```\nand `@juma` too, **@baraka**")

	if !slices.Equal(got, []string{"amina", "baraka"}) {
		t.Errorf("MentionsIn = %v, want amina and baraka, the bold one included", got)
	}
}
