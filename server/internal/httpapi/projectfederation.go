package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"uuid"

	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/federation"
	"github.com/AnoRebel/tdc/server/internal/projects"
	"github.com/AnoRebel/tdc/server/internal/settings"
)

/*
Projects shared between servers: the home server offering a replica to each
server hosting one of its members, and withdrawing it from one that no longer
does; and the replica, rebuilt from the project's stream as it arrives.
*/

// handleProjectOffer starts a replica of a project the signing server is home
// to, for the people of this server it names.
func (s *Server) handleProjectOffer(c *echo.Context) error {
	origin, _ := c.Get(peerOrigin).(string)
	home, err := federation.Host(origin)
	if err != nil || s.projects == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "unidentified peer")
	}
	var offer projects.Offer
	if err := c.Bind(&offer); err != nil || offer.Project == uuid.Nil() {
		return echo.NewHTTPError(http.StatusBadRequest, "could not read the offer")
	}
	if err := s.projects.AcceptOffer(c.Request().Context(), offer, home, origin); err != nil {
		if errors.Is(err, projects.ErrNobodyHere) || errors.Is(err, projects.ErrProjectNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, err.Error())
		}
		s.log.Warn("could not accept a project", "peer", home, "project", offer.Project, "error", err.Error())
		return echo.NewHTTPError(http.StatusInternalServerError, "could not accept the project")
	}
	return c.JSON(http.StatusOK, map[string]any{"accepted": true})
}

// handleProjectDiscard removes a replica at its home server's word. Answered
// the same whether there was one or not, so the home stops asking either way.
func (s *Server) handleProjectDiscard(c *echo.Context) error {
	origin, _ := c.Get(peerOrigin).(string)
	home, err := federation.Host(origin)
	if err != nil || s.projects == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "unidentified peer")
	}
	var request struct {
		Project uuid.UUID `json:"project"`
	}
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "could not read the request")
	}
	discarded, err := s.projects.DiscardReplica(c.Request().Context(), request.Project, home)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "could not discard the replica")
	}
	return c.JSON(http.StatusOK, map[string]any{"discarded": discarded})
}

/*
offerAndWithdraw does the home server's side of a delivery pass: brings who
holds which replica into line, then makes each offer and announces each
discard that is due. An offer accepted makes the server a peer of the project
from this pass on.
*/
func (s *Server) offerAndWithdraw(ctx context.Context, sender *federation.Sender) {
	if s.projects == nil {
		return
	}
	if err := s.projects.SyncReplicas(ctx); err != nil {
		s.log.Error("could not bring project replicas into line", "error", err.Error())
		return
	}
	work, err := s.projects.ClaimReplicaWork(ctx, federationDeliveryBatch)
	if err != nil || len(work) == 0 {
		return
	}
	for _, each := range work {
		address := each.Address
		if address == "" {
			address = s.peerAddressFor(ctx, each.Server)
		}
		if err := s.replicaWork(ctx, sender, each, address); err != nil {
			s.log.Warn("a project offer or withdrawal failed", "server", each.Server, "project", each.ProjectID, "error", err.Error())
			if recordErr := s.projects.ReplicaWorkFailed(ctx, each, err.Error(), federation.RetryDelay(int(each.Failures)+1)); recordErr != nil {
				s.log.Error("could not record a failed offer", "error", recordErr.Error())
			}
		}
	}
	if err := s.projects.SyncReplicas(ctx); err != nil {
		s.log.Error("could not bring project replicas into line", "error", err.Error())
	}
}

func (s *Server) replicaWork(ctx context.Context, sender *federation.Sender, work projects.ReplicaWork, address string) error {
	path, payload := "/api/federation/projects/discard", any(map[string]any{"project": work.ProjectID})
	if !work.Discarding {
		offer, err := s.projects.OfferFor(ctx, work)
		if err != nil {
			return err
		}
		path, payload = "/api/federation/projects/offer", offer
	}
	response, err := sender.Send(ctx, address, path, payload)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		reason, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return fmt.Errorf("%s answered %s: %s", work.Server, response.Status, strings.TrimSpace(string(reason)))
	}
	if work.Discarding {
		return s.projects.ForgetReplica(ctx, work)
	}
	return s.projects.ReplicaOffered(ctx, work, address)
}

