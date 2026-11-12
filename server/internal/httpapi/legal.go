package httpapi

import (
	"context"
	"strings"

	"github.com/AnoRebel/tdc/server/internal/settings"
)

/*
legalResponse is where this server's operator publishes its policies.

Part of the unauthenticated server description because somebody deciding
whether to create an account is exactly who needs to read them, and they are
public by nature. Each link is omitted when the operator has not set it, so a
client shows nothing rather than an empty link — and never the templates in the
repository, which are not this server's promises.
*/
type legalResponse struct {
	PrivacyPolicyURL string `json:"privacyPolicyUrl,omitempty"`
	TermsURL         string `json:"termsUrl,omitempty"`
}

/*
legal reads the operator's policy links.

Nil when neither is set, so the whole field is absent and a client can treat a
server that has published nothing the same way as an older server that does not
know about these settings.
*/
func (s *Server) legal(ctx context.Context) *legalResponse {
	if s.settings == nil {
		return nil
	}

	links := legalResponse{
		PrivacyPolicyURL: strings.TrimSpace(s.settings.Get(ctx, settings.LegalPrivacyPolicyURL)),
		TermsURL:         strings.TrimSpace(s.settings.Get(ctx, settings.LegalTermsURL)),
	}
	if links == (legalResponse{}) {
		return nil
	}
	return &links
}
