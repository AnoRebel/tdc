Scope note: these tasks implement `calls/voice-video` against the running platform. Tasks reference the spec for what to build and `design.md` for how.

**Version rule: this change's milestone tags are reserved in the range `v1.49.0-dev` to `v1.56.0-dev`.**

**Documentation rule: documentation is written as the work is done.** The hosting guide's calling section and the user guide's calls section stay current with each phase.

**Phasing rule: every phase ends in a running, demonstrable system**, and a server with calling off behaves exactly as before.

## 1. Phase 1 — Calls exist, between two people

- [ ] 1.1 Add the calling feature toggle (off by default), and settings for STUN/TURN URLs, the TURN shared secret, mesh limit and participant limit; verify with calling off no calling endpoint answers, nothing is advertised, and the client shows no calling interface
- [ ] 1.2 Add `calls` and `call_participants` with at most one open call per room, `call.started`/`call.ended` in the room's log; enforce `call.start` and `call.join` at channel scope on top of `channel.post`, bans and restrictions; grant both to the Member role on new servers and by migration on existing ones; verify a role with posting but not calling can post and cannot start or join
- [ ] 1.3 Signalling frames on the live connection (`call.signal`, `call.state`), each authorised per frame for sender and recipient, crossing nodes through the broker's private path; verify a frame addressed to a non-participant, or from somebody no longer allowed, is refused and not relayed
- [ ] 1.4 `GET /api/calls/ice` with short-lived TURN credentials in coturn's shared-secret scheme; coturn as an optional compose service; verify credentials expire and are per person
- [ ] 1.5 Shared `useCall` and `CallPanel`: start, join with or without video, mute, camera, leave; nothing captured before permission; participants' state shown; departure updates everybody; verify two browsers on one machine complete a call, one audio-only
- [ ] **MILESTONE 1** — Demonstrate: an operator enables calling, two members of a channel hold a video call, one mutes and the other sees it, one leaves and the call ends in the conversation's history; with a TURN relay, a call between two networks that cannot reach each other connects. Tag `v1.49.0-dev`.

## 2. Phase 2 — Group calls

- [ ] 2.1 Mesh calls up to the mesh limit; the participant limit refused with its reason
- [ ] 2.2 The built-in forwarding unit (Pion), off unless enabled, one peer connection per participant, simulcast layers per receiver; a call moves to it when it passes the mesh limit; verify a six-person call works with the unit on and is refused past the mesh limit with it off
- [ ] **MILESTONE 2** — Demonstrate a six-person call through the forwarding unit and a four-person mesh call without it. Tag `v1.50.0-dev`.

## 3. Phase 3 — Screens, presentations and recording

- [ ] 3.1 Screen, window or tab sharing in a call (with tab audio where offered), shown to everybody; absent where the platform cannot
- [ ] 3.2 Presenting: one presenter at a time, held by the server, on everybody's main stage; handed over, stopped, or cleared by a conversation moderator; verify a second person cannot present over the first
- [ ] 3.3 `calls.recording` setting (off) and `call.record` permission (granted to nobody by default); verify recording is refused with either missing
- [ ] 3.4 Starting a recording records `call.recording_started` in the room's log and the audit log and sets the call's recording state before the recorder's client begins; every participant is told, an indicator stays up, and somebody joining is told before their media starts; verify a recording cannot begin without the event and the state reaching the others
- [ ] 3.5 Device-side recording (stage and mixed audio, MediaRecorder); on stop the file is saved to the conversation as an attachment through the ordinary path (limits, scanning, retention), or offered as a download when larger than the server accepts; `call.recording_stopped` recorded, including when the recorder leaves or drops
- [ ] **MILESTONE 3** — Demonstrate: one person presents a browser tab, another records with everybody told, the recording appears in the conversation, and the conversation and audit log show who recorded and when. Tag `v1.51.0-dev`.

## 4. Phase 4 — Constrained networks and every client

- [ ] 4.1 Degrade video then audio as bandwidth falls, never the call first; the client says what it changed
- [ ] 4.2 Warn before starting or joining on a connection reported as metered
- [ ] 4.3 Desktop: Windows, macOS (usage descriptions) and Linux (WebKitGTK with GStreamer WebRTC, declared in the packages) verified; a desktop that cannot call says why
- [ ] 4.4 Keyboard shortcuts and motion preferences honoured by the call interface
- [ ] **MILESTONE 4** — Demonstrate a call that steps down to audio on a throttled connection and recovers, and a call between the web client and the desktop client. Tag `v1.52.0-dev`.

## 5. Phase 5 — Ready to run

- [ ] 5.1 Hosting guide: enabling calls, the relay (ports, secret), the forwarding unit (ports, what it can see), limits, and recording (what it is and is not)
- [ ] 5.2 User guide: starting, joining, controls, what others can see
- [ ] 5.3 Browser checks for two-person calls (fake media devices) in Chromium and Firefox, WebKit in CI
- [ ] **MILESTONE 5** — A stranger following the hosting guide enables calling with a relay and holds a call. Tag `v1.53.0-dev`.
