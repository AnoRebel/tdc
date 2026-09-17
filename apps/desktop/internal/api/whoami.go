package api

import "sync"

/*
Who is signed in, as far as Go needs to know it.

Only the address — "someone@server" — and only so that a message this person
sent themselves does not raise a banner on their own machine. The session, the
password and everything else stay where they are; this is a label, not a
credential, and it is deliberately not enough to act as anybody.

Told rather than discovered: the webview learns who it is from /api/me during
sign-in, and Go has no reason to ask a second time. It is cleared on sign-out
for the same reason a session is — the next person at this machine is not the
last one.
*/
type identity struct {
	mu      sync.RWMutex
	address string
}

// SignedInAs records who the webview signed in as.
func (s *Service) SignedInAs(address string) {
	s.who.mu.Lock()
	defer s.who.mu.Unlock()
	s.who.address = address
}

// SignedOut forgets it.
func (s *Service) SignedOut() {
	s.who.mu.Lock()
	defer s.who.mu.Unlock()
	s.who.address = ""
}

/*
isMe reports whether an address is the person using this client.

Empty is nobody: before sign-in, or after sign-out, nothing is suppressed —
which errs towards showing a notification rather than swallowing one, and a
banner for a message nobody sent is not a state this can reach.
*/
func (s *Service) isMe(address string) bool {
	s.who.mu.RLock()
	defer s.who.mu.RUnlock()
	return s.who.address != "" && s.who.address == address
}
