package httpapi

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v5"

	"github.com/AnoRebel/tdc/server/internal/audit"
	"github.com/AnoRebel/tdc/server/internal/cache"
	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
	"github.com/AnoRebel/tdc/server/internal/identity"
	"github.com/AnoRebel/tdc/server/internal/settings"
)

/*
What somebody is called, changed by them.

Two names with very different weight. The display name is how people read
somebody and nothing is built on it, so it changes freely. The username is half
of the address user@server — what links, filters, other servers and old
conversations name somebody by — so changing it is paced by the operator, asks
the person to prove it is them, keeps the old name theirs for a while, and is
refused outright where another server knows them by the old one.
*/
func init() {
	registerRoutes(func(s *Server) {
		me := s.echo.Group("/api/me", requireAuthentication)
		me.PUT("/display-name", s.handleSetDisplayName)
		me.GET("/username", s.handleUsernamePolicy)
		me.GET("/username/availability", s.handleUsernameAvailability)
		me.PUT("/username", s.handleChangeUsername)
	})
	documentRoutes(map[string]entry{
		"PUT /api/me/display-name":          {Summary: "Change your display name", Authentication: "session or token"},
		"GET /api/me/username":              {Summary: "Whether you may change your username now, and what it asks", Authentication: "session or token"},
		"GET /api/me/username/availability": {Summary: "Whether a username is free for you to take", Authentication: "session or token"},
		"PUT /api/me/username":              {Summary: "Change your username", Authentication: "session"},
	})
}

var (
	// Generous: a display name is fixed by retyping it, and somebody fixing a
	// typo should not meet a limit. Bounded, so a name cannot flicker.
	displayNameChanges = cache.RateLimit{Attempts: 20, Window: time.Hour}
	/*
		Attempts, not changes: each one may carry a password, so this is also
		what stops the endpoint being a way to guess one with a stolen session.
	*/
	usernameChangeAttempts = cache.RateLimit{Attempts: 5, Window: time.Hour, LockoutAfter: time.Hour}
	// Asked as somebody types, debounced by the client; enough for that and
	// not enough to walk the namespace.
	usernameChecks = cache.RateLimit{Attempts: 60, Window: 5 * time.Minute}
)

/*
recentSignIn is how fresh a session must be to change the username of an
account with no password to ask for.

Somebody with only a passkey or a provider sign-in has nothing to type, so the
proof is having just signed in — which, where they have a second factor, means
having just given it.
*/
const recentSignIn = 15 * time.Minute

type displayNameRequest struct {
	DisplayName string `json:"displayName"`
}

func (s *Server) handleSetDisplayName(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)

	decision, err := s.limiter.Allow(ctx, "display-name:"+user.ID.String(), displayNameChanges)
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return retryAfter(c, decision.RetryAfter)
	}

	var request displayNameRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	name := strings.TrimSpace(request.DisplayName)
	if err := identity.ValidateDisplayName(name); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	// Empty means "show my username", as it does at registration.
	if name == "" {
		name = user.Username
	}

	updated, err := s.queries.UpdateUserProfile(ctx, dbgen.UpdateUserProfileParams{ID: user.ID, DisplayName: name})
	if err != nil {
		return err
	}

	if updated.DisplayName != user.DisplayName {
		s.recordModeration(c, user, audit.ActionDisplayNameChanged, "user", user.ID.String(),
			map[string]any{"previous": user.DisplayName, "displayName": updated.DisplayName})
	}

	return c.JSON(http.StatusOK, s.userResponse(updated))
}

// formerName is a username an account gave up, and when.
type formerName struct {
	Username  string    `json:"username"`
	ChangedAt time.Time `json:"changedAt"`
}

