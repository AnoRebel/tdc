import { createGlobalState } from "@vueuse/core";

import type { ControlMessage } from "#layers/ui/app/composables/useCall";
import type {
  ControlCapability,
  ControlInput,
  ControlRefusal,
} from "#layers/ui/app/composables/useRemoteControlHost";
import { nameOf } from "#layers/ui/app/lib/names";

/**
 * Remote control of a shared screen, from both sides.
 *
 * Somebody watching a screen being presented may ask to control it. The ask
 * goes to the presenter on the call's own data channel, peer to peer, and so
 * does everything after it. The presenter's client answers at once with a
 * refusal that says why, when it can only refuse — they share from a browser,
 * they share a window or a tab rather than a whole screen, or their computer
 * cannot be driven — and otherwise asks the presenter, who allows or declines.
 *
 * Allowing hands control to that one person in the desktop application's Go
 * side, which applies their input only while that grant stands. Taking it back
 * — from the call window, by the key kept for it, by granting somebody else,
 * or by the share ending or either person leaving — ends it at once, and the
 * controller is told.
 *
 * Must first be called from a component's setup — the call host — so that it
 * finds the desktop's way in.
 */
export const useRemoteControl = createGlobalState(() => {
  const c = useCall();
  const { user } = useAuth();
  const { $ts } = useNuxtApp();
  const host = useRemoteControlHost();

  // ----- Sharing: who has asked, and who holds control. -----

  /** People asking to control our screen, oldest first. */
  const asking = ref<{ room: string; userId: string; name: string }[]>([]);
  /** Who controls our screen now. */
  const controller = ref<{ room: string; userId: string; name: string } | null>(null);
  const capability = ref<ControlCapability | null>(null);

  function nameIn(room: string, userId: string): string {
    const person = c.calls.value[room]?.participants.find((each) => each.userId === userId);
    return person ? nameOf(person) : "";
  }

  /** Whether what we share is a whole screen, which is all that can be controlled. */
  function sharingWholeScreen(): boolean {
    const track = c.screen.value?.getVideoTracks()[0];
    const settings = track?.getSettings() as
      (MediaTrackSettings & { displaySurface?: string }) | undefined;
    return settings?.displaySurface === "monitor";
  }

  async function refusalFor(): Promise<ControlRefusal | null> {
    if (!host) return "browser";
    if (!c.screen.value || !sharingWholeScreen()) return "not-screen";
    capability.value = await host.capability();
    if (!capability.value.available)
      return (capability.value.reason as ControlRefusal) ?? "platform";
    return null;
  }

  async function onAsk(room: string, from: string) {
    const refusal = await refusalFor();
    if (refusal) {
      c.sendControl(room, from, { t: "refused", reason: refusal });
      return;
    }
    if (asking.value.some((each) => each.userId === from) || controller.value?.userId === from)
      return;
    asking.value = [...asking.value, { room, userId: from, name: nameIn(room, from) }];
  }

  /** Lets somebody who asked control our screen, taking it from anybody else. */
  async function allow(userId: string) {
    const request = asking.value.find((each) => each.userId === userId);
    if (!request || !host) return;
    asking.value = asking.value.filter((each) => each.userId !== userId);
    const previous = controller.value;
    try {
      await host.grant(
        request.userId,
        request.name,
        $ts("calls.control.noticeOnScreen", {
          name: request.name,
          key: capability.value?.revokeKey ?? "",
        }),
      );
    } catch {
      c.sendControl(request.room, request.userId, { t: "refused", reason: "platform" });
      return;
    }
    if (previous) c.sendControl(previous.room, previous.userId, { t: "revoked" });
    controller.value = request;
    c.sendControl(request.room, request.userId, { t: "granted" });
  }

  function decline(userId: string) {
    const request = asking.value.find((each) => each.userId === userId);
    asking.value = asking.value.filter((each) => each.userId !== userId);
    if (request) c.sendControl(request.room, request.userId, { t: "declined" });
  }

  /** Takes control back at once. */
  async function revoke() {
    const held = controller.value;
    controller.value = null;
    await host?.revoke();
    if (held) c.sendControl(held.room, held.userId, { t: "revoked" });
  }

  // Go may end control on its own — the revoke key — and says so.
  host?.onChange((state) => {
    if (!state.controller && controller.value) {
      const held = controller.value;
      controller.value = null;
      c.sendControl(held.room, held.userId, { t: "revoked" });
    }
  });

  // A share beginning tells Go its size; one ending ends any control with it.
  watch(
    () => c.screen.value,
    (shared) => {
      if (!host) return;
      const track = shared?.getVideoTracks()[0];
      if (track && sharingWholeScreen()) {
        const { width = 0, height = 0 } = track.getSettings();
        void host.beginShare(width, height);
        return;
      }
      void host.endShare();
      for (const request of asking.value) decline(request.userId);
      if (controller.value) void revoke();
    },
  );

  // ----- Controlling: our ask, and our hold on somebody's screen. -----

  /** Where our ask, or our control, of the screen shown stands. */
  const mine = ref<{
    room: string;
    sharer: string;
    state: "asking" | "controlling" | "declined" | "refused" | "revoked";
    reason?: ControlRefusal;
  } | null>(null);

  /** Asks the presenter of the call shown for control of their screen. */
  function ask() {
    const room = c.roomId.value;
    const presenter = c.call.value?.presenter;
    if (!room || !presenter || presenter === user.value?.id) return;
    if (!c.sendControl(room, presenter, { t: "ask" })) {
      mine.value = { room, sharer: presenter, state: "refused", reason: "platform" };
      return;
    }
    mine.value = { room, sharer: presenter, state: "asking" };
  }

  /** Gives control back. */
  function release() {
    const held = mine.value;
    if (held && (held.state === "controlling" || held.state === "asking")) {
      c.sendControl(held.room, held.sharer, { t: "release" });
    }
    mine.value = null;
  }

  /** Sends one piece of input to the screen we control. */
  function send(input: ControlInput) {
    const held = mine.value;
    if (held?.state !== "controlling") return;
    c.sendControl(held.room, held.sharer, { t: "input", ...input });
  }

  // The presenter stopping, or somebody else taking the stage, ends ours.
  watch(
    () => c.call.value?.presenter,
    (presenter) => {
      if (mine.value && presenter !== mine.value.sharer) mine.value = null;
    },
  );

  // ----- Both: what arrives on the channel. -----

  c.onControl((room, from, message: ControlMessage) => {
    switch (message.t) {
      case "ask":
        void onAsk(room, from);
        return;
      case "input":
        // Go checks again, and is what decides; this only saves the trip.
        if (host && controller.value?.userId === from) {
          void host.apply(from, message as unknown as ControlInput).catch(() => {});
        }
        return;
      case "release":
        asking.value = asking.value.filter((each) => each.userId !== from);
        if (controller.value?.userId === from) void revoke();
        return;
      case "gone":
        asking.value = asking.value.filter((each) => each.userId !== from);
        if (controller.value?.userId === from) void revoke();
        if (mine.value?.sharer === from) mine.value = null;
        return;
    }
    if (mine.value?.sharer !== from) return;
    if (message.t === "granted") mine.value = { ...mine.value, state: "controlling" };
    else if (message.t === "declined") mine.value = { ...mine.value, state: "declined" };
    else if (message.t === "revoked") mine.value = { ...mine.value, state: "revoked" };
    else if (message.t === "refused") {
      mine.value = { ...mine.value, state: "refused", reason: message.reason as ControlRefusal };
    }
  });

  return { asking, controller, capability, allow, decline, revoke, mine, ask, release, send };
});
