import { cva, type VariantProps } from "class-variance-authority";

export { default as Bubble } from "./Bubble.vue";

/**
 * A message, as a bubble.
 *
 * The shape carries the sender: a bubble on the left with one square corner
 * points back at its author's avatar, and the reader's own messages sit right
 * in the accent colour. That corner is what makes a run of messages read as a
 * conversation rather than a list — it is the reason to have bubbles at all,
 * so it is not rounded away.
 *
 * The width cap keeps a long paragraph from running the full width of a
 * desktop window, where the eye loses the line.
 */
export const bubbleVariants = cva(
  [
    "relative w-fit max-w-[min(42rem,72%)] px-3 py-1.5 text-sm leading-relaxed",
    "break-words whitespace-pre-wrap transition-shadow duration-200",
  ],
  {
    variants: {
      variant: {
        // Someone else's message: a raised surface, so it reads as sitting on
        // the timeline rather than painted onto it.
        default: "bg-surface-raised text-foreground shadow-xs",
        // The reader's own, in the accent so their own line is findable at a
        // glance without reading the name.
        mine: "bg-primary text-primary-foreground shadow-sm",
        // Removed content keeps its place, so the conversation does not appear
        // never to have contained it. Dashed and hollow: visibly an absence.
        redacted: "border border-dashed border-border bg-transparent text-muted-foreground italic",
      },
      align: {
        // One square corner points back at the author's avatar.
        start: "rounded-2xl rounded-tl-sm",
        end: "rounded-2xl rounded-tr-sm",
      },
      /**
       * Within a run from one person, only the first bubble needs the corner;
       * the rest are fully rounded, so the run reads as one turn.
       */
      continued: { true: "", false: "" },
    },
    compoundVariants: [
      { continued: true, align: "start", class: "rounded-tl-2xl" },
      { continued: true, align: "end", class: "rounded-tr-2xl" },
    ],
    defaultVariants: { variant: "default", align: "start", continued: false },
  },
);

export type BubbleVariants = VariantProps<typeof bubbleVariants>;
