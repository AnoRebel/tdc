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

## 2. Phase 2 — Ringing, and who may call whom

- [x] 2.1 Ringing: `POST /api/rooms/:id/call/ring` rings people who may join (everybody else by default in a direct conversation); a `call.ring` frame reaches every device of theirs, across nodes; answering on one device stops the others; declining tells the caller; an unanswered ring leaves a `missed_call` notification; rate-limited per caller; verify somebody who may not join is not rung, and is counted back without being named; a ring also goes by web push to a browser in the background and as a banner from the desktop application
- [x] 2.2 Starting a call with somebody from their profile or a member list opens (or reuses) the direct conversation and rings them
- [x] 2.3 Who may call me, per person, with the other privacy settings: from this server — everybody, people I share a conversation with, or nobody; from connected servers — allowed or not; enforced by the server on every ring and every call started at somebody, and a refusal says nothing about why; verify each setting refuses what it says and nothing else
- [x] 2.4 The incoming call: a small, persistent window that never blocks the page, draggable anywhere and movable from the keyboard like the call window, showing who is calling and from which conversation, with accept by voice, accept with video, and decline; a ring sound that follows the person's sound and motion preferences; verify it stays while the person moves between pages and disappears on every device when answered on one
- [x] 2.5 Several calls at once: every call ringing is listed with its own answer and decline; while in a call, answering says it will end the current one
- [x] 2.6 Declining, or ending a call to take another, with a message: quick replies or the person's own words, posted in the conversation and shown to the caller at once
- [x] 2.7 Holding and switching: answering or placing a call while in another puts the first on hold — nothing sent to it, its people told you are holding — and the person switches between their calls freely; verify media goes only to the active call
- [x] 2.8 Merging into a conference: the people of a held call are invited into the active one as call participants, joining only if they accept, and seeing nothing of the active call's conversation if they are not in it; verify a merged participant cannot read the conversation
- [ ] **MILESTONE 2** — Demonstrate: somebody rings a colleague from their profile; it rings on two of the colleague's devices without covering what they were doing; answering on one stops the other; somebody whose privacy says "people I share a conversation with" cannot be rung by a stranger. Tag `v1.50.0-dev`.

## 3. Phase 3 — Call links, public or protected

- [x] 3.1 `call.link` permission (granted to nobody by default) and `call_links` with hashed tokens, public or protected, passcode and/or lobby, expiry, use limit, revocation, all audited; verify an expired, used-up or revoked link refuses
- [x] 3.2 The link page and joining by link as a call participant who is not a room member; verify every room route still refuses them and they see nothing of the conversation but its name and who is in the call
- [x] 3.3 Protected links: constant-time passcode check with failures limited per link and per address and recorded; a lobby that people in the call admit from; verify a wrong passcode past the limit is refused even with the right one
- [x] 3.4 Guests (`calls.guests`, off by default): a pass valid for one call only, a chosen name and a guest label, expiring with their leaving or the call's end; verify a guest pass opens nothing but that call's routes and connection
- [ ] **MILESTONE 3** — Demonstrate: a protected link with a lobby admits a signed-in person from elsewhere on the server and, with guests allowed, a guest without an account, neither of whom can see the conversation. Tag `v1.51.0-dev`.

## 4. Phase 4 — Calls across servers

