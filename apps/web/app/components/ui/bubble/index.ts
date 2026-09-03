import { cva, type VariantProps } from "class-variance-authority";

export { default as Bubble } from "./Bubble.vue";

export const bubbleVariants = cva(
  "relative w-fit max-w-[min(38rem,80%)] rounded-lg px-3 py-2 text-sm break-words whitespace-pre-wrap",
  {
    variants: {
      variant: {
        default: "bg-muted text-foreground",
        // The sender's own messages, so a person can find their own line at a
        // glance without reading the name.
        mine: "bg-primary text-primary-foreground",
        // A message whose content has been removed. It stays in place so the
        // conversation does not appear never to have contained it.
        redacted: "bg-transparent text-muted-foreground italic border border-dashed border-border",
      },
    },
    defaultVariants: { variant: "default" },
  },
);

export type BubbleVariants = VariantProps<typeof bubbleVariants>;
