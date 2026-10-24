import type { DirectTransferSignal } from "@tdc/types";

/**
 * Sending a file straight between two devices, over a WebRTC data channel.
 *
 * Loaded only when somebody sends or accepts a direct transfer. Negotiation
 * travels over the server's live connection (the signal function handed in);
 * the file goes over the data channel and never touches the server, unless the
 * operator's relay carries the connection.
 *
 * The file is sent in 64 KiB messages, the largest every browser accepts. Two
 * things keep a fast sender from queueing the whole file in memory: it pauses
 * whenever the channel's own buffer passes 4 MiB, until it drains to 1 MiB; and
 * the recipient acknowledges every 4 MiB it has written, and the sender stops
 * when it is 16 MiB ahead — so a slow disk on the far side slows the sender
 * rather than filling either device's memory.
 *
 * There is no resuming. A transfer that fails part way fails, and the sender is
 * offered an ordinary upload instead, which does resume.
 */

type Signal = DirectTransferSignal["signal"];

const pieceBytes = 64 * 1024;
const bufferHigh = 4 * 1024 * 1024;
const bufferLow = 1024 * 1024;
const ackEvery = 4 * 1024 * 1024;
const window = 16 * 1024 * 1024;

/** Where a received file is written. */
export interface Sink {
  write(data: ArrayBuffer): Promise<void>;
  /** Finishes the file; for a sink held in memory, offers it as a download. */
  close(): Promise<void>;
  abort(): void;
  /** Whether it is written straight to disk as it arrives. */
  readonly toDisk: boolean;
}

interface WritableFile {
  write(data: ArrayBuffer): Promise<void>;
  close(): Promise<void>;
  abort(): Promise<void>;
}
type SavePicker = (options: { suggestedName: string }) => Promise<{
  createWritable(): Promise<WritableFile>;
}>;

/**
 * Asks where to save a file, where the browser can: written straight to disk
 * as it arrives, any size. Elsewhere the pieces are kept as Blobs — which a
 * browser may hold on disk rather than in memory — and offered as a download at
 * the end. Must be called from the click that accepted, as the picker needs it.
 * Resolves null when somebody closes the picker without choosing.
 */
export async function openSink(filename: string, contentType?: string): Promise<Sink | null> {
  const picker = (globalThis as { showSaveFilePicker?: SavePicker }).showSaveFilePicker;
  if (typeof picker === "function") {
    try {
      const writable = await (await picker({ suggestedName: filename })).createWritable();
      return {
        toDisk: true,
        write: (data) => writable.write(data),
        close: () => writable.close(),
        abort: () => void writable.abort().catch(() => {}),
      };
    } catch (cause) {
      if (cause instanceof DOMException && cause.name === "AbortError") return null;
      // A picker that failed for any other reason: fall back to memory.
    }
  }

  const parts: Blob[] = [];
  return {
    toDisk: false,
    write: async (data) => {
      parts.push(new Blob([data]));
    },
    close: async () => {
      const blob = new Blob(parts, { type: contentType || "application/octet-stream" });
      const link = document.createElement("a");
      link.href = URL.createObjectURL(blob);
      link.download = filename;
      link.click();
      // Long enough for the download to have begun.
      setTimeout(() => URL.revokeObjectURL(link.href), 60_000);
    },
    abort: () => {
      parts.length = 0;
    },
  };
}

export interface SessionHandlers {
  signal(body: Signal): void;
  onProgress(transferred: number): void;
  onDone(): void;
  onFail(reason: "connection" | "incomplete"): void;
}

export interface Session {
  onSignal(body: Signal): void;
  close(): void;
}

/** Negotiation messages, applied one after another, as they arrived. */
function negotiator(connection: RTCPeerConnection, handlers: SessionHandlers, answering: boolean) {
  let queue = Promise.resolve();
  const pending: RTCIceCandidateInit[] = [];

  connection.onicecandidate = (event) =>
    handlers.signal({ candidate: event.candidate?.toJSON() ?? null });
  connection.onconnectionstatechange = () => {
    if (connection.connectionState === "failed") handlers.onFail("connection");
  };

  return (body: Signal) => {
    queue = queue
      .then(async () => {
        if (body.description) {
          await connection.setRemoteDescription(body.description);
          // Candidates that arrived before the description can be used now.
          for (const candidate of pending.splice(0)) await connection.addIceCandidate(candidate);
          if (answering && body.description.type === "offer") {
            await connection.setLocalDescription(await connection.createAnswer());
            if (connection.localDescription) {
              handlers.signal({ description: connection.localDescription.toJSON() });
            }
          }
        } else if (body.candidate) {
          if (connection.remoteDescription) await connection.addIceCandidate(body.candidate);
          else pending.push(body.candidate);
        }
      })
      .catch(() => handlers.onFail("connection"));
  };
}