- [ ] 4.1 Server setting `calls.federated` (off by default): whether people on connected servers may ring and call people here; checked before the person's own setting, so an operator's no is final
- [ ] 4.2 Federated call signalling: rings, call state and negotiation between servers as signed federated requests (the protocol's RFC 9421 signing), each server authorising its own people — the receiving server applies its policy and its person's privacy; each side gets relay credentials from its own server; verify a server not trusted, or with calls not federated, refuses and nothing reaches the person
- [ ] 4.3 Calls in rooms shared with other servers, with the home server holding the call as it holds the room
- [ ] 4.4 Protocol documentation for the call family; verify an independent implementation could ring and call a TDC user from the document
- [ ] **MILESTONE 4** — Demonstrate: a person on one server rings a person on another, who answers by video; with either server's federated calling off, or the person's setting refusing other servers, the ring never arrives. Tag `v1.52.0-dev`.

## 5. Phase 5 — Screens, presentations and recording

- [x] 5.1 Screen, window or tab sharing in a call (with tab audio where offered), shown to everybody; absent where the platform cannot
- [x] 5.2 Presenting: one presenter at a time, held by the server, on everybody's main stage; handed over, stopped, or cleared by a conversation moderator; verify a second person cannot present over the first
- [x] 5.3 `calls.recording` setting (off) and `call.record` permission (granted to nobody by default); verify recording is refused with either missing
- [x] 5.4 Starting a recording records `call.recording_started` in the room's log and the audit log and sets the call's recording state before the recorder's client begins; every participant is told, an indicator stays up, and somebody joining is told before their media starts; verify a recording cannot begin without the event and the state reaching the others
- [x] 5.5 Device-side recording (stage and mixed audio, MediaRecorder); on stop the file is saved to the conversation as an attachment through the ordinary path (limits, scanning, retention), or offered as a download when larger than the server accepts; `call.recording_stopped` recorded, including when the recorder leaves or drops
- [x] 5.6 Reactions in a call: raise and lower a hand (kept in order, shown to everybody, lowered by the person or a moderator), and passing reactions — clap, laugh, heart, thumbs up, surprise — floated over the sender's tile and counted for whoever presents; respect the motion preference; rate-limited per person; verify a reaction reaches only the call's participants
- [x] 5.7 Remote control of a shared screen: the person sharing their whole screen from the desktop application may hand control to one participant at a time, who sends pointer and keyboard input on a data channel; control is asked for, granted explicitly, shown on the sharer's screen, revocable at once (and by a key the controller cannot send), ends with the share, and is refused where the sharer is in a browser (which cannot move the pointer) or on a platform where input cannot be injected — saying which; verify no input is applied without a grant in force
- [ ] **MILESTONE 5** — Demonstrate: one person presents a browser tab, another records with everybody told, the recording appears in the conversation, and the conversation and audit log show who recorded and when. Tag `v1.53.0-dev`.

## 6. Phase 6 — Group calls

- [x] 6.1 Mesh calls up to `calls.mesh_limit`; the participant limit refused with its reason
- [x] 6.2 The built-in forwarding unit (Pion), off unless enabled, one peer connection per participant, simulcast layers per receiver; a call moves to it when it passes the mesh limit; verify a six-person call works with the unit on and is refused past the mesh limit with it off
- [ ] **MILESTONE 6** — Demonstrate a six-person call through the forwarding unit and a four-person mesh call without it. Tag `v1.54.0-dev`.

## 7. Phase 7 — Constrained networks and every client

- [x] 7.1 Degrade video then audio as bandwidth falls, never the call first; the client says what it changed
- [x] 7.2 Warn before starting or joining on a connection reported as metered
- [x] 7.3 Desktop: Windows, macOS (usage descriptions) and Linux (WebKitGTK with GStreamer WebRTC, declared in the packages) verified; a desktop that cannot call says why
- [x] 7.4 Keyboard shortcuts and motion preferences honoured by the call interface
- [x] 7.5 A call's start and end shown as lines in the conversation's timeline
- [ ] **MILESTONE 7** — Demonstrate a call that steps down to audio on a throttled connection and recovers, and a call between the web client and the desktop client. Tag `v1.55.0-dev`.

## 8. Phase 8 — Ready to run

- [ ] 8.1 Hosting guide: enabling calls, the relay (ports, secret), the forwarding unit (ports, what it can see), limits, federated calling, and recording (what it is and is not)
- [ ] 8.2 User guide: starting, joining, ringing, who may call you, links, controls, what others can see
- [x] 8.4 What hosting costs: a worked guide to the resources and money an event takes — voice only and with video, for audiences of 10, 50, 200 and 1,000, on one server and spread over several connected servers — with bandwidth, CPU and memory per person through the relay and the forwarding unit, and example prices from common hosts
- [ ] 8.3 Browser checks for two-person calls (fake media devices) in Chromium and Firefox, WebKit in CI
- [ ] **MILESTONE 8** — A stranger following the hosting guide enables calling with a relay and holds a call. Tag `v1.56.0-dev`.
