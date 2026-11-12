package attachments

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"io"
)

/*
Obscured placeholders for media marked sensitive.

A CSS blur is not obscuring. The browser is given the whole image and asked to
draw it out of focus, which anybody can undo in one click of the developer
tools — and which every image the page loaded is sitting in the cache in full
regardless. Blurring in the client is a curtain painted on a window.

So the server decides. Media marked sensitive is served, until the viewer asks
otherwise, as a placeholder generated here: the image reduced to a handful of
pixels and scaled back up. The detail is not hidden, it is gone — averaged away
by the downscale and unrecoverable from what was sent, because it never left
this process.

The result still looks like the original in the way that matters for this
purpose. Somebody sees the shape and the colours, enough to decide whether they
want to look, and not enough to see what was in it.
*/

/*
obscuredWidth is how wide the reduced image is before it is scaled back up.

Sixteen pixels across. Small enough that a face is not a face and text is not
text; large enough that the placeholder still reads as this image rather than a
grey rectangle, which is what makes it useful instead of decorative.
*/
const obscuredWidth = 16

// obscuredScale is how much the reduction is enlarged again, so a client
// receives something the size of a preview rather than a stamp it has to
// stretch — leaving the smoothing to the browser would put the choice of how
// much detail survives back in the client.
const obscuredScale = 12

/*
GenerateObscured makes the placeholder shown in place of sensitive media.

Takes the preview rather than the original wherever one exists: it is already
small, already decoded once, and the result is identical after a reduction this
aggressive.
*/
func GenerateObscured(r io.Reader) ([]byte, error) {
	source, _, err := image.Decode(r)
	if err != nil {
		return nil, fmt.Errorf("attachments: decoding to obscure: %w", err)
	}

	bounds := source.Bounds()
	if bounds.Dx() == 0 || bounds.Dy() == 0 {
		return nil, fmt.Errorf("attachments: obscuring an image with no area")
	}

	// The reduction keeps the original's proportions, so the placeholder
	// occupies the same shape the image will when revealed and the
	// conversation does not jump.
	width := obscuredWidth
	height := bounds.Dy() * width / bounds.Dx()
	if height < 1 {
		height = 1
	}

	// Reduced, then enlarged. The second step recovers no detail — there is
	// none left to recover — it only makes the result a convenient size.
	reduced := resize(source, width, height)
	enlarged := resize(reduced, width*obscuredScale, height*obscuredScale)

	var out bytes.Buffer
	if err := png.Encode(&out, enlarged); err != nil {
		return nil, fmt.Errorf("attachments: encoding an obscured image: %w", err)
	}

	return out.Bytes(), nil
}
