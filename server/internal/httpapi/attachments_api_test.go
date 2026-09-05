package httpapi_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Real magic bytes, so detection sees what a real file would look like.
var (
	testPNG = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 13, 'I', 'H', 'D', 'R'}
	testELF = []byte{0x7F, 'E', 'L', 'F', 2, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2, 0, 0x3E, 0}
	testSVG = []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)

	// A two-frame animated GIF, written out by hand so the test does not
	// depend on an image library being present. Its Netscape block is what
	// makes it loop, and the two image descriptors are what make it animated.
	testAnimatedGIF = []byte{
		0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x01, 0x00, 0x01, 0x00, 0xF0, 0x00,
		0x00, 0x00, 0x00, 0x00, 0xFF, 0xFF, 0xFF, 0x21, 0xFF, 0x0B, 0x4E, 0x45,
		0x54, 0x53, 0x43, 0x41, 0x50, 0x45, 0x32, 0x2E, 0x30, 0x03, 0x01, 0x00,
		0x00, 0x00, 0x21, 0xF9, 0x04, 0x00, 0x32, 0x00, 0x00, 0x00, 0x2C, 0x00,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x02, 0x02, 0x44, 0x00,
		0x00, 0x21, 0xF9, 0x04, 0x00, 0x32, 0x00, 0x00, 0x00, 0x2C, 0x00, 0x00,
		0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x02, 0x02, 0x44, 0x01, 0x00,
		0x3B,
	}
)

