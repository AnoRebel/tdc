package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/federation"
	"github.com/AnoRebel/tdc/server/internal/projects"
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
	if err := c.Bind(&offer); err != nil || offer.Project == uuid.Nil {
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
		address := s.peerAddressFor(ctx, each.Server)
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
