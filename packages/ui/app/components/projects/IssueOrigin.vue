<script setup lang="ts">
import type { IssueOrigin } from "@tdc/types";
import { fully, whenBriefly } from "#layers/ui/app/lib/dates";

const { $t, $ts, $localeRoute: localeRoute } = useI18n();

/**
 * The conversation an issue was made from.
 *
 * Shown as the reader's own access allows. Somebody who can read the room sees
 * the message as it reads there now — edited, or removed — with a way back to
 * it. Anybody else is told only that the issue came from a conversation they
 * cannot see: not which, not who said what, and not whether it is still there.
 */
const props = defineProps<{ origin: IssueOrigin }>();

function who(address: string): string {
  return address.split("@")[0] ?? address;
}
</script>

<template>
  <section
    class="rounded-lg border bg-muted/20 px-3 py-2.5 text-sm"
    :aria-label="$ts('projects.origin.title')"
  >
    <h2 class="flex items-center gap-1.5 text-xs font-semibold text-muted-foreground">
      <Icon name="lucide:message-square-quote" class="size-3.5" aria-hidden="true" />
      {{ $t("projects.origin.title") }}
    </h2>

    <p v-if="!props.origin.disclosed" class="mt-1 text-xs text-muted-foreground">
      {{ $t("projects.origin.undisclosed") }}
    </p>

    <template v-else>
      <p v-if="props.origin.message?.removed" class="mt-1 text-xs text-muted-foreground italic">
        {{ $t("projects.origin.removed", { room: props.origin.roomName ?? "" }) }}
      </p>
      <blockquote v-else-if="props.origin.message" class="mt-1.5 border-l-2 pl-2.5">
        <p class="text-xs break-words whitespace-pre-wrap">{{ props.origin.message.body }}</p>
        <footer class="mt-1 text-[0.6875rem] text-muted-foreground">
          {{ who(props.origin.message.sender) }} ·
          <time :datetime="props.origin.message.sentAt" :title="fully(props.origin.message.sentAt)">
            {{ whenBriefly(props.origin.message.sentAt) }}
          </time>
          <template v-if="props.origin.message.edited">
            · {{ $t("projects.origin.edited") }}</template
          >
        </footer>
      </blockquote>
      <NuxtLink
        :to="
          localeRoute({
            name: 'rooms-id',
            params: { id: props.origin.roomId ?? '' },
            query: { at: props.origin.eventId, jump: String(Date.now()) },
          })
        "
        class="mt-1.5 inline-block text-xs text-primary hover:underline"
      >
        {{ $t("projects.origin.open", { room: props.origin.roomName ?? "" }) }}
      </NuxtLink>
    </template>
  </section>
</template>
