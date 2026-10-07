import type { Call, CallDeclined, CallRing, CallSignal, IceConfiguration } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { createGlobalState } from "@vueuse/core";

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

interface Peer {
  connection: RTCPeerConnection;
  audio: RTCRtpSender;
  video: RTCRtpSender;
  polite: boolean;
  makingOffer: boolean;
  ignoreOffer: boolean;
}

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

  /** People ringing us now, newest last. */
  const incoming = ref<CallRing[]>([]);
  /** The last person who declined our ring, for a moment's notice. */
  const declined = ref<CallDeclined | null>(null);

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

  /** Whether this device can take part at all. */
  const supported = computed(
    () =>
      !import.meta.server &&
      typeof RTCPeerConnection !== "undefined" &&
      Boolean(navigator.mediaDevices?.getUserMedia),
  );

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
    const sending = live(room);
    const peer: Peer = {
      connection,
      audio: connection.addTransceiver((sending && track("audio")) || "audio", {
        direction: "sendrecv",
      }).sender,
      video: connection.addTransceiver((sending && track("video")) || "video", {
        direction: "sendrecv",
      }).sender,
      // Decided by something both sides know and agree on, so exactly one of
      // each pair yields when both offer at once.
      polite: (user.value?.id ?? "") > other,
      makingOffer: false,
      ignoreOffer: false,
    };

    const arriving = new MediaStream();
    connection.ontrack = ({ track: arrived }) => {
      arriving.addTrack(arrived);
      const streams = new Map(remotes.get(room));
      streams.set(other, arriving);
      remotes.set(room, streams);
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
    peers.get(room)?.get(other)?.connection.close();
    peers.get(room)?.delete(other);
    const streams = remotes.get(room);
    if (streams?.has(other)) {
      const rest = new Map(streams);
      rest.delete(other);
      remotes.set(room, rest);
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
  async function feed(room: string, sending: boolean) {
    for (const peer of peers.get(room)?.values() ?? []) {
      await peer.audio.replaceTrack(sending ? track("audio") : null);
      await peer.video.replaceTrack(sending ? track("video") : null);
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
    await feed(room, false);
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
    await feed(room, true);
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
      const started = await api.post<Call>(
        options.asGuestOf ? `/api/calls/${options.asGuestOf}` : `/api/rooms/${room}/call`,
        { audio: microphone.value, video: camera.value },
      );
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
          for (const peer of peers.get(room)?.values() ?? []) await peer.video.replaceTrack(video);
        }
        camera.value = true;
      } catch {
        error.value = "permission";
        return;
      }
    } else {
      const video = track("video");
      if (room) {
        for (const peer of peers.get(room)?.values() ?? []) await peer.video.replaceTrack(null);
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
  async function leave(room: string | null = roomId.value, options: { keepMedia?: boolean } = {}) {
    if (!room || !calls.value[room]) return;
    const path = callPath(room);
    for (const other of [...(peers.get(room)?.keys() ?? [])]) closePeer(room, other);
    peers.delete(room);
    remotes.delete(room);
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

  /** Who is in a call changed. */
  function onState(state: Call) {
    const mine = calls.value[state.roomId];
    if (!mine || mine.id !== state.id) return;
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
    isIn,
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
