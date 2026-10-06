// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ref } from "vue";

/*
 * How calls ring, on this device.
 *
 * The settings belong to a server, so the desktop client's servers do not
 * share them; a person's own tone wins over the default; a tone that has gone
 * falls back rather than ringing silence; and vibration is separate from the
 * sound, so a muted ringer still buzzes.
 */

const server = ref({ canonicalAddress: "one.example", name: "One" });

async function ringtones() {
  vi.resetModules();
  vi.stubGlobal("useServerInfo", () => ({ info: server }));
  const module = await import("./useRingtones");
  return module.useRingtones();
}

beforeEach(() => {
  localStorage.clear();
  server.value = { canonicalAddress: "one.example", name: "One" };
});
afterEach(() => vi.unstubAllGlobals());

describe("ringtones", () => {
  it("keeps each server's settings apart", async () => {
    const r = await ringtones();
    r.setDefault("soft");
    expect(r.toneFor("amina@one.example")).toBe("soft");

    server.value = { canonicalAddress: "two.example", name: "Two" };
    expect(r.toneFor("amina@one.example")).toBe("classic");
  });

  it("rings somebody's own tone, and falls back when it is gone", async () => {
    const r = await ringtones();
    r.setDefault("low");
    r.setPersonTone("baraka@one.example", "bright");
    expect(r.toneFor("baraka@one.example")).toBe("bright");
    expect(r.toneFor("amina@one.example")).toBe("low");

    r.setPersonTone("baraka@one.example", "custom-missing");
    expect(r.toneFor("baraka@one.example")).toBe("classic");
  });

  it("refuses a file that is not audio, or too large to keep", async () => {
    const r = await ringtones();
    expect(await r.addTone(new File(["x"], "notes.txt", { type: "text/plain" }))).toBe("not-audio");
    const big = new File([new Uint8Array(500 * 1024)], "song.mp3", { type: "audio/mpeg" });
    expect(await r.addTone(big)).toBe("too-large");
    expect(r.settings.value.custom).toHaveLength(0);
  });

  it("vibrates with the ringer muted, and stops when the ring does", async () => {
    const vibrate = vi.fn(() => true);
    Object.defineProperty(navigator, "vibrate", { value: vibrate, configurable: true });
    const r = await ringtones();
    r.setMuted(true);

    const stop = r.ring("amina@one.example");
    expect(vibrate).toHaveBeenCalledWith([400, 200, 400]);
    stop();
    expect(vibrate).toHaveBeenLastCalledWith(0);

    r.setVibrate(false);
    vibrate.mockClear();
    r.ring("amina@one.example")();
    expect(vibrate).not.toHaveBeenCalled();
  });
});
