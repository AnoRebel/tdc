<script setup lang="ts">
import { nameOf } from "#layers/ui/app/lib/names";
import { useForm } from "@tanstack/vue-form";
import type { AnyFieldApi } from "@tanstack/vue-form";
import * as v from "valibot";
import { Archive, ArchiveRestore, Globe, Lock, Settings2, UserMinus, X } from "lucide-vue-next";

import type { Message, Room, RoomMember } from "@tdc/types";
import { isApiError } from "@tdc/types";
import { Avatar } from "#layers/ui/app/components/ui/avatar";
import { Button } from "#layers/ui/app/components/ui/button";
import { Field } from "#layers/ui/app/components/ui/field";
import { Input } from "#layers/ui/app/components/ui/input";
import { Spinner } from "#layers/ui/app/components/ui/spinner";
import { Textarea } from "#layers/ui/app/components/ui/textarea";

/**
 * A channel's settings: its name and topic, who may find it, whether it is
 * archived, and who is in it.
 *
 * Lives in the conversation's header as one element. What it shows there is
 * small — that the channel is archived, for everybody, and a way into the
 * settings for whoever may change something — and everything else opens on
 * request.
 *
 * Each control appears only where the server says the person may use it, asked
 * per channel because a role can be given one channel to look after. The server
 * decides regardless; hiding a control is a courtesy, not the guard.
 *
 * Changes made elsewhere arrive as events in the conversation, which is passed
 * in, so the header follows a rename or an archive by somebody else without a
 * second live connection of its own.
 */

const { $t, $ts } = useI18n();

const props = defineProps<{
  room: Room | null;
  /** The conversation's events, as the room shows them. */
  events: Message[];
}>();

const emit = defineEmits<{
  /** The channel as it now is, after a change here or elsewhere. */
  changed: [room: Room];
}>();

const roomId = computed(() => props.room?.id ?? "");
const admin = useChannelAdmin(roomId);
const roomsApi = useRooms();
const { user, can } = useAuth();
const refreshRooms = inject<() => Promise<void>>("refreshRooms", async () => {});

const open = ref(false);
const error = ref<string | null>(null);
const busy = ref(false);
const members = ref<RoomMember[]>([]);
const loadingMembers = ref(false);

// The two changes that cannot be taken back by the person making them: a
// history opened to the whole server, and somebody taken out of a channel
// they may not be able to return to.
const confirmingPublic = ref(false);
const removing = ref<RoomMember | null>(null);

watch(
  roomId,
  (id) => {
    open.value = false;
    error.value = null;
    if (id && props.room?.member) void admin.refresh();
  },
  { immediate: true },
);

// Joining or leaving changes what may be asked: only a member may change
// anything about a channel.
watch(
  () => props.room?.member,
  (member) => {
    if (member) void admin.refresh();
  },
);

/*
 * Following changes made by somebody else.
 *
 * Only events after the ones already on screen when the room opened: the
 * history holds every rename the channel ever had, and replaying them would
 * refetch the room once for each.
 */
const administrative = new Set([
  "room.renamed",
  "room.archived",
  "room.restored",
  "room.visibility_changed",
  "member.removed",
]);
let seenUpTo = 0;
watch(
  () => [roomId.value, props.events.length] as const,
  ([id], before) => {
    const newest = props.events.at(-1)?.sequence ?? 0;
    // A different room, or this one's history arriving: a baseline, not news.
    if (!before || before[0] !== id || before[1] === 0) {
      seenUpTo = newest;
      return;
    }
    const arrived = props.events.filter(
      (event) => event.sequence > seenUpTo && administrative.has(event.type),
    );
    seenUpTo = Math.max(seenUpTo, newest);
    if (arrived.length) void reload(arrived);
  },
);

async function reload(arrived: Message[]) {
  if (!props.room) return;
  try {
    const now = await roomsApi.get(props.room.id);
    emit("changed", now);
    void refreshRooms();
    if (!now.member) {
      open.value = false;
      return;
    }
    if (open.value && arrived.some((event) => event.type === "member.removed")) {
      await loadMembers();
    }
  } catch {
    // A channel this person can no longer see — made private while they were
    // removed from it. The conversation's own load will say so.
  }
}

