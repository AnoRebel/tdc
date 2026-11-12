package attachments

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// checkerboard is an image with detail at the pixel level, which is exactly
// what a placeholder must not preserve.
func checkerboard(width, height, square int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	for y := range height {
		for x := range width {
			shade := color.RGBA{R: 20, G: 20, B: 20, A: 255}
			if (x/square+y/square)%2 == 0 {
				shade = color.RGBA{R: 235, G: 235, B: 235, A: 255}
			}
			img.Set(x, y, shade)
		}
	}

	var out bytes.Buffer
	_ = png.Encode(&out, img)
	return out.Bytes()
}

// decodeImage reads bytes back for inspection.
func decodeImage(t *testing.T, data []byte) image.Image {
	t.Helper()

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decoding: %v", err)
	}
	return img
}

/*
TestDetailIsDestroyedNotHidden is the whole point of doing this on the server.

A CSS blur hands the browser the real image and asks it to draw it out of
focus, which anybody undoes in the developer tools. This has to send something
from which the detail cannot be recovered, because it was never there.

A fine checkerboard is the strongest case: every pair of neighbouring pixels
differs as much as pixels can. If that survives, nothing is being destroyed.
*/
func TestDetailIsDestroyedNotHidden(t *testing.T) {
	obscured, err := GenerateObscured(bytes.NewReader(checkerboard(320, 320, 2)))
	if err != nil {
		t.Fatalf("GenerateObscured: %v", err)
	}

	img := decodeImage(t, obscured)
	bounds := img.Bounds()

	// Averaging a fine checkerboard leaves mid grey everywhere. Anything still
	// near black or white means squares survived the reduction.
	extremes := 0
	total := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, _, _, _ := img.At(x, y).RGBA()
			level := r >> 8
			total++
			if level < 40 || level > 215 {
				extremes++
			}
		}
	}

	if total == 0 {
		t.Fatal("the placeholder has no pixels")
	}
	if share := float64(extremes) / float64(total); share > 0.05 {
		t.Errorf("%.0f%% of the placeholder is still near black or white; the "+
			"detail survived the reduction", share*100)
	}
}

/*
TestTheShapeSurvives is what makes the placeholder useful rather than
decorative.

Somebody should see roughly what is there — the colours, the arrangement —
enough to decide whether to look. A uniform grey rectangle would obscure
perfectly and tell them nothing.
*/
func TestTheShapeSurvives(t *testing.T) {
	// Left half red, right half blue: coarse structure that should survive.
	img := image.NewRGBA(image.Rect(0, 0, 320, 160))
	for y := range 160 {
		for x := range 320 {
			if x < 160 {
				img.Set(x, y, color.RGBA{R: 220, A: 255})
			} else {
				img.Set(x, y, color.RGBA{B: 220, A: 255})
			}
		}
	}
	var source bytes.Buffer
	if err := png.Encode(&source, img); err != nil {
		t.Fatalf("encoding: %v", err)
	}

	obscured, err := GenerateObscured(bytes.NewReader(source.Bytes()))
	if err != nil {
		t.Fatalf("GenerateObscured: %v", err)
	}

	result := decodeImage(t, obscured)
	bounds := result.Bounds()

	leftRed, _, _, _ := result.At(bounds.Dx()/8, bounds.Dy()/2).RGBA()
	_, _, rightBlue, _ := result.At(bounds.Dx()*7/8, bounds.Dy()/2).RGBA()

	if leftRed>>8 < 120 {
		t.Errorf("the left side lost its colour: %d", leftRed>>8)
	}
	if rightBlue>>8 < 120 {
		t.Errorf("the right side lost its colour: %d", rightBlue>>8)
	}
}

// TestProportionsAreKept stops the conversation jumping when media is
// revealed.
func TestProportionsAreKept(t *testing.T) {
	obscured, err := GenerateObscured(bytes.NewReader(checkerboard(400, 200, 8)))
	if err != nil {
		t.Fatalf("GenerateObscured: %v", err)
	}

	bounds := decodeImage(t, obscured).Bounds()
	ratio := float64(bounds.Dx()) / float64(bounds.Dy())

	if ratio < 1.8 || ratio > 2.2 {
		t.Errorf("the placeholder is %dx%d, a ratio of %.2f; the original was 2.00",
			bounds.Dx(), bounds.Dy(), ratio)
	}
}

/*
TestThePlaceholderIsSmall keeps this cheap to send.

It is served in place of a preview to everybody who has not asked to see the
media, so a placeholder heavier than what it replaces would make marking
something sensitive cost the conversation more than not marking it.
*/
func TestThePlaceholderIsSmall(t *testing.T) {
	obscured, err := GenerateObscured(bytes.NewReader(checkerboard(1200, 800, 3)))
	if err != nil {
		t.Fatalf("GenerateObscured: %v", err)
	}

	// Sixteen pixels across of averaged colour compresses to very little.
	if len(obscured) > 20*1024 {
		t.Errorf("the placeholder is %d bytes, which is more than it should need",
			len(obscured))
	}
}

// TestSomethingThatIsNotAnImageIsRefused keeps a decoder failure from becoming
// a served file.
func TestSomethingThatIsNotAnImageIsRefused(t *testing.T) {
	if _, err := GenerateObscured(bytes.NewReader([]byte("not an image"))); err == nil {
		t.Error("bytes that are not an image produced a placeholder")
	}
}
