package httpapi

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
)

/*
Social metadata for links shared outside this server.

A client-rendered application returns an empty shell to anything that does not
run JavaScript, which is every link preview in every chat application, every
crawler, and every accessibility tool that reads a page before it loads. A
public channel shared into another product therefore previewed as a blank box
with a URL — which is indistinguishable from a broken link.

So the shell's head is rewritten on the way out, for public pages only. What is
private stays private: a preview is generated for anybody who has the link,
including somebody with no account here, so anything put in one is published.
*/

// socialMeta is what a preview shows.
type socialMeta struct {
	Title       string
	Description string
	// URL is the canonical address of the thing, so a preview links back to
	// where it came from rather than to whatever redirect produced it.
	URL string
	// Kind is the OpenGraph type. "website" for everything here: there is no
	// article, and claiming one produces a preview asking for an author and a
	// publication date that do not exist.
	Kind string
}

/*
socialMetaFor decides what a shared link should preview as, or nothing.

Nothing is the common answer and the safe one. A page that is not public gets
no metadata at all rather than a generic one, because a preview naming a
private channel — even to say it is private — confirms it exists to whoever
holds the link.
*/
func (s *Server) socialMetaFor(ctx context.Context, urlPath string) *socialMeta {
	segments := strings.Split(strings.Trim(urlPath, "/"), "/")

	switch {
	case len(segments) == 2 && segments[0] == "rooms":
		return s.roomMeta(ctx, segments[1])

	case len(segments) == 2 && segments[0] == "invite":
		return s.inviteMeta(ctx, segments[1])

	case urlPath == "/" || urlPath == "":
		// The server itself, which is the link somebody shares to invite
		// people to it at all.
		return &socialMeta{
			Title:       s.config.ServerName,
			Description: s.config.ServerDescription,
			URL:         s.canonicalURL("/"),
			Kind:        "website",
		}
	}

	return nil
}

/*
roomMeta describes a public channel, or nothing.

Only public. A private channel produces no metadata, so a link to one previews
as bare text — which is what somebody who cannot read it should see, and what
stops a preview from being a way to enumerate what exists.
*/
func (s *Server) roomMeta(ctx context.Context, raw string) *socialMeta {
	roomID, err := uuid.Parse(raw)
	if err != nil {
		return nil
	}

	room, err := s.queries.GetRoomByID(ctx, roomID)
	if err != nil {
		return nil
	}

	// Public channels only, and never a direct conversation — which is
	// private by construction whatever its visibility column says.
	if room.Visibility != "public" || room.Kind == "direct" {
		return nil
	}

	// An archived channel is still public, and a link to one should say what
	// it was rather than nothing.
	description := room.Topic
	if description == "" {
		description = fmt.Sprintf("A channel on %s", s.config.ServerName)
	}
	if room.ArchivedAt != nil {
		description = "Archived — " + description
	}

	name := room.Name
	if room.Slug != nil && *room.Slug != "" {
		name = "#" + *room.Slug
	}

	return &socialMeta{
		Title:       fmt.Sprintf("%s · %s", name, s.config.ServerName),
		Description: description,
		URL:         s.canonicalURL("/rooms/" + roomID.String()),
		Kind:        "website",
	}
}

/*
inviteMeta describes an invitation link, without disclosing anything.

An invitation is posted into a chat application, which fetches the link to
preview it — so whatever this returns is shown to everybody in that
conversation, not only to the person being invited.

That rules out naming the channel. An invitation to a private channel would
otherwise announce its existence and its name to a room full of people who were
not invited, which is exactly what a private channel is not.

It also rules out saying whether the invitation is valid. A preview that read
"expired" for an expired one and "join" for a working one would let anybody
test invitations by pasting them and reading the preview — the same oracle the
redemption endpoint refuses to be, reached another way.

So every invitation link, valid or not, to a public channel or a private one,
previews identically: the server's name and an invitation to join it. The
secret in the URL is never looked up at all.
*/
func (s *Server) inviteMeta(_ context.Context, _ string) *socialMeta {
	return &socialMeta{
		Title:       "You have been invited to " + s.config.ServerName,
		Description: "Open this link to accept the invitation.",
		// Deliberately without the invitation itself: a canonical URL is
		// copied and republished by whatever renders the preview, and a
		// bearer credential should not be spread by being previewed.
		URL:  s.canonicalURL("/"),
		Kind: "website",
	}
}

