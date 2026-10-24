import type { CallUnsupported } from "#layers/ui/app/composables/useCallSupport";
import { callSupportKey } from "#layers/ui/app/composables/useCallSupport";
import type {
  Call,
  CallDeclined,
  CallLinkUse,
  CallLobby,
  CallReaction,
  CallReactionKind,
  CallRing,
  CallSignal,
  IceConfiguration,
} from "@tdc/types";
import { isApiError } from "@tdc/types";
import { createGlobalState } from "@vueuse/core";

import {
  encodingFor,
  initialQuality,
  stepQuality,
  type QualityLevel,
  type QualityState,
} from "#layers/ui/app/lib/callQuality";
import { nameOf } from "#layers/ui/app/lib/names";

/**
 * A conversation's call, from this person's side.
 *
 * Media goes peer to peer: one RTCPeerConnection per other participant, each
 * negotiated with the "perfect negotiation" pattern, so it does not matter who
 * offers first or whether both do at once — the impolite side's offer wins, the
 * polite side rolls back. The server only relays the negotiation and says who
 * is in the call; nothing a call carries passes through it.
 *
 * Every peer gets an audio and a video transceiver from the start, so turning
 * the camera or microphone on and off swaps or mutes a track without
 * renegotiating.
 *
 * Calls belong to the whole application, not to a page: a call goes on while
 * somebody reads another conversation, opens a project or changes a setting.
 * The call window in the layout shows it and the call host holds the
 * connection its negotiation travels on; a conversation only offers to start
 * or join.
 *
 * Somebody can be in several calls at once, talking in one and holding the
 * rest, as on a phone. One microphone and camera serve them all: the active
 * call's connections carry them, a held call's carry nothing, and nothing a
 * held call sends is played. The others in a held call are told, so they know
 * why it went quiet.
 */

type SignalSender = (call: string, to: string, body: CallSignal["signal"]) => void;

/** How the call window is shown: large, small in a corner, or a bar. */
export type CallView = "expanded" | "docked" | "minimized";

/*
 * What each connection carries, in this order: microphone, camera, shared
 * screen, the screen's sound. The order is the same on both sides of every
 * connection, which is how a receiver tells a camera from a screen.
 */
const SLOTS = ["audio", "video", "video", "audio"] as const;

interface Peer {
  connection: RTCPeerConnection;
  /**
   * The senders, by slot — null on the polite side until the other side's
   * first offer arrives, since its transceivers are made from that offer.
   */
  senders: RTCRtpSender[] | null;
  polite: boolean;
  makingOffer: boolean;
  ignoreOffer: boolean;
  /** A data channel between the two, for remote control of a shared screen. */
  control: RTCDataChannel | null;
}

/** A message on a peer's control channel; `gone` is said here when the peer goes. */
export type ControlMessage = { t: string } & Record<string, unknown>;
type ControlListener = (room: string, from: string, message: ControlMessage) => void;