async function loadMembers() {
  if (!admin.allowed.value.removeMembers) return;
  loadingMembers.value = true;
  try {
    members.value = await admin.members();
  } catch {
    members.value = [];
  } finally {
    loadingMembers.value = false;
  }
}

function show() {
  error.value = null;
  open.value = true;
  form.reset({ name: props.room?.name ?? "", topic: props.room?.topic ?? "" });
  void loadMembers();
}

onKeyStroke("Escape", () => {
  if (open.value && !confirmingPublic.value && !removing.value) open.value = false;
});

/** Runs a change, reporting the server's refusal in its own words. */
async function attempt(change: () => Promise<Room | void>) {
  busy.value = true;
  error.value = null;
  try {
    const now = await change();
    if (now) emit("changed", now);
    void refreshRooms();
    await admin.refresh();
  } catch (cause) {
    error.value = isApiError(cause) ? cause.message : $ts("channelAdmin.failed");
  } finally {
    busy.value = false;
  }
}

const details = v.object({
  name: v.pipe(
    v.string(),
    v.trim(),
    v.minLength(1, $ts("channelAdmin.nameRequired")),
    v.maxLength(80, $ts("channelAdmin.nameTooLong")),
  ),
  topic: v.pipe(v.string(), v.trim(), v.maxLength(500, $ts("channelAdmin.topicTooLong"))),
});

function firstError(field: AnyFieldApi): string | undefined {
  if (!field.state.meta.isTouched) return undefined;
  const [issue] = field.state.meta.errors;
  if (!issue) return undefined;
  return typeof issue === "string" ? issue : (issue as { message?: string }).message;
}

const form = useForm({
  defaultValues: { name: "", topic: "" },
  validators: { onSubmit: details },
  async onSubmit({ value }) {
    await attempt(() => admin.rename({ name: value.name.trim(), topic: value.topic.trim() }));
  },
});

function makePrivate() {
  void attempt(() => admin.setVisibility("private"));
}

function makePublic() {
  confirmingPublic.value = false;
  void attempt(() => admin.setVisibility("public"));
}

function archive() {
  void attempt(() => admin.archive());
}

function restore() {
  void attempt(() => admin.restore());
}

async function remove(member: RoomMember) {
  removing.value = null;
  await attempt(() => admin.remove(member.id));
  await loadMembers();
}

/** Everybody but the person looking: leaving is done by leaving. */
const others = computed(() => members.value.filter((member) => member.id !== user.value?.id));
</script>

