<script setup lang="ts">
import { ExternalLink, Scale, Shield } from "lucide-vue-next";
/**
 * This server's own privacy policy and terms, where its operator published them.
 *
 * Only what the operator set. The repository's templates are never offered in
 * their place: a template is not a promise this server has made, and showing
 * one as if it were would be worse than saying nothing was published.
 */
const props = withDefaults(
  defineProps<{
    /** Inline, for a footer; or a block that says when nothing is published. */
    compact?: boolean;
  }>(),
  { compact: false },
);

const { $t, $ts } = useI18n();
const { info } = useServerInfo();

const privacy = computed(() => info.value?.legal?.privacyPolicyUrl);
const terms = computed(() => info.value?.legal?.termsUrl);
</script>

<template>
  <nav
    v-if="privacy || terms"
    :aria-label="$ts('help.policies.title')"
    class="flex flex-wrap items-center gap-x-4 gap-y-1"
    :class="props.compact ? 'text-xs' : 'text-sm'"
  >
    <a
      v-if="privacy"
      :href="privacy"
      target="_blank"
      rel="noopener noreferrer"
      class="inline-flex items-center gap-1.5 text-muted-foreground hover:text-foreground hover:underline"
    >
      <Shield class="size-3.5" aria-hidden="true" />
      {{ $t("help.policies.privacy") }}
      <ExternalLink class="size-3 opacity-60" aria-hidden="true" />
    </a>
    <a
      v-if="terms"
      :href="terms"
      target="_blank"
      rel="noopener noreferrer"
      class="inline-flex items-center gap-1.5 text-muted-foreground hover:text-foreground hover:underline"
    >
      <Scale class="size-3.5" aria-hidden="true" />
      {{ $t("help.policies.terms") }}
      <ExternalLink class="size-3 opacity-60" aria-hidden="true" />
    </a>
  </nav>
  <p v-else-if="!props.compact && info" class="text-sm text-muted-foreground">
    {{ $t("help.policies.none") }}
  </p>
</template>
