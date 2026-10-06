## Context

The live connection (`server/internal/realtime`, `/api/live`) already authenticates people, subscribes them to rooms they may read, delivers frames per person (`SendToUser`) and between nodes (the broker, including private frames). Channel permissions are enforced per channel with the Member baseline (`authz.AllowsChannel`). Settings and feature toggles exist (`settings.define`, `requireFeature`). WebRTC needs three things from a server: an authorised relay for offers, answers and candidates; ICE servers; and, for larger calls, something that forwards media.

## Decisions

### A call is a row and two events

`calls` (id, room, started_by, started_at, ended_at, mode mesh|sfu) and `call_participants` (call, user, joined_at, left_at, audio, video). `call.started` and `call.ended` are appended to the room's log so a conversation shows that a call happened and how long it lasted; joining and leaving are live state, not history. At most one call per room is open at a time (a partial unique index), so "join the call" is unambiguous.

### Signalling is frames on the live connection

New frame types `call.signal` (offer, answer, candidate, addressed to one participant) and `call.state` (who is in the call, muted, camera). The server checks, for every signal frame: the sender is a current participant of that call; the recipient is too; the room still allows both `call.join` and reading for both. Frames cross nodes through the broker's private path, which already exists for exactly this shape: one person's connections, wherever they are.

*Why not a separate signalling service?* The connection, its authentication and its multi-node delivery exist; another one would duplicate each.

### Mesh first, forwarding unit above a size

A mesh call of n people sends n−1 uploads from each; past four or five that exceeds a typical home connection. `calls.mesh_limit` (default 4) chooses the mode when a call grows past it: with `calls.sfu_enabled` the call moves to the built-in forwarding unit; without it, the limit is the participant limit and further joins are refused with the reason. `calls.max_participants` caps either way.

The forwarding unit is Pion inside the server process: each participant has one peer connection to the server, tracks are forwarded without transcoding, simulcast layers chosen per receiver by bandwidth. It runs only when enabled, binds a configured UDP port range, and needs the relay for clients that cannot reach it directly.

*Why built in rather than LiveKit or another service?* One binary is the deployment the rest of TDC assumes, and a community's calls are small. A separate SFU stays possible later behind the same signalling.

### ICE and the relay

`GET /api/calls/ice` returns ICE servers for the person: STUN and TURN URLs from settings, and TURN credentials minted with coturn's REST shared-secret scheme (username = expiry:user, password = HMAC-SHA1(secret, username)), valid for the call's likely length. With no relay configured the list holds STUN only (or nothing, if none is set), and the client says when a call fails to connect that a relay would be needed. No public STUN server is configured by default: that would be a third party learning who calls; the operator chooses.

### Permissions

Starting needs `call.start`, joining `call.join`, both at channel scope and both in addition to `channel.post` (the spec: calling requires the same authorisation as posting, and its own permission). The Member role gains both on new servers and through a migration on existing ones, as `channel.read`/`channel.post` did, so enabling calls does not need a permissions exercise. Bans and posting restrictions apply.

### Client

A shared `useCall` composable owns the peer connections and media; `CallPanel.vue` shows tiles, controls and state; the conversation header shows a call in progress. Capture starts only after `getUserMedia` resolves with the person's permission, audio-only is a first-class join, and `navigator.connection` (where present) triggers the metered warning. Bandwidth: `RTCRtpSender.setParameters` steps video resolution and frame rate down, then disables video, before audio is touched, driven by `getStats` and, in SFU mode, by the server's estimates.

### Desktop

WebView2 (Windows) and WKWebView (macOS, with the camera and microphone usage descriptions in Info.plist) support WebRTC; WebKitGTK does when built with it and with GStreamer's WebRTC plugins present, which the Linux packages declare as dependencies. Each is verified, and a desktop that cannot place calls says why instead of failing silently.

### Screen sharing and presenting

`getDisplayMedia` gives a screen, window or tab track, added to every peer connection as a second video track (in the forwarding unit, one more track to forward). The call's live state gains `presenter`: one person at a time; setting it is a request to the server, which refuses a second presenter until the first stops and lets a conversation moderator clear it. Clients show the presenter's screen on the main stage and everybody else as a strip. Where a webview has no `getDisplayMedia`, the share control is absent rather than broken.

### Recording on the recorder's device

The recorder's client composes what it receives — the stage onto a canvas, every participant's audio into one Web Audio destination — and records it with `MediaRecorder` (WebM; MP4 where that is all the platform offers). The server's part is permission and disclosure, not media:

- `calls.recording` setting, off by default; `call.record` permission, granted to nobody by default (a moderator's).
- Starting is a request; the server records `call.recording_started` in the room's log and the audit log, sets `recording` on the call's state, and only then does the recorder's client begin. Every participant's client shows the notice and keeps an indicator up; a person joining a call being recorded is shown that before their own media starts, and may leave instead.
- Stopping (or the recorder leaving, or their connection dropping — the sweep handles that) records `call.recording_stopped`. The file is uploaded as an attachment to the conversation through the existing upload path, so the attachment limit, scanning and retention all apply; a file over the limit is offered as a download instead, and the client says so.

*Why not record on the server?* Mesh calls never pass media through the server, and the spec's privacy requirement is easiest to keep by not changing that. A server-side recorder would need the forwarding unit for every recorded call and would hold everybody's media in the clear.

## Risks / Trade-offs

- **SFU sees media** unencrypted in the server process (end-to-end encryption through insertable streams is not universally available in webviews). Stated in the hosting guide; mesh calls never pass media through the server.
- **No relay, no call for some people.** Stated, and diagnosed in the client.
- **WebKitGTK variance** across distributions. Verified on the supported Linux builds; the rest are told why calls are unavailable.
- **A recording is only as good as the recorder's connection**: what they did not receive is not in it. Stated in the user guide.
- **A participant can always record their own screen with other software.** The recording feature is about doing it openly, with notice and a record; it cannot prevent what it does not control, and the user guide says so.
