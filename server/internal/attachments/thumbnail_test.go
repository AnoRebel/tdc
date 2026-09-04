package attachments_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"runtime"
	"testing"
	"time"

	"github.com/AnoRebel/tdc/server/internal/attachments"
)

// drawTestImage makes a picture with actual detail in it, so that scaling has
// something to average and a broken resize is visible in the output.
func drawTestImage(width, height int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{
				R: uint8(x % 256),
				G: uint8(y % 256),
				B: uint8((x + y) % 256),
				A: 255,
			})
		}
	}
	return img
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatalf("encoding the test image: %v", err)
	}
	return buffer.Bytes()
}

func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, img, nil); err != nil {
		t.Fatalf("encoding the test image: %v", err)
	}
	return buffer.Bytes()
}

// TestThumbnailReducesLargeImages checks the ordinary case.
func TestThumbnailReducesLargeImages(t *testing.T) {
	source := encodePNG(t, drawTestImage(2000, 1500))

	thumbnail, err := attachments.GenerateThumbnail(bytes.NewReader(source), "image/png")
	if err != nil {
		t.Fatalf("generating the preview: %v", err)
	}

	if thumbnail.Width > 512 || thumbnail.Height > 512 {
		t.Errorf("the preview is %dx%d, beyond the 512 bound",
			thumbnail.Width, thumbnail.Height)
	}
	// 2000x1500 is 4:3, so the longest edge should be the width.
	if thumbnail.Width != 512 {
		t.Errorf("the longest edge is %d, expected 512", thumbnail.Width)
	}
	if thumbnail.Height != 384 {
		t.Errorf("the height is %d; 2000x1500 scaled to 512 wide is 384", thumbnail.Height)
	}
	if len(thumbnail.Data) == 0 {
		t.Error("the preview carries no data")
	}
	if len(thumbnail.Data) >= len(source) {
		t.Errorf("the preview is %d bytes against a %d-byte source, so it saves nothing",
			len(thumbnail.Data), len(source))
	}

	// It must be a real image, not merely bytes.
	if _, _, err := image.Decode(bytes.NewReader(thumbnail.Data)); err != nil {
		t.Errorf("the preview does not decode: %v", err)
	}
}

// TestThumbnailDoesNotEnlarge checks that a small image is left alone.
//
// Scaling up produces a larger file that looks worse, which is the opposite of
// what a preview is for.
func TestThumbnailDoesNotEnlarge(t *testing.T) {
	source := encodePNG(t, drawTestImage(64, 48))

	thumbnail, err := attachments.GenerateThumbnail(bytes.NewReader(source), "image/png")
	if err != nil {
		t.Fatalf("generating the preview: %v", err)
	}

	if thumbnail.Width != 64 || thumbnail.Height != 48 {
		t.Errorf("a 64x48 image became %dx%d", thumbnail.Width, thumbnail.Height)
	}
}

// TestThumbnailPreservesAspectRatio checks several shapes, including extreme
// ones that would divide to zero under a careless implementation.
func TestThumbnailPreservesAspectRatio(t *testing.T) {
	for _, shape := range []struct{ width, height, wantW, wantH int }{
		{1024, 1024, 512, 512}, // square
		{1024, 512, 512, 256},  // landscape
		{512, 1024, 256, 512},  // portrait
		{4096, 16, 512, 2},     // a very wide banner
		{16, 4096, 2, 512},     // and its transpose
	} {
		source := encodePNG(t, drawTestImage(shape.width, shape.height))

		thumbnail, err := attachments.GenerateThumbnail(bytes.NewReader(source), "image/png")
		if err != nil {
			t.Errorf("%dx%d: %v", shape.width, shape.height, err)
			continue
		}

		if thumbnail.Width != shape.wantW || thumbnail.Height != shape.wantH {
			t.Errorf("%dx%d became %dx%d, expected %dx%d",
				shape.width, shape.height,
				thumbnail.Width, thumbnail.Height,
				shape.wantW, shape.wantH)
		}
	}
}

// bombPNG builds a small file that claims enormous dimensions.
//
// This is the attack the pixel cap exists for: the header says 50,000 by
// 50,000, which is 2.5 billion pixels and 10GB once decoded, while the file
// itself is a few dozen bytes. A server that decodes first and checks afterwards
// has already lost by the time it has anything to check.
func bombPNG(width, height uint32) []byte {
	var file bytes.Buffer

	file.Write([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A})

	var header bytes.Buffer
	header.WriteString("IHDR")
	binary.Write(&header, binary.BigEndian, width)
	binary.Write(&header, binary.BigEndian, height)
	header.Write([]byte{8, 6, 0, 0, 0}) // 8-bit RGBA, no interlace

	binary.Write(&file, binary.BigEndian, uint32(header.Len()-4))
	file.Write(header.Bytes())
	binary.Write(&file, binary.BigEndian, crc32.ChecksumIEEE(header.Bytes()))

	return file.Bytes()
}