type usernamePolicyResponse struct {
	Username string `json:"username"`
	// CanChange is whether a change would be accepted now, password aside.
	CanChange bool `json:"canChange"`
	/*
		Refusal says why not: "disabled" (the operator does not allow it),
		"federated" (another server knows this person by their address),
		"waiting" (changed too recently), or "token" (asked with an API token).
		Message says the same to a person.
	*/
	Refusal string `json:"refusal,omitempty"`
	Message string `json:"message,omitempty"`
	// NextChangeAt is when waiting ends.
	NextChangeAt *time.Time `json:"nextChangeAt,omitempty"`
	// IntervalDays and HoldDays are the operator's pacing, so the client can
	// say what a change commits somebody to before they make it.
	IntervalDays int64 `json:"intervalDays"`
	HoldDays     int64 `json:"holdDays"`
	/*
		Confirm is what the change will ask for: "password", or
		"recent-sign-in" for an account with no password, whose session must be
		younger than RecentSignInUntil allows.
	*/
	Confirm           string       `json:"confirm"`
	RecentSignInUntil *time.Time   `json:"recentSignInUntil,omitempty"`
	FormerUsernames   []formerName `json:"formerUsernames"`
}

// usernameBlock is why somebody may not change their username now, if they
// may not.
type usernameBlock struct {
	refusal, message string
	next             *time.Time
}

/*
usernameChangeBlocked decides whether a change may go ahead, before asking
for any proof.

Read by the policy route, so the client can explain before somebody types, and
again by the change itself — the client's copy is a courtesy, this is the rule.
*/
func (s *Server) usernameChangeBlocked(c *echo.Context, ctx context.Context, store dbgen.Querier, user dbgen.User) (*usernameBlock, error) {
	if _, isToken := currentAPIToken(c); isToken {
		return &usernameBlock{refusal: "token",
			message: "a username can be changed only by the person, signed in, not by a program holding a token"}, nil
	}

	if s.settings.Get(ctx, settings.UsernameChanges) != "true" {
		return &usernameBlock{refusal: "disabled",
			message: "this server does not allow usernames to be changed"}, nil
	}

	federated, err := store.InFederatedRoom(ctx, dbgen.InFederatedRoomParams{UserID: user.ID, ServerName: s.serverName()})
	if err != nil {
		return nil, err
	}
	if federated {
		return &usernameBlock{refusal: "federated",
			message: "you are in a conversation shared with another server, which knows you by your current address and cannot be told it changed; leave those conversations to change your username"}, nil
	}

	if days := s.settings.Int(ctx, settings.UsernameChangeDays); days > 0 {
		last, err := store.LastUsernameChange(ctx, user.ID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return nil, err
		default:
			next := last.Add(time.Duration(days) * 24 * time.Hour)
			if time.Now().Before(next) {
				return &usernameBlock{refusal: "waiting", next: &next,
					message: "you changed your username recently; you can change it again after " + next.UTC().Format("2 January 2006")}, nil
			}
		}
	}

	return nil, nil
}

// confirmation is what a username change asks of this account.
func (s *Server) confirmation(ctx context.Context, user dbgen.User) (hasPassword bool, err error) {
	_, err = s.queries.GetPasswordCredential(ctx, user.ID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	case err != nil:
		return false, err
	}
	return true, nil
}

func (s *Server) handleUsernamePolicy(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)

	block, err := s.usernameChangeBlocked(c, ctx, s.queries, user)
	if err != nil {
		return err
	}
	hasPassword, err := s.confirmation(ctx, user)
	if err != nil {
		return err
	}

	response := usernamePolicyResponse{
		Username:        user.Username,
		CanChange:       block == nil,
		IntervalDays:    s.settings.Int(ctx, settings.UsernameChangeDays),
		HoldDays:        s.settings.Int(ctx, settings.UsernameHoldDays),
		Confirm:         "password",
		FormerUsernames: []formerName{},
	}
	if block != nil {
		response.Refusal, response.Message, response.NextChangeAt = block.refusal, block.message, block.next
	}
	if !hasPassword {
		response.Confirm = "recent-sign-in"
		if session, ok := currentSession(c); ok {
			until := session.CreatedAt.Add(recentSignIn)
			response.RecentSignInUntil = &until
		}
	}

	former, err := s.queries.ListFormerUsernames(ctx, user.ID)
	if err != nil {
		return err
	}
	for _, row := range former {
		response.FormerUsernames = append(response.FormerUsernames, formerName{Username: row.Username, ChangedAt: row.ChangedAt})
	}

	return c.JSON(http.StatusOK, response)
}

