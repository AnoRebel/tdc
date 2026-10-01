<script setup lang="ts">
import { useForm } from "@tanstack/vue-form";
import type { AnyFieldApi } from "@tanstack/vue-form";
import * as v from "valibot";
import { Plus, Trash2 } from "lucide-vue-next";

import type { ProfileDocument } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { Button } from "#layers/ui/app/components/ui/button";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { Textarea } from "#layers/ui/app/components/ui/textarea";
import { Spinner } from "#layers/ui/app/components/ui/spinner";
import VisibilityPicker from "#layers/ui/app/components/directory/VisibilityPicker.vue";
import SkillPicker from "#layers/ui/app/components/directory/SkillPicker.vue";
import { Select } from "#layers/ui/app/components/ui/select";
import { Switch } from "#layers/ui/app/components/ui/switch";
import { daysFromNow, onDate } from "#layers/ui/app/lib/dates";

const { $t, $ts } = useI18n();

/**
 * Somebody writing their own profile.
 *
 * One form for the whole profile, saved as one: the server replaces the
 * profile with what is sent, so a form that saved sections separately would
 * race itself. The visibility choices sit beside the sections they govern
 * rather than in a settings panel somewhere else, because "who sees this" is a
 * question about what is written right there.
 *
 * The rules here are a courtesy — the server checks everything again and has
 * the last word. What they buy is being told at the field.
 */

const props = defineProps<{
  initial: ProfileDocument;
  /** Whether this creates the profile rather than changing it. */
  creating: boolean;
}>();

const emit = defineEmits<{ saved: [ProfileDocument] }>();

const directory = useDirectory();
const error = ref<string | null>(null);

const webLink = (message: string) =>
  v.pipe(
    v.string(),
    v.trim(),
    v.check((value) => /^https?:\/\/[^\s/]+/i.test(value), message),
  );

const schema = computed(() =>
  v.object({
    headline: v.pipe(v.string(), v.maxLength(120, $ts("directory.form.tooLong", { count: 120 }))),
    bio: v.pipe(v.string(), v.maxLength(2000, $ts("directory.form.tooLong", { count: 2000 }))),
    location: v.pipe(v.string(), v.maxLength(80, $ts("directory.form.tooLong", { count: 80 }))),
    experience: v.pipe(v.string(), v.maxLength(500, $ts("directory.form.tooLong", { count: 500 }))),
    contactEmail: v.union([
      v.literal(""),
      v.pipe(v.string(), v.trim(), v.email($ts("directory.form.notAnEmail"))),
    ]),
    contactPhone: v.pipe(v.string(), v.maxLength(200)),
    contactUrl: v.union([v.literal(""), webLink($ts("directory.form.notAWebLink"))]),
    visibility: v.picklist(["public", "members", "hidden"]),
    detailsVisibility: v.picklist(["public", "members", "hidden"]),
    contactVisibility: v.picklist(["public", "members", "hidden"]),
    portfolio: v.pipe(
      v.array(
        v.object({
          id: v.optional(v.string()),
          title: v.pipe(
            v.string(),
            v.trim(),
            v.minLength(1, $ts("directory.form.titleRequired")),
            v.maxLength(120, $ts("directory.form.tooLong", { count: 120 })),
          ),
          url: webLink($ts("directory.form.notAWebLink")),
          description: v.optional(v.string()),
        }),
      ),
      v.maxLength(20, $ts("directory.form.tooManyLinks", { count: 20 })),
    ),
    skills: v.array(
      v.object({
        id: v.optional(v.string()),
        name: v.string(),
        status: v.optional(v.picklist(["active", "proposed"])),
        description: v.pipe(
          v.string(),
          v.maxLength(300, $ts("directory.form.tooLong", { count: 300 })),
        ),
      }),
    ),
    openToWork: v.boolean(),
    availabilityNote: v.pipe(
      v.string(),
      v.maxLength(200, $ts("directory.form.tooLong", { count: 200 })),
    ),
    availableUntil: v.optional(v.string()),
    acceptsEnquiries: v.nullable(v.boolean()),
  }),
);

/*
 * How long somebody is available for, chosen as a span rather than a date.
 *
 * A span is what people think in — "for the next month" — and it cannot be a
 * date in the past or years away, both of which the server refuses. The
 * choice becomes an absolute instant the moment it is made.
 */
const spans = computed(() => [
  { value: 7, label: $ts("directory.availability.oneWeek") },
  { value: 14, label: $ts("directory.availability.twoWeeks") },
  { value: 30, label: $ts("directory.availability.oneMonth") },
  { value: 90, label: $ts("directory.availability.threeMonths") },
  { value: 180, label: $ts("directory.availability.sixMonths") },
]);
const span = ref<number | undefined>(props.initial.openToWork ? undefined : 30);

