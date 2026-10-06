<!-- Replaces the layer's: the name, and beside it the version being read. -->
<template>
  <div class="flex items-center gap-3">
    <NuxtLink to="/" aria-label="TDC documentation" class="flex items-center gap-2">
      <img src="/brand/icon-192.png" alt="" class="size-6 rounded" />
      <span class="font-semibold">TDC</span>
      <span class="text-muted hidden sm:inline">docs</span>
    </NuxtLink>
    <VersionSwitcher />
  </div>
</template>
