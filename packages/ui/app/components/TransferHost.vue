<script setup lang="ts">
import type {
  Attachment,
  DirectTransfer,
  DirectTransferSignal,
  Message,
  RoomMember,
} from "@tdc/types";
import { isApiError } from "@tdc/types";
import {
  AlertCircle,
  ArrowDownToLine,
  ArrowUpFromLine,
  Check,
  ShieldAlert,
  X,
} from "lucide-vue-next";
import { Button } from "#layers/ui/app/components/ui/button";
import { readableSize } from "#layers/ui/app/lib/sizes";
import type { Session } from "#layers/ui/app/lib/directTransfer";

/**
 * Direct transfers, wherever somebody is: a file offered straight to another
 * person's device, and the progress of every one under way.
 *
 * Loaded only on servers that offer direct transfers, and the data-channel
 * code only once a transfer actually starts. Holds its own live connection,
 * as calls do, because offers and negotiation arrive on it whatever page is
 * open.
 *
 * Every way a direct transfer can end without the file arriving — the other
 * person offline, declining, not answering, or a connection that cannot be
 * made — offers the sender an ordinary upload of the same file instead.
 */

type Phase =
  | "waiting" // offered, not answered
  | "offered" // offered to me
  | "connecting"
  | "sending"
  | "done"
  | "declined"
  | "failed"
  | "cancelled"
  | "expired"
  | "uploading"; // fallen back to an upload

interface Item {
  transfer: DirectTransfer;
  outgoing: boolean;
  phase: Phase;
  transferred: number;
  startedAt: number;
  /** Whether the received file went straight to disk. */
  toDisk?: boolean;
  error?: string;
}

const { $t, $ts } = useI18n();
const api = useApi();
const { user } = useAuth();
const sendPiece = useUploadTransfer();

const items = ref<Item[]>([]);
// Outside reactive state: these hold browser objects, not data.
const sessions = new Map<string, Session>();
const files = new Map<string, File>();

const live = useLiveConnection({
  onEvent: () => {},
  onResync: () => {},
  onTransferOffered: (transfer) => {
    if (!find(transfer.id)) items.value.push(fresh(transfer, false, "offered"));
  },
  onTransferUpdated: (transfer) => void updated(transfer),
  onTransferSignal: (signal: DirectTransferSignal) =>
    sessions.get(signal.transfer)?.onSignal(signal.signal),
});

function fresh(transfer: DirectTransfer, outgoing: boolean, phase: Phase): Item {
  return { transfer, outgoing, phase, transferred: 0, startedAt: Date.now() };
}

function find(id: string): Item | undefined {
  return items.value.find((item) => item.transfer.id === id);
}

function end(id: string) {
  sessions.get(id)?.close();
  sessions.delete(id);
}

/** What changed, as the server tells both people. */
async function updated(transfer: DirectTransfer) {
  const item = find(transfer.id);
  if (!item) return;
  item.transfer = transfer;
  switch (transfer.state) {
    case "accepted":
      // The sender starts connecting once the recipient has said yes.
      if (item.outgoing && !sessions.has(transfer.id)) await startSending(item);
      break;
    case "finished":
      end(transfer.id);
      item.phase = "done";
      break;
    case "declined":
    case "cancelled":
    case "expired":
    case "failed":
      end(transfer.id);
      if (item.phase !== "done" && item.phase !== "uploading") item.phase = transfer.state;
      break;
  }
}

async function iceServers(): Promise<RTCIceServer[]> {
  try {
    return (await api.get<{ iceServers: RTCIceServer[] }>("/api/transfers/ice")).iceServers;
  } catch {
    return [];
  }
}

function handlersFor(item: Item) {
  const id = item.transfer.id;
  return {
    signal: (body: DirectTransferSignal["signal"]) => live.transferSignal(id, body),
    onProgress: (transferred: number) => {
      item.phase = "sending";
      item.transferred = transferred;
    },
    onDone: () => {
      if (item.phase === "done") return;
      item.phase = "done";
      item.transferred = item.transfer.size;
      // The recipient says it arrived; the sender hears it from the server.
      if (!item.outgoing) void api.post(`/api/transfers/${id}/finish`).catch(() => {});
      end(id);
    },
    onFail: (reason: "connection" | "incomplete") => {
      if (item.phase === "done") return;
      item.phase = "failed";
      item.error =
        reason === "incomplete" ? $ts("transfer.incomplete") : $ts("transfer.couldNotConnect");
      void api.post(`/api/transfers/${id}/fail`).catch(() => {});
      end(id);
    },
  };
}

async function startSending(item: Item) {
  const file = files.get(item.transfer.id);
  if (!file) return;
  item.phase = "connecting";
  item.startedAt = Date.now();
  const [{ sendFile }, servers] = await Promise.all([
    import("#layers/ui/app/lib/directTransfer"),
    iceServers(),
  ]);
  sessions.set(item.transfer.id, sendFile(file, servers, handlersFor(item)));
}

