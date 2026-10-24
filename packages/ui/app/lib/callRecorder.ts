/**
 * Recording a call on this device.
 *
 * What is recorded is what this device receives and shows: the stage — the
 * screen being presented, or everybody's tiles — drawn onto a canvas, and
 * every voice mixed into one track. Nothing passes through the server for it;
 * the file is made here and saved afterwards like any other attachment.
 *
 * Loaded only when somebody starts recording, so the call window does not
 * carry it.
 */

/** One tile on the stage: who it is, and what they are sending. */
export interface StageTile {
  name: string;
  stream: MediaStream | null;
}

/** What the recorder draws and mixes, read afresh on every frame. */
export interface RecordingSource {
  /** A presented screen, which fills the stage while there is one. */
  presented(): MediaStream | null;
  /** Everybody in the call, in order. */
  tiles(): StageTile[];
  /** Every stream whose sound belongs in the recording. */
  sound(): MediaStream[];
}

export interface CallRecording {
  /** The file's type, and the extension it is saved with. */
  mimeType: string;
  extension: string;
  /** Stops recording and gives back the file's bytes. */
  stop(): Promise<Blob>;
}

const width = 1280;
const height = 720;
const framesPerSecond = 30;

/** The first type this platform can record, WebM preferred. */
export function recordingType(): { mimeType: string; extension: string } | null {
  if (typeof MediaRecorder === "undefined") return null;
  for (const mimeType of [
    "video/webm;codecs=vp9,opus",
    "video/webm;codecs=vp8,opus",
    "video/webm",
    "video/mp4",
  ]) {
    if (MediaRecorder.isTypeSupported(mimeType)) {
      return { mimeType, extension: mimeType.startsWith("video/mp4") ? "mp4" : "webm" };
    }
  }
  return null;
}

/** Whether this device can record a call at all. */
export function canRecord(): boolean {
  return (
    recordingType() !== null &&
    typeof AudioContext !== "undefined" &&
    typeof HTMLCanvasElement.prototype.captureStream === "function"
  );
}

export function startRecording(source: RecordingSource): CallRecording {
  const type = recordingType();
  if (!type) throw new Error("recording is not available on this device");

  const canvas = document.createElement("canvas");
  canvas.width = width;
  canvas.height = height;
  const context = canvas.getContext("2d")!;

  // One hidden, muted player per stream, so a frame can be drawn from it.
  const players = new Map<MediaStream, HTMLVideoElement>();
  function player(stream: MediaStream): HTMLVideoElement {
    let video = players.get(stream);
    if (!video) {
      video = document.createElement("video");
      video.muted = true;
      video.playsInline = true;
      video.srcObject = stream;
      void video.play().catch(() => {});
      players.set(stream, video);
    }
    return video;
  }
  const hasVideo = (stream: MediaStream | null): stream is MediaStream =>
    Boolean(stream?.getVideoTracks().some((each) => each.readyState === "live" && each.enabled));

  function drawInto(video: HTMLVideoElement, x: number, y: number, w: number, h: number) {
    if (!video.videoWidth) return;
    const scale = Math.min(w / video.videoWidth, h / video.videoHeight);
    const dw = video.videoWidth * scale;
    const dh = video.videoHeight * scale;
    context.drawImage(video, x + (w - dw) / 2, y + (h - dh) / 2, dw, dh);
  }

  function draw() {
    context.fillStyle = "#111";
    context.fillRect(0, 0, width, height);
    const presented = source.presented();
    if (hasVideo(presented)) {
      drawInto(player(presented), 0, 0, width, height);
      return;
    }
    const tiles = source.tiles();
    const columns = Math.ceil(Math.sqrt(tiles.length || 1));
    const rows = Math.ceil((tiles.length || 1) / columns);
    const w = width / columns;
    const h = height / rows;
    tiles.forEach((tile, index) => {
      const x = (index % columns) * w;
      const y = Math.floor(index / columns) * h;
      if (hasVideo(tile.stream)) drawInto(player(tile.stream), x + 4, y + 4, w - 8, h - 8);
      context.fillStyle = "rgba(0,0,0,0.6)";
      context.font = "20px system-ui, sans-serif";
      const label = tile.name;
      const measured = context.measureText(label).width;
      context.fillRect(x + 12, y + h - 44, measured + 16, 32);
      context.fillStyle = "#fff";
      context.fillText(label, x + 20, y + h - 21);
    });
  }
  // ponytail: a timer, which a browser slows to once a second in a hidden tab;
  // the recording then stutters while the window is hidden. A worker-driven
  // clock if that matters.
  const timer = setInterval(draw, 1000 / framesPerSecond);
  draw();

  // Every voice into one track. Keyed by track, since a stream gains its
  // sound some time after it arrives.
  const audio = new AudioContext();
  const mixed = audio.createMediaStreamDestination();
  const mixing = new Map<string, MediaStreamAudioSourceNode>();
  function mix() {
    const live = new Set<string>();
    for (const stream of source.sound()) {
      for (const track of stream.getAudioTracks()) {
        live.add(track.id);
        if (mixing.has(track.id)) continue;
        const node = audio.createMediaStreamSource(new MediaStream([track]));
        node.connect(mixed);
        mixing.set(track.id, node);
      }
    }
    for (const [id, node] of mixing) {
      if (!live.has(id)) {
        node.disconnect();
        mixing.delete(id);
      }
    }
  }
  mix();
  const mixer = setInterval(mix, 1000);

  const recorded = new MediaStream([
    ...canvas.captureStream(framesPerSecond).getVideoTracks(),
    ...mixed.stream.getAudioTracks(),
  ]);
  const recorder = new MediaRecorder(recorded, {
    mimeType: type.mimeType,
    videoBitsPerSecond: 1_500_000,
  });
  // ponytail: the pieces are kept in memory until the end, about 700 MB an
  // hour at this rate; written to disk as they come if long recordings need it.
  const parts: Blob[] = [];
  recorder.ondataavailable = ({ data }) => {
    if (data.size) parts.push(data);
  };
  // In pieces of a second, so a crash of the recorder loses little.
  recorder.start(1000);

  return {
    ...type,
    stop() {
      return new Promise<Blob>((resolve) => {
        recorder.onstop = () => {
          clearInterval(timer);
          clearInterval(mixer);
          for (const video of players.values()) video.srcObject = null;
          recorded.getTracks().forEach((each) => each.stop());
          void audio.close();
          resolve(new Blob(parts, { type: type.mimeType.split(";")[0] }));
        };
        if (recorder.state === "inactive") recorder.onstop(new Event("stop"));
        else recorder.stop();
      });
    },
  };
}