type usernameAvailability struct {
	Username  string `json:"username"`
	Available bool   `json:"available"`
	// Reason is "invalid", "current", "taken" or "held" when it is not.
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

/*
usernameAvailable says whether this person may take a name, and if not why.

The same answer the change gives, so a name the field said was free is not
refused on submit for a reason it could have said sooner. The change still
asks again inside its transaction: two people can be told one name is free.
*/
func (s *Server) usernameAvailable(ctx context.Context, user dbgen.User, wanted string) (usernameAvailability, error) {
	wanted = strings.TrimSpace(wanted)
	answer := usernameAvailability{Username: wanted}

	if err := identity.ValidateUsername(wanted); err != nil {
		answer.Reason, answer.Message = "invalid", err.Error()
		return answer, nil
	}
	if wanted == user.Username {
		answer.Reason, answer.Message = "current", "that is already your username"
		return answer, nil
	}

	folded := identity.NormaliseUsername(wanted)
	holder, err := s.queries.GetUserByCurrentUsername(ctx, folded)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return answer, err
	case holder.ID != user.ID:
		answer.Reason, answer.Message = "taken", "somebody already has that username"
		return answer, nil
	}

	held, err := s.identity.UsernameHeld(ctx, folded, user.ID)
	if err != nil {
		return answer, err
	}
	if held {
		answer.Reason, answer.Message = "held",
			"somebody had that username recently, and it is kept for them for a while"
		return answer, nil
	}

	answer.Available = true
	return answer, nil
}

func (s *Server) handleUsernameAvailability(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)

	decision, err := s.limiter.Allow(ctx, "username-check:"+user.ID.String(), usernameChecks)
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return retryAfter(c, decision.RetryAfter)
	}

	answer, err := s.usernameAvailable(ctx, user, c.QueryParam("username"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, answer)
}

type changeUsernameRequest struct {
	Username string `json:"username"`
	// Password is the account's current password, where it has one.
	Password string `json:"password"`
}

func (s *Server) handleChangeUsername(c *echo.Context) error {
	ctx := c.Request().Context()
	user, _ := currentUser(c)

	// Counted before anything else, including the password check, so that
	// a stolen session cannot use this to guess the password underneath it.
	decision, err := s.limiter.Allow(ctx, "username-change:"+user.ID.String(), usernameChangeAttempts)
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return retryAfter(c, decision.RetryAfter)
	}

	var request changeUsernameRequest
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the request body could not be read")
	}

	if block, err := s.usernameChangeBlocked(c, ctx, s.queries, user); err != nil {
		return err
	} else if block != nil {
		return echo.NewHTTPError(http.StatusForbidden, block.message)
	}

	if err := s.confirmIdentity(c, user, request.Password); err != nil {
		return err
	}

	answer, err := s.usernameAvailable(ctx, user, request.Username)
	if err != nil {
		return err
	}
	if !answer.Available {
		status := http.StatusConflict
		if answer.Reason == "invalid" || answer.Reason == "current" {
			status = http.StatusBadRequest
		}
		return echo.NewHTTPError(status, answer.Message)
	}

	updated, err := s.renameUser(c, user.ID, answer.Username)
	if err != nil {
		return err
	}

	response := s.userResponse(updated)
	response.FormerAddresses, err = s.formerAddresses(ctx, updated.ID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, response)
}

/*
confirmIdentity asks the person to prove it is them.

The password where the account has one. Where it has none — a passkey or a
provider is how they sign in — a session they started within the last few
minutes, so a session left open on a borrowed computer cannot be used to
take somebody's name.
*/
func (s *Server) confirmIdentity(c *echo.Context, user dbgen.User, password string) error {
	ctx := c.Request().Context()

	hasPassword, err := s.confirmation(ctx, user)
	if err != nil {
		return err
	}

	if hasPassword {
		if password == "" {
			return echo.NewHTTPError(http.StatusForbidden, "enter your password to confirm it is you")
		}
		if _, err := s.identity.VerifyPasswordOnly(ctx,
			identity.Credentials{Username: user.Username, Password: password}); err != nil {
			if errors.Is(err, identity.ErrAuthenticationFailed) {
				// 403 rather than 401: the session is fine, and a client that
				// reads 401 as "signed out" would sign somebody out for a typo.
				return echo.NewHTTPError(http.StatusForbidden, "that password is not right")
			}
			return err
		}
		return nil
	}

	session, ok := currentSession(c)
	if !ok || time.Since(session.CreatedAt) > recentSignIn {
		return echo.NewHTTPError(http.StatusForbidden,
			"sign out and in again, then change your username within 15 minutes, to confirm it is you")
	}
	return nil
}

