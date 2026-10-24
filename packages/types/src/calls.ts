/** Somebody in a call, and what they are sending. */
export interface CallParticipant {
  userId: string;
  username: string;
  address: string;
  audio: boolean;
  video: boolean;
  /** In another call for now, and sending nothing to this one. */
  held: boolean;
  /** Presenting their screen. */
  screen: boolean;
  /** The name to show them by. */
  displayName?: string;
  /** In the call without an account here. */
  guest?: boolean;
  /** When they raised their hand, while it is up. */
  handRaisedAt?: string;
  joinedAt: string;
}

/** A call in progress in a conversation. */
export interface Call {
  id: string;
  roomId: string;
  startedAt: string;
  participants: CallParticipant[];
  /** Who is presenting, if anybody: one person at a time. */
  presenter?: string;
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

/** Somebody ringing you into a call. */
export interface CallRing {
  call: string;
  roomId: string;
  roomName: string;
  kind: "channel" | "direct" | "disposable" | "conference";
  /**
   * An invitation into a call merged with another: the person invited is not
   * in its conversation, and `roomName` is empty.
   */
  conference?: boolean;
  /** Who is ringing; `displayName` is what to call them, never empty. */
  from: { userId: string; username: string; displayName: string; address: string };
}

/** Somebody you rang declined. */
export interface CallDeclined {
  call: string;
  userId: string;
  username: string;
  /** What to call them: their display name, or their username without one. */
  displayName: string;
  /** A few words sent with the decline, if any. */
  message?: string;
}

/** A link to a conversation's call, as the people who manage it see it. */
export interface CallLink {
  id: string;
  label: string;
  protected: boolean;
  lobby: boolean;
  guests: boolean;
  expiresAt?: string;
  maxUses?: number;
  uses: number;
  createdAt: string;
  /** Present only in the answer to making the link; it is not kept. */
  secret?: string;
}

/** What a call link leads to, as anybody holding it is told. */
export interface CallLinkTarget {
  roomName: string;
  protected: boolean;
  lobby: boolean;
  guests: boolean;
  started: boolean;
  people: string[];
}

/** What using a link answers: the call, or that the person is waiting. */
export interface CallLinkUse {
  callId: string;
  roomId: string;
  waiting: boolean;
}

/** Who is waiting to be let into a call. */
export interface CallLobby {
  call: string;
  roomId: string;
  waiting: { userId: string; username: string; displayName: string; since: string }[];
}

/** The passing reactions somebody can send in a call. */
export type CallReactionKind = "clap" | "laugh" | "heart" | "thumbs_up" | "surprise" | "celebrate";

/** A passing reaction somebody in a call sent. Not kept anywhere. */
export interface CallReaction {
  call: string;
  roomId: string;
  from: string;
  username: string;
  /** What to call them: their display name, or their username without one. */
  displayName: string;
  kind: CallReactionKind;
}
