<script setup lang="ts">
/**
 * The layout for signing in, registering and first-run setup.
 *
 * Split rather than a centred card: the left half carries the product's
 * identity, the right half carries the form. A lone card on an empty page is
 * the default every application arrives with, and says nothing about this one.
 */

const panel = ref<HTMLElement | null>(null);

/**
 * Where the wash sits, in pixels from the panel's top-left.
 *
 * Starts off-centre rather than at the middle, so the page has the same
 * composition before the pointer has moved — and so it looks deliberate to
 * someone who never moves a mouse at all.
 */
const restingPosition = { x: 240, y: 200 };
const glow = ref({ ...restingPosition });

/**
 * Someone who has asked for less motion gets a wash that does not chase them.
 *
 * A large soft shape tracking the pointer is exactly the kind of continuous
 * movement that setting exists to stop.
 */
const reducedMotion = useMediaQuery("(prefers-reduced-motion: reduce)");

const { $t } = useI18n();

/**
 * Follows the pointer across the panel.
 *
 * Deliberately only the panel, not the window: the form lives on the other
 * half, and a glow that drifted under it would put a moving light behind the
 * text somebody is trying to type into. Keeping it here means the effect can
 * never cost legibility, which is the one thing a sign-in page cannot afford.
 */
function follow(event: PointerEvent) {
  if (reducedMotion.value) return;

  const element = panel.value;
  if (!element) return;

  // Pixels rather than percentages: a percentage inside a transform resolves
  // against the element being transformed, not its parent, so a percentage
  // here would move the wash by a fraction of its own size instead of the
  // panel's.
  const bounds = element.getBoundingClientRect();
  glow.value = {
    x: event.clientX - bounds.left,
    y: event.clientY - bounds.top,
  };
}

// Returning to the resting position when the pointer leaves, rather than
// freezing wherever it happened to exit.
function rest() {
  glow.value = { ...restingPosition };
}
</script>

<template>
  <div class="grid min-h-dvh lg:grid-cols-[1fr_28rem]">
    <!-- Hidden on small screens, where the form deserves the whole width. -->
    <aside
      ref="panel"
      class="relative hidden flex-col justify-between overflow-hidden bg-surface-sunken p-10 lg:flex"
      @pointermove="follow"
      @pointerleave="rest"
    >
      <!-- An ambient wash that follows the pointer, so the panel has depth and
           a little life without a photograph to load.

           Positioned by percentage and transformed to its own centre, so it
           tracks the pointer exactly rather than trailing it by half its width.
           The transition is long and eased: a light this large moving quickly
           reads as a glitch, and moving slowly reads as depth. -->
      <div
        aria-hidden="true"
        class="pointer-events-none absolute top-0 left-0 size-[36rem] rounded-full opacity-60 blur-3xl transition-transform duration-500 ease-out motion-reduce:transition-none"
        :style="{
          transform: `translate3d(calc(${glow.x}px - 50%), calc(${glow.y}px - 50%), 0)`,
          background: `radial-gradient(
            circle,
            color-mix(in oklch, var(--primary) 30%, transparent),
            transparent 70%
          )`,
        }"
      />

      <div class="relative flex items-center gap-2.5">
        <span
          class="grid size-8 place-items-center rounded-lg bg-primary text-xs font-bold text-primary-foreground"
        >
          TDC
        </span>
        <span class="font-semibold">{{ $t("auth.communityName") }}</span>
      </div>

      <div class="relative max-w-md">
        <p class="text-2xl leading-snug font-semibold text-balance">
          {{ $t("app.tagline") }}
        </p>
        <p class="mt-3 text-sm text-muted-foreground text-pretty">
          {{ $t("auth.communityDescription") }}
        </p>
      </div>

      <p class="relative text-xs text-muted-foreground">
        {{ $t("auth.freeAndOpenSource") }}
      </p>
    </aside>

    <main class="flex flex-col justify-center px-6 py-12 sm:px-10">
      <!-- The mark repeats here for the small-screen layout, which has no
           panel to carry it. -->
      <div class="mb-8 flex items-center gap-2.5 lg:hidden">
        <span
          class="grid size-8 place-items-center rounded-lg bg-primary text-xs font-bold text-primary-foreground"
        >
          TDC
        </span>
        <span class="font-semibold">{{ $t("auth.communityNameShort") }}</span>
      </div>

      <div class="w-full max-w-sm">
        <slot />
      </div>
    </main>
  </div>
</template>
