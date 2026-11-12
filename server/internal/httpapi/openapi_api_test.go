package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/AnoRebel/tdc/server/internal/openapigen"
)

/*
The OpenAPI document describes the routes this server serves, under the
version a client should pin, with bodies taken from the handlers' own types.
*/
func TestTheOpenAPIDocumentDescribesTheAPI(t *testing.T) {
	handler := newTestAPI(t)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/openapi.json", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("the document answered %d", recorder.Code)
	}
	var document struct {
		OpenAPI string                               `json:"openapi"`
		Paths   map[string]map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if document.OpenAPI != "3.1.0" {
		t.Errorf("openapi is %q", document.OpenAPI)
	}

	send := document.Paths["/api/v1/rooms/{id}/messages"]["post"]
	if send == nil {
		t.Fatalf("sending a message is not described; paths include %d entries", len(document.Paths))
	}
	body, _ := json.Marshal(send)
	for _, want := range []string{`"requestBody"`, `"201"`, `"name":"id"`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("sending a message is described without %s: %s", want, body)
		}
	}
	rename, _ := json.Marshal(document.Paths["/api/v1/rooms/{id}"]["patch"])
	if !strings.Contains(string(rename), `"x-tdc-permission":"channel.rename"`) {
		t.Errorf("renaming a channel does not name its permission: %s", rename)
	}
	if !strings.Contains(recorder.Body.String(), `"clientToken"`) {
		t.Error("the message request's fields are not in the document")
	}
}

// The recorded bodies are what the handlers bind and write today.
func TestTheRecordedBodiesAreCurrent(t *testing.T) {
	want, err := openapigen.Generate(".")
	if err != nil {
		t.Fatal(err)
	}
	have, err := os.ReadFile("openapi_bodies_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	if string(have) != string(want) {
		t.Error("openapi_bodies_gen.go is behind the handlers; run go generate ./internal/httpapi")
	}
}
