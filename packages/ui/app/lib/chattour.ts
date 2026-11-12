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
export function chatTour(say: (key: string) => string): DriveStep[] {
  return [
    {
      element: "[data-tour='channels']",
      popover: {
        title: say("tour.conversations.title"),
        description: say("tour.conversations.body"),
        side: "right",
        align: "start",
      },
    },
    {
      element: "[data-tour='search']",
      popover: {
        title: say("tour.finding.title"),
        description: say("tour.finding.body"),
        side: "right",
        align: "start",
      },
    },
    {
      element: "[data-tour='create']",
      popover: {
        title: say("tour.starting.title"),
        description: say("tour.starting.body"),
        side: "right",
        align: "end",
      },
    },
    {
      element: "[data-tour='notifications']",
      popover: {
        title: say("tour.missed.title"),
        description: say("tour.missed.body"),
        side: "top",
        align: "end",
      },
    },
  ];
}