/*
renameUser gives an account a new username and keeps the old one for it.

In one transaction, under a lock on each name involved and on the account:

  - the account lock stops two changes by the same person both reading the
    same "last changed" and both passing the wait;
  - the name locks stop somebody claiming a name in the instant its owner is
    giving it up, before the record that it is held exists.

Locks are taken in a fixed order, so two people swapping names cannot deadlock.
The audit entry is written inside the same transaction: a username never
changes without the record of what it was.
*/
func (s *Server) renameUser(c *echo.Context, userID uuid.UUID, wanted string) (dbgen.User, error) {
	ctx := c.Request().Context()

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return dbgen.User{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)

	if err := queries.LockUsername(ctx, "account:"+userID.String()); err != nil {
		return dbgen.User{}, err
	}
	current, err := queries.GetUserByID(ctx, userID)
	if err != nil {
		return dbgen.User{}, err
	}

	names := []string{current.UsernameFolded, identity.NormaliseUsername(wanted)}
	sort.Strings(names)
	for _, name := range names {
		if err := queries.LockUsername(ctx, name); err != nil {
			return dbgen.User{}, err
		}
	}

	// Asked again now that nothing else can move: the wait, and whether the
	// name is still free.
	if block, err := s.usernameChangeBlocked(c, ctx, queries, current); err != nil {
		return dbgen.User{}, err
	} else if block != nil {
		return dbgen.User{}, echo.NewHTTPError(http.StatusForbidden, block.message)
	}
	held, err := s.identity.UsernameHeld(ctx, identity.NormaliseUsername(wanted), userID)
	if err != nil {
		return dbgen.User{}, err
	}
	if held {
		return dbgen.User{}, echo.NewHTTPError(http.StatusConflict,
			"somebody had that username recently, and it is kept for them for a while")
	}

	updated, err := queries.RenameUser(ctx, dbgen.RenameUserParams{
		ID: userID, Username: wanted, UsernameFolded: identity.NormaliseUsername(wanted),
	})
	if err != nil {
		if isUniqueViolation(err) {
			return dbgen.User{}, echo.NewHTTPError(http.StatusConflict, "somebody already has that username")
		}
		return dbgen.User{}, err
	}

	if err := queries.RecordFormerUsername(ctx, dbgen.RecordFormerUsernameParams{
		UserID: userID, Username: current.Username, UsernameFolded: current.UsernameFolded,
	}); err != nil {
		return dbgen.User{}, err
	}

	if err := audit.NewRecorder(queries, s.log).Record(ctx, audit.Entry{
		ActorID:    &userID,
		ActorLabel: updated.Username,
		Action:     audit.ActionUsernameChanged,
		TargetKind: "user",
		TargetID:   userID.String(),
		SourceIP:   requestIP(c),
		UserAgent:  c.Request().UserAgent(),
		Detail:     map[string]any{"previous": current.Username, "username": updated.Username},
	}); err != nil {
		return dbgen.User{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return dbgen.User{}, err
	}
	return updated, nil
}

// formerAddresses are the addresses this account was known by, newest first,
// so a client can still recognise the messages it sent under them as its own.
func (s *Server) formerAddresses(ctx context.Context, userID uuid.UUID) ([]string, error) {
	former, err := s.queries.ListFormerUsernames(ctx, userID)
	if err != nil {
		return nil, err
	}
	addresses := make([]string, 0, len(former))
	for _, row := range former {
		addresses = append(addresses, row.Username+"@"+s.serverName())
	}
	return addresses, nil
}
