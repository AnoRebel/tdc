package attachments

import (
	"bytes"
	"encoding/binary"
	"io"
)

/*
IsAnimated reports whether an image moves.

Needed because a thumbnail is a still frame, and the timeline shows the
thumbnail where one exists — so an animated GIF arrived as a frozen first
frame. A GIF that does not move is not a GIF: somebody sending one has sent the
one thing about it that matters, and it was being removed.

Decided by reading the file's structure rather than by decoding it. Decoding
every frame of an animation to answer a yes-or-no question is exactly the work
this design keeps out of the server, and the structure is unambiguous.
*/
func IsAnimated(r io.Reader, contentType string) bool {
	// Bounded: the answer lives in the first frames, and an animation of any
	// size declares itself well before the end.
	source, err := io.ReadAll(io.LimitReader(r, animationScanBytes))
	if err != nil {
		return false
	}

	switch contentType {
	case "image/gif":
		return gifIsAnimated(source)
	case "image/webp":
		return webpIsAnimated(source)
	case "image/apng", "image/png":
		return pngIsAnimated(source)
	default:
		return false
	}
}

/*
animationScanBytes bounds how far to look.

Generous enough that a second frame is found in any real animation — the
header, palette and first frame of a large GIF are well under this — and small
enough that the check costs nothing on a file that is not one.
*/
const animationScanBytes = 512 * 1024

/*
gifIsAnimated counts image descriptors.

A GIF is a sequence of blocks; each frame is an image descriptor, introduced by
0x2C. One descriptor is a still image, two or more is an animation. The blocks
have to be walked rather than searched for, because 0x2C occurs freely inside
compressed pixel data and counting occurrences would find animation everywhere.
*/
func gifIsAnimated(source []byte) bool {
	if len(source) < 13 || !bytes.HasPrefix(source, []byte("GIF")) {
		return false
	}

	at := 13

	// The global colour table, when the packed field says there is one.
	if source[10]&0x80 != 0 {
		at += 3 << ((source[10] & 0x07) + 1)
	}

	frames := 0
	for at < len(source) {
		switch source[at] {
		case 0x2C: // Image descriptor: one frame.
			frames++
			if frames > 1 {
				return true
			}

			at += 10
			if at > len(source) {
				return false
			}
			// A local colour table, if this frame has one.
			if at-1 < len(source) && source[at-1]&0x80 != 0 {
				at += 3 << ((source[at-1] & 0x07) + 1)
			}
			at++ // The LZW minimum code size.
			at = skipSubBlocks(source, at)

		case 0x21: // Extension: comment, graphic control, application.
			at += 2
			at = skipSubBlocks(source, at)

		case 0x3B: // Trailer: the end.
			return false

		default:
			// Not a structure this understands. Reporting "not animated" is
			// the safe answer: it keeps the still preview, which is wrong only
			// for a file that was already malformed.
			return false
		}

		if at <= 0 {
			return false
		}
	}

	return false
}

// skipSubBlocks walks the length-prefixed chunks a GIF block is made of,
// returning the offset just past the terminating zero length.
func skipSubBlocks(source []byte, at int) int {
	for at < len(source) {
		length := int(source[at])
		if length == 0 {
			return at + 1
		}
		at += length + 1
	}
	return len(source)
}

/*
webpIsAnimated looks for the ANIM chunk.

An animated WebP is a RIFF container with an ANIM chunk declaring the loop
count. A still WebP has no such chunk, so its presence is the whole answer.
*/
func webpIsAnimated(source []byte) bool {
	if len(source) < 16 ||
		!bytes.HasPrefix(source, []byte("RIFF")) ||
		!bytes.Equal(source[8:12], []byte("WEBP")) {
		return false
	}

	// Only within the container's own declared length, so a chunk name
	// appearing inside trailing junk is not mistaken for a real one.
	declared := int(binary.LittleEndian.Uint32(source[4:8])) + 8
	if declared > len(source) || declared < 12 {
		declared = len(source)
	}

	return bytes.Contains(source[12:declared], []byte("ANIM"))
}

/*
pngIsAnimated looks for the acTL chunk.

An APNG is an ordinary PNG with an animation control chunk before the first
frame, which is what makes it both a valid still image to anything that ignores
the chunk and an animation to anything that does not.
*/
func pngIsAnimated(source []byte) bool {
	if len(source) < 8 || !bytes.HasPrefix(source, []byte{0x89, 'P', 'N', 'G'}) {
		return false
	}

	// Before IDAT: acTL must precede the image data to be valid, and stopping
	// there avoids scanning the whole file for a chunk that cannot be later.
	if end := bytes.Index(source, []byte("IDAT")); end > 0 {
		source = source[:end]
	}

	return bytes.Contains(source, []byte("acTL"))
}
