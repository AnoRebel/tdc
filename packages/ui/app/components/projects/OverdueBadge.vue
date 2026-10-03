<script setup lang="ts">
const { $t } = useI18n();

/**
 * That a milestone's day has passed with work unfinished.
 *
 * Words with the icon, never colour alone: "overdue" has to read the same to
 * somebody who cannot tell this red from the rest.
 */
</script>

<template>
  <span
    class="inline-flex items-center gap-1 rounded border border-destructive/40 px-1.5 py-0.5 text-[0.65rem] font-medium text-destructive"
    data-overdue
  >
    <Icon name="lucide:clock-alert" class="size-3" aria-hidden="true" />
    {{ $t("projects.planning.overdue") }}
  </span>
</template>
