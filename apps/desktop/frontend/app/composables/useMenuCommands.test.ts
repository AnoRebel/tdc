// @vitest-environment jsdom
import { mount } from "@vue/test-utils";
import { defineComponent } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";

/*
 * Whether a menu item reaches the interface.
 *
 * The chain is: Go emits a name, the Wails runtime delivers it, this
 * composable maps it to a function on the current screen. Running the
 * application demonstrates the first two links; this covers the third, which
 * is the one that changes every time a screen is added.
 *
 * The runtime is replaced because it talks to a native host that does not
 * exist in a test process. Everything below it — which events are subscribed
 * to, what happens when one arrives, what is released on unmount — is the real
 * composable.
 */

const subscriptions = new Map<string, () => void>();
const unsubscribed: string[] = [];

vi.mock("@wailsio/runtime", () => ({
  Events: {
    On(name: string, handler: () => void) {
      subscriptions.set(name, handler);
      return () => unsubscribed.push(name);
    },
  },
}));

// Nuxt provides these globally in an application; a test process does not.
vi.stubGlobal("onMounted", (fn: () => void) => fn());
vi.stubGlobal("onBeforeUnmount", (fn: () => void) => teardown.push(fn));
const teardown: Array<() => void> = [];

const { useMenuCommands } = await import("./useMenuCommands");
const { menuEvents } = await import("~/lib/menu-events");

/** Mounts a component that answers the menu, as a screen does. */
function screenAnswering(commands: Parameters<typeof useMenuCommands>[0]) {
  return mount(
    defineComponent({
      setup() {
        useMenuCommands(commands);
        return () => null;
      },
    }),
  );
}

describe("the application menu", () => {
  beforeEach(() => {
    subscriptions.clear();
    unsubscribed.length = 0;
    teardown.length = 0;
  });

  it("runs what a menu item asks for", () => {
    const search = vi.fn();
    screenAnswering({ search });

    subscriptions.get(menuEvents.search)?.();

    expect(search, "choosing Search in the menu did nothing").toHaveBeenCalledOnce();
  });

  it("subscribes to every command a screen answers", () => {
    screenAnswering({ search: vi.fn(), newChannel: vi.fn() });

    expect(subscriptions.has(menuEvents.search)).toBe(true);
    expect(subscriptions.has(menuEvents.newChannel)).toBe(true);
  });

  it("leaves an item this screen does not answer unsubscribed", () => {
    screenAnswering({ search: vi.fn() });

    // Not an empty handler: the menu can ask what is listening and disable the
    // rest, which it cannot do if everything claims to be handled.
    expect(subscriptions.has(menuEvents.preferences)).toBe(false);
  });

  it("stops listening when the screen goes away", () => {
    screenAnswering({ search: vi.fn() });

    for (const stop of teardown) stop();

    expect(
      unsubscribed,
      "a handler outliving its screen acts on something nobody is looking at",
    ).toContain(menuEvents.search);
  });
});
