import type { Call, CallSignal, IceConfiguration } from "@tdc/types";
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
 * One call for the whole application, not one per page: a call goes on while
 * somebody reads another conversation, opens a project or changes a setting.
 * The call window in the layout shows it and holds the connection its
 * negotiation travels on; a conversation only offers to start or join.
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
  const { user } = useAuth();

  /**
   * The conversation whose call we are in, or are joining. Shared state by
   * name, so the layout can mount the call window when a call begins without
   * loading any of the call's code before then.
   */
  const roomId = useState<string | null>("call.room", () => null);
  const view = ref<CallView>("docked");
  let sendSignal: SignalSender = () => {};
  /** Set by the call window, whose connection carries the negotiation. */
  function setSignaller(sender: SignalSender) {
    sendSignal = sender;
  }

  /** The call we are in, as the server last described it. */
  const call = ref<Call | null>(null);
  const joined = ref(false);
  const joining = ref(false);
  const microphone = ref(true);
  const camera = ref(false);
  const error = ref<string | null>(null);
  /** Our own camera, to show ourselves. */
  const local = shallowRef<MediaStream | null>(null);
  /** What each other participant is sending, by their user id. */
  const remote = shallowReactive(new Map<string, MediaStream>());

  const peers = new Map<string, Peer>();
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

  function createPeer(other: string): Peer {
    const connection = new RTCPeerConnection({ iceServers });
    const peer: Peer = {
      connection,
      audio: connection.addTransceiver(track("audio") ?? "audio", { direction: "sendrecv" }).sender,
      video: connection.addTransceiver(track("video") ?? "video", { direction: "sendrecv" }).sender,
      // Decided by something both sides know and agree on, so exactly one of
      // each pair yields when both offer at once.
      polite: (user.value?.id ?? "") > other,
      makingOffer: false,
      ignoreOffer: false,
    };

    const incoming = new MediaStream();
    connection.ontrack = ({ track: arrived }) => {
      incoming.addTrack(arrived);
      remote.set(other, incoming);
    };
    connection.onicecandidate = ({ candidate }) => {
      if (call.value) sendSignal(call.value.id, other, { candidate: candidate?.toJSON() ?? null });
    };
    connection.onnegotiationneeded = async () => {
      try {
        peer.makingOffer = true;
        await connection.setLocalDescription();
        if (call.value && connection.localDescription) {
          sendSignal(call.value.id, other, { description: connection.localDescription.toJSON() });
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

    peers.set(other, peer);
    return peer;
  }

  function closePeer(other: string) {
    peers.get(other)?.connection.close();
    peers.delete(other);
    remote.delete(other);
  }

  /** Brings the peer connections in line with who is in the call. */
  function reconcile() {
    if (!joined.value || !call.value) return;
    const me = user.value?.id;
    const present = new Set(call.value.participants.map((each) => each.userId));
    for (const other of present) {
      if (other !== me && !peers.has(other)) createPeer(other);
    }
    for (const other of [...peers.keys()]) {
      if (!present.has(other)) closePeer(other);
    }
  }

  /**
   * Joins the call, or starts one.
   *
   * The microphone (and camera, if asked) first: nothing is sent, and nobody is
   * told we are in the call, until the person has granted it.
   */
  async function join(room: string, withVideo: boolean) {
    if (joining.value) return;
    if (joined.value) {
      if (roomId.value === room) return;
      // One call at a time: joining another leaves this one.
      await leave();
    }
    roomId.value = room;
    joining.value = true;
    error.value = null;
    try {
      local.value = await navigator.mediaDevices.getUserMedia({
        audio: true,
        video: withVideo,
      });
    } catch {
      error.value = "permission";
      joining.value = false;
      roomId.value = null;
      return;
    }
    try {
      iceServers = (await api.get<IceConfiguration>("/api/calls/ice")).iceServers;
      call.value = await api.post<Call>(`/api/rooms/${roomId.value}/call`, {
        audio: true,
        video: withVideo,
      });
      joined.value = true;
      microphone.value = true;
      camera.value = withVideo;
      view.value = "docked";
      reconcile();
      const waiting = early;
      early = [];
      for (const signal of waiting) await onSignal(signal);
    } catch (cause) {
      stopLocal();
      early = [];
      roomId.value = null;
      error.value = isApiError(cause) ? cause.message : "unreachable";
    } finally {
      joining.value = false;
    }
  }

  function stopLocal() {
    local.value?.getTracks().forEach((each) => each.stop());
    local.value = null;
  }

  async function report() {
    if (!joined.value) return;
    try {
      call.value = await api.patch<Call>(`/api/rooms/${roomId.value}/call`, {
        audio: microphone.value,
        video: camera.value,
      });
    } catch {
      // The others see the old state until the next change; the media itself
      // has already changed.
    }
  }

  async function toggleMicrophone() {
    microphone.value = !microphone.value;
    const audio = track("audio");
    if (audio) audio.enabled = microphone.value;
    await report();
  }

  async function toggleCamera() {
    if (!camera.value) {
      try {
        const [video] = (
          await navigator.mediaDevices.getUserMedia({ video: true })
        ).getVideoTracks();
        if (!video) return;
        local.value?.addTrack(video);
        local.value = new MediaStream(local.value?.getTracks() ?? [video]);
        for (const peer of peers.values()) await peer.video.replaceTrack(video);
        camera.value = true;
      } catch {
        error.value = "permission";
        return;
      }
    } else {
      const video = track("video");
      for (const peer of peers.values()) await peer.video.replaceTrack(null);
      video?.stop();
      if (video) local.value?.removeTrack(video);
      local.value = new MediaStream(local.value?.getTracks() ?? []);
      camera.value = false;
    }
    await report();
  }

  async function leave() {
    if (!joined.value) return;
    for (const other of [...peers.keys()]) closePeer(other);
    stopLocal();
    joined.value = false;
    const room = roomId.value;
    call.value = null;
    roomId.value = null;
    try {
      await api.delete(`/api/rooms/${room}/call`);
    } catch {
      // The server takes us out by itself once the connection is gone.
    }
  }

  /** Who is in the call changed. */
  function onState(state: Call) {
    if (!joined.value || state.roomId !== roomId.value) return;
    call.value = state;
    reconcile();
  }

  /** Another participant's negotiation, handled by the perfect-negotiation rules. */
  async function onSignal(message: CallSignal) {
    const { call: callId, from, signal } = message;
    if (!joined.value) {
      if (joining.value) early.push(message);
      return;
    }
    if (callId !== call.value?.id) return;
    const peer = peers.get(from) ?? createPeer(from);
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
    joined,
    joining,
    microphone,
    camera,
    error,
    local,
    remote,
    metered,
    supported,
    join,
    leave,
    toggleMicrophone,
    toggleCamera,
    onState,
    onSignal,
  };
});
