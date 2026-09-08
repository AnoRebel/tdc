import { afterFailure, afterSending, readFailure, sendOrder, type Queued } from "~/lib/outbox";

/*
 * storageKey holds the queue across a restart.
 *
 * A message composed on a train and then closed is still a message somebody
 * wrote. Losing it because the tab was closed would defeat the point of
 * queueing it at all.
 */
const storageKey = "tdc.outbox";

/**
 * Messages waiting for a server that could not be reached.
 *
 * Shared across the application rather than per component: the queue is a
 * property of the session, and a message composed in one conversation must not
 * vanish because somebody navigated to another.
 */
export function useOutbox() {
  const queue = useState<Queued[]>("outbox", () => []);

  /** Reads what was left behind by an earlier visit. */
  function restore() {
    if (import.meta.server) return;

    try {
      const stored = localStorage.getItem(storageKey);
      if (!stored) return;

      const parsed: unknown = JSON.parse(stored);
      if (Array.isArray(parsed)) queue.value = parsed as Queued[];
    } catch {
      /*
       * Unreadable, so discarded. localStorage throws in a private window in
       * some browsers, and a queue that cannot be parsed is one written by a
       * version that shaped it differently — neither is worth a blank page.
       */
      queue.value = [];
    }
  }

  function persist() {
    if (import.meta.server) return;

    try {
      localStorage.setItem(storageKey, JSON.stringify(queue.value));
    } catch {
      // Full, or refused. The queue still works for this session; it simply
      // will not survive a restart, which is better than failing to send.
    }
  }

  /** Adds a message that could not be sent. */
  function hold(message: Queued) {
    queue.value = [...queue.value, message];
    persist();
  }

  /**
   * Holds a message if the failure was a network one, and says whether it did.
   *
   * The decision and the holding are one step so that no caller has to know
   * the difference between a refusal and an outage. A caller that got them the
   * wrong way round would either lose messages or retry rejected ones forever.
   */
  function holdIfUnsent(message: Omit<Queued, "composedAt" | "attempts">, cause: unknown): boolean {
    if (readFailure(cause) !== "unreachable") return false;

    hold({ ...message, composedAt: Date.now(), attempts: 0 });
    return true;
  }

  /** Records that a queued message was sent. */
  function sent(clientToken: string) {
    queue.value = afterSending(queue.value, clientToken);
    persist();
  }

  /**
   * Records that sending failed, and says whether it will be tried again.
   *
   * The caller needs the answer to decide what to tell somebody: a message
   * that is still queued is waiting, and one that is not has been lost.
   */
  function failed(clientToken: string, cause: unknown): boolean {
    const failure = readFailure(cause);
    queue.value = afterFailure(queue.value, clientToken, failure);
    persist();

    return queue.value.some((message) => message.clientToken === clientToken);
  }

  /** What is waiting for one conversation, oldest first. */
  function waitingIn(roomId: string): Queued[] {
    return sendOrder(queue.value.filter((message) => message.roomId === roomId));
  }

  /**
   * Sends everything that is waiting, oldest first.
   *
   * Sequential rather than parallel, deliberately: these are somebody's own
   * sentences, and firing them at once would let them arrive in whatever order
   * the network settled on.
   */
  async function flush(send: (message: Queued) => Promise<void>) {
    for (const message of sendOrder(queue.value)) {
      try {
        await send(message);
        sent(message.clientToken);
      } catch (cause) {
        failed(message.clientToken, cause);

        /*
         * Stops at the first unreachable one.
         *
         * If the network is still down, the rest will fail too — and each
         * failure counts an attempt, so continuing would burn the whole
         * queue's retries on a single outage.
         */
        if (readFailure(cause) === "unreachable") return;
      }
    }
  }

  return { queue, restore, hold, holdIfUnsent, sent, failed, waitingIn, flush };
}