// uploadFile posts a file to a room the way a browser would.
func uploadFile(
	t *testing.T,
	handler http.Handler,
	token, roomID, filename string,
	content []byte,
) *httptest.ResponseRecorder {
	t.Helper()

	var body bytes.Buffer
	form := multipart.NewWriter(&body)

	part, err := form.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("building the upload form: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("writing the upload body: %v", err)
	}
	if err := form.Close(); err != nil {
		t.Fatalf("closing the upload form: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/rooms/"+roomID+"/attachments", &body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	if token != "" {
		request.AddCookie(&http.Cookie{Name: "tdc_session", Value: token})
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

// TestUploadAndDownloadRoundTrip covers the ordinary case end to end.
func TestUploadAndDownloadRoundTrip(t *testing.T) {
	handler := newTestAPI(t)

	token := completeSetup(t, handler)
	roomID := makeRoom(t, handler, token, "general", "public")

	recorder := uploadFile(t, handler, token, roomID, "screenshot.png", testPNG)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("upload returned %d: %s", recorder.Code, recorder.Body)
	}

	var uploaded struct {
		ID          string `json:"id"`
		Filename    string `json:"filename"`
		ContentType string `json:"contentType"`
		Size        int64  `json:"size"`
		URL         string `json:"url"`
		Renderable  bool   `json:"renderable"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &uploaded); err != nil {
		t.Fatalf("decoding the upload response: %v", err)
	}

	if uploaded.ContentType != "image/png" {
		t.Errorf("content type is %q, expected image/png", uploaded.ContentType)
	}
	if uploaded.Size != int64(len(testPNG)) {
		t.Errorf("size is %d, uploaded %d bytes", uploaded.Size, len(testPNG))
	}
	if !uploaded.Renderable {
		t.Error("a PNG is not reported as renderable")
	}

	download := call(t, handler, http.MethodGet, uploaded.URL, nil, token)
	if download.Code != http.StatusOK {
		t.Fatalf("download returned %d: %s", download.Code, download.Body)
	}
	if !bytes.Equal(download.Body.Bytes(), testPNG) {
		t.Error("the downloaded bytes differ from what was uploaded")
	}
}

// TestUploadDetectsTypeFromContent is the "declared type is not trusted"
// requirement, exercised through the API rather than the unit under it.
func TestUploadDetectsTypeFromContent(t *testing.T) {
	handler := newTestAPI(t)

	token := completeSetup(t, handler)
	roomID := makeRoom(t, handler, token, "general", "public")

	// A PNG, uploaded under a name that claims it is something else entirely.
	recorder := uploadFile(t, handler, token, roomID, "invoice.pdf", testPNG)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("upload returned %d: %s", recorder.Code, recorder.Body)
	}

	var uploaded struct {
		ContentType string `json:"contentType"`
		Filename    string `json:"filename"`
	}
	json.Unmarshal(recorder.Body.Bytes(), &uploaded)

	if uploaded.ContentType != "image/png" {
		t.Errorf("the server believed the filename: type is %q, content is a PNG",
			uploaded.ContentType)
	}
	// The name is still kept, because it is what the download is called.
	if uploaded.Filename != "invoice.pdf" {
		t.Errorf("the filename was changed to %q", uploaded.Filename)
	}
}

// TestBlockedTypeIsRefused covers the default protection against binaries.
func TestBlockedTypeIsRefused(t *testing.T) {
	handler := newTestAPI(t)

	token := completeSetup(t, handler)
	roomID := makeRoom(t, handler, token, "general", "public")

	// Named as an image, but an ELF binary inside.
	recorder := uploadFile(t, handler, token, roomID, "kitten.png", testELF)

	if recorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("an executable upload returned %d, expected 415: %s",
			recorder.Code, recorder.Body)
	}
	// The refusal has to say what was wrong, or the person cannot act on it.
	if !strings.Contains(recorder.Body.String(), "x-executable") {
		t.Errorf("the refusal does not name the detected type: %s", recorder.Body)
	}
}

// TestAttachmentsAreServedInertly is the second half of the same protection.
//
// An SVG carrying script, served from the application's origin with its real
// type, would run that script with the application's cookies available. This
// checks the headers that stop it.
func TestAttachmentsAreServedInertly(t *testing.T) {
	handler := newTestAPI(t)

	token := completeSetup(t, handler)
	roomID := makeRoom(t, handler, token, "general", "public")

	recorder := uploadFile(t, handler, token, roomID, "diagram.svg", testSVG)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("upload returned %d: %s", recorder.Code, recorder.Body)
	}

	var uploaded struct {
		URL         string `json:"url"`
		ContentType string `json:"contentType"`
		Renderable  bool   `json:"renderable"`
	}
	json.Unmarshal(recorder.Body.Bytes(), &uploaded)

	// Stored as what it is.
	if uploaded.ContentType != "image/svg+xml" {
		t.Errorf("the SVG was stored as %q", uploaded.ContentType)
	}
	// But never offered for inline display.
	if uploaded.Renderable {
		t.Error("the SVG is reported as renderable")
	}

	download := call(t, handler, http.MethodGet, uploaded.URL, nil, token)
	if download.Code != http.StatusOK {
		t.Fatalf("download returned %d", download.Code)
	}

	header := download.Header()

	// Served as something no browser will execute.
	if served := header.Get("Content-Type"); !strings.HasPrefix(served, "application/octet-stream") {
		t.Errorf("the SVG is served as %q; a browser would render it", served)
	}
	if header.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("nosniff is missing, so a browser may guess the type back")
	}
	if disposition := header.Get("Content-Disposition"); !strings.HasPrefix(disposition, "attachment") {
		t.Errorf("Content-Disposition is %q, so this renders as a page", disposition)
	}
	if policy := header.Get("Content-Security-Policy"); !strings.Contains(policy, "sandbox") {
		t.Errorf("the policy does not sandbox the content: %q", policy)
	}
	if header.Get("X-Frame-Options") != "DENY" {
		t.Error("the attachment can be framed back into the application")
	}
	if cache := header.Get("Cache-Control"); !strings.Contains(cache, "no-store") {
		t.Errorf("a private attachment is cacheable: %q", cache)
	}
}

// TestNonMemberCannotUploadOrDownload checks that the room's permissions govern
// its files.
//
// Knowing an identifier is not authorisation: an attachment must not be a way
// to read a room, and a room must not be usable as storage by someone who
// cannot post to it.
func TestNonMemberCannotUploadOrDownload(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "private-plans", "public")

	recorder := uploadFile(t, handler, owner, roomID, "notes.png", testPNG)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("the owner's upload returned %d: %s", recorder.Code, recorder.Body)
	}

	var uploaded struct {
		URL string `json:"url"`
	}
	json.Unmarshal(recorder.Body.Bytes(), &uploaded)

	// A second person, who is not in that room.
	outsider := registerAndSignIn(t, handler, "juma")

	download := call(t, handler, http.MethodGet, uploaded.URL, nil, outsider)
	if download.Code == http.StatusOK {
		t.Error("a non-member downloaded the attachment")
	}

	upload := uploadFile(t, handler, outsider, roomID, "theirs.png", testPNG)
	if upload.Code == http.StatusCreated {
		t.Error("a non-member uploaded to a room they are not in")
	}
}

// TestUnauthenticatedAccessIsRefused checks the routes are behind the session
// middleware at all — the kind of thing a unit test on the handler cannot see.
func TestUnauthenticatedAccessIsRefused(t *testing.T) {
	handler := newTestAPI(t)

	token := completeSetup(t, handler)
	roomID := makeRoom(t, handler, token, "general", "public")

	recorder := uploadFile(t, handler, token, roomID, "notes.png", testPNG)
	var uploaded struct {
		URL string `json:"url"`
	}
	json.Unmarshal(recorder.Body.Bytes(), &uploaded)

	// No cookie at all.
	if anonymous := call(t, handler, http.MethodGet, uploaded.URL, nil, ""); anonymous.Code != http.StatusUnauthorized {
		t.Errorf("an unauthenticated download returned %d, expected 401", anonymous.Code)
	}
	if anonymous := uploadFile(t, handler, "", roomID, "x.png", testPNG); anonymous.Code != http.StatusUnauthorized {
		t.Errorf("an unauthenticated upload returned %d, expected 401", anonymous.Code)
	}
}

// TestOversizedUploadNamesTheLimit checks that a refusal is specific.
func TestOversizedUploadNamesTheLimit(t *testing.T) {
	handler := newTestAPI(t)

	token := completeSetup(t, handler)
	roomID := makeRoom(t, handler, token, "general", "public")

	// Past the 25 MiB default, built from a real PNG header so the file is
	// refused for its size rather than its type.
	oversized := append(append([]byte{}, testPNG...), bytes.Repeat([]byte{0}, (25<<20)+1024)...)

	recorder := uploadFile(t, handler, token, roomID, "huge.png", oversized)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("an oversized upload returned %d, expected 413: %s",
			recorder.Code, truncate(recorder.Body.String()))
	}
	if !strings.Contains(recorder.Body.String(), "file size") {
		t.Errorf("the refusal does not name the limit: %s", truncate(recorder.Body.String()))
	}
}

// TestLimitsAreAdvertised checks that a client can learn the rules before it
// spends someone's bandwidth breaking them.
func TestLimitsAreAdvertised(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)

	recorder := call(t, handler, http.MethodGet, "/api/attachments/limits", nil, token)
	if recorder.Code != http.StatusOK {
		t.Fatalf("limits returned %d: %s", recorder.Code, recorder.Body)
	}

	var limits struct {
		MaxFileSize   int64    `json:"maxFileSize"`
		MaxPerMessage int      `json:"maxPerMessage"`
		BlockedTypes  []string `json:"blockedTypes"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &limits); err != nil {
		t.Fatalf("decoding the limits: %v", err)
	}

	if limits.MaxFileSize <= 0 {
		t.Error("the advertised file-size limit is not usable")
	}
	if limits.MaxPerMessage <= 0 {
		t.Error("the advertised per-message limit is not usable")
	}
	if len(limits.BlockedTypes) == 0 {
		t.Error("no blocked types are advertised, so a client cannot warn in advance")
	}
}

// TestFilenameIsNeutralised checks that a hostile filename cannot escape the
// header it is written into, nor imply a path.
func TestFilenameIsNeutralised(t *testing.T) {
	handler := newTestAPI(t)

	token := completeSetup(t, handler)
	roomID := makeRoom(t, handler, token, "general", "public")

	for _, hostile := range []string{
		`../../etc/passwd`,
		`..\..\windows\system32`,
		`evil".png`,
		"line\nbreak.png",
	} {
		recorder := uploadFile(t, handler, token, roomID, hostile, testPNG)
		if recorder.Code != http.StatusCreated {
			// Refusing outright is also acceptable.
			continue
		}

		var uploaded struct {
			Filename string `json:"filename"`
			URL      string `json:"url"`
		}
		json.Unmarshal(recorder.Body.Bytes(), &uploaded)

		if strings.ContainsAny(uploaded.Filename, `/\`) {
			t.Errorf("%q was stored as %q, still carrying a separator", hostile, uploaded.Filename)
		}
		if strings.ContainsAny(uploaded.Filename, "\r\n") {
			t.Errorf("%q was stored as %q, still carrying a line break", hostile, uploaded.Filename)
		}

		download := call(t, handler, http.MethodGet, uploaded.URL, nil, token)
		disposition := download.Header().Get("Content-Disposition")
		if strings.ContainsAny(disposition, "\r\n") {
			t.Errorf("%q produced a header that breaks across lines: %q", hostile, disposition)
		}
		// Images now play inline, so either disposition is correct here. What
		// matters is that the name carries no separator and no line break: those
		// are what would let a filename escape the header or imply a path.
		if !strings.HasPrefix(disposition, "attachment") && !strings.HasPrefix(disposition, "inline") {
			t.Errorf("%q produced a disposition of %q", hostile, disposition)
		}
	}
}

// truncate keeps a failure message readable when a body is large.
func truncate(s string) string {
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}

// TestImagePreviewIsGeneratedAndServed covers the preview path end to end.
//
// A conversation full of photographs is unusable on metered mobile data if
// every one is fetched at full size, which is what previews exist to prevent.
func TestImagePreviewIsGeneratedAndServed(t *testing.T) {
	handler := newTestAPI(t)

	token := completeSetup(t, handler)
	roomID := makeRoom(t, handler, token, "general", "public")

	// Big enough that a preview is meaningfully smaller than the original.
	large := image.NewRGBA(image.Rect(0, 0, 1600, 1200))
	for y := 0; y < 1200; y++ {
		for x := 0; x < 1600; x++ {
			large.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, large); err != nil {
		t.Fatalf("encoding the test image: %v", err)
	}

	recorder := uploadFile(t, handler, token, roomID, "photo.png", encoded.Bytes())
	if recorder.Code != http.StatusCreated {
		t.Fatalf("upload returned %d: %s", recorder.Code, truncate(recorder.Body.String()))
	}

	var uploaded struct {
		URL        string `json:"url"`
		PreviewURL string `json:"previewUrl"`
		Width      int    `json:"width"`
		Height     int    `json:"height"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &uploaded); err != nil {
		t.Fatalf("decoding the upload response: %v", err)
	}

	if uploaded.PreviewURL == "" {
		t.Fatal("an image was uploaded without a preview")
	}
	// The original's dimensions let a client reserve space before it arrives.
	if uploaded.Width != 1600 || uploaded.Height != 1200 {
		t.Errorf("the original is reported as %dx%d, uploaded 1600x1200",
			uploaded.Width, uploaded.Height)
	}

	preview := call(t, handler, http.MethodGet, uploaded.PreviewURL, nil, token)
	if preview.Code != http.StatusOK {
		t.Fatalf("the preview returned %d: %s", preview.Code, truncate(preview.Body.String()))
	}

	decoded, _, err := image.Decode(bytes.NewReader(preview.Body.Bytes()))
	if err != nil {
		t.Fatalf("the preview does not decode as an image: %v", err)
	}
	if decoded.Bounds().Dx() > 512 || decoded.Bounds().Dy() > 512 {
		t.Errorf("the preview is %dx%d, beyond the bound",
			decoded.Bounds().Dx(), decoded.Bounds().Dy())
	}

	// The whole point: it must be substantially cheaper than the original.
	if preview.Body.Len() >= encoded.Len() {
		t.Errorf("the preview is %d bytes against a %d-byte original",
			preview.Body.Len(), encoded.Len())
	}

	// A preview is private content, held to the same authorisation.
	outsider := registerAndSignIn(t, handler, "juma")
	if refused := call(t, handler, http.MethodGet, uploaded.PreviewURL, nil, outsider); refused.Code == http.StatusOK {
		t.Error("a non-member fetched the preview")
	}
}

// TestUnpreviewableUploadHasNoPreviewURL checks that a file with no preview
// degrades to a plain attachment rather than a broken image.
func TestUnpreviewableUploadHasNoPreviewURL(t *testing.T) {
	handler := newTestAPI(t)

	token := completeSetup(t, handler)
	roomID := makeRoom(t, handler, token, "general", "public")

	recorder := uploadFile(t, handler, token, roomID, "notes.txt",
		[]byte("habari ya asubuhi, hii ni maandishi ya kawaida"))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("upload returned %d: %s", recorder.Code, recorder.Body)
	}

	var uploaded struct {
		PreviewURL string `json:"previewUrl"`
		Filename   string `json:"filename"`
		Size       int64  `json:"size"`
	}
	json.Unmarshal(recorder.Body.Bytes(), &uploaded)

	if uploaded.PreviewURL != "" {
		t.Errorf("a text file was given a preview at %q", uploaded.PreviewURL)
	}
	// It still carries what a client needs to display it usefully.
	if uploaded.Filename == "" || uploaded.Size == 0 {
		t.Error("an unpreviewable file lost its name or size")
	}
}

// TestMediaIsSeekableAndPlaysInline covers what a media element needs.
//
// A browser playing audio or video asks for byte ranges: it fetches the header
// to learn the duration, then seeks. Answering every range with the whole file
// leaves it unable to seek, and some browsers will not report a duration at
// all — the player then shows "0:00 of 0:00" and cannot be scrubbed.
func TestMediaIsSeekableAndPlaysInline(t *testing.T) {
	handler := newTestAPI(t)

	token := completeSetup(t, handler)
	roomID := makeRoom(t, handler, token, "general", "public")

	recorder := uploadFile(t, handler, token, roomID, "sauti.wav", testWAV())
	if recorder.Code != http.StatusCreated {
		t.Fatalf("upload returned %d: %s", recorder.Code, truncate(recorder.Body.String()))
	}

	var uploaded struct {
		URL  string `json:"url"`
		Kind string `json:"kind"`
	}
	json.Unmarshal(recorder.Body.Bytes(), &uploaded)

	if uploaded.Kind != "audio" {
		t.Errorf("a WAV was classified as %q", uploaded.Kind)
	}

	full := call(t, handler, http.MethodGet, uploaded.URL, nil, token)
	if full.Code != http.StatusOK {
		t.Fatalf("download returned %d", full.Code)
	}

	// Media plays where it stands rather than downloading.
	if disposition := full.Header().Get("Content-Disposition"); !strings.HasPrefix(disposition, "inline") {
		t.Errorf("audio is served as %q, so a browser would save it instead of playing it",
			disposition)
	}
	if full.Header().Get("Accept-Ranges") != "bytes" {
		t.Error("Accept-Ranges is missing, so a player cannot offer a seek bar")
	}

	// A sandbox directive on the response puts the resource in an opaque
	// origin, and a media element then refuses to load it at all — the request
	// hangs with no error. Found by playing a file in a browser; a unit test on
	// the headers alone would happily have asserted the stricter policy.
	policy := full.Header().Get("Content-Security-Policy")
	if strings.Contains(policy, "sandbox") {
		t.Errorf("media is served with %q; sandbox stops a player loading it", policy)
	}
	// The rest of the policy must survive, so this is still not a document.
	if !strings.Contains(policy, "default-src 'none'") {
		t.Errorf("media policy %q no longer restricts what it may load", policy)
	}
	if !strings.Contains(policy, "frame-ancestors 'none'") {
		t.Errorf("media policy %q allows framing back into the application", policy)
	}

	// And a range request must actually be honoured.
	request := httptest.NewRequest(http.MethodGet, uploaded.URL, nil)
	request.Header.Set("Range", "bytes=0-99")
	request.AddCookie(&http.Cookie{Name: "tdc_session", Value: token})

	ranged := httptest.NewRecorder()
	handler.ServeHTTP(ranged, request)

	if ranged.Code != http.StatusPartialContent {
		t.Fatalf("a range request returned %d, expected 206; seeking would not work",
			ranged.Code)
	}
	if got := ranged.Body.Len(); got != 100 {
		t.Errorf("the range returned %d bytes, asked for 100", got)
	}
	if ranged.Header().Get("Content-Range") == "" {
		t.Error("the partial response does not say which range it is")
	}
}

// TestNonMediaStillDownloads checks that making media inline did not make
// everything inline.
//
// An attachment disposition is one of the things keeping uploaded content from
// being rendered in the application's origin, so it must survive for anything
// that is not plainly media.
func TestNonMediaStillDownloads(t *testing.T) {
	handler := newTestAPI(t)

	token := completeSetup(t, handler)
	roomID := makeRoom(t, handler, token, "general", "public")

	for _, file := range []struct {
		name    string
		content []byte
	}{
		{"diagram.svg", testSVG},
		{"archive.zip", []byte{'P', 'K', 0x03, 0x04, 0x14, 0, 0, 0, 8, 0}},
	} {
		recorder := uploadFile(t, handler, token, roomID, file.name, file.content)
		if recorder.Code != http.StatusCreated {
			t.Fatalf("uploading %s returned %d", file.name, recorder.Code)
		}

		var uploaded struct {
			URL string `json:"url"`
		}
		json.Unmarshal(recorder.Body.Bytes(), &uploaded)

		download := call(t, handler, http.MethodGet, uploaded.URL, nil, token)
		disposition := download.Header().Get("Content-Disposition")
		if !strings.HasPrefix(disposition, "attachment") {
			t.Errorf("%s is served as %q; it must download", file.name, disposition)
		}

		// Anything not plainly media keeps the sandbox, which is what stops an
		// uploaded document acting in the application's origin.
		if policy := download.Header().Get("Content-Security-Policy"); !strings.Contains(policy, "sandbox") {
			t.Errorf("%s is served with %q; it must be sandboxed", file.name, policy)
		}
	}
}

// testWAV builds a real WAV, so the browser-facing behaviour is exercised
// against a file a player would actually accept.
func testWAV() []byte {
	const rate, seconds = 8000, 1
	samples := rate * seconds

	var file bytes.Buffer
	write := func(values ...any) {
		for _, value := range values {
			binary.Write(&file, binary.LittleEndian, value)
		}
	}

	file.WriteString("RIFF")
	write(uint32(36 + samples*2))
	file.WriteString("WAVEfmt ")
	write(uint32(16), uint16(1), uint16(1), uint32(rate), uint32(rate*2), uint16(2), uint16(16))
	file.WriteString("data")
	write(uint32(samples * 2))

	for i := 0; i < samples; i++ {
		write(int16(math.Sin(float64(i)/rate*440*2*math.Pi) * 8000))
	}

	return file.Bytes()
}

// TestCORSPermitsRangeRequests covers what a media element needs across
// origins.
//
// In development the app and the API are on different ports, so audio and video
// are cross-origin. A browser preflights the Range header, and a preflight that
// does not permit it makes the request stall with no error — the player never
// loads and nothing says why. The symptom is invisible until someone tries to
// play a file.
func TestCORSPermitsRangeRequests(t *testing.T) {
	handler := newTestAPIWithOrigins(t, []string{"http://localhost:3000"})

	request := httptest.NewRequest(http.MethodOptions, "/api/attachments/whatever", nil)
	request.Header.Set("Origin", "http://localhost:3000")
	request.Header.Set("Access-Control-Request-Method", http.MethodGet)
	request.Header.Set("Access-Control-Request-Headers", "range")

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	allowed := recorder.Header().Get("Access-Control-Allow-Headers")
	if !strings.Contains(strings.ToLower(allowed), "range") {
		t.Errorf("the preflight permits %q; without Range a media element stalls", allowed)
	}

	exposed := recorder.Header().Get("Access-Control-Expose-Headers")
	for _, header := range []string{"Content-Range", "Accept-Ranges"} {
		if !strings.Contains(exposed, header) {
			t.Errorf("%s is not exposed, so the player cannot see that seeking works", header)
		}
	}
}

// TestDeletingAMessageRemovesItsAttachments checks that deleting a message
// takes its files with it.
//
// Leaving them reachable would mean a deletion removed the words and kept the
// photograph, which is not what anyone deleting a message expects.
func TestDeletingAMessageRemovesItsAttachments(t *testing.T) {
	handler := newTestAPI(t)

	token := completeSetup(t, handler)
	roomID := makeRoom(t, handler, token, "general", "public")

	upload := uploadFile(t, handler, token, roomID, "siri.png", testPNG)
	if upload.Code != http.StatusCreated {
		t.Fatalf("upload returned %d", upload.Code)
	}

	var uploaded struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	json.Unmarshal(upload.Body.Bytes(), &uploaded)

	sent := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages", map[string]any{
		"body":          "Picha ya siri",
		"clientToken":   "delete-test-1",
		"attachmentIds": []string{uploaded.ID},
	}, token)
	if sent.Code != http.StatusCreated {
		t.Fatalf("sending returned %d: %s", sent.Code, truncate(sent.Body.String()))
	}

	var message struct {
		ID string `json:"id"`
	}
	json.Unmarshal(sent.Body.Bytes(), &message)

	// Reachable while the message stands.
	if before := call(t, handler, http.MethodGet, uploaded.URL, nil, token); before.Code != http.StatusOK {
		t.Fatalf("the attachment is not reachable before deletion: %d", before.Code)
	}

	deleted := call(t, handler, http.MethodDelete,
		"/api/rooms/"+roomID+"/messages/"+message.ID, nil, token)
	if deleted.Code != http.StatusCreated {
		t.Fatalf("deleting returned %d: %s", deleted.Code, truncate(deleted.Body.String()))
	}

	if after := call(t, handler, http.MethodGet, uploaded.URL, nil, token); after.Code == http.StatusOK {
		t.Error("the attachment is still reachable after its message was deleted")
	}
}

// TestSharedStorageSurvivesOneDeletion is the case content-addressed storage
// makes subtle.
//
// Two people uploading the same file share one stored object, because the key
// is derived from the content. Deleting one person's attachment must not remove
// the bytes the other still points at — a reference count is what makes that
// safe, and without it the second person's file would vanish silently.
func TestSharedStorageSurvivesOneDeletion(t *testing.T) {
	handler := newTestAPI(t)

	owner := completeSetup(t, handler)
	roomID := makeRoom(t, handler, owner, "general", "public")

	// The same bytes, uploaded twice. Both land on one stored object.
	first := uploadFile(t, handler, owner, roomID, "kwanza.png", testPNG)
	second := uploadFile(t, handler, owner, roomID, "pili.png", testPNG)
	if first.Code != http.StatusCreated || second.Code != http.StatusCreated {
		t.Fatalf("uploads returned %d and %d", first.Code, second.Code)
	}

	var a, b struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	json.Unmarshal(first.Body.Bytes(), &a)
	json.Unmarshal(second.Body.Bytes(), &b)

	if a.ID == b.ID {
		t.Fatal("the two uploads produced one attachment; they should be distinct records")
	}

	// Attach and delete the first, leaving the second unreferenced but present.
	sent := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/messages", map[string]any{
		"body":          "Ya kwanza",
		"clientToken":   "shared-storage-1",
		"attachmentIds": []string{a.ID},
	}, owner)
	if sent.Code != http.StatusCreated {
		t.Fatalf("sending returned %d", sent.Code)
	}

	var message struct {
		ID string `json:"id"`
	}
	json.Unmarshal(sent.Body.Bytes(), &message)

	if deleted := call(t, handler, http.MethodDelete,
		"/api/rooms/"+roomID+"/messages/"+message.ID, nil, owner); deleted.Code != http.StatusCreated {
		t.Fatalf("deleting returned %d", deleted.Code)
	}

	// The second attachment must still serve its bytes.
	survivor := call(t, handler, http.MethodGet, b.URL, nil, owner)
	if survivor.Code != http.StatusOK {
		t.Fatalf("the other attachment sharing the same bytes returned %d; its storage was reclaimed",
			survivor.Code)
	}
	if !bytes.Equal(survivor.Body.Bytes(), testPNG) {
		t.Error("the surviving attachment's bytes are wrong")
	}
}

