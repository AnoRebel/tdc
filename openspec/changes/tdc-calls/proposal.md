## Why

A community that meets in TDC still leaves it to talk. A pairing session, a mentorship hour, a chapter's weekly stand-up, a client walking a developer through what they need — each is a link to somebody else's meeting product, with somebody else's account, data and pricing. `calls/voice-video` has been specified since the foundation and is the one capability on the original roadmap with nothing built.

## What Changes

This change implements `calls/voice-video` in full, as an optional capability, off by default, that needs no paid service.

- **Calls in conversations.** A call belongs to a channel or a direct conversation; starting and joining take `call.start` and `call.join` (catalogued since the foundation, enforced for the first time), on top of being able to post there. Members see that a call is in progress and join it from the conversation.
- **Signalling over the connection clients already hold.** Session negotiation is relayed through the existing live connection as frames of their own, every one authorised against the call's conversation; the server never relays a frame to somebody who may not be in the call.
- **Media peer to peer, relayed when it must be.** WebRTC, encrypted in transit as WebRTC always is (DTLS-SRTP). Direct where the network permits; through a TURN relay where it does not. The relay is coturn (BSD licence), optional in the compose file, and the server issues short-lived relay credentials so the relay is not an open proxy.
- **Group calls.** Small calls are a full mesh and need nothing deployed. Above an operator-set size, a selective forwarding unit carries the call: built into the server with Pion (MIT), so it is one binary and no extra service, and off unless the operator turns it on. A per-call participant limit, enforced with the reason given.
- **Controls.** Mute, camera on and off, leave, audio-only joining, each participant's state shown to the others; nothing captured before the person grants the browser or system permission; a warning before starting on a connection the platform reports as metered.
- **Degrading rather than dropping.** Bandwidth estimates step video down, then off, before audio; the client says what it did.
- **Clients.** The web and desktop clients share the call interface through the shared layer. The desktop webviews' media support differs by platform and is verified on each.

- **Screen sharing and presentations.** Anybody in a call may share a screen, a window or a browser tab (with its audio where the platform offers it). Presenting puts one person's shared screen on everybody's main stage; one presenter at a time, handed over or stopped by them or by a moderator of the conversation.
- **Recording, saved to the conversation.** Off unless the operator enables it, and then only by somebody holding the new `call.record` permission in that conversation. Recording happens on the recorder's own device — the call's audio and the stage composed into one file in the browser — so no call media passes through or is retained by the server for it. Before recording begins every participant is told, and an indicator stays on screen for everybody while it lasts, including people who join later (who are told before their own media starts). When it stops, the file is saved to the conversation as an attachment through the ordinary attachment path — size limits, scanning and retention apply — or offered as a download where it is larger than the server accepts. Starting and stopping are in the conversation's log and the audit log.

**Out of scope:** recording on the server (it would need the server to see media it otherwise never does); calls in rooms shared with other servers (a federated room shows that calls are available to its home server's members only).

## Capabilities

### Modified Capabilities

- `calls/voice-video`: specified-but-unimplemented to implemented, with screen sharing, presentations and device-side recording added to its requirements.
- `platform/server-config`: the calling switch, relay settings, mesh size and participant limit.
- `identity/authorization`: `call.start` and `call.join` enforced; `call.record` added.

## Impact

- **Dependencies:** Pion WebRTC (MIT) in the server, only exercised when the forwarding unit is on; coturn as an optional compose service. Nothing paid, nothing AGPL.
- **Operators:** a relay needs UDP ports open; the hosting guide states which, and what works without one (calls between people on reachable networks, and nothing else).
- **Security:** the relay's credentials are minted per person per call and expire; signalling is authorised per frame; the forwarding unit sees media in the clear inside the server process, which the hosting guide states plainly because it differs from a mesh call.
