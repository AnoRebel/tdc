package httpapi_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

const smallestPiece = 256 << 10

// sendPiece sends one piece as raw bytes, the way the web client does.
func sendPiece(t *testing.T, handler http.Handler, token, uploadID string, piece int, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPut,
		"/api/uploads/"+uploadID+"/pieces/"+strconv.Itoa(piece), bytes.NewReader(data))
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

// beginUpload declares a file and returns the upload's identifier.
func beginUpload(t *testing.T, handler http.Handler, token, roomID, name string, size int, want int) string {
	t.Helper()
	recorder := call(t, handler, http.MethodPost, "/api/rooms/"+roomID+"/uploads",
		map[string]any{"filename": name, "size": size}, token)
	if recorder.Code != want {
		t.Fatalf("beginning an upload of %d bytes returned %d, want %d: %s", size, recorder.Code, want, recorder.Body)
	}
	if want != http.StatusCreated {
		return ""
	}
	id, _ := decode(t, recorder)["id"].(string)
	return id
}

// fileOf is a PNG of a given size: real magic bytes, then noise.
func fileOf(t *testing.T, size int) []byte {
	t.Helper()
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	copy(data, testPNG)
	return data
}

/*
A file sent in pieces arrives whole and identical, whether each piece travels
as raw bytes or as base64 in JSON — the two clients send it differently — and
the server says how much it holds, which is what a client resumes from.
*/
func TestAFileSentInPiecesArrivesWhole(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)
	room := makeRoom(t, handler, token, "files", "public")
	setSetting(t, handler, token, "files.upload_piece_bytes", strconv.Itoa(smallestPiece))

	data := fileOf(t, 2*smallestPiece+1234)
	id := beginUpload(t, handler, token, room, "big.png", len(data), http.StatusCreated)

	if got := sendPiece(t, handler, token, id, 0, data[:smallestPiece]); got.Code != http.StatusOK {
		t.Fatalf("piece 0 returned %d: %s", got.Code, got.Body)
	}

	// As the desktop sends it.
	mustCall(t, handler, http.MethodPut, "/api/uploads/"+id+"/pieces/1", map[string]any{
		"data": base64.StdEncoding.EncodeToString(data[smallestPiece : 2*smallestPiece]),
	}, token, http.StatusOK)

	// Interrupted here: the server says where to carry on from.
	status := decode(t, mustCall(t, handler, http.MethodGet, "/api/uploads/"+id, nil, token, http.StatusOK))
	if status["pieces"] != float64(2) || status["received"] != float64(2*smallestPiece) {
		t.Fatalf("the server reports %v, want two pieces held", status)
	}

	if got := sendPiece(t, handler, token, id, 2, data[2*smallestPiece:]); got.Code != http.StatusOK {
		t.Fatalf("the last piece returned %d: %s", got.Code, got.Body)
	}

	attachment := decode(t, mustCall(t, handler, http.MethodPost, "/api/uploads/"+id+"/finish", nil, token, http.StatusCreated))
	if attachment["contentType"] != "image/png" || attachment["size"] != float64(len(data)) {
		t.Fatalf("the finished file is %v", attachment)
	}
	url, _ := attachment["url"].(string)
	download := mustCall(t, handler, http.MethodGet, url, nil, token, http.StatusOK)
	if !bytes.Equal(download.Body.Bytes(), data) {
		t.Fatal("the file downloaded is not the file sent")
	}

	// Finished is finished: the upload is gone.
	mustCall(t, handler, http.MethodGet, "/api/uploads/"+id, nil, token, http.StatusNotFound)
}

// Only the next piece, of the agreed size, is accepted, so the pieces joined
// in order are always the file.
func TestPiecesAreTakenOnlyInOrderAndWhole(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)
	room := makeRoom(t, handler, token, "files", "public")
	setSetting(t, handler, token, "files.upload_piece_bytes", strconv.Itoa(smallestPiece))

	data := fileOf(t, 2*smallestPiece)
	id := beginUpload(t, handler, token, room, "two.png", len(data), http.StatusCreated)

	if got := sendPiece(t, handler, token, id, 1, data[smallestPiece:]); got.Code != http.StatusConflict {
		t.Errorf("a piece out of order returned %d, want 409", got.Code)
	}
	if got := sendPiece(t, handler, token, id, 0, data[:100]); got.Code != http.StatusBadRequest {
		t.Errorf("a short piece that is not the last returned %d, want 400", got.Code)
	}
	if got := sendPiece(t, handler, token, id, 0, append(bytes.Clone(data[:smallestPiece]), 0)); got.Code == http.StatusOK {
		t.Error("a piece larger than agreed was accepted")
	}
	if got := sendPiece(t, handler, token, id, 0, data[:smallestPiece]); got.Code != http.StatusOK {
		t.Fatalf("piece 0 returned %d", got.Code)
	}
	// Sent again, as a client retrying after a lost answer would.
	if got := sendPiece(t, handler, token, id, 0, data[:smallestPiece]); got.Code != http.StatusConflict {
		t.Errorf("piece 0 sent twice returned %d, want 409", got.Code)
	}
	mustCall(t, handler, http.MethodPost, "/api/uploads/"+id+"/finish", nil, token, http.StatusConflict)
}

