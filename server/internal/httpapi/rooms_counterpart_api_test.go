package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

/*
A conversation between two names the other person, so their picture can be
shown beside it; a channel and a conversation among several name nobody.
*/
func TestADirectConversationNamesTheOtherPerson(t *testing.T) {
	handler := newTestAPI(t)
	admin := completeSetup(t, handler)
	amina := registerAndSignIn(t, handler, "amina")
	juma := registerAndSignIn(t, handler, "juma")
	registerAndSignIn(t, handler, "chausiku")

	pair := decode(t, mustCall(t, handler, http.MethodPost, "/api/direct",
		map[string]any{"usernames": []string{"juma"}}, amina, http.StatusOK))["id"].(string)
	group := decode(t, mustCall(t, handler, http.MethodPost, "/api/direct",
		map[string]any{"usernames": []string{"juma", "chausiku"}}, amina, http.StatusOK))["id"].(string)
	channel := makeRoom(t, handler, admin, "general", "public")

	counterparts := func(token string) map[string]string {
		var rooms []map[string]any
		decodeInto(t, mustCall(t, handler, http.MethodGet, "/api/rooms", nil, token, http.StatusOK), &rooms)
		found := map[string]string{}
		for _, room := range rooms {
			counterpart, _ := room["counterpart"].(string)
			found[room["id"].(string)] = counterpart
		}
		return found
	}

	mine := counterparts(amina)
	if !strings.HasPrefix(mine[pair], "juma@") {
		t.Errorf("amina's conversation with juma names %q", mine[pair])
	}
	if mine[group] != "" || mine[channel] != "" {
		t.Errorf("a group or channel named somebody: group %q, channel %q", mine[group], mine[channel])
	}
	if theirs := counterparts(juma)[pair]; !strings.HasPrefix(theirs, "amina@") {
		t.Errorf("juma's conversation with amina names %q", theirs)
	}
}
