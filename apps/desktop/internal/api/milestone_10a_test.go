package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/AnoRebel/tdc/apps/desktop/internal/api"
)

/*
The Milestone 10a demonstration, desktop half.

The milestone asks for a press from the desktop client, and the desktop does
not speak HTTP: it reaches the server over Wails bindings, through the Go
service in this package. Driving a browser against the built frontend would
exercise a transport the desktop never uses, and would prove the wrong thing
convincingly.

So this drives the real path — api.Service.Send, the same method the binding
calls — against a real server. What it does not cover is the webview drawing
the button, which needs a display and a Wails runtime; the web half of the
demonstration shows the button being pressed by a person, and the transport
contract suite (apps/web/app/lib/transport.test.ts) holds both adapters to one
set of expectations. Between them the claim is whole, and the seam is named
rather than hidden.

Skipped unless TDC_MILESTONE_SERVER names a running server, because it needs
one seeded exactly as the demonstration describes.
*/
func TestMilestone10aDesktopPressReachesTheProgram(t *testing.T) {
	address := os.Getenv("TDC_MILESTONE_SERVER")
	room := os.Getenv("TDC_MILESTONE_ROOM")
	message := os.Getenv("TDC_MILESTONE_MESSAGE")
	username := os.Getenv("TDC_MILESTONE_USER")
	password := os.Getenv("TDC_MILESTONE_PASSWORD")

	if address == "" || room == "" || message == "" || username == "" || password == "" {
		t.Skip("set TDC_MILESTONE_SERVER, _ROOM, _MESSAGE, _USER and _PASSWORD to run the demonstration")
	}

	service := api.New()
	if _, err := service.UseServer(address); err != nil {
		t.Fatalf("the desktop client would not use %s: %v", address, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	/*
		Signed in the way the desktop signs in.

		Not a bearer token injected into a header: the desktop holds a session
		in a cookie jar, and a demonstration that bypassed that would exercise
		a path the desktop does not have.
	*/
	credentials, err := json.Marshal(map[string]string{
		"username": username,
		"password": password,
	})
	if err != nil {
		t.Fatalf("encoding credentials: %v", err)
	}

	signIn, err := service.Send(ctx, api.Request{
		Method: http.MethodPost,
		Path:   "/api/auth/signin",
		Body:   credentials,
	})
	if err != nil {
		t.Fatalf("the desktop client could not reach the server: %v", err)
	}
	if !signIn.OK {
		t.Fatalf("the desktop client could not sign in: %d %s", signIn.Status, signIn.Body)
	}

	/*
		The press, made the way the desktop makes every request.

		The identifier is percent-encoded, which is what the shared composable
		does — and which found a server bug that every test sending a raw "$"
		had missed.
	*/
	path := fmt.Sprintf("/api/rooms/%s/messages/%s/actions/%s",
		room, encode(message), "hold")

	response, err := service.Send(ctx, api.Request{Method: http.MethodPost, Path: path})
	if err != nil {
		t.Fatalf("the desktop client could not reach the server: %v", err)
	}
	if !response.OK {
		t.Fatalf("the press was refused with %d: %s", response.Status, response.Body)
	}

	var pressed struct {
		Type    string `json:"type"`
		Content struct {
			Action  string `json:"action"`
			Pressed string `json:"pressed"`
		} `json:"content"`
	}
	if err := json.Unmarshal(response.Body, &pressed); err != nil {
		t.Fatalf("the press did not come back as an event: %v", err)
	}

	if pressed.Type != "action.pressed" {
		t.Errorf("the desktop press produced a %q event", pressed.Type)
	}
	if pressed.Content.Action != "hold" {
		t.Errorf("the desktop pressed %q", pressed.Content.Action)
	}
	if pressed.Content.Pressed != message {
		t.Errorf("the press names message %q, want %q", pressed.Content.Pressed, message)
	}

	t.Logf("the desktop client pressed %q on %s, and the server recorded it",
		pressed.Content.Action, pressed.Content.Pressed)
}

// encode percent-encodes a path segment the way encodeURIComponent does.
func encode(segment string) string {
	var out []byte
	for i := 0; i < len(segment); i++ {
		c := segment[i]
		unreserved := (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' || c == '~'
		if unreserved {
			out = append(out, c)
			continue
		}
		out = append(out, '%', "0123456789ABCDEF"[c>>4], "0123456789ABCDEF"[c&15])
	}
	return string(out)
}