/** Sends a file to the other device, once its recipient has accepted. */
export function sendFile(
  file: File,
  iceServers: RTCIceServer[],
  handlers: SessionHandlers,
): Session {
  const connection = new RTCPeerConnection({ iceServers });
  const channel = connection.createDataChannel("file", { ordered: true });
  channel.binaryType = "arraybuffer";
  channel.bufferedAmountLowThreshold = bufferLow;
  const negotiate = negotiator(connection, handlers, false);

  let acknowledged = 0;
  let closed = false;
  let wake: (() => void) | null = null;
  const nudge = () => {
    wake?.();
    wake = null;
  };
  channel.addEventListener("bufferedamountlow", nudge);
  channel.onmessage = (event) => {
    if (typeof event.data !== "string") return;
    try {
      const control = JSON.parse(event.data) as { ack?: number };
      if (typeof control.ack === "number") acknowledged = control.ack;
      if (acknowledged >= file.size) handlers.onDone();
    } catch {
      // Not ours; ignored.
    }
    nudge();
  };
  channel.onclose = nudge;

  channel.onopen = async () => {
    let sent = 0;
    try {
      while (sent < file.size && !closed) {
        while (!closed && (channel.bufferedAmount > bufferHigh || sent - acknowledged > window)) {
          await new Promise<void>((resolve) => (wake = resolve));
        }
        if (closed || channel.readyState !== "open") return;
        const piece = await file.slice(sent, sent + pieceBytes).arrayBuffer();
        channel.send(piece);
        sent += piece.byteLength;
        handlers.onProgress(sent);
      }
      if (!closed) channel.send(JSON.stringify({ done: file.size }));
    } catch {
      if (!closed) handlers.onFail("connection");
    }
  };

  void (async () => {
    try {
      await connection.setLocalDescription(await connection.createOffer());
      if (connection.localDescription) {
        handlers.signal({ description: connection.localDescription.toJSON() });
      }
    } catch {
      handlers.onFail("connection");
    }
  })();

  return {
    onSignal: negotiate,
    close() {
      closed = true;
      nudge();
      channel.close();
      connection.close();
    },
  };
}

/** Receives a file from the other device into a sink. */
export function receiveFile(
  size: number,
  sink: Sink,
  iceServers: RTCIceServer[],
  handlers: SessionHandlers,
): Session {
  const connection = new RTCPeerConnection({ iceServers });
  const negotiate = negotiator(connection, handlers, true);
  let closed = false;

  connection.ondatachannel = (event) => {
    const channel = event.channel;
    channel.binaryType = "arraybuffer";
    let received = 0;
    let acknowledgedAt = 0;
    // Writes in order, one after another, whatever order the disk finishes in.
    let writing = Promise.resolve();

    channel.onmessage = (message) => {
      if (closed) return;
      if (message.data instanceof ArrayBuffer) {
        const data = message.data;
        received += data.byteLength;
        const upTo = received;
        handlers.onProgress(upTo);
        writing = writing.then(async () => {
          await sink.write(data);
          if (upTo - acknowledgedAt >= ackEvery) {
            acknowledgedAt = upTo;
            channel.send(JSON.stringify({ ack: upTo }));
          }
        });
        return;
      }
      try {
        const control = JSON.parse(String(message.data)) as { done?: number };
        if (typeof control.done !== "number") return;
        writing = writing.then(async () => {
          if (received !== size || control.done !== size) {
            sink.abort();
            handlers.onFail("incomplete");
            return;
          }
          await sink.close();
          channel.send(JSON.stringify({ ack: received }));
          handlers.onDone();
        });
      } catch {
        // Not ours; ignored.
      }
    };
  };

  return {
    onSignal: negotiate,
    close() {
      closed = true;
      connection.close();
    },
  };
}
