import type { Attachment } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { createGlobalState } from "@vueuse/core";

import type { UploadTransfer } from "#layers/ui/app/composables/useUploadTransfer";
import type { SaveFile } from "#layers/ui/app/composables/useSaveFile";
import type { CallRecording } from "#layers/ui/app/lib/callRecorder";
import { nameOf } from "#layers/ui/app/lib/names";

/**
 * Recording the call shown, on this device.
 *
 * The server is asked first, and says yes only once the call's state says
 * this person is recording, the start is in the conversation's log and the
 * audit log, and everybody in the call has been sent that state. Only then
 * does anything here begin to record. Stopping — or leaving, holding the call
 * or switching away from it — ends the recording, and the file is saved to
 * the conversation as an ordinary attachment, or offered to save here when
 * the server will not take it.
 *
 * Belongs to the application, like the call: the call window can change
 * size, or move to picture-in-picture, without the recording stopping.
 */
export type RecordingPhase = "idle" | "starting" | "recording" | "saving" | "saved" | "failed";

export const useCallRecording = createGlobalState(() => {
  const api = useApi();
  const c = useCall();
  const { user } = useAuth();
  const { $ts } = useNuxtApp();

  const phase = ref<RecordingPhase>("idle");
  /** The room being recorded. */
  const room = ref<string | null>(null);
  const error = ref<string | null>(null);
  /** How much of the file has reached the server while saving. */
  const saved = ref({ sent: 0, size: 0 });
  /** A recording the server would not take, to save on this device instead. */
  const kept = shallowRef<{ file: File; reason: string } | null>(null);

  let recording: CallRecording | null = null;
  let sendPiece: UploadTransfer | null = null;
  let saveFile: SaveFile | null = null;

  /** Begins recording the call shown. */
  async function start(transfer: UploadTransfer, saver: SaveFile) {
    const target = c.roomId.value;
    if (!target || phase.value === "starting" || phase.value === "recording") return;
    sendPiece = transfer;
    saveFile = saver;
    phase.value = "starting";
    error.value = null;
    kept.value = null;
    try {
      // Everybody is told before this answers; nothing records until it has.
      const state = await api.put<import("@tdc/types").Call>(`/api/rooms/${target}/call/recording`);
      c.calls.value = { ...c.calls.value, [target]: state };
    } catch (cause) {
      phase.value = "idle";
      error.value = isApiError(cause) ? cause.message : $ts("calls.record.couldNotStart");
      return;
    }
    try {
      const { startRecording } = await import("#layers/ui/app/lib/callRecorder");
      recording = startRecording(sourceFor(target));
      room.value = target;
      phase.value = "recording";
    } catch {
      phase.value = "idle";
      error.value = $ts("calls.record.unavailable");
      void api.delete(`/api/rooms/${target}/call/recording`).catch(() => {});
    }
  }

  /** What the recorder draws and hears: the stage of the call shown. */
  function sourceFor(target: string) {
    return {
      presented: () => {
        const presenter = c.calls.value[target]?.presenter;
        if (!presenter) return null;
        return presenter === user.value?.id
          ? c.screen.value
          : (c.remoteScreen.value.get(presenter) ?? null);
      },
      tiles: () =>
        (c.calls.value[target]?.participants ?? []).map((each) => ({
          name: nameOf(each),
          stream:
            each.userId === user.value?.id
              ? c.local.value
              : (c.remote.value.get(each.userId) ?? null),
        })),
      sound: () =>
        [
          c.local.value,
          c.screen.value,
          ...c.remote.value.values(),
          ...c.remoteScreen.value.values(),
        ].filter((each): each is MediaStream => Boolean(each)),
    };
  }

  /** Stops recording and saves the file to the conversation. */
  async function stop() {
    const target = room.value;
    const running = recording;
    if (!target || !running || phase.value !== "recording") return;
    recording = null;
    phase.value = "saving";
    const blob = await running.stop();
    // Still in the call: say it has stopped. Gone from it: the server did.
    if (c.calls.value[target]) {
      try {
        const state = await api.delete<import("@tdc/types").Call>(
          `/api/rooms/${target}/call/recording`,
        );
        if (c.calls.value[target]) c.calls.value = { ...c.calls.value, [target]: state };
      } catch {
        // The server stops it when we leave, or when the connection goes.
      }
    }
    const stamp = new Date().toISOString().slice(0, 16).replace("T", " ").replace(":", ".");
    const file = new File(
      [blob],
      `${$ts("calls.record.fileName", { when: stamp })}.${running.extension}`,
      {
        type: blob.type,
      },
    );
    await upload(target, file);
  }

  async function upload(target: string, file: File) {
    saved.value = { sent: 0, size: file.size };
    const offer = (reason: string) => {
      kept.value = { file, reason };
      phase.value = "failed";
    };
    if (!sendPiece) return offer($ts("attachment.uploadUnreachable"));
    const { sendInPieces } = await import("#layers/ui/app/lib/pieceUpload");
    sendInPieces(
      api,
      sendPiece,
      target,
      file,
      {
        onProgress: (sent) => (saved.value = { sent, size: file.size }),
        onDone: (attachment: Attachment) => {
          void api
            .post(`/api/rooms/${target}/messages`, {
              body: "",
              clientToken: crypto.randomUUID(),
              attachmentIds: [attachment.id],
            })
            .then(() => (phase.value = "saved"))
            .catch((cause) => offer(isApiError(cause) ? cause.message : $ts("room.couldNotSend")));
        },
        onError: (message) => offer(message),
        onCancel: () => offer($ts("calls.record.notSent")),
      },
      $ts("attachment.uploadUnreachable"),
    );
  }

  /** Saves a recording the server would not take on this device. */
  async function keep() {
    const held = kept.value;
    if (!held || !saveFile) return;
    try {
      if (await saveFile(held.file, held.file.name)) dismiss();
    } catch {
      error.value = $ts("calls.record.couldNotSave");
    }
  }

  /** Puts away what is left of the last recording: a notice, or a file not kept. */
  function dismiss() {
    kept.value = null;
    error.value = null;
    if (phase.value === "saved" || phase.value === "failed") phase.value = "idle";
  }

  // A recording is of one call, talked in: leaving it, holding it, switching
  // away from it, or the server saying it has stopped all end it.
  watch(
    () => {
      const target = room.value;
      if (!target) return true;
      const current = c.calls.value[target];
      return (
        Boolean(current?.recording?.includes(user.value?.id ?? "")) &&
        c.roomId.value === target &&
        !c.holding.value.includes(target)
      );
    },
    (still) => {
      if (!still && phase.value === "recording") void stop();
    },
  );

  // Somebody leaving the page with a recording running would lose it.
  if (import.meta.client) {
    window.addEventListener("beforeunload", (event) => {
      if (phase.value === "recording" || phase.value === "saving") event.preventDefault();
    });
  }

  return { phase, room, error, saved, kept, start, stop, keep, dismiss };
});