/*
canonicalURL builds an absolute address from the operator's configured name.

From configuration rather than the request's Host header, deliberately: a
header is supplied by whoever is asking, and building a canonical URL from one
lets anybody make this server advertise their address as its own.
*/
func (s *Server) canonicalURL(path string) string {
	host := s.config.CanonicalAddress
	if host == "" {
		return path
	}

	scheme := "https://"
	// A canonical address that already names a scheme is used as given, which
	// is how a local deployment says http.
	if strings.Contains(host, "://") {
		scheme = ""
	} else if strings.HasPrefix(host, "localhost") || strings.HasPrefix(host, "127.0.0.1") {
		scheme = "http://"
	}

	return scheme + host + path
}

/*
tags renders the metadata as HTML.

Every value is escaped. These come from a channel's name and topic, which
anybody who can create a channel controls — an unescaped topic containing a
quote would end the attribute and let the rest be read as markup, in a document
served from this origin.
*/
func (m *socialMeta) tags() string {
	title := html.EscapeString(m.Title)
	description := html.EscapeString(m.Description)
	url := html.EscapeString(m.URL)

	var out strings.Builder

	// The title tag as well as the OpenGraph one: a crawler that understands
	// neither still has something to show, and a browser tab is titled before
	// the application has loaded.
	fmt.Fprintf(&out, "<title>%s</title>", title)
	fmt.Fprintf(&out, `<meta name="description" content="%s">`, description)

	// OpenGraph, which is what most chat applications read.
	fmt.Fprintf(&out, `<meta property="og:title" content="%s">`, title)
	fmt.Fprintf(&out, `<meta property="og:description" content="%s">`, description)
	fmt.Fprintf(&out, `<meta property="og:type" content="%s">`, html.EscapeString(m.Kind))
	if m.URL != "" {
		fmt.Fprintf(&out, `<meta property="og:url" content="%s">`, url)
	}

	// Twitter's own, which several other products read in preference to
	// OpenGraph. summary rather than summary_large_image: there is no image,
	// and asking for a large one produces an empty box where it would go.
	fmt.Fprintf(&out, `<meta name="twitter:card" content="summary">`)
	fmt.Fprintf(&out, `<meta name="twitter:title" content="%s">`, title)
	fmt.Fprintf(&out, `<meta name="twitter:description" content="%s">`, description)

	return out.String()
}

/*
withSocialMeta returns the shell with metadata inserted, or the shell unchanged.

Inserted immediately after <head>, so anything the built application declares
comes later and wins in a browser — the injected tags are for readers that
never run the application at all.
*/
func withSocialMeta(shell []byte, meta *socialMeta) []byte {
	if meta == nil {
		return shell
	}

	const marker = "<head>"
	at := strings.Index(string(shell), marker)
	if at < 0 {
		// A shell without a head is not one this understands. Serving it
		// unchanged is better than corrupting it.
		return shell
	}

	at += len(marker)

	var out strings.Builder
	out.Grow(len(shell) + 512)
	out.Write(shell[:at])
	out.WriteString(meta.tags())
	out.Write(shell[at:])

	return []byte(out.String())
}

// serveShellWithMeta returns the application shell, described for a crawler.
func (s *Server) serveShellWithMeta(c *echo.Context, shell []byte, urlPath string) error {
	meta := s.socialMetaFor(c.Request().Context(), urlPath)

	// Never cached with the metadata in it: the shell is shared by every route
	// and a cached copy would describe whichever page was fetched first.
	c.Response().Header().Set("Cache-Control", "no-cache")

	return c.HTMLBlob(http.StatusOK, withSocialMeta(shell, meta))
}
