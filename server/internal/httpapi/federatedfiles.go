package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/attachments"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/events"
	"github.com/AnoRebel/tdc/server/internal/federation"
	"github.com/AnoRebel/tdc/server/internal/rooms"
	"github.com/AnoRebel/tdc/server/internal/settings"
)

/*
Files in rooms shared with other servers.

A message describes its files in its signed content; every server it reaches
records the descriptions, and copies the bytes the first time one of its
members opens one. A replica copies from the room's home server, and the home
server from the server whose member sent it — the home server is already the
one every participant talks to, so this follows authorisation that exists
rather than inventing a check no replica could make.
*/

func init() {
	documentRoutes(map[string]entry{
		"GET /api/federation/rooms/:slug/files/:digest": {
			Summary: "The bytes of a file a message in a federated room carries", Authentication: "federation signature",
		},
	})
}

// recordFederatedFiles follows a message from elsewhere being stored here.
func (s *Server) recordFederatedFiles(ctx context.Context, room dbgen.Room, event events.Event) {
	if s.attachments == nil {
		return
	}
	switch event.Type {
	case events.TypeMessageSent:
		files := rooms.FilesIn(event.Content)
		if len(files) == 0 {
			return
		}
		if len(files) > rooms.MaxFilesPerMessage {
			s.log.Warn("a message from elsewhere describes too many files", "event", event.ID)
			return
		}

		// The author's own server, given its message back: its uploads are
		// here already, waiting for the event to bind to.
		if s.isOwnAddress(event.Sender) {
			s.bindOwnEcho(ctx, room, event, files)
			return
		}

		remote := make([]attachments.RemoteFile, 0, len(files))
		for _, file := range files {
			if err := file.Check(); err != nil {
				s.log.Warn("ignoring the files of a message from elsewhere", "event", event.ID, "error", err)
				return
			}
			remote = append(remote, attachments.RemoteFile{
				Filename: file.Filename, ContentType: file.ContentType, Size: file.Size,
				SHA256: file.SHA256, Width: file.Width, Height: file.Height, Caption: file.Caption,
			})
		}
		if err := s.attachments.RecordRemote(ctx, room.ID, event.ID, event.Sender, event.Origin, remote); err != nil {
			s.log.Error("cannot record the files of a message from elsewhere", "event", event.ID, "error", err)
		}

	case events.TypeMessageDeleted:
		// The home server's deletion removes the copies here too.
		var deletion struct {
			Redacts string `json:"redacts"`
		}
		if json.Unmarshal(event.Content, &deletion) == nil && deletion.Redacts != "" {
			if err := s.attachments.DeleteForEvent(ctx, deletion.Redacts); err != nil {
				s.log.Error("cannot remove the files of a deleted message", "event", deletion.Redacts, "error", err)
			}
		}
	}
}

// isOwnAddress reports whether an address names somebody on this server.
func (s *Server) isOwnAddress(address string) bool {
	host, err := federation.ServerOf(address)
	return err == nil && strings.EqualFold(host, s.rooms.ServerName())
}

// bindOwnEcho binds a member's uploads to the message of theirs another server
// sequenced, now that it exists here.
func (s *Server) bindOwnEcho(ctx context.Context, room dbgen.Room, event events.Event, files []rooms.FileRef) {
	username, _, _ := strings.Cut(event.Sender, "@")
	user, err := s.identity.UserByUsername(ctx, username)
	if err != nil {
		return
	}
	ids := make([]uuid.UUID, 0, len(files))
	for _, file := range files {
		if id, err := uuid.Parse(file.ID); err == nil {
			ids = append(ids, id)
		}
	}
	if _, err := s.attachments.Bind(ctx, event.ID, room.ID, user.ID, ids); err != nil {
		s.log.Error("cannot bind files to a message sent elsewhere", "event", event.ID, "error", err)
	}
}

