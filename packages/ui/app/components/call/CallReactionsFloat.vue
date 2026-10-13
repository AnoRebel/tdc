<script setup lang="ts">
/**
 * Reactions floating up over the call and fading — each with who sent it, for
 * a screen reader as for the eye. With reduced motion they fade in place.
 */

const c = useCall();
const emoji: Record<string, string> = {
  clap: "👏",
  laugh: "😂",
  heart: "❤️",
  thumbs_up: "👍",
  surprise: "😮",
  celebrate: "🎉",
};
// Spread across the bottom so a burst does not stack in one column.
function lane(id: number) {
  return `${10 + ((id * 37) % 70)}%`;
}
</script>

<template>
  <div class="pointer-events-none absolute inset-0 overflow-hidden" aria-live="polite">
    <span
      v-for="reaction in c.reactions.value"
      :key="reaction.id"
      class="float absolute bottom-16 flex flex-col items-center"
      :style="{ left: lane(reaction.id) }"
    >
      <span class="text-3xl" aria-hidden="true">{{ emoji[reaction.kind] }}</span>
      <span class="rounded bg-black/60 px-1 text-[0.625rem] text-white">{{
        reaction.username
      }}</span>
      <span class="sr-only">{{ reaction.username }} {{ reaction.kind }}</span>
    </span>
  </div>
</template>

<style scoped>
.float {
  animation: fade 3.8s ease-out forwards;
}
:root[data-motion="full"] .float {
  animation: rise 3.8s cubic-bezier(0.2, 0.7, 0.3, 1) forwards;
}
@keyframes rise {
  0% {
    transform: translateY(0) scale(0.6);
    opacity: 0;
  }
  10% {
    transform: translateY(-10px) scale(1.1);
    opacity: 1;
  }
  100% {
    transform: translateY(-180px) scale(1);
    opacity: 0;
  }
}
@keyframes fade {
  0%,
  70% {
    opacity: 1;
  }
  100% {
    opacity: 0;
  }
}
</style>