/*
handleProjectChange makes a change somebody on another server asked for, in a
project this server is home to.

Three things are checked before anything is done: that the signing server
speaks for the person named, which a server may only do for its own people;
that it holds an accepted replica of the project, so a server with no part in
it cannot act in it; and that the person's project role allows the change,
which RemoteAccess and the ordinary methods decide exactly as they would for
somebody here. A refusal of the first two is logged, naming the peer.
*/
func (s *Server) handleProjectChange(c *echo.Context) error {
	ctx := c.Request().Context()
	origin, _ := c.Get(peerOrigin).(string)
	server, err := federation.Host(origin)
	if err != nil || s.projects == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "unidentified peer")
	}
	var request struct {
		Project uuid.UUID       `json:"project"`
		Actor   string          `json:"actor"`
		Change  projects.Change `json:"change"`
	}
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "could not read the change")
	}
	if err := s.peerSpeaksFor(origin, request.Actor); err != nil {
		s.log.Warn("refused a project change for somebody the peer does not host",
			"peer", server, "actor", request.Actor, "project", request.Project)
		return echo.NewHTTPError(http.StatusForbidden, err.Error())
	}
	holds, err := s.projects.HoldsReplica(ctx, request.Project, server)
	if err != nil {
		return err
	}
	if !holds {
		s.log.Warn("refused a project change from a server holding no replica of it",
			"peer", server, "actor", request.Actor, "project", request.Project)
		return echo.NewHTTPError(http.StatusNotFound, "no such project")
	}
	access, err := s.projects.RemoteAccess(ctx, request.Project, request.Actor)
	if err != nil {
		return projectError(err)
	}
	issue, err := s.projects.Apply(ctx, projects.Actor{Address: strings.ToLower(request.Actor)}, access, request.Change)
	if err != nil {
		return changeError(err)
	}
	return c.JSON(http.StatusOK, map[string]any{"issue": issue.Number})
}

// changeError is the answer to a change that could not be made, as a local
// request making it would have been answered.
func changeError(err error) error {
	switch {
	case errors.Is(err, projects.ErrUnknownChange),
		errors.Is(err, projects.ErrCycleNotFound),
		errors.Is(err, projects.ErrMilestoneNotFound):
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	return structureError(err)
}

/*
atHome makes a change to a replica on the server it lives on, in the name of
the person asking here, and returns the number of the issue changed.

Whatever the home answers, this server then catches the replica up before
replying, so the person sees the result — or, refused, the issue as it now is
— as they would after a change made here, rather than after the next delivery.
A refusal is passed on in the home's own words.
*/
func (s *Server) atHome(c *echo.Context, access projects.Access, change projects.Change) (int64, error) {
	ctx := c.Request().Context()
	user, ok := currentUser(c)
	if !ok {
		return 0, echo.NewHTTPError(http.StatusUnauthorized, "sign in first")
	}
	home := projects.HomeOf(access.Project)
	if !s.federationEnabled(c) {
		return 0, echo.NewHTTPError(http.StatusConflict,
			"this project lives on "+home+", and this server no longer federates, so it cannot be changed from here")
	}
	sender := federation.NewSender(s.config.CanonicalAddress, s.rooms.Keyring(), s.federationTrust(ctx),
		s.settings.Get(ctx, settings.FederationAllowPrivateAddresses) == "true")
	response, err := sender.Send(ctx, s.peerAddressFor(ctx, home), "/api/federation/projects/change", map[string]any{
		"project": access.Project.ID, "actor": s.rooms.UserAddress(user.Username), "change": change,
	})
	// What the change found out about the home is what the minute's check
	// would: said at once, rather than up to a minute later.
	if recordErr := s.projects.SetHomeReachable(ctx, access.Project.ID, err == nil); recordErr != nil {
		s.log.Warn("could not record whether a replica's home answered", "project", access.Project.ID, "error", recordErr.Error())
	}
	if err != nil {
		return 0, echo.NewHTTPError(http.StatusBadGateway, home+" could not be reached, so nothing was changed: "+err.Error())
	}
	defer response.Body.Close()
	var answer struct {
		Issue   int64  `json:"issue"`
		Message string `json:"message"`
	}
	_ = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&answer)

	if room, err := s.rooms.ByID(ctx, access.Project.ID); err == nil {
		if _, _, err := s.catchUp(ctx, room); err != nil {
			s.log.Warn("could not catch a replica up after a change", "project", access.Project.ID, "error", err.Error())
		}
	}
	if response.StatusCode != http.StatusOK {
		message := answer.Message
		if message == "" {
			message = home + " refused the change: " + response.Status
		}
		return 0, echo.NewHTTPError(response.StatusCode, message)
	}
	return answer.Issue, nil
}

// replicaPerson is somebody named in a request about a replica, as the home
// knows them: an address, with somebody here given this server's name.
func (s *Server) replicaPerson(name string) string {
	if strings.Contains(name, "@") {
		return strings.ToLower(name)
	}
	return s.rooms.UserAddress(strings.ToLower(name))
}
