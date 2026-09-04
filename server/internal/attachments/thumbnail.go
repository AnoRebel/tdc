package attachments

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"

	// Registered for their decoders, so DecodeConfig recognises these formats.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	// WebP has no decoder in the standard library, and it is common enough that
	// claiming support without one would mean silently failing on a large share
	// of what people share. Decoding only; nothing here writes WebP.
	_ "golang.org/x/image/webp"
)

// Thumbnailing is deliberately bounded twice over: once on the dimensions a
// file claims, and once on the bytes it is allowed to occupy.
//
// A decompression bomb is a small file that declares enormous dimensions — a
// few kilobytes of PNG can claim 50,000 by 50,000 pixels, which is 10GB once
// decoded. Decoding first and checking afterwards is therefore not a check at
// all: the damage is done by the time there is anything to inspect.
//
// image.DecodeConfig reads only the header, so the dimensions are known before
// any pixel is allocated. That is the whole defence, and it is why the limit is
// on the pixel count rather than on the file's size.
const (
	// maxSourcePixels is the largest image that will be decoded at all.
	//
	// Roughly a 36-megapixel photograph, which is beyond any phone camera a TDC
	// member is likely to be using, and about 144MB decoded as RGBA.
	maxSourcePixels = 36_000_000

	// maxSourceBytes bounds what is read from the store, so a file that lies
	// about its length cannot be streamed indefinitely.
	maxSourceBytes = 64 << 20

	// thumbnailMaxDimension is the longest edge of a generated preview. Large
	// enough to look right on a dense display, small enough that a phone on
	// metered data is not paying for the full image.
	thumbnailMaxDimension = 512

	// thumbnailQuality trades size against artefacts. 80 is the point where
	// most images stop looking obviously compressed.
	thumbnailQuality = 80
)

// ErrNotThumbnailable means no preview can be made from this file.
//
// Not a failure: most attachments are not images, and a client shows the name,
// size and type instead. Callers distinguish this from a real error so that an
// ordinary PDF does not fill the log with warnings.
var ErrNotThumbnailable = errors.New("attachments: no preview can be made for this type")

// ErrSourceTooLarge means the image declared more pixels than will be decoded.
var ErrSourceTooLarge = errors.New("attachments: image is too large to preview")

// Thumbnail is a generated preview.
type Thumbnail struct {
	Data        []byte
	ContentType string
	Width       int
	Height      int
}

