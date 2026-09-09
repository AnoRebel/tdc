// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";

/*
 * Which of three sources decides each colour.
 *
 * The order is: what this person chose, then what the operator chose, then
 * what the palette itself carries — and each of the three resolves
 * independently, so somebody may have chosen a palette and left the accent to
 * the operator.
 *
 * This is where the bug was. The operator's accent used to be applied by a
 * watcher that called removeProperty whenever the operator had set none, which
 * wiped a person's own accent every time server information reloaded — a
 * setting that worked until something unrelated refreshed.
 */

const state = new Map<string, { value: unknown }>();

vi.stubGlobal("useState", (key: string, init: () => unknown) => {
  if (!state.has(key)) state.set(key, { value: init() });
  return state.get(key)!;
});
vi.stubGlobal("onMounted", () => {});
vi.stubGlobal("onUnmounted", () => {});
vi.stubGlobal("useApi", () => ({ put: async () => ({}), get: async () => ({}) }));

const { useTheme } = await import("./useTheme");

/** What the document ended up carrying. */
function documentState() {
  const root = document.documentElement;
  return {
    palette: root.getAttribute("data-theme"),
    // The pair, since one value cannot serve both appearances.
    accentLight: root.style.getPropertyValue("--accent-light"),
    accentDark: root.style.getPropertyValue("--accent-dark"),
    dark: root.classList.contains("dark"),
  };
}

describe("resolving the colours", () => {
  beforeEach(() => {
    state.clear();
    document.documentElement.removeAttribute("data-theme");
    document.documentElement.removeAttribute("style");
    document.documentElement.className = "";
  });

  it("takes the operator's defaults when this person has chosen nothing", () => {
    useTheme().settle({ accent: "#b45309", palette: "congrator" }, undefined);

    expect(documentState().palette).toBe("congrator");
    expect(documentState().accentLight).not.toBe("");
  });

  it("prefers this person's choice over the operator's", () => {
    useTheme().settle(
      { accent: "#b45309", palette: "congrator" },
      { palette: "default", mode: "light", accent: "#4f46e5" },
    );

    // The default palette is the absence of the attribute, so choosing it
    // explicitly has to clear the operator's — not leave theirs in place.
    expect(documentState().palette).toBeNull();
    expect(documentState().accentLight).not.toBe("");
  });

  /*
   * The bug, as a test. An operator with no accent set must not clear one this
   * person chose — an empty value from the operator means "I have no opinion",
   * not "use none".
   */
  it("does not let an unset operator accent clear this person's", () => {
    const theme = useTheme();
    theme.settle({}, { palette: "", mode: "", accent: "#4f46e5" });

    expect(documentState().accentLight).not.toBe("");

    // And again, as happens whenever server information reloads.
    theme.settle({}, { palette: "", mode: "", accent: "#4f46e5" });

    expect(documentState().accentLight).not.toBe("");
  });

  it("clears the accent only when nobody has set one", () => {
    const theme = useTheme();
    theme.settle({ accent: "#b45309" }, undefined);
    expect(documentState().accentLight).not.toBe("");

    theme.settle({}, { palette: "", mode: "", accent: "" });
    expect(documentState().accentLight).toBe("");
  });

  /* Each of the three independently: a person who chose a palette and left the
     accent alone should keep the operator's accent. */
  it("resolves each choice on its own", () => {
    useTheme().settle(
      { accent: "#b45309", palette: "default" },
      { palette: "congrator", mode: "", accent: "" },
    );

    expect(documentState().palette).toBe("congrator");
    expect(documentState().accentLight).not.toBe("");
  });

  /*
   * Light or dark is the one the operator has no say in: it is a property of
   * the room somebody is sitting in, not of the instance they are using.
   */
  it("lets nobody but this person decide light or dark", () => {
    useTheme().settle({ palette: "congrator" }, { palette: "", mode: "dark", accent: "" });

    expect(documentState().dark).toBe(true);
  });

  /* A value the server should not have sent, which a program could still put
     there — the client refuses it rather than setting an attribute that
     selects no rules. */
  it("ignores a palette it does not have", () => {
    useTheme().settle({ palette: "chartreuse" }, { palette: "vermilion", mode: "", accent: "" });

    expect(documentState().palette).toBeNull();
  });
});
