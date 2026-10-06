/** Somebody in a call, and what they are sending. */
export interface CallParticipant {
  userId: string;
  username: string;
  address: string;
  audio: boolean;
  video: boolean;
  joinedAt: string;
}

/** A call in progress in a conversation. */
export interface Call {
  id: string;
  roomId: string;
  startedAt: string;
  participants: CallParticipant[];
}

/** The servers a client connects a call through. */
export interface IceConfiguration {
  iceServers: RTCIceServer[];
  /** Whether a relay is offered, for calls that cannot connect directly. */
  relay: boolean;
}

/** One piece of a call's negotiation from another participant. */
export interface CallSignal {
  call: string;
  from: string;
  signal: { description?: RTCSessionDescriptionInit; candidate?: RTCIceCandidateInit | null };
}