// TestUnreachableScannerRefusesUploadByDefault is the requirement that matters
// most about scanning.
//
// A scanner that is down must not become a silent bypass: the protection would
// then disappear exactly when something is wrong, which is when it is most
// likely to be needed. Accepting anyway is a legitimate choice for a small
// community, but it has to be a choice.
func TestUnreachableScannerRefusesUploadByDefault(t *testing.T) {
	// A port nothing is listening on, which is what a stopped clamd looks like.
	handler := newTestAPIWithScanner(t, "127.0.0.1:1", false)

	token := completeSetup(t, handler)
	roomID := makeRoom(t, handler, token, "general", "public")

	recorder := uploadFile(t, handler, token, roomID, "picha.png", testPNG)
	if recorder.Code == http.StatusCreated {
		t.Fatal("an upload was accepted while the scanner was unreachable")
	}
}

// TestFailOpenAcceptsWhenConfigured checks that the operator's choice is
// honoured, so a community that would rather stay usable can say so.
func TestFailOpenAcceptsWhenConfigured(t *testing.T) {
	handler := newTestAPIWithScanner(t, "127.0.0.1:1", true)

	token := completeSetup(t, handler)
	roomID := makeRoom(t, handler, token, "general", "public")

	recorder := uploadFile(t, handler, token, roomID, "picha.png", testPNG)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("fail-open was configured but the upload returned %d: %s",
			recorder.Code, truncate(recorder.Body.String()))
	}
}

