<script setup lang="ts">
/**
 * The layout for signing in, registering and first-run setup.
 *
 * Split rather than a centred card: the left half carries the product's
 * identity, the right half carries the form. A lone card on an empty page is
 * the default every application arrives with, and says nothing about this one.
 */
</script>

<template>
  <div class="grid min-h-dvh lg:grid-cols-[1fr_28rem]">
    <!-- Hidden on small screens, where the form deserves the whole width. -->
    <aside
      class="relative hidden flex-col justify-between overflow-hidden bg-surface-sunken p-10 lg:flex"
    >
      <!-- An ambient wash, off-centre, so the panel has depth without a
           photograph to load. -->
      <div
        aria-hidden="true"
        class="pointer-events-none absolute -top-32 -left-24 size-[36rem] rounded-full opacity-60 blur-3xl"
        style="
          background: radial-gradient(
            circle,
            color-mix(in oklch, var(--primary) 30%, transparent),
            transparent 70%
          );
        "
      />

      <div class="relative flex items-center gap-2.5">
        <span
          class="grid size-8 place-items-center rounded-lg bg-primary text-xs font-bold text-primary-foreground"
        >
          TDC
        </span>
        <span class="font-semibold">Tanzania Developers Community</span>
      </div>

      <div class="relative max-w-md">
        <p class="text-2xl leading-snug font-semibold text-balance">
          Where the community talks, plans, and finds each other.
        </p>
        <p class="mt-3 text-sm text-muted-foreground text-pretty">
          Channels and threads for discussion, projects to track the work, and a directory
          connecting developers with the people who need them. Run on your own server, by the
          community, for the community.
        </p>
      </div>

      <p class="relative text-xs text-muted-foreground">
        Free and open source. Self-hosted, always.
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
        <span class="font-semibold">Tanzania Developers</span>
      </div>

      <div class="w-full max-w-sm">
        <slot />
      </div>
    </main>
  </div>
</template>