/** Somebody chose to send a file directly, from the composer. */
async function offer(roomId: string, file: File) {
  try {
    const members = await api.get<RoomMember[]>(`/api/rooms/${roomId}/members`);
    const other = members.find((member) => member.id !== user.value?.id);
    if (!other) return;
    const transfer = await api.post<DirectTransfer>(`/api/rooms/${roomId}/transfers`, {
      recipient: other.id,
      filename: file.name,
      size: file.size,
      contentType: file.type,
    });
    files.set(transfer.id, file);
    items.value.push(fresh(transfer, true, "waiting"));
  } catch (cause) {
    const item = fresh(
      {
        id: crypto.randomUUID(),
        roomId,
        senderId: "",
        sender: "",
        recipientId: "",
        filename: file.name,
        size: file.size,
        state: "failed",
        expiresAt: "",
        recipientOnline: false,
      },
      true,
      "failed",
    );
    item.error = isApiError(cause) ? cause.message : $ts("transfer.couldNotOffer");
    files.set(item.transfer.id, file);
    items.value.push(item);
  }
}

async function accept(item: Item) {
  const { openSink, receiveFile } = await import("#layers/ui/app/lib/directTransfer");
  // Asked now, while this is still the click that accepted.
  const sink = await openSink(item.transfer.filename, item.transfer.contentType);
  if (!sink) return;
  try {
    item.transfer = await api.post<DirectTransfer>(`/api/transfers/${item.transfer.id}/accept`);
  } catch (cause) {
    sink.abort();
    item.phase = "failed";
    item.error = isApiError(cause) ? cause.message : $ts("transfer.couldNotConnect");
    return;
  }
  item.phase = "connecting";
  item.toDisk = sink.toDisk;
  item.startedAt = Date.now();
  sessions.set(
    item.transfer.id,
    receiveFile(item.transfer.size, sink, await iceServers(), handlersFor(item)),
  );
}

async function decline(item: Item) {
  dismiss(item);
  await api.post(`/api/transfers/${item.transfer.id}/decline`).catch(() => {});
}

async function cancel(item: Item) {
  end(item.transfer.id);
  item.phase = "cancelled";
  await api.post(`/api/transfers/${item.transfer.id}/cancel`).catch(() => {});
}

function dismiss(item: Item) {
  end(item.transfer.id);
  files.delete(item.transfer.id);
  items.value = items.value.filter((other) => other !== item);
}

/** The fallback: the same file, uploaded and sent as an ordinary message. */
async function sendThroughServer(item: Item) {
  const file = files.get(item.transfer.id);
  if (!file) return;
  if (item.phase === "waiting")
    void api.post(`/api/transfers/${item.transfer.id}/cancel`).catch(() => {});
  end(item.transfer.id);
  item.phase = "uploading";
  item.transferred = 0;
  item.error = undefined;
  const { sendInPieces } = await import("#layers/ui/app/lib/pieceUpload");
  sendInPieces(
    api,
    sendPiece,
    item.transfer.roomId,
    file,
    {
      onProgress: (transferred) => (item.transferred = transferred),
      onDone: (attachment: Attachment) => {
        void api
          .post<Message>(`/api/rooms/${item.transfer.roomId}/messages`, {
            body: "",
            clientToken: crypto.randomUUID(),
            attachmentIds: [attachment.id],
          })
          .then(() => {
            item.phase = "done";
            item.transferred = item.transfer.size;
          })
          .catch((cause) => {
            item.phase = "failed";
            item.error = isApiError(cause) ? cause.message : $ts("room.couldNotSend");
          });
      },
      onError: (message) => {
        item.phase = "failed";
        item.error = message;
      },
      onCancel: () => (item.phase = "cancelled"),
    },
    $ts("attachment.uploadUnreachable"),
  );
}

/** A file somebody picked to send directly, from wherever they picked it. */
function onOfferRequested(event: Event) {
  const detail = (event as CustomEvent<{ roomId: string; file: File }>).detail;
  if (detail?.roomId && detail.file) void offer(detail.roomId, detail.file);
}
onMounted(() => window.addEventListener("tdc:offer-transfer", onOfferRequested));
onBeforeUnmount(() => {
  window.removeEventListener("tdc:offer-transfer", onOfferRequested);
  for (const id of sessions.keys()) end(id);
});

// Leaving with a transfer under way would cut it off; the browser asks first.
useEventListener(window, "beforeunload", (event: BeforeUnloadEvent) => {
  if (items.value.some((item) => item.phase === "sending" || item.phase === "connecting")) {
    event.preventDefault();
  }
});

function percent(item: Item): number {
  return item.transfer.size ? Math.round((item.transferred / item.transfer.size) * 100) : 100;
}

function speed(item: Item): string {
  const seconds = (Date.now() - item.startedAt) / 1000;
  return seconds > 1 ? `${readableSize(item.transferred / seconds)}/s` : "";
}

const canFallBack = (item: Item) =>
  item.outgoing &&
  files.has(item.transfer.id) &&
  ["waiting", "declined", "failed", "expired"].includes(item.phase);

