package httpapi

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // the scheme under test is HMAC-SHA1.
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

/*
Relay credentials are coturn's use-auth-secret scheme: the username is when
they expire and who they are for, the password the HMAC-SHA1 of the username
under the shared secret. A relay checking them computes the same thing; any
difference and every relayed call fails.
*/
func TestTURNCredentialsAreWhatTheRelayExpects(t *testing.T) {
	person := uuid.New()
	expiry := time.Unix(1_900_000_000, 0)
	username, password := turnCredential("shared-secret", person, expiry)

	if username != "1900000000:"+person.String() {
		t.Errorf("username %q does not lead with its expiry", username)
	}
	mac := hmac.New(sha1.New, []byte("shared-secret"))
	mac.Write([]byte(username))
	if want := base64.StdEncoding.EncodeToString(mac.Sum(nil)); password != want {
		t.Errorf("password %q, the relay would compute %q", password, want)
	}
	if _, other := turnCredential("another-secret", person, expiry); other == password || strings.Contains(username, "secret") {
		t.Error("the credential does not depend on the secret, or leaks it")
	}
}