// TestDecompressionBombIsRefusedCheaply is the important one.
//
// A file declaring 50,000 by 50,000 must be refused from its header alone,
// without allocating the 10GB it describes. The test asserts both halves: that
// it is refused, and that refusing it costs almost nothing.
func TestDecompressionBombIsRefusedCheaply(t *testing.T) {
	bomb := bombPNG(50_000, 50_000)

	if len(bomb) > 200 {
		t.Fatalf("the test's bomb is %d bytes; it should be tiny", len(bomb))
	}

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	started := time.Now()
	_, err := attachments.GenerateThumbnail(bytes.NewReader(bomb), "image/png")
	elapsed := time.Since(started)

	runtime.ReadMemStats(&after)

	if err == nil {
		t.Fatal("a 2.5-gigapixel image was accepted for preview")
	}
	if !errors.Is(err, attachments.ErrSourceTooLarge) {
		t.Errorf("refused with %v, expected ErrSourceTooLarge", err)
	}

	// The refusal must come from the header. Anything approaching the declared
	// size means the check ran after decoding, which is no check at all.
	allocated := after.TotalAlloc - before.TotalAlloc
	if allocated > 50<<20 {
		t.Errorf("refusing the bomb allocated %d MB; it must be refused from its header",
			allocated>>20)
	}
	if elapsed > 2*time.Second {
		t.Errorf("refusing the bomb took %v", elapsed)
	}
}

// TestMalformedFilesFailCleanly checks that broken input produces an error
// rather than a panic or a hang.
//
// A panic in a preview would take down the request that triggered it and,
// depending on where it happened, the connection serving other people.
func TestMalformedFilesFailCleanly(t *testing.T) {
	valid := encodePNG(t, drawTestImage(100, 100))

	for name, content := range map[string][]byte{
		"empty":            {},
		"header only":      valid[:8],
		"truncated":        valid[:len(valid)/2],
		"header then junk": append(append([]byte{}, valid[:33]...), bytes.Repeat([]byte{0xFF}, 500)...),
		"not an image":     []byte("habari ya asubuhi, hii si picha"),
		"zero dimensions":  bombPNG(0, 0),
		"one dimension":    bombPNG(1000, 0),
	} {
		t.Run(name, func(t *testing.T) {
			// A panic here fails the test rather than the process.
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("previewing %s panicked: %v", name, recovered)
				}
			}()

			done := make(chan struct{})
			go func() {
				defer close(done)
				attachments.GenerateThumbnail(bytes.NewReader(content), "image/png")
			}()

			select {
			case <-done:
				// Whatever it returned, it returned.
			case <-time.After(5 * time.Second):
				t.Fatalf("previewing %s did not finish", name)
			}
		})
	}
}

// TestUnpreviewableTypesAreReportedNotFailed checks that an ordinary PDF is not
// treated as an error.
//
// Most attachments are not images. If those produced errors, the log would be
// full of them and a real failure would be invisible.
func TestUnpreviewableTypesAreReportedNotFailed(t *testing.T) {
	for _, contentType := range []string{
		"application/pdf",
		"video/mp4",
		"text/plain",
		"application/zip",
		"image/svg+xml", // deliberately excluded: rendering it means parsing hostile XML
	} {
		if attachments.CanThumbnail(contentType) {
			t.Errorf("%q is reported as previewable", contentType)
		}

		_, err := attachments.GenerateThumbnail(bytes.NewReader([]byte("x")), contentType)
		if !errors.Is(err, attachments.ErrNotThumbnailable) {
			t.Errorf("%q gave %v, expected ErrNotThumbnailable", contentType, err)
		}
	}

	for _, contentType := range []string{"image/png", "image/jpeg", "image/gif", "image/webp"} {
		if !attachments.CanThumbnail(contentType) {
			t.Errorf("%q is not reported as previewable", contentType)
		}
	}
}

// TestJPEGSourceStaysJPEG checks the output format choice.
//
// A photograph re-encoded as PNG is several times larger for no visible gain,
// which defeats the purpose on a metered connection.
func TestJPEGSourceStaysJPEG(t *testing.T) {
	source := encodeJPEG(t, drawTestImage(1024, 768))

	thumbnail, err := attachments.GenerateThumbnail(bytes.NewReader(source), "image/jpeg")
	if err != nil {
		t.Fatalf("generating the preview: %v", err)
	}

	if thumbnail.ContentType != "image/jpeg" {
		t.Errorf("a JPEG preview is %q", thumbnail.ContentType)
	}
}

// TestTransparencyIsPreserved checks that a logo does not gain a black
// background, which is what re-encoding transparent PNG as JPEG produces.
func TestTransparencyIsPreserved(t *testing.T) {
	transparent := image.NewRGBA(image.Rect(0, 0, 800, 800))
	for y := 200; y < 600; y++ {
		for x := 200; x < 600; x++ {
			transparent.Set(x, y, color.RGBA{R: 255, A: 255})
		}
	}

	thumbnail, err := attachments.GenerateThumbnail(
		bytes.NewReader(encodePNG(t, transparent)), "image/png")
	if err != nil {
		t.Fatalf("generating the preview: %v", err)
	}

	if thumbnail.ContentType != "image/png" {
		t.Fatalf("a transparent source produced %q, which cannot carry transparency",
			thumbnail.ContentType)
	}

	decoded, _, err := image.Decode(bytes.NewReader(thumbnail.Data))
	if err != nil {
		t.Fatalf("decoding the preview: %v", err)
	}

	// The corner was transparent in the source and must still be.
	if _, _, _, alpha := decoded.At(2, 2).RGBA(); alpha > 0x2000 {
		t.Errorf("the transparent corner has alpha %d; transparency was lost", alpha)
	}
}
