package settings

import (
	"fmt"
	"strings"
)

/*
Voice and video calls.

Off unless an operator turns them on: a call needs things a chat server does
not — somewhere for media to go when two people cannot reach each other
directly — and an operator should decide to provide them rather than discover
their members trying calls that cannot connect.
*/
const FeatureCalls Feature = "calls"

const (
	/*
		CallsSTUNURLs and CallsTURNURLs are the servers a client uses to find a
		way to the other people in a call, comma-separated.

		Empty by default, on purpose: a public STUN server is a third party
		told who is calling whom, and a relay is something the operator runs.
		With neither, calls connect between people who can reach each other
		directly — on one network, or with addresses that are not hidden — and
		the client says what is missing when one does not.
	*/
	CallsSTUNURLs Key = "calls.stun_urls"
	CallsTURNURLs Key = "calls.turn_urls"

	/*
		CallsMaxParticipants caps a call. Every participant sends to every
		other, so past four or five a home connection runs out of upload
		before the call runs out of interest.
	*/
	CallsMaxParticipants Key = "calls.max_participants"

	/*
		CallsGuests lets people without an account join a call through a link
		made to allow them. Off by default: a guest is somebody the server
		knows nothing about, and an operator should choose to admit them.
	*/
	CallsGuests Key = "calls.guests"

	/*
		CallsRecording lets people holding call.record record a call on their
		own device, with everybody told first. Off by default: recording is a
		choice an operator makes for their community, not a default.
	*/
	CallsRecording Key = "calls.recording"
)

func init() {
	defineFeature(FeatureCalls, false,
		"Voice and video calls in channels and direct conversations. Nothing about calls is shown while this is off.")

	define(map[Key]definition{
		CallsSTUNURLs: {
			fallback:    "",
			description: "STUN servers clients use to find a direct path, comma-separated (stun:host:port).",
			validate:    iceURLs("stun:", "stuns:"),
		},
		CallsTURNURLs: {
			fallback:    "",
			description: "TURN relays for calls that cannot connect directly, comma-separated (turn:host:3478?transport=udp, turns:host:5349). Needs calls.turn_secret in configuration.",
			validate:    iceURLs("turn:", "turns:"),
		},
		CallsGuests: {
			fallback:    "false",
			description: "Whether people without an account may join a call through a link that allows guests.",
			validate:    boolean,
		},
		CallsRecording: {
			fallback:    "false",
			description: "Whether people holding call.record may record a call on their own device, everybody in it told first; the file is saved to the conversation as an attachment.",
			validate:    boolean,
		},
		CallsMaxParticipants: {
			fallback:    "4",
			description: "The most people one call may have.",
			validate:    atLeastOne,
		},
	})
}

// ListOf splits a comma-separated setting into its non-empty parts.
func ListOf(value string) []string {
	var parts []string
	for _, part := range strings.Split(value, ",") {
		if part = strings.TrimSpace(part); part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

// iceURLs accepts a comma-separated list whose every entry has one of the
// given schemes, so a typo is refused when set rather than failing every call.
func iceURLs(schemes ...string) func(string) error {
	return func(value string) error {
		for _, url := range ListOf(value) {
			ok := false
			for _, scheme := range schemes {
				ok = ok || strings.HasPrefix(url, scheme)
			}
			if !ok {
				return fmt.Errorf("%w: %q should start with %s", ErrInvalidValue, url, strings.Join(schemes, " or "))
			}
		}
		return nil
	}
}
