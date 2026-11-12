package httpapi_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// aPicture is a small PNG, as the interface sends one after cropping.
func aPicture(t *testing.T, shade uint8) []byte {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for x := range 64 {
		for y := range 64 {
			picture.Set(x, y, color.RGBA{R: shade, G: 80, B: 160, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, picture); err != nil {
		t.Fatalf("encoding: %v", err)
	}
	return encoded.Bytes()
}

func putAvatar(t *testing.T, handler http.Handler, token string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPut, "/api/me/avatar", bytes.NewReader(body))
	request.Header.Set("Content-Type", "image/png")
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

/*
A picture is chosen, listed, served without a session, replaced and removed.

Served without a session because the desktop client draws pictures with plain
image elements; listed only to people signed in. A replaced picture's address
stops working and its file is queued for deletion, so a picture somebody took
down is not still being served from where it was.
*/
func TestAPictureIsChosenServedAndReplaced(t *testing.T) {
	handler, services := newTestAPIWithServices(t)
	token := completeSetup(t, handler)

	first := putAvatar(t, handler, token, aPicture(t, 20))
	if first.Code != http.StatusOK {
		t.Fatalf("choosing a picture: status %d, body %s", first.Code, first.Body.String())
	}
	firstURL, _ := decode(t, first)["url"].(string)
	if !strings.HasPrefix(firstURL, "/api/avatars/") {
		t.Fatalf("the picture is at %q", firstURL)
	}

	if listed := call(t, handler, http.MethodGet, "/api/avatars", nil, ""); listed.Code != http.StatusUnauthorized {
		t.Errorf("listing pictures signed out: status %d, want 401", listed.Code)
	}
	listed := call(t, handler, http.MethodGet, "/api/avatars", nil, token)
	if !strings.Contains(listed.Body.String(), firstURL) || !strings.Contains(listed.Body.String(), "founder@") {
		t.Errorf("the list does not name the picture: %s", listed.Body.String())
	}

	served := call(t, handler, http.MethodGet, firstURL, nil, "")
	if served.Code != http.StatusOK || !strings.HasPrefix(served.Header().Get("Content-Type"), "image/") {
		t.Fatalf("serving the picture: status %d, type %q", served.Code, served.Header().Get("Content-Type"))
	}
	if _, _, err := image.Decode(served.Body); err != nil {
		t.Errorf("the served picture is not an image: %v", err)
	}
	if !strings.Contains(served.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("the picture is not cached for good: %q", served.Header().Get("Cache-Control"))
	}

	second := putAvatar(t, handler, token, aPicture(t, 200))
	secondURL, _ := decode(t, second)["url"].(string)
	if secondURL == firstURL {
		t.Fatal("a new picture kept the old address, so caches would show the old one")
	}
	if gone := call(t, handler, http.MethodGet, firstURL, nil, ""); gone.Code != http.StatusNotFound {
		t.Errorf("the replaced picture is still served: status %d", gone.Code)
	}
	var queued int
	if err := services.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM storage_deletions WHERE key LIKE 'avatars/%'").Scan(&queued); err != nil {
		t.Fatalf("reading the deletion queue: %v", err)
	}
	if queued != 1 {
		t.Errorf("%d pictures queued for deletion after one was replaced, want 1", queued)
	}

	if removed := call(t, handler, http.MethodDelete, "/api/me/avatar", nil, token); removed.Code != http.StatusNoContent {
		t.Fatalf("removing the picture: status %d", removed.Code)
	}
	if gone := call(t, handler, http.MethodGet, secondURL, nil, ""); gone.Code != http.StatusNotFound {
		t.Errorf("a removed picture is still served: status %d", gone.Code)
	}
}

// The desktop client sends the picture as base64 in JSON, through Go.
func TestAPictureMaySendAsJSON(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)

	sent := call(t, handler, http.MethodPut, "/api/me/avatar",
		map[string]string{"image": base64.StdEncoding.EncodeToString(aPicture(t, 90))}, token)
	if sent.Code != http.StatusOK {
		t.Fatalf("a picture as JSON: status %d, body %s", sent.Code, sent.Body.String())
	}
	url, _ := decode(t, sent)["url"].(string)
	if served := call(t, handler, http.MethodGet, url, nil, ""); served.Code != http.StatusOK {
		t.Errorf("serving it: status %d", served.Code)
	}
}

// Anything that is not a picture, or is too large, is refused with why.
func TestAPictureMustBeASmallImage(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)

	if refused := putAvatar(t, handler, token, []byte("<svg onload=alert(1)>")); refused.Code != http.StatusUnsupportedMediaType {
		t.Errorf("a non-image: status %d, want 415", refused.Code)
	}
	if refused := putAvatar(t, handler, token, bytes.Repeat([]byte{0x89}, 2<<20+10)); refused.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("an oversized upload: status %d, want 413", refused.Code)
	}
}
