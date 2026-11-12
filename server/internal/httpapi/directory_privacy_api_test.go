package httpapi_test

import (
	"net/http"
	"net/url"
	"testing"
)

/*
Two sets of settings, each deciding only what it says it decides.

user_privacy decides what a member list shows about somebody — whether they are
online, their status, when they were last seen. A directory profile's
visibility decides who can find the profile. Every combination of the two is
checked from the outside, and each answer must depend on its own setting and
on nothing else: a public profile does not make somebody visible as online,
hiding one's presence does not take a profile out of the directory, and a
directory profile never carries presence at all.
*/
func TestPrivacyAndDirectoryVisibilityCompose(t *testing.T) {
	handler, founder := withDirectory(t)
	roomID := makeRoom(t, handler, founder, "general", "public")
	juma := registerAndSignIn(t, handler, "juma")
	amina := registerAndSignIn(t, handler, "amina")
	joinRoom(t, handler, juma, roomID)
	joinRoom(t, handler, amina, roomID)

	presence := []string{"online", "status", "statusText", "lastSeenAt"}
	privacies := map[string]bool{"discloses presence": true, "withholds presence": false}
	// The directory's three visibilities, and having no profile at all.
	visibilities := []string{"public", "members", "hidden", "none"}

	for privacyName, discloses := range privacies {
		for _, visibility := range visibilities {
			setPrivacy(t, handler, juma, map[string]any{
				"showsPresence": discloses, "showsStatus": discloses, "showsLastSeen": discloses,
			})
			if visibility == "none" {
				call(t, handler, http.MethodDelete, "/api/directory/me", nil, juma)
			} else {
				saveProfile(t, handler, juma, map[string]any{
					"visibility": visibility, "detailsVisibility": "public", "headline": "Developer",
				})
			}
			case_ := privacyName + ", profile " + visibility

			// The member list follows user_privacy alone.
			member := memberNamed(t, handler, amina, roomID, "juma")
			for _, field := range []string{"online"} {
				if _, present := member[field]; present != discloses {
					t.Errorf("%s: the member list has %s=%v, want present=%v", case_, field, present, discloses)
				}
			}
			// So does the presence a conversation polls, which is a second
			// answer to the same question and must not be the one that leaks.
			polled := presenceOf(t, handler, amina, roomID, member["id"].(string))
			for _, field := range []string{"state", "status", "lastSeenAt"} {
				if _, present := polled[field]; present != discloses {
					t.Errorf("%s: room presence has %s present=%v, want %v", case_, field, present, discloses)
				}
			}

			// The directory follows its own visibility alone.
			memberMaySee := visibility == "public" || visibility == "members"
			strangerMaySee := visibility == "public"
			status, profile := viewProfile(t, handler, amina, "juma")
			if (status == http.StatusOK) != memberMaySee {
				t.Errorf("%s: a member viewing the profile got %d", case_, status)
			}
			if status, _ := viewProfile(t, handler, "", "juma"); (status == http.StatusOK) != strangerMaySee {
				t.Errorf("%s: somebody not signed in viewing the profile got %d", case_, status)
			}
			names, _ := searchDirectory(t, handler, amina, url.Values{"q": {"Developer"}})
			if has(names, "juma") != memberMaySee {
				t.Errorf("%s: a member's directory search found juma=%v", case_, has(names, "juma"))
			}

			// And the directory never says anything about presence, whatever
			// either setting is.
			for _, field := range presence {
				if value, present := profile[field]; present {
					t.Errorf("%s: the directory profile carries %s=%v", case_, field, value)
				}
			}
		}
	}
}
