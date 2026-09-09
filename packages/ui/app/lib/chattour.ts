import type { DriveStep } from "driver.js";

/**
 * The introduction somebody gets the first time they arrive.
 *
 * Four things, which is about as much as anybody takes in at once: where the
 * conversations are, how to find something, how to start a channel, and where
 * they will be told about a message. Everything else is discoverable by
 * looking, and a tour that explains what is already obvious is the kind people
 * skip — which then costs the useful parts too.
 *
 * Targets are data-tour attributes rather than classes or structure. A tour
 * that breaks when somebody restyles a button is a tour that quietly stops
 * working, and nobody notices until a new colleague asks what happened to it.
 */
export function chatTour(): DriveStep[] {
  return [
    {
      element: "[data-tour='channels']",
      popover: {
        title: "Your conversations",
        description:
          "Channels you have joined, and the people you are talking to. " +
          "Whatever you were last reading is where you come back to.",
        side: "right",
        align: "start",
      },
    },
    {
      element: "[data-tour='search']",
      popover: {
        title: "Finding something",
        description:
          "Search messages, channels and people from here — or press the " +
          "shortcut shown, from anywhere.",
        side: "right",
        align: "start",
      },
    },
    {
      element: "[data-tour='create']",
      popover: {
        title: "Starting a channel",
        description:
          "A channel is a room for a subject. Anyone on this server can be " +
          "in one, unless you make it private.",
        side: "right",
        align: "end",
      },
    },
    {
      element: "[data-tour='notifications']",
      popover: {
        title: "What you have missed",
        description:
          "Unread messages gather here. You decide per channel how much you " +
          "want to hear, in its settings.",
        side: "top",
        align: "end",
      },
    },
  ];
}
