package settings

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/AnoRebel/tdc/server/internal/db/dbgen"
)

/*
Feature toggles.

A capability an operator can turn off at runtime, without a redeploy. Some
exist because a server may not want them at all — a private team server with no
use for public channel directories — and some because an operator is dealing
with a problem right now and needs the thing off before they can think about
why.

The rule that makes this worth having as a framework rather than a scattering
of booleans: one toggle governs both whether the capability is advertised and
whether requests to it are accepted. A client hides a control it is told is
absent, but hiding a control is not enforcement — anybody can still make the
request. Deriving both from the same value is what stops the advertised set and
the enforced set drifting apart, which is the failure that turns a disabled
feature into an undocumented one.
*/

// Feature names an optional capability.
type Feature string

const (
	// FeatureGifSearch is searching a third-party GIF library. Off unless an
	// operator has configured a provider, since it sends a query to somebody
	// else's service.
	FeatureGifSearch Feature = "gif-search"

	// FeatureLinkPreviews is fetching a summary of a linked page. Off by
	// default: it makes the server fetch a URL somebody else chose.
	FeatureLinkPreviews Feature = "link-previews"

	// FeaturePublicDirectory is listing public channels to somebody who has
	// not joined them. A server for one organisation may want every channel
	// found only by invitation.
	FeaturePublicDirectory Feature = "public-directory"

	// FeatureInvitations is issuing invitation links and codes. Turning this
	// off leaves existing invitations unusable and is how an operator stops
	// the flow of new people without closing registration outright.
	FeatureInvitations Feature = "invitations"

	// FeatureScheduledMessages is sending a message at a chosen time.
	FeatureScheduledMessages Feature = "scheduled-messages"

	// FeatureAttachments is uploading files at all. An operator with no
	// storage budget can turn it off without removing what is already there.
	FeatureAttachments Feature = "attachments"
)

/*
featureDefaults says whether a feature is on when nobody has chosen.

Everything a self-hosted server can do without reaching another service
defaults on: a fresh server should work, and an operator should not have to
discover a list of switches before their team can send a file. The two that
default off both talk to somebody else — a GIF provider, and whatever host a
pasted link points at — which is a decision an operator should make rather than
inherit.
*/
var featureDefaults = map[Feature]bool{
	FeatureGifSearch:         false,
	FeatureLinkPreviews:      false,
	FeaturePublicDirectory:   true,
	FeatureInvitations:       true,
	FeatureScheduledMessages: true,
	FeatureAttachments:       true,
}

// featureDescriptions is what an operator reading the list sees.
var featureDescriptions = map[Feature]string{
	FeatureGifSearch:         "Searching a third-party GIF library. Sends the search text to that provider.",
	FeatureLinkPreviews:      "Fetching a summary of a linked page. Makes this server request a URL somebody pasted.",
	FeaturePublicDirectory:   "Listing public channels to people who have not joined them.",
	FeatureInvitations:       "Issuing invitation links and codes.",
	FeatureScheduledMessages: "Sending a message at a chosen time.",
	FeatureAttachments:       "Uploading files and images.",
}

// featureKey is where a feature's toggle is stored.
//
// Namespaced under "features." so an operator reading the settings table can
// see at a glance which rows are toggles, and so a feature can never collide
// with a policy setting.
func featureKey(feature Feature) Key {
	return Key("features." + string(feature))
}

// Features lists every toggle, in a stable order so a list does not reshuffle
// between requests.
func Features() []Feature {
	features := make([]Feature, 0, len(featureDefaults))
	for feature := range featureDefaults {
		features = append(features, feature)
	}
	sort.Slice(features, func(i, j int) bool { return features[i] < features[j] })
	return features
}

// KnownFeature reports whether a name is a feature this server defines.
func KnownFeature(feature Feature) bool {
	_, ok := featureDefaults[feature]
	return ok
}

// DescribeFeature returns what a feature means and whether it is on by
// default.
func DescribeFeature(feature Feature) (description string, byDefault, ok bool) {
	byDefault, found := featureDefaults[feature]
	if !found {
		return "", false, false
	}
	return featureDescriptions[feature], byDefault, true
}

/*
Enabled reports whether a feature is on.

A feature whose toggle cannot be read falls back to its default, for the same
reason Get does: a server that refused to serve attachments because a settings
query timed out would be worse than one that briefly applied the default.
*/
func (s *Service) Enabled(ctx context.Context, feature Feature) bool {
	byDefault, known := featureDefaults[feature]
	if !known {
		// An undefined feature is off. A typo should not silently enable
		// something, and a caller asking about a feature this server does not
		// have is a bug worth failing closed on.
		return false
	}

	/*
		Read through raw rather than Get.

		Get is keyed on the definitions map, and returns "" for anything absent
		from it — which every feature key deliberately is, since a toggle is
		on-or-off where a setting is validated free text. Routing through Get
		would mean every feature silently sat at its default however the
		operator set it, which is exactly the bug this comment exists to stop
		coming back.
	*/
	value := s.raw(ctx, featureKey(feature))
	switch value {
	case "on":
		return true
	case "off":
		return false
	default:
		return byDefault
	}
}

// ErrUnknownFeature is returned for a feature this server does not define.
//
// Separate from ErrUnknownSetting so a caller can tell a misspelt toggle from
// a misspelt policy key, and so neither can be silently created.
var ErrUnknownFeature = errors.New("that is not a feature this server has")

/*
SetFeature turns a feature on or off.

Separate from Set rather than routed through it, because the value is a
different shape — a feature is on or off, where a setting is free text with a
per-key validator — and because a caller that can write arbitrary settings
should still not be able to invent a feature.

Written straight to the store rather than through Set, whose validation is
keyed on the definitions map that features deliberately do not appear in.
*/
func (s *Service) SetFeature(ctx context.Context, feature Feature, on bool, by string) error {
	if !KnownFeature(feature) {
		return fmt.Errorf("%w: %q", ErrUnknownFeature, feature)
	}

	value := "off"
	if on {
		value = "on"
	}

	key := featureKey(feature)
	err := s.queries.SetServerConfigValue(ctx, dbgen.SetServerConfigValueParams{
		Key:   string(key),
		Value: value,
		// A toggle is never a credential; there is nothing here to redact.
		IsSecret:  false,
		UpdatedBy: optional(by),
	})
	if err != nil {
		return fmt.Errorf("settings: writing %s: %w", key, err)
	}

	s.invalidate()

	s.log.Info("a feature was toggled", "feature", feature, "on", on, "by", by)
	return nil
}

/*
EnabledSet is every feature currently on.

This is what the server advertises, and it is derived from the same values
Enabled reads. A client is therefore told exactly what the server will accept:
there is no second list to keep in step.
*/
func (s *Service) EnabledSet(ctx context.Context) []string {
	enabled := []string{}
	for _, feature := range Features() {
		if s.Enabled(ctx, feature) {
			enabled = append(enabled, string(feature))
		}
	}
	return enabled
}
