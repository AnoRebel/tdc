import * as Api from "#bindings/api";

/**
 * "Keep messages off my lock screen", for the desktop's banners.
 *
 * The setting is the account's, the same one the web client applies to its
 * push notifications: a banner on a desktop is read by whoever is at the
 * screen just as a lock screen is. Go raises the banners, so it is told —
 * with the words to show instead, in this person's language.
 */
export function useBannerPrivacy() {
  const hide = useState("banner-privacy", () => false);
  const api = useApi();
  const { $ts } = useI18n();

  function tellGo() {
    void Api.Service.KeepContentPrivate(hide.value, $ts("notification.privateBody")).catch(
      () => {},
    );
  }

  /** Reads the account's setting and passes it on. */
  async function refresh() {
    const preference = await api.get<{ hide: boolean }>("/api/me/preferences/hide-push-content");
    hide.value = preference.hide;
    tellGo();
  }

  /** Changes it on the account, then here; unchanged if the server refuses. */
  async function set(wanted: boolean) {
    await api.put("/api/me/preferences/hide-push-content", { hide: wanted });
    hide.value = wanted;
    tellGo();
  }

  return { hide, refresh, set };
}
