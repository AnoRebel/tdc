package attachments_test

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"testing"

	"github.com/AnoRebel/tdc/server/internal/attachments"
)

// stillGIF is a single-frame GIF, which is what a screenshot saved as one is.
func stillGIF(t *testing.T) []byte {
	t.Helper()

	frame := image.NewPaletted(image.Rect(0, 0, 4, 4),
		color.Palette{color.Black, color.White})

	var buffer bytes.Buffer
	if err := gif.Encode(&buffer, frame, nil); err != nil {
		t.Fatalf("encoding a still gif: %v", err)
	}
	return buffer.Bytes()
}

// animatedGIF is a two-frame GIF, built with the standard library so the test
// exercises the same structure a real encoder produces.
func animatedGIF(t *testing.T) []byte {
	t.Helper()

	palette := color.Palette{color.Black, color.White}
	frames := []*image.Paletted{
		image.NewPaletted(image.Rect(0, 0, 4, 4), palette),
		image.NewPaletted(image.Rect(0, 0, 4, 4), palette),
	}
	frames[1].SetColorIndex(1, 1, 1)

	var buffer bytes.Buffer
	err := gif.EncodeAll(&buffer, &gif.GIF{
		Image: frames,
		Delay: []int{10, 10},
	})
	if err != nil {
		t.Fatalf("encoding an animated gif: %v", err)
	}
	return buffer.Bytes()
}

func stillPNG(t *testing.T) []byte {
	t.Helper()

	var buffer bytes.Buffer
	if err := png.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatalf("encoding a png: %v", err)
	}
	return buffer.Bytes()
}

/*
TestAnimationIsRecognised covers the distinction the whole feature turns on.

Getting this wrong in either direction is visible: a still image reported as
animated loses its preview and downloads in full to draw a thumbnail, and an
animation reported as still is shown frozen.
*/
func TestAnimationIsRecognised(t *testing.T) {
	tests := []struct {
		name        string
		content     []byte
		contentType string
		animated    bool
	}{
		{"an animated gif", animatedGIF(t), "image/gif", true},
		{"a single-frame gif", stillGIF(t), "image/gif", false},
		{"a png", stillPNG(t), "image/png", false},
		{"a jpeg is never animated", []byte{0xFF, 0xD8, 0xFF}, "image/jpeg", false},
		{"empty", nil, "image/gif", false},
		{"not an image at all", []byte("this is plain text"), "image/gif", false},
		{"a truncated gif header", []byte("GIF89a"), "image/gif", false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := attachments.IsAnimated(bytes.NewReader(test.content), test.contentType)
			if got != test.animated {
				t.Errorf("IsAnimated = %v, want %v", got, test.animated)
			}
		})
	}
}

/*
TestGifFrameCountingWalksBlocks is the reason frames are counted by walking the
structure rather than by searching for the descriptor byte.

0x2C occurs freely inside compressed pixel data. A still image whose pixels
happen to contain it would be reported as animated by any search, which is why
this fills a frame with varied content rather than a flat colour.
*/
func TestGifFrameCountingWalksBlocks(t *testing.T) {
	palette := color.Palette{color.Black, color.White}
	frame := image.NewPaletted(image.Rect(0, 0, 64, 64), palette)

	// Varied pixels, so the compressed data is not a single run and stands a
	// real chance of containing the descriptor byte.
	for y := range 64 {
		for x := range 64 {
			frame.SetColorIndex(x, y, uint8((x*7+y*13)%2))
		}
	}

	var buffer bytes.Buffer
	if err := gif.Encode(&buffer, frame, nil); err != nil {
		t.Fatalf("encoding: %v", err)
	}

	if attachments.IsAnimated(bytes.NewReader(buffer.Bytes()), "image/gif") {
		t.Error("a single-frame gif was reported as animated")
	}
}

// TestAnimatedWebPIsRecognised covers the ANIM chunk.
func TestAnimatedWebPIsRecognised(t *testing.T) {
	// A RIFF container declaring WEBP with an ANIM chunk, which is what an
	// animated WebP is; the pixel data is irrelevant to the question.
	animated := []byte("RIFF")
	animated = append(animated, 0x1C, 0x00, 0x00, 0x00)
	animated = append(animated, "WEBPVP8X"...)
	animated = append(animated, 0x0A, 0x00, 0x00, 0x00)
	animated = append(animated, make([]byte, 10)...)
	animated = append(animated, "ANIM"...)

	if !attachments.IsAnimated(bytes.NewReader(animated), "image/webp") {
		t.Error("an animated webp was not recognised")
	}

	still := []byte("RIFF")
	still = append(still, 0x10, 0x00, 0x00, 0x00)
	still = append(still, "WEBPVP8 "...)
	still = append(still, make([]byte, 8)...)

	if attachments.IsAnimated(bytes.NewReader(still), "image/webp") {
		t.Error("a still webp was reported as animated")
	}
}
