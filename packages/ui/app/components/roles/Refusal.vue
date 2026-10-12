<script setup lang="ts">
import { ShieldAlert } from "lucide-vue-next";

/**
 * What somebody sees when the server refused to show them roles.
 *
 * Said rather than shown as an empty list: an empty list and a refused one look
 * the same and mean opposite things.
 */
</script>

<template>
  <div class="flex gap-3 rounded-lg border border-destructive/30 bg-destructive/8 p-4" role="alert">
    <ShieldAlert class="mt-0.5 size-4 shrink-0 text-destructive" aria-hidden="true" />
    <div class="text-sm">
      <p class="font-medium text-destructive">{{ $t("roles.cannotManage") }}</p>
      <p class="mt-1 text-muted-foreground">
        {{ $t("admin.needsPermissionBefore") }}
        <code class="rounded bg-muted px-1 py-0.5 font-mono text-xs">role.manage</code>
        {{ $t("admin.needsPermissionOr") }}
        <code class="rounded bg-muted px-1 py-0.5 font-mono text-xs">role.assign</code>
        {{ $t("admin.needsPermissionAfter") }}
      </p>
    </div>
  </div>
</template>