/** The first thing wrong with a field, once somebody has touched it. */
function firstError(field: AnyFieldApi): string | undefined {
  if (!field.state.meta.isTouched && !form.state.submissionAttempts) return undefined;
  const [issue] = field.state.meta.errors;
  if (!issue) return undefined;
  return typeof issue === "string" ? issue : (issue as { message?: string }).message;
}

const form = useForm({
  defaultValues: structuredClone(toRaw(props.initial)),
  validators: { onSubmit: schema.value },
  async onSubmit({ value }) {
    error.value = null;
    try {
      const saved = await directory.saveProfile(value);
      emit("saved", saved.profile);
    } catch (cause) {
      error.value = isApiError(cause) ? cause.message : $ts("directory.couldNotReach");
    }
  },
});
</script>

<template>
  <form class="space-y-8" novalidate @submit.prevent="form.handleSubmit()">
    <p
      v-if="error"
      class="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
      role="alert"
    >
      {{ error }}
    </p>

    <!-- The profile as a whole: whether it is in the directory at all. -->
    <section class="space-y-3">
      <form.Field name="visibility">
        <template #default="{ field }">
          <VisibilityPicker
            :model-value="field.state.value"
            :legend="$ts('directory.profile.whoCanFindYou')"
            :hint="$ts('directory.profile.whoCanFindYouHint')"
            @update:model-value="field.handleChange($event)"
          />
        </template>
      </form.Field>

      <!-- Taking enquiries is its own decision, separate from who may send
           a direct message. Until chosen it follows the profile: public takes
           them. -->
      <form.Subscribe>
        <template #default="{ values }">
          <div class="flex items-start gap-3 rounded-lg border p-3">
            <Switch
              :model-value="values.acceptsEnquiries ?? values.visibility === 'public'"
              :label="$ts('directory.enquiry.takeThem')"
              @update:model-value="
                (taking: boolean) => form.setFieldValue('acceptsEnquiries', taking)
              "
            />
            <span class="min-w-0">
              <span class="block text-sm">{{ $t("directory.enquiry.takeThem") }}</span>
              <span class="block text-xs text-muted-foreground">
                {{ $t("directory.enquiry.takeThemHint") }}
              </span>
            </span>
          </div>
        </template>
      </form.Subscribe>
    </section>

    <section class="space-y-4">
      <div>
        <h2 class="text-sm font-semibold">{{ $t("directory.profile.aboutYou") }}</h2>
        <p class="mt-0.5 text-xs text-muted-foreground">
          {{ $t("directory.profile.aboutYouHint") }}
        </p>
      </div>

      <form.Field name="headline">
        <template #default="{ field }">
          <Field
            :label="$ts('directory.profile.headline')"
            :hint="$ts('directory.profile.headlineHint')"
            :error="firstError(field)"
          >
            <template #default="{ id, invalid, describedBy }">
              <Input
                :id="id"
                :model-value="field.state.value"
                :invalid="invalid"
                :described-by="describedBy"
                @update:model-value="field.handleChange($event)"
                @blur="field.handleBlur"
              />
            </template>
          </Field>
        </template>
      </form.Field>

      <div class="grid gap-4 sm:grid-cols-2">
        <form.Field name="location">
          <template #default="{ field }">
            <Field :label="$ts('directory.profile.location')" :error="firstError(field)">
              <template #default="{ id, invalid, describedBy }">
                <Input
                  :id="id"
                  :model-value="field.state.value"
                  autocomplete="address-level2"
                  :invalid="invalid"
                  :described-by="describedBy"
                  @update:model-value="field.handleChange($event)"
                  @blur="field.handleBlur"
                />
              </template>
            </Field>
          </template>
        </form.Field>

        <form.Field name="experience">
          <template #default="{ field }">
            <Field
              :label="$ts('directory.profile.experience')"
              :hint="$ts('directory.profile.experienceHint')"
              :error="firstError(field)"
            >
              <template #default="{ id, invalid, describedBy }">
                <Input
                  :id="id"
                  :model-value="field.state.value"
                  :invalid="invalid"
                  :described-by="describedBy"
                  @update:model-value="field.handleChange($event)"
                  @blur="field.handleBlur"
                />
              </template>
            </Field>
          </template>
        </form.Field>
      </div>

      <form.Field name="bio">
        <template #default="{ field }">
          <Field :label="$ts('directory.profile.bio')" :error="firstError(field)">
            <template #default="{ id, invalid, describedBy }">
              <Textarea
                :id="id"
                rows="5"
                :model-value="field.state.value"
                :invalid="invalid"
                :described-by="describedBy"
                @update:model-value="field.handleChange($event)"
                @blur="field.handleBlur"
              />
            </template>
          </Field>
        </template>
      </form.Field>

      <!-- Links, not uploads: the work already lives somewhere, and a profile
           is read on phones over metered data. -->
      <form.Field name="portfolio" mode="array">
        <template #default="{ field: links }">
          <fieldset class="space-y-3">
            <legend class="text-sm font-medium">{{ $t("directory.profile.portfolio") }}</legend>
            <p v-if="!links.state.value.length" class="text-xs text-muted-foreground">
              {{ $t("directory.profile.portfolioEmpty") }}
            </p>

            <div
              v-for="(_, index) in links.state.value"
              :key="index"
              class="space-y-3 rounded-lg border p-3"
            >
              <div class="grid gap-3 sm:grid-cols-2">
                <form.Field :name="`portfolio[${index}].title`">
                  <template #default="{ field }">
                    <Field :label="$ts('directory.profile.linkTitle')" :error="firstError(field)">
                      <template #default="{ id, invalid, describedBy }">
                        <Input
                          :id="id"
                          :model-value="field.state.value"
                          :invalid="invalid"
                          :described-by="describedBy"
                          @update:model-value="field.handleChange($event)"
                          @blur="field.handleBlur"
                        />
                      </template>
                    </Field>
                  </template>
                </form.Field>
                <form.Field :name="`portfolio[${index}].url`">
                  <template #default="{ field }">
                    <Field :label="$ts('directory.profile.linkUrl')" :error="firstError(field)">
                      <template #default="{ id, invalid, describedBy }">
                        <Input
                          :id="id"
                          type="url"
                          placeholder="https://"
                          :model-value="field.state.value"
                          :invalid="invalid"
                          :described-by="describedBy"
                          @update:model-value="field.handleChange($event)"
                          @blur="field.handleBlur"
                        />
                      </template>
                    </Field>
                  </template>
                </form.Field>
              </div>
              <form.Field :name="`portfolio[${index}].description`">
                <template #default="{ field }">
                  <Field :label="$ts('directory.profile.linkDescription')">
                    <template #default="{ id, describedBy }">
                      <Input
                        :id="id"
                        :model-value="field.state.value ?? ''"
                        :described-by="describedBy"
                        @update:model-value="field.handleChange($event)"
                        @blur="field.handleBlur"
                      />
                    </template>
                  </Field>
                </template>
              </form.Field>
              <div class="flex justify-end">
                <Button variant="danger" size="small" @click="links.removeValue(index)">
                  <Trash2 class="size-3.5" />
                  {{ $t("directory.profile.removeLink") }}
                </Button>
              </div>
            </div>

            <Button
              variant="outline"
              :disabled="links.state.value.length >= 20"
              @click="links.pushValue({ title: '', url: '', description: '' })"
            >
              <Plus class="size-3.5" />
              {{ $t("directory.profile.addLink") }}
            </Button>
          </fieldset>
        </template>
      </form.Field>

      <form.Field name="skills">
        <template #default="{ field }">
          <SkillPicker
            :model-value="field.state.value"
            @update:model-value="field.handleChange($event)"
          />
        </template>
      </form.Field>

      <!-- Availability: a statement about work, nothing to do with whether
           somebody is online, and one that lapses on its own. -->
      <fieldset class="space-y-3 rounded-lg border p-3">
        <legend class="px-1 text-sm font-medium">{{ $t("directory.availability.title") }}</legend>
        <form.Field name="openToWork">
          <template #default="{ field }">
            <div class="flex items-center gap-3">
              <Switch
                :model-value="field.state.value"
                :label="$ts('directory.availability.openToWork')"
                @update:model-value="
                  (open: boolean) => {
                    field.handleChange(open);
                    if (open && !form.state.values.availableUntil) {
                      form.setFieldValue('availableUntil', daysFromNow(span ?? 30));
                    }
                  }
                "
              />
              <span class="text-sm">{{ $t("directory.availability.openToWork") }}</span>
            </div>
          </template>
        </form.Field>

        <form.Subscribe>
          <template #default="{ values }">
            <div v-if="values.openToWork" class="grid gap-3 sm:grid-cols-2">
              <div>
                <Select
                  :model-value="span"
                  :options="spans"
                  :label="$ts('directory.availability.forHowLong')"
                  :placeholder="$ts('directory.availability.forHowLong')"
                  @update:model-value="
                    (days) => {
                      span = days;
                      if (days) form.setFieldValue('availableUntil', daysFromNow(days));
                    }
                  "
                />
                <p v-if="values.availableUntil" class="mt-1 text-xs text-muted-foreground">
                  {{ $t("directory.availability.until", { date: onDate(values.availableUntil) }) }}
                </p>
              </div>
              <form.Field name="availabilityNote">
                <template #default="{ field }">
                  <Field
                    :label="$ts('directory.availability.note')"
                    :hint="$ts('directory.availability.noteHint')"
                    :error="firstError(field)"
                  >
                    <template #default="{ id, invalid, describedBy }">
                      <Input
                        :id="id"
                        :model-value="field.state.value"
                        :invalid="invalid"
                        :described-by="describedBy"
                        @update:model-value="field.handleChange($event)"
                        @blur="field.handleBlur"
                      />
                    </template>
                  </Field>
                </template>
              </form.Field>
            </div>
            <p v-else class="text-xs text-muted-foreground">
              {{ $t("directory.availability.closedHint") }}
            </p>
          </template>
        </form.Subscribe>
      </fieldset>

      <form.Field name="detailsVisibility">
        <template #default="{ field }">
          <VisibilityPicker
            :model-value="field.state.value"
            :legend="$ts('directory.profile.whoSeesDetails')"
            :hint="$ts('directory.profile.whoSeesDetailsHint')"
            @update:model-value="field.handleChange($event)"
          />
        </template>
      </form.Field>
    </section>

    <!-- Contact details carry their own visibility, hidden until chosen:
         being findable by strangers is not the same as being reachable by
         them. -->
    <section class="space-y-4">
      <div>
        <h2 class="text-sm font-semibold">{{ $t("directory.profile.contact") }}</h2>
        <p class="mt-0.5 text-xs text-muted-foreground">
          {{ $t("directory.profile.contactHint") }}
        </p>
      </div>

      <div class="grid gap-4 sm:grid-cols-3">
        <form.Field name="contactEmail">
          <template #default="{ field }">
            <Field :label="$ts('directory.profile.contactEmail')" :error="firstError(field)">
              <template #default="{ id, invalid, describedBy }">
                <Input
                  :id="id"
                  type="email"
                  autocomplete="email"
                  :model-value="field.state.value"
                  :invalid="invalid"
                  :described-by="describedBy"
                  @update:model-value="field.handleChange($event)"
                  @blur="field.handleBlur"
                />
              </template>
            </Field>
          </template>
        </form.Field>
        <form.Field name="contactPhone">
          <template #default="{ field }">
            <Field :label="$ts('directory.profile.contactPhone')" :error="firstError(field)">
              <template #default="{ id, invalid, describedBy }">
                <Input
                  :id="id"
                  type="tel"
                  autocomplete="tel"
                  :model-value="field.state.value"
                  :invalid="invalid"
                  :described-by="describedBy"
                  @update:model-value="field.handleChange($event)"
                  @blur="field.handleBlur"
                />
              </template>
            </Field>
          </template>
        </form.Field>
        <form.Field name="contactUrl">
          <template #default="{ field }">
            <Field :label="$ts('directory.profile.contactUrl')" :error="firstError(field)">
              <template #default="{ id, invalid, describedBy }">
                <Input
                  :id="id"
                  type="url"
                  placeholder="https://"
                  :model-value="field.state.value"
                  :invalid="invalid"
                  :described-by="describedBy"
                  @update:model-value="field.handleChange($event)"
                  @blur="field.handleBlur"
                />
              </template>
            </Field>
          </template>
        </form.Field>
      </div>

      <form.Field name="contactVisibility">
        <template #default="{ field }">
          <VisibilityPicker
            :model-value="field.state.value"
            :legend="$ts('directory.profile.whoSeesContact')"
            :hint="$ts('directory.profile.whoSeesContactHint')"
            @update:model-value="field.handleChange($event)"
          />
        </template>
      </form.Field>
    </section>

    <form.Subscribe>
      <template #default="{ isSubmitting }">
        <div class="flex justify-end">
          <Button type="submit" class="h-9" :disabled="isSubmitting">
            <Spinner v-if="isSubmitting" size="sm" />
            {{
              props.creating
                ? $t("directory.profile.create")
                : isSubmitting
                  ? $t("directory.profile.saving")
                  : $t("directory.profile.save")
            }}
          </Button>
        </div>
      </template>
    </form.Subscribe>
  </form>
</template>