/*
handleFederatedFile serves a peer the bytes of a file in a room it takes part
in.

Only for a federated room the signed peer participates in, and only for a file
a sent message there carries: anything else is reported as absent, so a peer
cannot learn what has been uploaded here by asking for digests in turn. A home
server asked for a file a member elsewhere sent copies it from them first.
*/
func (s *Server) handleFederatedFile(c *echo.Context) error {
	origin, _ := c.Get(peerOrigin).(string)
	if origin == "" || s.attachments == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "unidentified peer")
	}
	ctx := c.Request().Context()
	absent := echo.NewHTTPError(http.StatusNotFound, "no such file")

	room, err := s.rooms.FederatedRoom(ctx, c.Param("slug"))
	if err != nil {
		return absent
	}
	host, err := federation.Host(origin)
	if err != nil {
		return absent
	}
	if _, err := s.queries.RoomPeer(ctx, dbgen.RoomPeerParams{RoomID: room.ID, Peer: host}); err != nil {
		return absent
	}

	attachment, err := s.attachments.RoomFile(ctx, room.ID, c.Param("digest"))
	if err != nil {
		return absent
	}
	if err := s.ensureCopied(ctx, room, attachment); err != nil {
		return copyError(err)
	}

	reader, err := s.attachments.Open(ctx, attachment)
	if err != nil {
		return attachmentError(err)
	}
	defer reader.Close()

	header := c.Response().Header()
	header.Set("Content-Length", strconv.FormatInt(attachment.Size, 10))
	header.Set("X-Content-Type-Options", "nosniff")
	return c.Stream(http.StatusOK, "application/octet-stream", reader)
}

/*
ensureCopied makes sure a file's bytes are here, copying them if this file came
from another server and they are not.
*/
func (s *Server) ensureCopied(ctx context.Context, room dbgen.Room, attachment *attachments.Attachment) error {
	if attachment.OriginServer == "" || s.attachments.HeldHere(ctx, attachment) {
		if attachment.OriginServer != "" && !attachment.Fetched {
			// Here already as some other file, or copied by another node.
			return s.attachments.CopyRemote(ctx, attachment, 0, nil)
		}
		return nil
	}

	if err := s.withinStorageLimit(ctx, attachment.Size); err != nil {
		return err
	}

	source, err := s.copySource(ctx, room, attachment)
	if err != nil {
		return err
	}
	slug := ""
	if room.Slug != nil {
		slug = *room.Slug
	}
	allowPrivate := s.settings != nil &&
		s.settings.Get(ctx, settings.FederationAllowPrivateAddresses) == "true"
	sender := federation.NewSender(s.config.CanonicalAddress, s.rooms.Keyring(), s.federationTrust(ctx), allowPrivate)

	return s.attachments.CopyRemote(ctx, attachment, s.limitFor(ctx, settings.FilesFederationCopyMaxBytes),
		func(ctx context.Context) (io.ReadCloser, error) {
			return sender.FetchFile(ctx, source, slug, attachment.StorageKey)
		})
}

// copySource is where to copy a file from: the room's home server, or — on
// the home server — the server whose member sent it.
func (s *Server) copySource(ctx context.Context, room dbgen.Room, attachment *attachments.Attachment) (string, error) {
	unavailable := errors.New("this server does not know where that file is held")
	host := room.HomeServer
	if s.rooms.HostedHere(room) {
		host = attachment.OriginServer
	}
	peer, err := s.queries.RoomPeer(ctx, dbgen.RoomPeerParams{RoomID: room.ID, Peer: strings.ToLower(host)})
	if err != nil {
		return "", unavailable
	}
	if peer.Address != "" {
		return peer.Address, nil
	}
	return "https://" + peer.Peer, nil
}

// copyError says why a file from elsewhere cannot be shown here.
func copyError(err error) error {
	var http_ *echo.HTTPError
	switch {
	case errors.As(err, &http_):
		return err
	case errors.Is(err, attachments.ErrTooLargeToCopy):
		return echo.NewHTTPError(http.StatusRequestEntityTooLarge, err.Error())
	case errors.Is(err, attachments.ErrCopyRefused):
		return echo.NewHTTPError(http.StatusBadGateway, err.Error())
	default:
		return attachmentError(err)
	}
}