// TestNoScannerMeansNoRequirement checks the default: a server with no scanner
// configured works without one, and needs no external service at all.
func TestNoScannerMeansNoRequirement(t *testing.T) {
	handler := newTestAPI(t)

	token := completeSetup(t, handler)
	roomID := makeRoom(t, handler, token, "general", "public")

	if recorder := uploadFile(t, handler, token, roomID, "picha.png", testPNG); recorder.Code != http.StatusCreated {
		t.Fatalf("an upload failed with no scanner configured: %d", recorder.Code)
	}
}

/*
TestAnimatedGifKeepsAnimating is the point of treating GIFs as attachments at
all.

A thumbnail is a static PNG, and the timeline shows the thumbnail when one
exists — so an animated GIF arrived as a frozen first frame. A GIF that does
not move is not a GIF, and somebody sending one has sent the one thing about it
that matters and had it removed.
*/
func TestAnimatedGifKeepsAnimating(t *testing.T) {
	handler := newTestAPI(t)

	token := completeSetup(t, handler)
	roomID := makeRoom(t, handler, token, "general", "public")

	recorder := uploadFile(t, handler, token, roomID, "reaction.gif", testAnimatedGIF)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("upload returned %d: %s", recorder.Code, truncate(recorder.Body.String()))
	}

	var uploaded struct {
		ID          string `json:"id"`
		Kind        string `json:"kind"`
		ContentType string `json:"contentType"`
		PreviewURL  string `json:"previewUrl"`
		Animated    bool   `json:"animated"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &uploaded); err != nil {
		t.Fatalf("decoding the upload: %v", err)
	}

	if uploaded.ContentType != "image/gif" {
		t.Errorf("the type is %q, expected image/gif", uploaded.ContentType)
	}
	if !uploaded.Animated {
		t.Error("an animated GIF is not reported as animated")
	}

	// The client decides what to show from this: a still preview is right for
	// a photograph and wrong for a GIF, and only the server knows which this
	// is without downloading the whole file.
	if uploaded.PreviewURL != "" {
		t.Errorf("an animated GIF was given a still preview at %q, which would "+
			"be shown in place of the animation", uploaded.PreviewURL)
	}
}

// TestStillImageKeepsItsPreview checks the change above did not throw away
// previews for the images that should have them.
//
// A photograph is the case previews exist for: showing the full file in a
// timeline would download megabytes to draw a thumbnail.
func TestStillImageKeepsItsPreview(t *testing.T) {
	handler := newTestAPI(t)

	token := completeSetup(t, handler)
	roomID := makeRoom(t, handler, token, "general", "public")

	recorder := uploadFile(t, handler, token, roomID, "photo.png", realPNG(t))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("upload returned %d: %s", recorder.Code, truncate(recorder.Body.String()))
	}

	var uploaded struct {
		PreviewURL string `json:"previewUrl"`
		Animated   bool   `json:"animated"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &uploaded); err != nil {
		t.Fatalf("decoding the upload: %v", err)
	}

	if uploaded.Animated {
		t.Error("a still image is reported as animated")
	}
	if uploaded.PreviewURL == "" {
		t.Error("a still image lost its preview")
	}
}

// realPNG builds a small but genuine PNG, since the shared testPNG fixture is
// only a header and cannot be decoded into a preview.
func realPNG(t *testing.T) []byte {
	t.Helper()

	image := image.NewRGBA(image.Rect(0, 0, 8, 8))
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, image); err != nil {
		t.Fatalf("building a test image: %v", err)
	}
	return buffer.Bytes()
}
