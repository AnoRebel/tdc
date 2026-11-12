package httpapi_test

import (
	"net/http"
	"testing"
)

/*
One presenter at a time: a second is refused until the first stops; leaving
clears the stage; a moderator can clear it; and somebody not in the call
cannot take it.
*/
func TestTheStageHoldsOnePresenter(t *testing.T) {
	handler, admin, room, amina, baraka := withCalls(t)
	for _, person := range []string{admin, amina, baraka} {
		mustCall(t, handler, http.MethodPost, "/api/rooms/"+room+"/call", map[string]any{"audio": true}, person, http.StatusOK)
	}
	stage := "/api/rooms/" + room + "/call/stage"

	presenting := decode(t, mustCall(t, handler, http.MethodPut, stage, nil, amina, http.StatusOK))
	if presenting["presenter"] != accountID(t, handler, amina) {
		t.Fatalf("the stage is %v", presenting["presenter"])
	}
	mustCall(t, handler, http.MethodPut, stage, nil, baraka, http.StatusConflict)

	mustCall(t, handler, http.MethodDelete, stage, nil, amina, http.StatusOK)
	mustCall(t, handler, http.MethodPut, stage, nil, baraka, http.StatusOK)

	// Leaving clears the stage.
	mustCall(t, handler, http.MethodDelete, "/api/rooms/"+room+"/call", nil, baraka, http.StatusNoContent)
	mustCall(t, handler, http.MethodPut, stage, nil, amina, http.StatusOK)

	// A member without moderation cannot clear somebody else; a moderator can.
	mustCall(t, handler, http.MethodDelete, stage+"/presenter", nil, baraka, http.StatusForbidden)
	cleared := decode(t, mustCall(t, handler, http.MethodDelete, stage+"/presenter", nil, admin, http.StatusOK))
	if cleared["presenter"] != nil {
		t.Errorf("after clearing, the stage is %v", cleared["presenter"])
	}

	outsider := registerAndSignIn(t, handler, "outsider")
	mustCall(t, handler, http.MethodPut, stage, nil, outsider, http.StatusNotFound)
}
