Scope note: these tasks implement `calls/voice-video` against the running platform. Tasks reference the spec for what to build and `design.md` for how.

**Version rule: this change's milestone tags are reserved in the range `v1.49.0-dev` to `v1.56.0-dev`.**

**Documentation rule: documentation is written as the work is done.** The hosting guide's calling section and the user guide's calls section stay current with each phase.

**Phasing rule: every phase ends in a running, demonstrable system**, and a server with calling off behaves exactly as before.

## 1. Phase 1 — Calls exist, between two people

- [x] 1.1 Add the calling feature toggle (off by default), and settings for STUN/TURN URLs, the TURN shared secret, mesh limit and participant limit; verify with calling off no calling endpoint answers, nothing is advertised, and the client shows no calling interface
- [x] 1.2 Add `calls` and `call_participants` with at most one open call per room, `call.started`/`call.ended` in the room's log; enforce `call.start` and `call.join` at channel scope on top of `channel.post`, bans and restrictions; grant both to the Member role on new servers and by migration on existing ones; verify a role with posting but not calling can post and cannot start or join
- [x] 1.3 Signalling frames on the live connection (`call.signal`, `call.state`), each authorised per frame for sender and recipient, crossing nodes through the broker's private path; verify a frame addressed to a non-participant, or from somebody no longer allowed, is refused and not relayed
- [x] 1.4 `GET /api/calls/ice` with short-lived TURN credentials in coturn's shared-secret scheme; coturn as an optional compose service; verify credentials expire and are per person
- [x] 1.5 Shared `useCall` and `CallPanel`: start, join with or without video, mute, camera, leave; nothing captured before permission; participants' state shown; departure updates everybody; verify two browsers on one machine complete a call, one audio-only
- [ ] **MILESTONE 1** — Demonstrate: an operator enables calling, two members of a channel hold a video call, one mutes and the other sees it, one leaves and the call ends in the conversation's history; with a TURN relay, a call between two networks that cannot reach each other connects. Tag `v1.49.0-dev`.

## 2. Phase 2 — Group calls

- [ ] 2.1 Mesh calls up to the mesh limit; the participant limit refused with its reason
- [ ] 2.2 The built-in forwarding unit (Pion), off unless enabled, one peer connection per participant, simulcast layers per receiver; a call moves to it when it passes the mesh limit; verify a six-person call works with the unit on and is refused past the mesh limit with it off
- [ ] **MILESTONE 2** — Demonstrate a six-person call through the forwarding unit and a four-person mesh call without it. Tag `v1.50.0-dev`.

## 3. Phase 3 — Invites and links

- [ ] 3.1 Ringing: invite people who may join, every device of theirs rings (across nodes), accept joins and decline tells the caller, a missed call leaves a notification; rate-limited per caller; verify somebody who may not join is not rung and is listed back
- [ ] 3.2 `call.link` permission (granted to nobody by default) and `call_links` with hashed tokens, public or protected, passcode and/or lobby, expiry, use limit, revocation, all audited; verify an expired, used-up or revoked link refuses
- [ ] 3.3 The link page and joining by link as a call participant who is not a room member; verify every room route still refuses them and they see nothing of the conversation but its name and who is in the call
- [ ] 3.4 Protected links: constant-time passcode check with failures limited per link and per address and recorded; a lobby that people in the call admit from; verify a wrong passcode past the limit is refused even with the right one
- [ ] 3.5 Guests (`calls.guests`, off by default): a pass valid for one call only, a chosen name and a guest label, expiring with their leaving or the call's end; verify a guest pass opens nothing but that call's routes and connection
- [ ] **MILESTONE 3** — Demonstrate: a direct call rings on two devices and is answered on one; a protected link with a lobby admits a signed-in person from elsewhere on the server and, with guests allowed, a guest without an account, neither of whom can see the conversation. Tag `v1.51.0-dev`.

## 4. Phase 4 — Screens, presentations and recording

- [ ] 4.1 Screen, window or tab sharing in a call (with tab audio where offered), shown to everybody; absent where the platform cannot
- [ ] 4.2 Presenting: one presenter at a time, held by the server, on everybody's main stage; handed over, stopped, or cleared by a conversation moderator; verify a second person cannot present over the first
- [ ] 4.3 `calls.recording` setting (off) and `call.record` permission (granted to nobody by default); verify recording is refused with either missing
- [ ] 4.4 Starting a recording records `call.recording_started` in the room's log and the audit log and sets the call's recording state before the recorder's client begins; every participant is told, an indicator stays up, and somebody joining is told before their media starts; verify a recording cannot begin without the event and the state reaching the others
- [ ] 4.5 Device-side recording (stage and mixed audio, MediaRecorder); on stop the file is saved to the conversation as an attachment through the ordinary path (limits, scanning, retention), or offered as a download when larger than the server accepts; `call.recording_stopped` recorded, including when the recorder leaves or drops
- [ ] **MILESTONE 4** — Demonstrate: one person presents a browser tab, another records with everybody told, the recording appears in the conversation, and the conversation and audit log show who recorded and when. Tag `v1.52.0-dev`.

## 5. Phase 5 — Constrained networks and every client

- [ ] 5.1 Degrade video then audio as bandwidth falls, never the call first; the client says what it changed
- [ ] 5.2 Warn before starting or joining on a connection reported as metered
- [ ] 5.3 Desktop: Windows, macOS (usage descriptions) and Linux (WebKitGTK with GStreamer WebRTC, declared in the packages) verified; a desktop that cannot call says why
- [ ] 5.4 Keyboard shortcuts and motion preferences honoured by the call interface
- [ ] **MILESTONE 5** — Demonstrate a call that steps down to audio on a throttled connection and recovers, and a call between the web client and the desktop client. Tag `v1.53.0-dev`.

## 6. Phase 6 — Ready to run

- [ ] 6.1 Hosting guide: enabling calls, the relay (ports, secret), the forwarding unit (ports, what it can see), limits, and recording (what it is and is not)
- [ ] 6.2 User guide: starting, joining, controls, what others can see
- [ ] 6.3 Browser checks for two-person calls (fake media devices) in Chromium and Firefox, WebKit in CI
- [ ] **MILESTONE 6** — A stranger following the hosting guide enables calling with a relay and holds a call. Tag `v1.54.0-dev`.
