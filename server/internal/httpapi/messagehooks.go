package httpapi

import (
	"context"

	"github.com/google/uuid"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

/*
beforeNotifying is work a feature needs done after a message is stored and
before anybody is told about it.

For rooms that belong to something else. An issue's discussion is a room like
any other, and a comment on it notifies the room's members exactly as a
channel message does — but who counts as a member there depends on the issue:
somebody named in a comment who may see the issue should be told, as a
member of a channel would be. Doing that here, before the member list is
read, is what lets the ordinary notification path do the telling, with
people's own preferences deciding, instead of a second path beside it.

Registered from init by the feature that needs it. Each hook runs in the
background work that notifies, so a slow one delays notifications and never
the message.
*/
var beforeNotifying []func(s *Server, ctx context.Context, event dbgen.Event, body string, roomID uuid.UUID)