export const useCall = createGlobalState(() => {
  const api = useApi();
  const rooms = useRooms();
  const { user } = useAuth();

  /**
   * The conversation of the call shown — the one being talked in, or the one
   * last talked in while every call is held. Shared state by name, so the
   * layout can mount the call window when a call begins without loading any of
   * the call's code before then.
   */
  const roomId = useState<string | null>("call.room", () => null);
  const view = ref<CallView>("docked");
  let sendSignal: SignalSender = () => {};
  let controlListener: ControlListener = () => {};
  /** Set by remote control, which is the only thing that talks on the channel. */
  function onControl(listener: ControlListener) {
    controlListener = listener;
  }
  /** Sends a message to one person in a call on its control channel. */
  function sendControl(room: string, to: string, message: ControlMessage): boolean {
    const channel = peers.get(room)?.get(to)?.control;
    if (channel?.readyState !== "open") return false;
    channel.send(JSON.stringify(message));
    return true;
  }
  /** Set by the call host, whose connection carries the negotiation. */
  function setSignaller(sender: SignalSender) {
    sendSignal = sender;
  }

  /** Every call we are in, by conversation, as the server last described it. */
  const calls = ref<Record<string, Call>>({});
  /** The conversations whose calls we are holding. */
  const holding = ref<string[]>([]);
  /**
   * The conversations whose calls we were let into without being in the
   * conversation — merged in by somebody. Their calls are reached by the
   * call's own address, and nothing offers to open the conversation.
   */
  const guestOf = ref<string[]>([]);
  /** The call shown. */
  const call = computed<Call | null>(
    () => (roomId.value ? calls.value[roomId.value] : null) ?? null,
  );
  const joined = computed(() => Boolean(call.value));
  /** Whether the call shown is one we are holding. */
  const onHold = computed(() => Boolean(roomId.value && holding.value.includes(roomId.value)));
  /** The calls we are in other than the one shown. */
  const others = computed(() =>
    Object.values(calls.value).filter((each) => each.roomId !== roomId.value),
  );
  const joining = ref(false);
  const microphone = ref(true);
  const camera = ref(false);
  const error = ref<string | null>(null);
  /** Our own microphone and camera, shared by every call we are in. */
  const local = shallowRef<MediaStream | null>(null);
  /** What each other participant is sending, by conversation and user id. */
  const remotes = shallowReactive(new Map<string, Map<string, MediaStream>>());
  /** What the people in the call shown are sending, by user id. */
  const remote = computed(() => remotes.get(roomId.value ?? "") ?? new Map<string, MediaStream>());
  /** The screens people are sharing, by conversation and user id. */
  const remoteScreens = shallowReactive(new Map<string, Map<string, MediaStream>>());
  const remoteScreen = computed(
    () => remoteScreens.get(roomId.value ?? "") ?? new Map<string, MediaStream>(),
  );
  /** Our own shared screen, while we share it. */
  const screen = shallowRef<MediaStream | null>(null);
  /** Whether this device can share a screen at all. */
  const canShareScreen = computed(
    () => !import.meta.server && Boolean(navigator.mediaDevices?.getDisplayMedia),
  );

  /**
   * A call being recorded that we are joining: who is recording it, shown
   * before anything of ours is sent, until the person says whether to stay.
   */
  const recordingNotice = ref<{ room: string; names: string[] } | null>(null);
  let answerRecordingNotice: ((stay: boolean) => void) | null = null;
  /** Stays in a call being recorded, or leaves it before anything is sent. */
  function answerRecording(stay: boolean) {
    answerRecordingNotice?.(stay);
  }

  /** People ringing us now, newest last. */
  const incoming = ref<CallRing[]>([]);
  /** The last person who declined our ring, for a moment's notice. */
  const declined = ref<CallDeclined | null>(null);

  /** Who is waiting to be let into each call we are in, by call. */
  const lobbies = ref<Record<string, CallLobby["waiting"]>>({});
  /**
   * Our own wait in a call's lobby, after using a link that has one: until
   * somebody in the call lets us in or turns us away.
   */
  const waitingIn = ref<{
    callId: string;
    roomId: string;
    video: boolean;
    outcome: "waiting" | "turned-away";
  } | null>(null);

  /** Each call's connections, by conversation and then by the other person. */
  const peers = new Map<string, Map<string, Peer>>();
  /*
   * Negotiation that arrives while we are still joining. The others learn we
   * are in the call as soon as the server records it, and may send an offer
   * before our own request has come back; dropped, that offer would leave the
   * two sides waiting on each other for good.
   */
  let early: CallSignal[] = [];
  let iceServers: RTCIceServer[] = [];

  /**
   * Whether the platform says this connection is metered or asks to save data,
   * so a person can be warned before video starts. Absent where the platform
   * does not say.
   */
  const metered = computed(() => {
    if (import.meta.server) return false;
    const connection = (
      navigator as Navigator & { connection?: { saveData?: boolean; type?: string } }
    ).connection;
    return Boolean(connection?.saveData || connection?.type === "cellular");
  });

  /*
   * Why this device cannot take part, when it cannot: what the page can see
   * — no secure context, no WebRTC, no way to the microphone — and what only
   * the application around it knows, such as the desktop's missing GStreamer
   * plugins on Linux. Said to the person rather than hidden, so a call button
   * that is not there has a reason.
   */
  const platformMissing = ref("");
  const checkPlatform = inject(callSupportKey, null);
  if (checkPlatform && !import.meta.server) {
    void checkPlatform()
      .then((missing) => (platformMissing.value = missing))
      .catch(() => {});
  }
  const unsupported = computed<CallUnsupported | null>(() => {
    if (import.meta.server) return null;
    if (typeof RTCPeerConnection === "undefined")
      return checkPlatform ? "no-webrtc-desktop" : "no-webrtc";
    if (!navigator.mediaDevices?.getUserMedia)
      return window.isSecureContext ? "no-media" : "insecure";
    if (platformMissing.value === "gstreamer-webrtc") return "gstreamer-webrtc";
    return null;
  });
  /** Whether this device can take part at all. */
  const supported = computed(() => !import.meta.server && unsupported.value === null);

  function track(kind: "audio" | "video"): MediaStreamTrack | null {
    return local.value?.getTracks().find((each) => each.kind === kind) ?? null;
  }

  /** Whether a conversation's call is the one being talked in. */
  function live(room: string): boolean {
    return room === roomId.value && !holding.value.includes(room);
  }

  /** Where a conversation's call is reached: by the conversation, or as a guest by the call. */
  function callPath(room: string): string {
    const current = calls.value[room];
    return current && guestOf.value.includes(room)
      ? `/api/calls/${current.id}`
      : `/api/rooms/${room}/call`;
  }

  /** Whether we are in a conversation's call, talking or holding. */
  function isIn(room: string): boolean {
    return Boolean(calls.value[room]);
  }

  function roomOfCall(callId: string): string | null {
    return Object.values(calls.value).find((each) => each.id === callId)?.roomId ?? null;
  }

  function createPeer(room: string, other: string): Peer {
    const connection = new RTCPeerConnection({ iceServers });
    // Decided by something both sides know and agree on, so exactly one of
    // each pair yields when both offer at once.
    const polite = (user.value?.id ?? "") > other;
    /*
     * Only one side of each pair makes the transceivers; the other takes them
     * from its first offer. Transceivers both sides made for themselves would
     * never be matched up — each side would send on its own and receive on
     * the other's, and positions would stop meaning the same thing.
     */
    const senders = polite
      ? null
      : SLOTS.map((kind) => connection.addTransceiver(kind, { direction: "sendrecv" }).sender);
    const peer: Peer = {
      connection,
      senders,
      polite,
      makingOffer: false,
      ignoreOffer: false,
      control: null,
    };
    // The side that makes the slots makes the channel too, in the same offer.
    const attach = (channel: RTCDataChannel) => {
      peer.control = channel;
      channel.onmessage = ({ data }) => {
        try {
          const message = JSON.parse(String(data)) as ControlMessage;
          if (typeof message?.t === "string") controlListener(room, other, message);
        } catch {
          // Not something either side sends: ignored.
        }
      };
    };
    if (!polite) attach(connection.createDataChannel("control"));
    connection.ondatachannel = ({ channel }) => {
      if (channel.label === "control") attach(channel);
    };
    if (senders) void syncPeer(room, peer);

    const arriving = new MediaStream();
    const arrivingScreen = new MediaStream();
    connection.ontrack = ({ track: arrived, transceiver }) => {
      // One list of transceivers, the same on both sides, so the position
      // says which is the camera and which the screen.
      const isScreen = connection.getTransceivers().indexOf(transceiver) >= 2;
      const into = isScreen ? arrivingScreen : arriving;
      const store = isScreen ? remoteScreens : remotes;
      into.addTrack(arrived);
      const streams = new Map(store.get(room));
      streams.set(other, into);
      store.set(room, streams);
    };
    connection.onicecandidate = ({ candidate }) => {
      const current = calls.value[room];
      if (current) sendSignal(current.id, other, { candidate: candidate?.toJSON() ?? null });
    };
    connection.onnegotiationneeded = async () => {
      try {
        peer.makingOffer = true;
        await connection.setLocalDescription();
        const current = calls.value[room];
        if (current && connection.localDescription) {
          sendSignal(current.id, other, { description: connection.localDescription.toJSON() });
        }
      } finally {
        peer.makingOffer = false;
      }
    };
    connection.onconnectionstatechange = () => {
      if (connection.connectionState === "failed") {
        connection.restartIce();
      }
    };

    if (!peers.has(room)) peers.set(room, new Map());
    peers.get(room)!.set(other, peer);
    return peer;
  }

  function closePeer(room: string, other: string) {
    if (peers.get(room)?.has(other)) controlListener(room, other, { t: "gone" });
    peers.get(room)?.get(other)?.connection.close();
    peers.get(room)?.delete(other);
    for (const store of [remotes, remoteScreens]) {
      const streams = store.get(room);
      if (streams?.has(other)) {
        const rest = new Map(streams);
        rest.delete(other);
        store.set(room, rest);
      }
    }
  }

  /** Brings a call's connections in line with who is in it. */
  function reconcile(room: string) {
    const current = calls.value[room];
    if (!current) return;
    const me = user.value?.id;
    const present = new Set(current.participants.map((each) => each.userId));
    for (const other of present) {
      if (other !== me && !peers.get(room)?.has(other)) createPeer(room, other);
    }
    for (const other of [...(peers.get(room)?.keys() ?? [])]) {
      if (!present.has(other)) closePeer(room, other);
    }
  }

  /** Points a call's connections at our microphone and camera, or at nothing. */
  /** What a slot should be sending in a conversation's call now. */
  function wanted(room: string, slot: number): MediaStreamTrack | null {
    if (!live(room)) return null;
    // Too little bandwidth for the camera: sound and any shared screen go on.
    if (slot === 1 && quality.value.level >= 3) return null;
    return (
      [track("audio"), track("video"), screenTrack("video"), screenTrack("audio")][slot] ?? null
    );
  }

  async function syncPeer(room: string, peer: Peer) {
    if (!peer.senders) return;
    for (const [slot, sender] of peer.senders.entries()) {
      const next = wanted(room, slot);
      if (sender.track !== next) await sender.replaceTrack(next);
    }
    if (quality.value.level > 0) await tunePeer(peer);
  }

  /*
   * Stepping down on a poor connection. Every two seconds the call being
   * talked in is asked how much its connections can send; video gives way
   * first, then the camera, and only then the sound, and each comes back once
   * the connection has stayed comfortable (see lib/callQuality). What changed
   * is said in the call window.
   */
  const quality = ref<QualityState>(initialQuality);
  /** The last step taken, for the call window to say; `recovered` when back to full. */
  const qualityNotice = ref<QualityLevel | "recovered" | null>(null);
  let sampling: ReturnType<typeof setInterval> | null = null;

  function watchQuality() {
    if (sampling || import.meta.server) return;
    sampling = setInterval(() => void sampleQuality(), 2000);
  }

  async function sampleQuality() {
    const room = roomId.value;
    if (!Object.keys(calls.value).length) {
      if (sampling) clearInterval(sampling);
      sampling = null;
      quality.value = initialQuality;
      qualityNotice.value = null;
      return;
    }
    const connections = [...(peers.get(room ?? "")?.values() ?? [])];
    if (!room || !live(room) || !connections.length) return;
    let kbps: number | null = null;
    let limited = false;
    for (const { connection } of connections) {
      const reports = await connection.getStats().catch(() => null);
      reports?.forEach((report) => {
        if (
          report.type === "candidate-pair" &&
          report.nominated &&
          typeof report.availableOutgoingBitrate === "number"
        ) {
          kbps = Math.min(kbps ?? Infinity, report.availableOutgoingBitrate / 1000);
        }
        if (
          report.type === "outbound-rtp" &&
          report.kind === "video" &&
          report.qualityLimitationReason === "bandwidth"
        ) {
          limited = true;
        }
      });
    }
    const before = quality.value.level;
    quality.value = stepQuality(quality.value, { kbps, limited });
    if (quality.value.level === before) return;
    qualityNotice.value = quality.value.level === 0 ? "recovered" : quality.value.level;
    if (quality.value.level === 0) {
      setTimeout(() => {
        if (qualityNotice.value === "recovered") qualityNotice.value = null;
      }, 6000);
    }
    await sync(room);
    for (const peer of connections) await tunePeer(peer);
  }

  /** Sets what each of a connection's senders may use, for the current step. */
  async function tunePeer(peer: Peer) {
    if (!peer.senders) return;
    const slots = ["audio", "camera", "screen"] as const;
    for (const [index, slot] of slots.entries()) {
      const sender = peer.senders[index];
      if (!sender?.track) continue;
      const parameters = sender.getParameters();
      const encoding = parameters.encodings?.[0];
      if (!encoding) continue;
      for (const [key, value] of Object.entries(encodingFor(quality.value.level, slot))) {
        if (value === undefined) delete (encoding as Record<string, unknown>)[key];
        else (encoding as Record<string, unknown>)[key] = value;
      }
      await sender.setParameters(parameters).catch(() => {});
    }
  }

  /** Puts on every connection of a call exactly what it should be sending now. */
  async function sync(room: string) {
    for (const peer of peers.get(room)?.values() ?? []) await syncPeer(room, peer);
  }

  function screenTrack(kind: "audio" | "video"): MediaStreamTrack | null {
    return screen.value?.getTracks().find((each) => each.kind === kind) ?? null;
  }

  /**
   * Shares a screen, window or tab in the call shown — with its sound where
   * the browser offers it. The server holds one presenter at a time; if
   * somebody else is presenting, nothing is shared and the reason is given.
   */
  async function shareScreen() {
    const room = roomId.value;
    if (!room || !live(room) || screen.value) return;
    let captured: MediaStream;
    try {
      captured = await navigator.mediaDevices.getDisplayMedia({ video: true, audio: true });
    } catch {
      return; // Cancelled at the browser's picker: nothing to say.
    }
    try {
      calls.value = {
        ...calls.value,
        [room]: await api.put<Call>(`/api/rooms/${room}/call/stage`),
      };
    } catch (cause) {
      captured.getTracks().forEach((each) => each.stop());
      error.value = isApiError(cause) ? cause.message : "unreachable";
      return;
    }
    screen.value = captured;
    // The browser's own "Stop sharing" ends it too.
    captured.getVideoTracks()[0]?.addEventListener("ended", () => void stopSharing());
    await sync(room);
  }

  /** Stops sharing, here and on the stage. */
  async function stopSharing() {
    const shared = screen.value;
    if (!shared) return;
    screen.value = null;
    for (const room of peers.keys()) await sync(room);
    shared.getTracks().forEach((each) => each.stop());
    const room = Object.values(calls.value).find((each) =>
      each.participants.some((p) => p.userId === user.value?.id && p.screen),
    )?.roomId;
    if (!room) return;
    try {
      calls.value = {
        ...calls.value,
        [room]: await api.delete<Call>(`/api/rooms/${room}/call/stage`),
      };
    } catch {
      // The stage clears when we leave the call, or by a moderator.
    }
  }

  /** Takes whoever is presenting off the stage, for a moderator. */
  async function clearStage() {
    const room = roomId.value;
    if (!room) return;
    try {
      calls.value = {
        ...calls.value,
        [room]: await api.delete<Call>(`/api/rooms/${room}/call/stage/presenter`),
      };
    } catch (cause) {
      error.value = isApiError(cause) ? cause.message : "unreachable";
    }
  }

  async function report(room: string | null = roomId.value) {
    if (!room || !calls.value[room]) return;
    try {
      const state = await api.patch<Call>(callPath(room), {
        audio: microphone.value,
        video: camera.value,
        held: holding.value.includes(room),
      });
      if (calls.value[room]) calls.value = { ...calls.value, [room]: state };
    } catch {
      // The others see the old state until the next change; the media itself
      // has already changed.
    }
  }

  /** Holds the call being talked in, if there is one. */
  async function holdCurrent() {
    const room = roomId.value;
    if (!room || !live(room)) return;
    holding.value = [...holding.value, room];
    await sync(room);
    await report(room);
  }

  /**
   * Talks in a call we are in: the one being talked in is held, and this one
   * gets the microphone and camera.
   */
  async function switchTo(room: string) {
    if (!calls.value[room] || live(room)) return;
    if (room !== roomId.value) await holdCurrent();
    roomId.value = room;
    holding.value = holding.value.filter((each) => each !== room);
    await sync(room);
    await report(room);
  }

  /**
   * Joins a conversation's call, or starts one.
   *
   * The microphone (and camera, if asked) first: nothing is sent, and nobody is
   * told we are in the call, until the person has granted it. A call we are
   * already talking in is held, unless asked to end it.
   */
  async function join(
    room: string,
    withVideo: boolean,
    options: { ring?: boolean; endCurrent?: boolean; asGuestOf?: string } = {},
  ) {
    if (joining.value) return;
    if (calls.value[room]) {
      await switchTo(room);
      return;
    }
    joining.value = true;
    error.value = null;
    try {
      if (!local.value) {
        local.value = await navigator.mediaDevices.getUserMedia({
          audio: true,
          video: withVideo,
        });
        microphone.value = true;
        camera.value = withVideo;
      }
    } catch {
      error.value = "permission";
      joining.value = false;
      return;
    }
    try {
      iceServers = (await api.get<IceConfiguration>("/api/calls/ice")).iceServers;
      if (options.endCurrent && roomId.value) await leave(roomId.value, { keepMedia: true });
      else await holdCurrent();
      let started = await api.post<Call>(
        options.asGuestOf ? `/api/calls/${options.asGuestOf}` : `/api/rooms/${room}/call`,
        { audio: microphone.value, video: camera.value },
      );
      /*
       * Being recorded: the person is told who is recording before any of
       * their sound or picture goes out, and may leave instead. Until they
       * answer the call is not ours here, so nothing is sent to it, and
       * whatever negotiation arrives waits with the rest of the early signals.
       */
      const me = user.value?.id;
      const recorders = (started.recording ?? []).filter((id) => id !== me);
      if (recorders.length) {
        const stay = await new Promise<boolean>((resolve) => {
          recordingNotice.value = {
            room,
            names: started.participants
              .filter((each) => recorders.includes(each.userId))
              .map((each) => nameOf(each)),
          };
          answerRecordingNotice = resolve;
        });
        recordingNotice.value = null;
        answerRecordingNotice = null;
        if (!stay) {
          early = [];
          await api.delete(`/api/calls/${started.id}`).catch(() => {});
          if (!Object.keys(calls.value).length) stopLocal();
          return;
        }
        started = await api.get<Call>(`/api/calls/${started.id}`).catch(() => started);
      }
      if (options.asGuestOf) guestOf.value = [...guestOf.value, room];
      calls.value = { ...calls.value, [room]: started };
      roomId.value = room;
      holding.value = holding.value.filter((each) => each !== room);
      view.value = "docked";
      reconcile(room);
      const waiting = early;
      early = [];
      for (const signal of waiting) await onSignal(signal);
      incoming.value = incoming.value.filter((ring) => ring.roomId !== room);
      if (withVideo && !camera.value) await toggleCamera();
      if (options.ring) await ring();
      watchQuality();
    } catch (cause) {
      if (!Object.keys(calls.value).length) stopLocal();
      early = [];
      error.value = isApiError(cause) ? cause.message : "unreachable";
    } finally {
      joining.value = false;
    }
  }

  function stopLocal() {
    local.value?.getTracks().forEach((each) => each.stop());
    local.value = null;
  }

  async function toggleMicrophone() {
    microphone.value = !microphone.value;
    const audio = track("audio");
    if (audio) audio.enabled = microphone.value;
    await report();
  }

  async function toggleCamera() {
    const room = roomId.value;
    const sending = Boolean(room && live(room));
    if (!camera.value) {
      try {
        const [video] = (
          await navigator.mediaDevices.getUserMedia({ video: true })
        ).getVideoTracks();
        if (!video) return;
        local.value?.addTrack(video);
        local.value = new MediaStream(local.value?.getTracks() ?? [video]);
        if (sending && room) {
          await sync(room);
        }
        camera.value = true;
      } catch {
        error.value = "permission";
        return;
      }
    } else {
      const video = track("video");
      if (room) {
        // Taken off the wire before the camera stops, so nobody sees it freeze.
        if (video) local.value?.removeTrack(video);
        await sync(room);
      }
      video?.stop();
      if (video) local.value?.removeTrack(video);
      local.value = new MediaStream(local.value?.getTracks() ?? []);
      camera.value = false;
    }
    await report();
  }

  /**
   * Leaves a call — the one shown unless another is named. Leaving the one
   * shown shows another we are holding, still held: nobody is put back on
   * the air without choosing to be.
   */
  async function leave(
    room: string | null = roomId.value,
    options: { keepMedia?: boolean; local?: boolean } = {},
  ) {
    if (!room || !calls.value[room]) return;
    if (screen.value && room === roomId.value) await stopSharing();
    const path = callPath(room);
    for (const other of [...(peers.get(room)?.keys() ?? [])]) closePeer(room, other);
    peers.delete(room);
    remotes.delete(room);
    remoteScreens.delete(room);
    const rest = { ...calls.value };
    delete rest[room];
    calls.value = rest;
    holding.value = holding.value.filter((each) => each !== room);
    guestOf.value = guestOf.value.filter((each) => each !== room);
    if (roomId.value === room) {
      const next = Object.keys(rest).at(-1) ?? null;
      roomId.value = next;
      if (next && !holding.value.includes(next)) holding.value = [...holding.value, next];
    }
    if (!Object.keys(rest).length && !options.keepMedia) stopLocal();
    if (options.local) return;
    try {
      await api.delete(path);
    } catch {
      // The server takes us out by itself once the connection is gone.
    }
  }

  /** Leaves every call we are in. */
  async function leaveAll() {
    for (const room of Object.keys(calls.value)) await leave(room);
  }

  /**
   * Rings people into the call shown: the given people, or everybody else in
   * a direct conversation. Who could not be rung is not said, by design.
   */
  async function ring(users: string[] = []) {
    if (!joined.value || !roomId.value) return;
    try {
      await api.post(`/api/rooms/${roomId.value}/call/ring`, { users });
    } catch {
      // Ringing is a courtesy; the call goes on without it.
    }
  }

  function onRing(incomingRing: CallRing) {
    if (roomOfCall(incomingRing.call)) return;
    if (incoming.value.some((each) => each.call === incomingRing.call)) return;
    incoming.value = [...incoming.value, incomingRing];
  }

  function onRingEnded(callId: string) {
    incoming.value = incoming.value.filter((each) => each.call !== callId);
  }

  function onDeclined(notice: CallDeclined) {
    if (!roomOfCall(notice.call)) return;
    declined.value = notice;
    setTimeout(
      () => {
        if (declined.value === notice) declined.value = null;
      },
      notice.message ? 12000 : 5000,
    );
  }

  /** Posts a few words in a conversation, as the person. */
  async function say(room: string, words: string) {
    if (!words.trim()) return;
    try {
      await rooms.send(room, words.trim(), crypto.randomUUID());
    } catch {
      // The call goes on; the words are the only thing lost.
    }
  }

  /**
   * Answers somebody calling. A call being talked in is held, or ended when
   * asked — with a few words to the people in it, if given.
   */
  async function answer(
    incomingRing: CallRing,
    withVideo: boolean,
    options: { endCurrent?: boolean; farewell?: string } = {},
  ) {
    onRingEnded(incomingRing.call);
    if (options.farewell && roomId.value && !guestOf.value.includes(roomId.value)) {
      await say(roomId.value, options.farewell);
    }
    // Being merged: the calls shared with whoever merges are the ones being
    // merged, and are left once in the conference rather than held.
    const merged = incomingRing.conference
      ? Object.values(calls.value)
          .filter((each) => each.participants.some((p) => p.userId === incomingRing.from.userId))
          .map((each) => each.roomId)
      : [];
    await join(incomingRing.roomId, withVideo, {
      endCurrent: options.endCurrent,
      asGuestOf: incomingRing.conference ? incomingRing.call : undefined,
    });
    if (calls.value[incomingRing.roomId]) {
      for (const room of merged) await leave(room, { keepMedia: true });
    }
  }

  /** Declines somebody calling, with a few words for them if given. */
  async function decline(incomingRing: CallRing, message = "") {
    onRingEnded(incomingRing.call);
    // An invitation into somebody else's conversation's call has nowhere to
    // post words and nobody to tell; it is simply not taken.
    if (incomingRing.conference) return;
    try {
      await api.post(`/api/rooms/${incomingRing.roomId}/call/decline`, {
        message: message.trim(),
      });
    } catch {
      // The ring ends by itself; declining only says so sooner.
    }
    await say(incomingRing.roomId, message);
  }

  /** Whether a held call can be merged into the one being talked in. */
  function canMerge(held: string): boolean {
    const active = roomId.value;
    return Boolean(
      active &&
      held !== active &&
      live(active) &&
      calls.value[held] &&
      !guestOf.value.includes(active),
    );
  }

  /**
   * Merges a held call into the one being talked in: its people are invited,
   * and the held call is left once they have all arrived — or after a minute,
   * for whoever has not.
   */
  async function merge(held: string) {
    const active = roomId.value;
    if (!active || !canMerge(held)) return;
    const me = user.value?.id;
    const people = (calls.value[held]?.participants ?? [])
      .map((each) => each.userId)
      .filter((id) => id !== me);
    try {
      await api.post(`/api/rooms/${active}/call/invite`, { users: people });
    } catch {
      error.value = "unreachable";
      return;
    }
    const arrived = () =>
      people.every((id) => calls.value[active]?.participants.some((each) => each.userId === id));
    const stop = watch(
      () => calls.value[active]?.participants,
      () => {
        if (arrived()) finish();
      },
    );
    const timer = setTimeout(() => finish(), 60_000);
    function finish() {
      stop();
      clearTimeout(timer);
      if (calls.value[held]) void leave(held, { keepMedia: true });
    }
  }

  /**
   * Uses a call link: straight into the call, or into its lobby to wait.
   * Throws the server's refusal — a wrong passcode, a link that is used up,
   * a call not started — for the link page to say.
   */
  async function joinByLink(secret: string, passcode: string, withVideo: boolean) {
    const use = await api.post<CallLinkUse>(`/api/call-links/${encodeURIComponent(secret)}/join`, {
      passcode,
    });
    if (use.waiting) {
      waitingIn.value = {
        callId: use.callId,
        roomId: use.roomId,
        video: withVideo,
        outcome: "waiting",
      };
      return;
    }
    await join(use.roomId, withVideo, { asGuestOf: use.callId });
  }

  /** Gives up waiting in a lobby. */
  async function stopWaiting() {
    const waiting = waitingIn.value;
    waitingIn.value = null;
    if (!waiting) return;
    try {
      await api.delete(`/api/calls/${waiting.callId}/lobby`);
    } catch {
      // The lobby forgets us when the call ends anyway.
    }
  }

  function onLobby(lobby: CallLobby) {
    lobbies.value = { ...lobbies.value, [lobby.call]: lobby.waiting };
  }

  async function onAdmitted(use: CallLinkUse) {
    const waiting = waitingIn.value;
    if (!waiting || waiting.callId !== use.callId) return;
    waitingIn.value = null;
    await join(use.roomId, waiting.video, { asGuestOf: use.callId });
  }

  function onTurnedAway(use: CallLinkUse) {
    if (waitingIn.value?.callId === use.callId) {
      waitingIn.value = { ...waitingIn.value, outcome: "turned-away" };
    }
  }

  /** Lets somebody waiting into the call shown, or turns them away. */
  async function admit(userId: string, letIn: boolean) {
    const current = call.value;
    if (!current) return;
    const before = lobbies.value[current.id] ?? [];
    lobbies.value = {
      ...lobbies.value,
      [current.id]: before.filter((each) => each.userId !== userId),
    };
    try {
      await api.post(`/api/calls/${current.id}/lobby/${userId}/${letIn ? "admit" : "turn-away"}`);
    } catch {
      lobbies.value = { ...lobbies.value, [current.id]: before };
    }
  }

  /**
   * Reactions passing over the call shown, each kept a few seconds to float
   * and fade; and how many of each kind the call has sent while somebody
   * presented, for the presenter.
   */
  const reactions = ref<(CallReaction & { id: number })[]>([]);
  const reactionCounts = ref<Partial<Record<CallReactionKind, number>>>({});
  let reactionId = 0;

  function onReaction(reaction: CallReaction) {
    if (reaction.roomId !== roomId.value) return;
    const id = ++reactionId;
    reactions.value = [...reactions.value.slice(-19), { ...reaction, id }];
    setTimeout(() => {
      reactions.value = reactions.value.filter((each) => each.id !== id);
    }, 4000);
    if (call.value?.presenter) {
      reactionCounts.value = {
        ...reactionCounts.value,
        [reaction.kind]: (reactionCounts.value[reaction.kind] ?? 0) + 1,
      };
    }
  }
  // Counts belong to one presentation.
  watch(
    () => call.value?.presenter,
    () => (reactionCounts.value = {}),
  );

  /** Sends a passing reaction to everybody in the call shown. */
  async function react(kind: CallReactionKind) {
    const room = roomId.value;
    if (!room || !live(room)) return;
    try {
      await api.post(`/api/rooms/${room}/call/reactions`, { kind });
    } catch {
      // A reaction that did not arrive is not worth an error.
    }
  }

  /** Raises or lowers our hand, or lowers somebody else's as presenter or moderator. */
  async function setHand(raised: boolean, userId?: string) {
    const room = roomId.value;
    if (!room) return;
    const path = userId ? `/api/rooms/${room}/call/hand/${userId}` : `/api/rooms/${room}/call/hand`;
    try {
      const state = raised ? await api.put<Call>(path) : await api.delete<Call>(path);
      calls.value = { ...calls.value, [room]: state };
    } catch (cause) {
      error.value = isApiError(cause) ? cause.message : "unreachable";
    }
  }

  /** Who has a hand up in the call shown, in the order they raised it. */
  const hands = computed(() =>
    (call.value?.participants ?? [])
      .filter((each) => each.handRaisedAt)
      .sort((a, b) => (a.handRaisedAt! < b.handRaisedAt! ? -1 : 1)),
  );
  const handUp = computed(() => hands.value.some((each) => each.userId === user.value?.id));

  /** Who is in a call changed. */
  function onState(state: Call) {
    const mine = calls.value[state.roomId];
    if (!mine || mine.id !== state.id) return;
    // A call that no longer lists us has let us go — it ended, or the server
    // took us out — so it is put down here too, without asking the server.
    if (!state.participants.some((each) => each.userId === user.value?.id)) {
      void leave(state.roomId, { local: true });
      return;
    }
    calls.value = { ...calls.value, [state.roomId]: state };
    reconcile(state.roomId);
  }

  /** Another participant's negotiation, handled by the perfect-negotiation rules. */
  async function onSignal(message: CallSignal) {
    const { call: callId, from, signal } = message;
    const room = roomOfCall(callId);
    if (!room) {
      if (joining.value) early.push(message);
      return;
    }
    const peer = peers.get(room)?.get(from) ?? createPeer(room, from);
    const { connection } = peer;

    if (signal.description) {
      const collision =
        signal.description.type === "offer" &&
        (peer.makingOffer || connection.signalingState !== "stable");
      peer.ignoreOffer = !peer.polite && collision;
      if (peer.ignoreOffer) return;

      await connection.setRemoteDescription(signal.description);
      if (!peer.senders) {
        // The polite side takes the transceivers the other side's offer made,
        // in their order, and starts sending on them.
        const transceivers = connection.getTransceivers().slice(0, SLOTS.length);
        if (transceivers.length === SLOTS.length) {
          for (const transceiver of transceivers) transceiver.direction = "sendrecv";
          peer.senders = transceivers.map((transceiver) => transceiver.sender);
          await syncPeer(room, peer);
        }
      }
      if (signal.description.type === "offer") {
        await connection.setLocalDescription();
        if (connection.localDescription) {
          sendSignal(callId, from, { description: connection.localDescription.toJSON() });
        }
      }
    } else if (signal.candidate !== undefined) {
      try {
        await connection.addIceCandidate(signal.candidate ?? undefined);
      } catch (cause) {
        if (!peer.ignoreOffer) throw cause;
      }
    }
  }

  return {
    roomId,
    view,
    setSignaller,
    call,
    calls,
    others,
    holding,
    onHold,
    joined,
    joining,
    microphone,
    camera,
    error,
    local,
    remote,
    metered,
    supported,
    unsupported,
    isIn,
    recordingNotice,
    answerRecording,
    onControl,
    sendControl,
    quality,
    qualityNotice,
    reactions,
    reactionCounts,
    onReaction,
    react,
    setHand,
    hands,
    handUp,
    screen,
    remoteScreen,
    canShareScreen,
    shareScreen,
    stopSharing,
    clearStage,
    lobbies,
    waitingIn,
    joinByLink,
    stopWaiting,
    onLobby,
    onAdmitted,
    onTurnedAway,
    admit,
    guestOf,
    canMerge,
    merge,
    join,
    switchTo,
    hold: holdCurrent,
    leave,
    leaveAll,
    toggleMicrophone,
    toggleCamera,
    onState,
    onSignal,
    incoming,
    declined,
    ring,
    answer,
    decline,
    onRing,
    onRingEnded,
    onDeclined,
  };
});
