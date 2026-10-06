// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { nextTick } from "vue";

/*
 * The interface preferences: motion, shortcuts and density.
 *
 * What matters is what reaches the document. Every animation in the stylesheet
 * is keyed off `data-motion` on the root element, so a preference that is
 * stored but not applied — or applied but not stored — is one that looks like
 * it works until the next reload.
 */

/**
 * A stand-in for the operating system's reduce-motion setting.
 *
 * jsdom has no matchMedia at all. This one answers the reduced-motion query
 * and can change its answer, the way somebody flipping the setting in their
 * system would, while the application is open.
 */
function stubSystemMotion(reduce: boolean) {
  const query = Object.assign(new EventTarget(), {
    matches: reduce,
    media: "(prefers-reduced-motion: reduce)",
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
  });

  vi.stubGlobal(
    "matchMedia",
    vi.fn((media: string) =>
      media.includes("prefers-reduced-motion")
        ? query
        : Object.assign(new EventTarget(), { matches: false, media }),
    ),
  );

  return {
    change(now: boolean) {
      query.matches = now;
      query.dispatchEvent(Object.assign(new Event("change"), { matches: now }));
    },
  };
}

/** A fresh copy of the composable, since its state is shared for the whole application. */
async function fresh() {
  vi.resetModules();
  const { useInterfacePreferences } = await import("./useInterfacePreferences");
  return useInterfacePreferences();
}

const root = () => document.documentElement;

describe("the motion preference", () => {
  beforeEach(() => {
    localStorage.clear();
    root().removeAttribute("data-motion");
    root().removeAttribute("data-density");
  });

  afterEach(() => vi.unstubAllGlobals());

  it("follows the operating system when nobody has chosen", async () => {
    stubSystemMotion(false);
    const preferences = await fresh();
    await nextTick();

    expect(preferences.motionPreference.value).toBe("system");
    expect(preferences.motion.value).toBe("full");
    expect(root().getAttribute("data-motion")).toBe("full");
  });

  /*
   * Reduced rather than off: somebody who asked their system for less motion
   * still gets the fade that says something changed, without anything moving
   * across the screen.
   */
  it("reduces motion by default when the operating system asks for less", async () => {
    stubSystemMotion(true);
    const preferences = await fresh();
    await nextTick();

    expect(preferences.motion.value).toBe("reduced");
    expect(root().getAttribute("data-motion")).toBe("reduced");
  });

  it("keeps following the operating system while the application is open", async () => {
    const system = stubSystemMotion(false);
    await fresh();
    await nextTick();
    expect(root().getAttribute("data-motion")).toBe("full");

    system.change(true);
    await nextTick();
    expect(root().getAttribute("data-motion")).toBe("reduced");
  });

  it("lets an explicit choice override the operating system", async () => {
    stubSystemMotion(true);
    const preferences = await fresh();

    preferences.motionPreference.value = "full";
    await nextTick();
    expect(root().getAttribute("data-motion")).toBe("full");

    preferences.motionPreference.value = "off";
    await nextTick();
    expect(root().getAttribute("data-motion")).toBe("off");
  });

  it("remembers the choice for the next visit", async () => {
    stubSystemMotion(false);
    const first = await fresh();
    first.motionPreference.value = "off";
    await nextTick();

    expect(localStorage.getItem("tdc-motion")).toBe("off");

    // A reload: a new copy of everything, reading what was stored.
    root().removeAttribute("data-motion");
    const second = await fresh();
    await nextTick();

    expect(second.motionPreference.value).toBe("off");
    expect(root().getAttribute("data-motion")).toBe("off");
  });

  /*
   * Not written until somebody chooses. A stored "system" would read the same
   * today, and quietly survive a later change to what the default should be.
   */
  it("stores nothing for somebody who has not chosen", async () => {
    stubSystemMotion(false);
    await fresh();
    await nextTick();

    expect(localStorage.getItem("tdc-motion")).toBeNull();
  });

  it("reads an unrecognised stored value as following the system", async () => {
    stubSystemMotion(true);
    localStorage.setItem("tdc-motion", "dazzling");
    const preferences = await fresh();
    await nextTick();

    expect(preferences.motionPreference.value).toBe("system");
    expect(root().getAttribute("data-motion")).toBe("reduced");
  });
});

describe("the other interface preferences", () => {
  beforeEach(() => {
    localStorage.clear();
    stubSystemMotion(false);
  });

  afterEach(() => vi.unstubAllGlobals());

  it("has shortcuts on until somebody turns them off, and remembers that", async () => {
    const first = await fresh();
    expect(first.shortcuts.value).toBe(true);

    first.shortcuts.value = false;
    await nextTick();

    const second = await fresh();
    expect(second.shortcuts.value).toBe(false);
  });

  it("marks the document compact only when compact is chosen", async () => {
    const preferences = await fresh();
    await nextTick();
    expect(root().hasAttribute("data-density")).toBe(false);

    preferences.density.value = "compact";
    await nextTick();
    expect(root().getAttribute("data-density")).toBe("compact");

    preferences.density.value = "comfortable";
    await nextTick();
    expect(root().hasAttribute("data-density")).toBe(false);
  });
});
