import { afterEach, describe, expect, it, vi } from "vitest";

import { commandKeyLabel, isMac } from "./platform";

/*
 * Which key means "command", decided from what the browser will admit to.
 *
 * navigator.platform is deprecated and a browser is allowed to freeze or empty
 * it — some WebKit builds already return an empty string. Emptiness must not be
 * read as "not a Mac" and leave a Mac binding Ctrl, so the user agent is the
 * fallback, and the reference that names the key reads from the same function
 * the shortcut binds with.
 */

function pretend(values: { platform?: string; userAgent?: string; uaData?: string }) {
  vi.stubGlobal("navigator", {
    platform: values.platform ?? "",
    userAgent: values.userAgent ?? "",
    ...(values.uaData ? { userAgentData: { platform: values.uaData } } : {}),
  });
}

afterEach(() => vi.unstubAllGlobals());

describe("which key means command", () => {
  it("reads a Mac from the modern platform hint", () => {
    pretend({ uaData: "macOS", platform: "", userAgent: "" });
    expect(isMac()).toBe(true);
    expect(commandKeyLabel()).toBe("⌘");
  });

  it("reads a Mac from the deprecated platform when that is all there is", () => {
    pretend({ platform: "MacIntel" });
    expect(isMac()).toBe(true);
  });

  /*
   * The case that made this a shared function: a webview reporting nothing for
   * platform. Reading that as "not a Mac" is right on Linux and wrong on a Mac,
   * so the user agent decides.
   */
  it("falls back to the user agent when the platform is empty", () => {
    pretend({ platform: "", userAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)" });
    expect(isMac()).toBe(true);
  });

  it("is not a Mac on a Linux webview that reports nothing useful", () => {
    pretend({ platform: "", userAgent: "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/605.1.15" });
    expect(isMac()).toBe(false);
    expect(commandKeyLabel()).toBe("Ctrl");
  });

  // The desktop client's own webview, which is the one this has to be right for.
  it("is not a Mac for WebKitGTK", () => {
    pretend({ platform: "Linux x86_64", userAgent: "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit" });
    expect(isMac()).toBe(false);
  });
});