function line(item: Item): string {
  const name = item.transfer.sender;
  switch (item.phase) {
    case "waiting":
      return item.transfer.recipientOnline
        ? $ts("transfer.waiting")
        : $ts("transfer.recipientOffline");
    case "offered":
      return $ts("transfer.offeredBy", { name });
    case "connecting":
      return $ts("transfer.connecting");
    case "sending":
      return `${readableSize(item.transferred)} / ${readableSize(item.transfer.size)} ${speed(item)}`;
    case "done":
      return item.outgoing
        ? $ts("transfer.delivered")
        : item.toDisk
          ? $ts("transfer.saved")
          : $ts("transfer.downloaded");
    case "declined":
      return $ts("transfer.declined");
    case "expired":
      return $ts("transfer.expired");
    case "cancelled":
      return $ts("transfer.cancelled");
    case "uploading":
      return $ts("transfer.uploading", {
        sent: readableSize(item.transferred),
        size: readableSize(item.transfer.size),
      });
    default:
      return item.error ?? $ts("transfer.couldNotConnect");
  }
}
</script>

<template>
  <section
    v-if="items.length"
    class="fixed top-14 right-4 z-50 flex w-[min(22rem,calc(100vw-2rem))] flex-col gap-2"
    :aria-label="$ts('transfer.region')"
  >
    <article
      v-for="item in items"
      :key="item.transfer.id"
      class="rounded-xl border bg-background p-3 shadow-xl"
      :class="item.phase === 'failed' && 'border-destructive/40'"
      :role="item.phase === 'offered' ? 'alertdialog' : 'status'"
      :aria-label="item.transfer.filename"
      data-direct-transfer
    >
      <div class="flex items-start gap-2.5">
        <span
          class="grid size-8 shrink-0 place-items-center rounded-md"
          :class="
            item.phase === 'failed'
              ? 'bg-destructive/15'
              : item.phase === 'done'
                ? 'bg-success/15'
                : 'bg-muted'
          "
        >
          <AlertCircle v-if="item.phase === 'failed'" class="size-4 text-destructive" />
          <Check v-else-if="item.phase === 'done'" class="size-4 text-success-text" />
          <ArrowUpFromLine v-else-if="item.outgoing" class="size-4 text-muted-foreground" />
          <ArrowDownToLine v-else class="size-4 text-muted-foreground" />
        </span>

        <div class="min-w-0 flex-1">
          <p class="flex items-baseline gap-2">
            <span class="truncate text-sm font-medium">{{ item.transfer.filename }}</span>
            <span class="tabular shrink-0 text-xs text-muted-foreground">
              {{ readableSize(item.transfer.size) }}
            </span>
          </p>
          <p
            class="mt-0.5 text-xs"
            :class="item.phase === 'failed' ? 'text-destructive' : 'text-muted-foreground'"
            aria-live="polite"
          >
            {{ line(item) }}
          </p>

          <div
            v-if="item.phase === 'sending' || item.phase === 'uploading'"
            class="mt-2 h-1 overflow-hidden rounded-full bg-muted"
            role="progressbar"
            :aria-valuenow="percent(item)"
            aria-valuemin="0"
            aria-valuemax="100"
            :aria-label="$ts('transfer.progress', { name: item.transfer.filename })"
          >
            <div
              class="h-full rounded-full bg-primary transition-[width] duration-200"
              :style="{ width: `${percent(item)}%` }"
            />
          </div>

          <!-- What accepting means, said before anybody accepts. -->
          <p
            v-if="item.phase === 'offered'"
            class="mt-2 flex gap-1.5 text-xs text-muted-foreground"
          >
            <ShieldAlert class="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
            <span>{{ $t("transfer.addressWarning") }}</span>
          </p>

          <div class="mt-2 flex flex-wrap gap-2 empty:hidden">
            <template v-if="item.phase === 'offered'">
              <Button @click="accept(item)">{{ $t("transfer.accept") }}</Button>
              <Button variant="outline" @click="decline(item)">
                {{ $t("transfer.decline") }}
              </Button>
            </template>
            <Button
              v-if="canFallBack(item)"
              :variant="item.phase === 'waiting' ? 'outline' : 'primary'"
              @click="sendThroughServer(item)"
            >
              {{ $t("transfer.sendThroughServer") }}
            </Button>
            <Button
              v-if="
                item.phase === 'connecting' ||
                item.phase === 'sending' ||
                (item.phase === 'waiting' && item.outgoing)
              "
              variant="ghost"
              @click="cancel(item)"
            >
              {{ $t("action.cancel") }}
            </Button>
          </div>
        </div>

        <Button
          v-if="
            item.phase !== 'offered' &&
            item.phase !== 'sending' &&
            item.phase !== 'connecting' &&
            item.phase !== 'uploading'
          "
          variant="ghost"
          size="icon"
          class="-mt-1 -mr-1 shrink-0"
          :label="$ts('action.close')"
          :title="$ts('action.close')"
          @click="dismiss(item)"
        >
          <X class="size-3.5" />
        </Button>
      </div>
    </article>
  </section>
</template>