// CanThumbnail reports whether a preview can be attempted for a media type.
//
// SVG is excluded deliberately. Rendering it means running an XML parser over
// hostile input to produce an image, which is the kind of work this design
// keeps out of the server; it is served as an opaque download instead.
func CanThumbnail(contentType string) bool {
	switch contentType {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

// GenerateThumbnail makes a preview from an image.
//
// The reader is consumed. A file that cannot be previewed — because of its
// type, its dimensions, or because it is malformed — produces an error rather
// than a partial result, and the caller stores the attachment without one.
func GenerateThumbnail(r io.Reader, contentType string) (*Thumbnail, error) {
	if !CanThumbnail(contentType) {
		return nil, ErrNotThumbnailable
	}

	// Bounded before anything else, so a store that returns more than it should
	// cannot make this read for ever.
	source, err := io.ReadAll(io.LimitReader(r, maxSourceBytes+1))
	if err != nil {
		return nil, fmt.Errorf("attachments: reading the image: %w", err)
	}
	if len(source) > maxSourceBytes {
		return nil, ErrSourceTooLarge
	}

	// The header alone, before a single pixel is allocated. This is what stops
	// a decompression bomb: a file claiming 50,000 by 50,000 is refused here,
	// having cost only the bytes of its header.
	config, _, err := image.DecodeConfig(bytes.NewReader(source))
	if err != nil {
		return nil, fmt.Errorf("attachments: reading the image header: %w", err)
	}
	if config.Width <= 0 || config.Height <= 0 {
		return nil, fmt.Errorf("attachments: image declares %dx%d", config.Width, config.Height)
	}
	// Multiplied as int64 so the product cannot overflow into a value that
	// passes the check.
	if int64(config.Width)*int64(config.Height) > maxSourcePixels {
		return nil, ErrSourceTooLarge
	}

	decoded, _, err := image.Decode(bytes.NewReader(source))
	if err != nil {
		return nil, fmt.Errorf("attachments: decoding the image: %w", err)
	}

	width, height := fitWithin(
		decoded.Bounds().Dx(), decoded.Bounds().Dy(), thumbnailMaxDimension,
	)
	reduced := resize(decoded, width, height)

	// PNG for images that may be transparent, JPEG otherwise: a photograph
	// stored as PNG is several times larger for no visible gain, and a logo
	// stored as JPEG loses its transparency and gains artefacts.
	var encoded bytes.Buffer
	outputType := "image/jpeg"

	if hasAlpha(contentType) {
		if err := png.Encode(&encoded, reduced); err != nil {
			return nil, fmt.Errorf("attachments: encoding the preview: %w", err)
		}
		outputType = "image/png"
	} else if err := jpeg.Encode(&encoded, reduced, &jpeg.Options{Quality: thumbnailQuality}); err != nil {
		return nil, fmt.Errorf("attachments: encoding the preview: %w", err)
	}

	return &Thumbnail{
		Data:        encoded.Bytes(),
		ContentType: outputType,
		Width:       width,
		Height:      height,
	}, nil
}

// hasAlpha reports whether a source type can carry transparency.
func hasAlpha(contentType string) bool {
	return contentType == "image/png" || contentType == "image/gif" || contentType == "image/webp"
}

// fitWithin scales dimensions to fit a square of the given size, preserving the
// aspect ratio and never enlarging.
//
// An image already smaller than the bound is left alone: scaling it up would
// produce a larger file that looks worse.
func fitWithin(width, height, bound int) (int, int) {
	if width <= bound && height <= bound {
		return width, height
	}

	if width >= height {
		scaled := height * bound / width
		if scaled < 1 {
			scaled = 1
		}
		return bound, scaled
	}

	scaled := width * bound / height
	if scaled < 1 {
		scaled = 1
	}
	return scaled, bound
}

// resize produces a reduced copy of an image.
//
// Box sampling: each destination pixel averages the source pixels it covers.
// Nearest-neighbour is faster but produces visible aliasing on the fine detail
// that screenshots and text are full of, which is most of what gets shared in a
// developer community. A full Lanczos filter would look marginally better and
// cost considerably more, on a server that may be doing this while also serving
// a conversation.
func resize(source image.Image, width, height int) image.Image {
	bounds := source.Bounds()
	if bounds.Dx() == width && bounds.Dy() == height {
		// Already the right size; still converted to RGBA so the encoder gets a
		// concrete type it handles well.
		converted := image.NewRGBA(image.Rect(0, 0, width, height))
		draw.Draw(converted, converted.Bounds(), source, bounds.Min, draw.Src)
		return converted
	}

	destination := image.NewRGBA(image.Rect(0, 0, width, height))

	xRatio := float64(bounds.Dx()) / float64(width)
	yRatio := float64(bounds.Dy()) / float64(height)

	for y := 0; y < height; y++ {
		sourceTop := bounds.Min.Y + int(float64(y)*yRatio)
		sourceBottom := bounds.Min.Y + int(float64(y+1)*yRatio)
		if sourceBottom <= sourceTop {
			sourceBottom = sourceTop + 1
		}

		for x := 0; x < width; x++ {
			sourceLeft := bounds.Min.X + int(float64(x)*xRatio)
			sourceRight := bounds.Min.X + int(float64(x+1)*xRatio)
			if sourceRight <= sourceLeft {
				sourceRight = sourceLeft + 1
			}

			var red, green, blue, alpha, count uint64
			for sy := sourceTop; sy < sourceBottom && sy < bounds.Max.Y; sy++ {
				for sx := sourceLeft; sx < sourceRight && sx < bounds.Max.X; sx++ {
					r, g, b, a := source.At(sx, sy).RGBA()
					red += uint64(r)
					green += uint64(g)
					blue += uint64(b)
					alpha += uint64(a)
					count++
				}
			}
			if count == 0 {
				continue
			}

			// RGBA() returns 16-bit values; shifting by 8 brings them back to
			// the 8-bit channels the destination holds.
			offset := destination.PixOffset(x, y)
			destination.Pix[offset+0] = uint8(red / count >> 8)
			destination.Pix[offset+1] = uint8(green / count >> 8)
			destination.Pix[offset+2] = uint8(blue / count >> 8)
			destination.Pix[offset+3] = uint8(alpha / count >> 8)
		}
	}

	return destination
}
