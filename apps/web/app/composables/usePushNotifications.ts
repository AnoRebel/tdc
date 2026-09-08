/** A device subscribed to notifications, as its owner sees it. */
export interface PushDevice {
  id: string;
  device: string;
  createdAt: string;
  lastUsedAt?: string;
  current: boolean;
}

/**
 * Notifications that arrive while the application is closed.
 *
 * Three things have to be true before one can: the browser supports push, the
 * operator has generated keys, and the person has granted permission. They
 * fail differently and are worth distinguishing — "your browser cannot do
 * this" and "this server has not set it up" send somebody to different places.
 */
export function usePushNotifications() {
  const api = useApi();

  const supported = ref(false);
  const offered = ref(false);
  const permission = ref<NotificationPermission>("default");
  const devices = ref<PushDevice[]>([]);
  const busy = ref(false);
  const error = ref<string | null>(null);

  /*
   * Whether message content may appear on a lock screen.
   *
   * Read from the account rather than assumed: a control that always opens
   * unchecked would tell somebody their messages are on their lock screen when
   * they are not, and the promise this setting makes is exactly that one.
   */
  const hideContent = ref(false);

  /** Whether this device is one of the subscribed ones. */
  const subscribedHere = computed(() => devices.value.some((device) => device.current));

  async function refresh() {
    if (import.meta.server) return;

    supported.value =
      "serviceWorker" in navigator && "PushManager" in window && "Notification" in window;
    if (supported.value) permission.value = Notification.permission;

    try {
      const config = await api.get<{ enabled: boolean; publicKey?: string }>("/api/push/config");
      offered.value = config.enabled;
    } catch {
      offered.value = false;
    }

    try {
      devices.value = await api.get<PushDevice[]>("/api/me/push/subscriptions");
    } catch {
      devices.value = [];
    }

    try {
      const preference = await api.get<{ hide: boolean }>("/api/me/preferences/hide-push-content");
      hideContent.value = preference.hide;
    } catch {
      // Left as it was. Guessing here would be guessing at the answer to
      // "are my messages on my lock screen", which is not a guess to make.
    }
  }

  /*
   * A base64url key as the browser wants it.
   *
   * The server sends the standard textual form and PushManager takes bytes,
   * so this is the conversion between them. Written out because it is four
   * lines and a dependency for four lines is a dependency to keep updated.
   */
  function keyBytes(base64: string): ArrayBuffer {
    const padded = (base64 + "=".repeat((4 - (base64.length % 4)) % 4))
      .replace(/-/g, "+")
      .replace(/_/g, "/");

    const binary = atob(padded);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i);

    /*
     * The buffer rather than the view. A Uint8Array is a BufferSource too, but
     * only when its backing buffer is known not to be shared — which the type
     * of `new Uint8Array(n)` no longer promises.
     */
    return bytes.buffer;
  }

  /**
   * Subscribes this device.
   *
   * Asks the browser for permission, then for an endpoint, then tells the
   * server where to send. Each step can be refused, and a refusal is reported
   * rather than retried: somebody who declined permission meant it.
   */
  async function subscribe() {
    error.value = null;
    busy.value = true;

    try {
      const config = await api.get<{ enabled: boolean; publicKey?: string }>("/api/push/config");
      if (!config.enabled || !config.publicKey) {
        error.value = "This server has not set up notifications.";
        return;
      }

      permission.value = await Notification.requestPermission();
      if (permission.value !== "granted") {
        /*
         * Not an error worth colouring red. Somebody who declined has made a
         * choice, and a browser that has been refused once will not ask again
         * — telling them where to change it is more use than an apology.
         */
        error.value =
          permission.value === "denied"
            ? "Notifications are blocked for this site in your browser's settings."
            : null;
        return;
      }

      const registration = await navigator.serviceWorker.ready;

      /*
       * Reuse an existing subscription rather than making a second.
       *
       * A browser that has subscribed before returns the same endpoint, and
       * subscribing twice would be two rows for one device — every
       * notification delivered twice.
       */
      const existing = await registration.pushManager.getSubscription();
      const subscription =
        existing ??
        (await registration.pushManager.subscribe({
          // Required by every browser: a push that shows nothing is not
          // allowed, which is why the worker always displays something.
          userVisibleOnly: true,
          applicationServerKey: keyBytes(config.publicKey),
        }));

      const payload = subscription.toJSON();
      await api.post("/api/me/push/subscriptions", {
        endpoint: payload.endpoint,
        keys: payload.keys,
      });

      await refresh();
    } catch (cause) {
      error.value = cause instanceof Error ? cause.message : "This device could not be subscribed.";
    } finally {
      busy.value = false;
    }
  }

  /**
   * Removes a device.
   *
   * The browser's own subscription is withdrawn too when it is this device:
   * leaving it would mean the push service still holds an endpoint this server
   * has forgotten, and nothing would ever clean it up.
   */
  async function unsubscribe(id: string) {
    busy.value = true;
    error.value = null;

    try {
      const device = devices.value.find((candidate) => candidate.id === id);

      await api.delete(`/api/me/push/subscriptions/${id}`);

      if (device?.current && "serviceWorker" in navigator) {
        const registration = await navigator.serviceWorker.ready;
        const subscription = await registration.pushManager.getSubscription();
        await subscription?.unsubscribe();
      }

      await refresh();
    } catch (cause) {
      error.value = cause instanceof Error ? cause.message : "That device could not be removed.";
    } finally {
      busy.value = false;
    }
  }

  /*
   * Records whether message content may appear on a lock screen.
   *
   * The local value follows the server rather than the control: a toggle that
   * shows its new position while the request is failing is a toggle that lies.
   */
  async function setHideContent(hide: boolean) {
    error.value = null;

    try {
      await api.put("/api/me/preferences/hide-push-content", { hide });
      hideContent.value = hide;
    } catch (cause) {
      error.value = cause instanceof Error ? cause.message : "That setting could not be saved.";
      throw cause;
    }
  }

  return {
    supported,
    offered,
    permission,
    devices,
    subscribedHere,
    hideContent,
    busy,
    error,
    refresh,
    subscribe,
    unsubscribe,
    setHideContent,
  };
}