<template>
  <!-- Said for everybody, not only those who may undo it: a channel that has
       stopped taking messages should say so where its name is. -->
  <span
    v-if="room?.archived"
    class="flex shrink-0 items-center gap-1.5 rounded-full bg-warning/15 px-2 py-0.5 text-xs font-medium text-warning"
    role="status"
  >
    <Archive class="size-3" aria-hidden="true" />
    {{ $t("channelAdmin.archived") }}
  </span>

  <Button
    v-if="room?.archived && admin.allowed.value.archive"
    variant="outline"
    size="small"
    :disabled="busy"
    @click="restore"
  >
    <ArchiveRestore class="size-3.5" aria-hidden="true" />
    {{ $t("channelAdmin.restore") }}
  </Button>

  <Button
    v-if="admin.anything.value || (admin.allowed.value.administrable && can('role.manage'))"
    variant="ghost"
    size="icon"
    :label="$ts('channelAdmin.open')"
    @click="show"
  >
    <Settings2 class="size-3.5" />
  </Button>

  <Teleport to="body">
    <!-- eslint-disable-next-line vuejs-accessibility/click-events-have-key-events, vuejs-accessibility/no-static-element-interactions -- a backdrop click closes the dialog; Escape closes it from the keyboard through onKeyStroke -->
    <div
      v-if="open && room"
      class="fixed inset-0 z-50 flex items-center justify-center bg-background/70 p-4 backdrop-blur-sm"
      role="dialog"
      aria-modal="true"
      :aria-label="$ts('channelAdmin.title', { name: room.name })"
      @click.self="open = false"
    >
      <div
        class="max-h-[90vh] w-[min(30rem,100%)] overflow-y-auto rounded-xl border border-border bg-card p-4 shadow-2xl"
      >
        <header class="flex items-center gap-2">
          <Settings2 class="size-4" aria-hidden="true" />
          <h2 class="min-w-0 flex-1 truncate text-sm font-semibold">
            {{ $t("channelAdmin.title", { name: room.name }) }}
          </h2>
          <Button variant="ghost" size="icon" :label="$ts('action.close')" @click="open = false">
            <X class="size-4" />
          </Button>
        </header>

        <p
          v-if="error"
          class="mt-3 rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive"
          role="alert"
        >
          {{ error }}
        </p>

        <!-- Name and topic. -->
        <form
          v-if="admin.allowed.value.rename && !room.archived"
          class="mt-4 space-y-3"
          @submit.prevent="form.handleSubmit()"
        >
          <form.Field name="name">
            <template #default="{ field }">
              <Field :label="$ts('channelAdmin.name')" :error="firstError(field)">
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

          <form.Field name="topic">
            <template #default="{ field }">
              <Field
                :label="$ts('channelAdmin.topic')"
                :hint="$ts('channelAdmin.topicHint')"
                :error="firstError(field)"
              >
                <template #default="{ id, describedBy }">
                  <Textarea
                    :id="id"
                    class="h-auto py-2"
                    rows="2"
                    :model-value="field.state.value"
                    :aria-describedby="describedBy"
                    @update:model-value="field.handleChange(String($event))"
                    @blur="field.handleBlur"
                  />
                </template>
              </Field>
            </template>
          </form.Field>

          <div class="flex justify-end">
            <Button type="submit" :disabled="busy || form.state.isSubmitting">
              <Spinner v-if="form.state.isSubmitting" size="sm" />
              {{ $t("channelAdmin.save") }}
            </Button>
          </div>
        </form>

        <!-- Who may find it. -->
        <section
          v-if="
            (admin.allowed.value.makePrivate ||
              admin.allowed.value.makePublic ||
              admin.allowed.value.federated) &&
            !room.archived
          "
          class="mt-5 border-t pt-4"
        >
          <h3 class="text-xs font-semibold">{{ $t("channelAdmin.visibility") }}</h3>
          <p class="mt-1 flex items-center gap-1.5 text-xs text-muted-foreground">
            <component :is="room.visibility === 'private' ? Lock : Globe" class="size-3.5" />
            {{
              room.visibility === "private"
                ? $t("channelAdmin.isPrivate")
                : $t("channelAdmin.isPublic")
            }}
          </p>

          <p v-if="admin.allowed.value.federated" class="mt-2 text-xs text-muted-foreground">
            {{ $t("channelAdmin.federatedHint") }}
          </p>

          <template v-else>
            <Button
              v-if="room.visibility === 'public' && admin.allowed.value.makePrivate"
              variant="outline"
              class="mt-2"
              :disabled="busy"
              @click="makePrivate"
            >
              <Lock class="size-3.5" aria-hidden="true" />
              {{ $t("channelAdmin.makePrivate") }}
            </Button>
            <Button
              v-if="room.visibility === 'private' && admin.allowed.value.makePublic"
              variant="outline"
              class="mt-2"
              :disabled="busy"
              @click="confirmingPublic = true"
            >
              <Globe class="size-3.5" aria-hidden="true" />
              {{ $t("channelAdmin.makePublic") }}
            </Button>
            <p
              v-if="room.visibility === 'private' && !admin.allowed.value.makePublic"
              class="mt-2 text-xs text-muted-foreground"
            >
              {{ $t("channelAdmin.makePublicNeedsServer") }}
            </p>
          </template>
        </section>

        <!-- Archiving: reversible, so no confirmation — only what cannot be
             undone asks first. -->
        <section v-if="admin.allowed.value.archive" class="mt-5 border-t pt-4">
          <h3 class="text-xs font-semibold">{{ $t("channelAdmin.archiving") }}</h3>
          <p class="mt-1 text-xs text-muted-foreground">
            {{ room.archived ? $t("channelAdmin.archivedHint") : $t("channelAdmin.archiveHint") }}
          </p>
          <Button
            v-if="room.archived"
            variant="outline"
            class="mt-2"
            :disabled="busy"
            @click="restore"
          >
            <ArchiveRestore class="size-3.5" aria-hidden="true" />
            {{ $t("channelAdmin.restore") }}
          </Button>
          <Button v-else variant="outline" class="mt-2" :disabled="busy" @click="archive">
            <Archive class="size-3.5" aria-hidden="true" />
            {{ $t("channelAdmin.archive") }}
          </Button>
        </section>

        <!-- Who may do what here: roles' entries for this channel alone. Its
             own chunk, fetched only when somebody who may change roles opens
             this, so nobody else pays for it. -->
        <LazyRolesChannelPermissions
          v-if="can('role.manage') && admin.allowed.value.administrable"
          :room-id="room.id"
          :room-name="room.name"
        />

        <!-- Who is in it. -->
        <section v-if="admin.allowed.value.removeMembers" class="mt-5 border-t pt-4">
          <h3 class="text-xs font-semibold">{{ $t("channelAdmin.members") }}</h3>
          <p class="mt-1 text-xs text-muted-foreground">{{ $t("channelAdmin.removeHint") }}</p>

          <p
            v-if="loadingMembers"
            class="mt-3 flex items-center gap-2 text-xs text-muted-foreground"
          >
            <Spinner size="sm" />
            {{ $t("channelAdmin.loadingMembers") }}
          </p>
          <p v-else-if="!others.length" class="mt-3 text-xs text-muted-foreground">
            {{ $t("channelAdmin.nobodyElse") }}
          </p>
          <ul v-else class="mt-3 space-y-1">
            <li
              v-for="member in others"
              :key="member.id"
              class="flex items-center gap-2 rounded-lg border border-border px-3 py-1.5"
            >
              <Avatar :address="member.address" :name="nameOf(member)" size="sm" />
              <span class="min-w-0 flex-1">
                <span class="block truncate text-sm">{{ nameOf(member) }}</span>
                <span class="block truncate text-[0.6875rem] text-muted-foreground">
                  {{ member.address
                  }}<template v-if="member.location"> · {{ member.location }}</template>
                </span>
              </span>
              <CallPerson :user-id="member.id" :name="nameOf(member)" />
              <Button
                variant="danger"
                size="icon"
                :label="$ts('channelAdmin.removeWhom', { name: nameOf(member) })"
                :disabled="busy"
                @click="removing = member"
              >
                <UserMinus class="size-3.5" />
              </Button>
            </li>
          </ul>
        </section>
      </div>
    </div>
  </Teleport>

  <ConfirmDialog
    v-if="confirmingPublic && room"
    :title="$ts('channelAdmin.makePublicTitle')"
    :detail="$ts('channelAdmin.makePublicConsequence', { name: room.name })"
    :confirm-label="$ts('channelAdmin.makePublic')"
    :cancel-label="$ts('action.cancel')"
    @cancel="confirmingPublic = false"
    @confirm="makePublic"
  />

  <ConfirmDialog
    v-if="removing && room"
    :title="$ts('channelAdmin.removeTitle', { name: nameOf(removing) })"
    :detail="
      room.visibility === 'private'
        ? $ts('channelAdmin.removePrivateConsequence')
        : $ts('channelAdmin.removePublicConsequence')
    "
    :confirm-label="$ts('channelAdmin.remove')"
    :cancel-label="$ts('action.cancel')"
    @cancel="removing = null"
    @confirm="remove(removing)"
  />
</template>