// Somebody else's upload does not exist as far as anybody else can tell.
func TestSomebodyElsesUploadIsOutOfReach(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	other := registerAndSignIn(t, handler, "amani")
	room := makeRoom(t, handler, founder, "files", "public")
	joinRoom(t, handler, other, room)

	data := fileOf(t, 1000)
	id := beginUpload(t, handler, founder, room, "mine.png", len(data), http.StatusCreated)

	mustCall(t, handler, http.MethodGet, "/api/uploads/"+id, nil, other, http.StatusNotFound)
	if got := sendPiece(t, handler, other, id, 0, data); got.Code != http.StatusNotFound {
		t.Errorf("somebody else's piece returned %d, want 404", got.Code)
	}
	mustCall(t, handler, http.MethodPost, "/api/uploads/"+id+"/finish", nil, other, http.StatusNotFound)
	mustCall(t, handler, http.MethodDelete, "/api/uploads/"+id, nil, other, http.StatusNotFound)

	// And somebody who is not in the room cannot begin one there.
	stranger := registerAndSignIn(t, handler, "stranger")
	private := makeRoom(t, handler, founder, "secret", "private")
	call := call(t, handler, http.MethodPost, "/api/rooms/"+private+"/uploads",
		map[string]any{"filename": "x.png", "size": 10}, stranger)
	if call.Code < 400 {
		t.Errorf("a stranger began an upload in a private room: %d", call.Code)
	}
}

/*
Every limit refuses at the beginning, against the declared size — and an upload
in progress reserves what it declared, so several at once cannot pass a limit
each would pass alone.
*/
func TestLimitsRefuseBeforeAnyPieceIsSent(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)
	room := makeRoom(t, handler, token, "files", "public")

	t.Run("the file limit, changed while running", func(t *testing.T) {
		setSetting(t, handler, token, "files.max_file_bytes", "5000")
		beginUpload(t, handler, token, room, "too.png", 5001, http.StatusRequestEntityTooLarge)
		if got := uploadFile(t, handler, token, room, "too.png", fileOf(t, 5001)); got.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("a single upload over the new limit returned %d", got.Code)
		}
		setSetting(t, handler, token, "files.max_file_bytes", strconv.Itoa(100<<20))
	})

	t.Run("a person's quota, with uploads in progress", func(t *testing.T) {
		setSetting(t, handler, token, "files.quota_per_person_bytes", "3000")
		beginUpload(t, handler, token, room, "a.png", 2000, http.StatusCreated)
		beginUpload(t, handler, token, room, "b.png", 2000, http.StatusInsufficientStorage)

		limits := decode(t, mustCall(t, handler, http.MethodGet, "/api/attachments/limits", nil, token, http.StatusOK))
		if limits["quota"] != float64(3000) || limits["used"] != float64(2000) {
			t.Errorf("the limits say %v, want a quota of 3000 with 2000 used", limits)
		}
		setSetting(t, handler, token, "files.quota_per_person_bytes", "0")
	})

	t.Run("the server's storage", func(t *testing.T) {
		setSetting(t, handler, token, "limits.max_storage_bytes", "4000")
		// 2000 is already reserved by the upload above.
		beginUpload(t, handler, token, room, "c.png", 2500, http.StatusInsufficientStorage)
		setSetting(t, handler, token, "limits.max_storage_bytes", "0")
	})
}

// Finishing takes the file through the same checks a single upload does: an
// executable is refused for what it is, however it arrived.
func TestFinishingChecksTheFileItself(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)
	room := makeRoom(t, handler, token, "files", "public")

	id := beginUpload(t, handler, token, room, "harmless.png", len(testELF), http.StatusCreated)
	if got := sendPiece(t, handler, token, id, 0, testELF); got.Code != http.StatusOK {
		t.Fatalf("the piece returned %d", got.Code)
	}
	mustCall(t, handler, http.MethodPost, "/api/uploads/"+id+"/finish", nil, token, http.StatusUnsupportedMediaType)
	mustCall(t, handler, http.MethodGet, "/api/uploads/"+id, nil, token, http.StatusNotFound)
}

// Uploads nobody came back to, and files never sent, are removed by the sweep.
func TestTheSweepReleasesWhatWasLeftBehind(t *testing.T) {
	handler := newTestAPI(t)
	token := completeSetup(t, handler)
	room := makeRoom(t, handler, token, "files", "public")
	pool := scheduledDatabase(t)

	id := beginUpload(t, handler, token, room, "left.png", 2000, http.StatusCreated)
	if got := sendPiece(t, handler, token, id, 0, fileOf(t, 2000)[:2000]); got.Code != http.StatusOK {
		t.Fatalf("the piece returned %d", got.Code)
	}
	unsent := uploadedID(t, handler, token, room, "unsent.png")
	kept := uploadedID(t, handler, token, room, "kept.png")
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/messages",
		map[string]any{"attachmentIds": []string{kept}}, token, http.StatusCreated)

	for _, statement := range []string{
		`UPDATE file_uploads SET updated_at = now() - interval '2 days'`,
		`UPDATE attachments SET created_at = now() - interval '2 days'`,
	} {
		if _, err := pool.Exec(context.Background(), statement); err != nil {
			t.Fatal(err)
		}
	}
	serverOf(handler).SweepFiles(context.Background())

	mustCall(t, handler, http.MethodGet, "/api/uploads/"+id, nil, token, http.StatusNotFound)
	mustCall(t, handler, http.MethodGet, "/api/attachments/"+unsent, nil, token, http.StatusNotFound)
	mustCall(t, handler, http.MethodGet, "/api/attachments/"+kept, nil, token, http.StatusOK)

	var pieces int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM file_uploads`).Scan(&pieces); err != nil || pieces != 0 {
		t.Errorf("%d uploads remain after the sweep (%v)", pieces, fmt.Sprint(err))
	}
}
