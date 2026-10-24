package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// uploadedID uploads a small file to a room and returns its identifier.
func uploadedID(t *testing.T, handler http.Handler, token, roomID, name string) string {
	t.Helper()
	recorder := uploadFile(t, handler, token, roomID, name, testPNG)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("upload returned %d: %s", recorder.Code, recorder.Body)
	}
	var uploaded struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &uploaded); err != nil {
		t.Fatalf("decoding the upload: %v", err)
	}
	return uploaded.ID
}

/*
A message may be nothing but its files, wherever a message can be sent.

This was refused with "message is empty" though both clients offered it, so
each place a message is posted is tried in turn: a channel, a thread, a direct
conversation and a scheduled message.
*/
func TestAMessageMayBeOnlyItsFiles(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	registerAndSignIn(t, handler, "amani")
	channel := makeRoom(t, handler, founder, "photos", "public")

	t.Run("in a channel", func(t *testing.T) {
		file := uploadedID(t, handler, founder, channel, "beach.png")
		sent := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+channel+"/messages",
			map[string]any{"body": "", "attachmentIds": []string{file}}, founder, http.StatusCreated))
		attached, _ := sent["attachments"].([]any)
		if len(attached) != 1 {
			t.Fatalf("the message carries %d files, want 1: %v", len(attached), sent)
		}
	})

	t.Run("in a thread", func(t *testing.T) {
		root := sendMessage(t, handler, founder, channel, "who has the pictures?")
		file := uploadedID(t, handler, founder, channel, "sunset.png")
		mustCall(t, handler, http.MethodPost, "/api/rooms/"+channel+"/threads/"+root+"/replies",
			map[string]any{"body": "  ", "attachmentIds": []string{file}}, founder, http.StatusCreated)
	})

	t.Run("in a direct conversation", func(t *testing.T) {
		direct := openDirectWith(t, handler, founder, "amani")
		file := uploadedID(t, handler, founder, direct, "receipt.png")
		mustCall(t, handler, http.MethodPost, "/api/rooms/"+direct+"/messages",
			map[string]any{"attachmentIds": []string{file}}, founder, http.StatusCreated)
	})

	t.Run("scheduled", func(t *testing.T) {
		pool := scheduledDatabase(t)
		file := uploadedID(t, handler, founder, channel, "later.png")
		scheduled := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+channel+"/scheduled", map[string]any{
			"body":        "",
			"attachments": []string{file},
			"sendAt":      whenSoon(),
		}, founder, http.StatusCreated))
		makeDue(t, pool)
		serverOf(handler).DeliverScheduled(context.Background())

		var sent *string
		var failed string
		if err := pool.QueryRow(context.Background(),
			`SELECT sent_event_id, failed_reason FROM scheduled_messages WHERE id = $1`,
			scheduled["id"]).Scan(&sent, &failed); err != nil {
			t.Fatalf("reading the scheduled message: %v", err)
		}
		if sent == nil || failed != "" {
			t.Fatalf("a scheduled message of files alone was not sent: %q", failed)
		}
	})

	t.Run("neither text nor a file is still refused", func(t *testing.T) {
		mustCall(t, handler, http.MethodPost, "/api/rooms/"+channel+"/messages",
			map[string]any{"body": "   "}, founder, http.StatusBadRequest)
		mustCall(t, handler, http.MethodPost, "/api/rooms/"+channel+"/scheduled", map[string]any{
			"body":   "",
			"sendAt": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		}, founder, http.StatusBadRequest)
	})
}

/*
The files a message carries are described in its content, which is what is
signed and what another server receives — and only the sender's own uploads may
be described.
*/
func TestFilesAreDescribedInTheMessage(t *testing.T) {
	handler := newTestAPI(t)
	founder := completeSetup(t, handler)
	amani := registerAndSignIn(t, handler, "amani")
	channel := makeRoom(t, handler, founder, "photos", "public")
	joinRoom(t, handler, amani, channel)

	file := uploadedID(t, handler, founder, channel, "beach.png")

	// Somebody else's upload cannot be put in your message.
	mustCall(t, handler, http.MethodPost, "/api/rooms/"+channel+"/messages",
		map[string]any{"body": "mine now", "attachmentIds": []string{file}}, amani, http.StatusNotFound)

	sent := decode(t, mustCall(t, handler, http.MethodPost, "/api/rooms/"+channel+"/messages",
		map[string]any{"body": "here", "attachmentIds": []string{file}}, founder, http.StatusCreated))
	id, _ := sent["id"].(string)

	var raw []byte
	if err := scheduledDatabase(t).QueryRow(context.Background(),
		`SELECT content FROM events WHERE id = $1`, id).Scan(&raw); err != nil {
		t.Fatalf("reading the stored event: %v", err)
	}
	var content struct {
		Attachments []struct {
			Filename string `json:"filename"`
			SHA256   string `json:"sha256"`
		} `json:"attachments"`
	}
	if err := json.Unmarshal(raw, &content); err != nil || len(content.Attachments) != 1 {
		t.Fatalf("the signed content describes %d files, want 1: %s", len(content.Attachments), raw)
	}
	if len(content.Attachments[0].SHA256) != 64 {
		t.Errorf("the description carries no digest: %s", raw)
	}
	if content.Attachments[0].Filename != "beach.png" {
		t.Errorf("the description names %q, want beach.png", content.Attachments[0].Filename)
	}
}
