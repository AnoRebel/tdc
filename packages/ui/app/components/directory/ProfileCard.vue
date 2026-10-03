<script setup lang="ts">
import type { ProfileView } from "@tdc/types";
import {
  BriefcaseBusiness,
  Building2,
  ExternalLink,
  Globe,
  Mail,
  MapPin,
  Phone,
} from "lucide-vue-next";
import { Avatar } from "#layers/ui/app/components/ui/avatar";
import { onDate } from "#layers/ui/app/lib/dates";
import VerifiedBadge from "#layers/ui/app/components/directory/VerifiedBadge.vue";

const { $t, $localeRoute: localeRoute } = useI18n();

/**
 * A profile as somebody else sees it.
 *
 * Draws exactly what arrived. A section the viewer may not see is absent from
 * the response, and absent here too — with no "hidden" placeholder, because a
 * placeholder would say there is something to hide, which is the one thing a
 * hidden section declines to say.
 */

const props = defineProps<{ profile: ProfileView }>();

const hasContact = computed(
  () => props.profile.contactEmail || props.profile.contactPhone || props.profile.contactUrl,
);
</script>

<template>
  <article class="space-y-6">
    <header class="flex items-start gap-4">
      <Avatar :name="props.profile.displayName || props.profile.username" size="lg" />
      <div class="min-w-0">
        <h1 class="text-lg font-semibold break-words">
          {{ props.profile.displayName || props.profile.username }}
        </h1>
        <p class="text-xs text-muted-foreground">{{ props.profile.address }}</p>
        <p
          v-if="props.profile.identityVerified"
          class="mt-1 flex flex-wrap items-center gap-2 text-xs"
        >
          <span class="text-muted-foreground">{{ $t("directory.verification.identity") }}</span>
          <VerifiedBadge :verified="props.profile.identityVerified" />
        </p>
        <p v-if="props.profile.headline" class="mt-1 text-sm">{{ props.profile.headline }}</p>
        <p
          v-if="props.profile.location"
          class="mt-1 flex items-center gap-1 text-xs text-muted-foreground"
        >
          <MapPin class="size-3.5" />
          {{ props.profile.location }}
        </p>
      </div>
    </header>

    <p
      v-if="props.profile.available"
      class="flex flex-wrap items-center gap-2 rounded-lg border border-success/40 bg-success/10 px-3 py-2 text-sm"
    >
      <BriefcaseBusiness class="size-4 shrink-0" />
      <span class="font-medium">{{ $t("directory.availability.available") }}</span>
      <span v-if="props.profile.availabilityNote">· {{ props.profile.availabilityNote }}</span>
      <span v-if="props.profile.availableUntil" class="text-xs text-muted-foreground">
        {{ $t("directory.availability.until", { date: onDate(props.profile.availableUntil) }) }}
      </span>
    </p>

    <slot name="actions" />

    <section v-if="props.profile.skills?.length">
      <h2 class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
        {{ $t("directory.skills.title") }}
      </h2>
      <ul class="mt-2 space-y-2">
        <li v-for="skill in props.profile.skills" :key="skill.id" class="rounded-lg border p-3">
          <p class="flex flex-wrap items-center gap-2 text-sm font-medium">
            {{ skill.name }}
            <VerifiedBadge v-if="skill.verified" :verified="skill.verified" />
          </p>
          <p v-if="skill.description" class="mt-1 text-xs text-muted-foreground">
            {{ skill.description }}
          </p>
          <slot name="skill" :skill="skill" />
        </li>
      </ul>
    </section>

    <section v-if="props.profile.bio">
      <h2 class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
        {{ $t("directory.profile.bio") }}
      </h2>
      <p class="mt-1 text-sm break-words whitespace-pre-wrap">{{ props.profile.bio }}</p>
    </section>

    <section v-if="props.profile.experience">
      <h2 class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
        {{ $t("directory.profile.experience") }}
      </h2>
      <p class="mt-1 text-sm break-words">{{ props.profile.experience }}</p>
    </section>

    <slot name="details" />

    <section v-if="props.profile.portfolio?.length">
      <h2 class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
        {{ $t("directory.profile.portfolio") }}
      </h2>
      <ul class="mt-2 space-y-2">
        <li v-for="item in props.profile.portfolio" :key="item.id" class="rounded-lg border p-3">
          <a
            :href="item.url"
            target="_blank"
            rel="noopener noreferrer nofollow ugc"
            class="flex items-center gap-1.5 text-sm font-medium text-primary hover:underline"
          >
            {{ item.title }}
            <ExternalLink class="size-3.5" />
          </a>
          <p v-if="item.description" class="mt-1 text-xs text-muted-foreground">
            {{ item.description }}
          </p>
          <VerifiedBadge v-if="item.verified" class="mt-1" :verified="item.verified" />
        </li>
      </ul>
    </section>

    <section v-if="props.profile.companies?.length">
      <h2 class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
        {{ $t("directory.company.companies") }}
      </h2>
      <ul class="mt-2 flex flex-wrap gap-2">
        <li v-for="company in props.profile.companies" :key="company.slug">
          <NuxtLink
            :to="localeRoute({ name: 'directory-companies-slug', params: { slug: company.slug } })"
            class="flex items-center gap-1.5 rounded-full border px-3 py-1 text-xs hover:bg-accent"
          >
            <Building2 class="size-3.5" />
            {{ company.name }}
            <span v-if="company.role" class="text-muted-foreground">· {{ company.role }}</span>
          </NuxtLink>
          <VerifiedBadge v-if="company.verified" class="mt-1" :verified="company.verified" />
        </li>
      </ul>
    </section>

    <section v-if="hasContact">
      <h2 class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
        {{ $t("directory.profile.contact") }}
      </h2>
      <ul class="mt-2 space-y-1 text-sm">
        <li v-if="props.profile.contactEmail" class="flex items-center gap-2">
          <Mail class="size-3.5 text-muted-foreground" />
          <a :href="`mailto:${props.profile.contactEmail}`" class="text-primary hover:underline">
            {{ props.profile.contactEmail }}
          </a>
        </li>
        <li v-if="props.profile.contactPhone" class="flex items-center gap-2">
          <Phone class="size-3.5 text-muted-foreground" />
          <a :href="`tel:${props.profile.contactPhone}`" class="text-primary hover:underline">
            {{ props.profile.contactPhone }}
          </a>
        </li>
        <li v-if="props.profile.contactUrl" class="flex items-center gap-2">
          <Globe class="size-3.5 text-muted-foreground" />
          <a
            :href="props.profile.contactUrl"
            target="_blank"
            rel="noopener noreferrer nofollow ugc"
            class="text-primary hover:underline"
          >
            {{ props.profile.contactUrl }}
          </a>
        </li>
      </ul>
    </section>
  </article>
</template>
