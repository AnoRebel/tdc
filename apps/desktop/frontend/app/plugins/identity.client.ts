import * as Api from "#bindings/api";

/**
 * Keeps Go told who is signed in.
 *
 * Go holds the live socket and raises the system notifications, so it needs to
 * know whose messages are this person's own — a banner for something somebody
 * just typed is the interruption that teaches people to switch notifications
 * off. Only the address travels, which is a label rather than a credential: the
 * session and the password are already on the Go side and never come back.
 *
 * Watched rather than called at each site. Signing out happens from the command
 * palette, which lives in the shared layer and cannot call a Wails binding, and
 * a session can also end because the server said so. Following the state covers
 * all of them, including the one nobody thought to wire up.
 */
export default defineNuxtPlugin({
  name: "tdc:identity",
  setup() {
    const { user } = useAuth();
    // Asked for here, in setup, and used whenever somebody signs in.
    const privacy = useBannerPrivacy();
    const { $ts, $getLocale } = useI18n();

    /*
     * What banners say around the names in them, in this person's language.
     * Go raises the banners and has no translations, so it is handed the
     * words, with their placeholders left for it to fill.
     */
    function sendBannerWords() {
      const word = (key: string) =>
        $ts(`desktop.banners.${key}`, { name: "{name}", room: "{room}", count: "{count}" });
      void Api.Service.UseBannerWords({
        calling: word("calling"),
        directCall: word("directCall"),
        conference: word("conference"),
        inChannel: word("inChannel"),
        startingSoon: word("startingSoon"),
        startsTomorrow: word("startsTomorrow"),
        startsInHour: word("startsInHour"),
        startsInHours: word("startsInHours"),
        startsInMinute: word("startsInMinute"),
        startsInMinutes: word("startsInMinutes"),
      }).catch(() => {});
    }
    watch(() => $getLocale(), sendBannerWords, { immediate: true });

    watch(
      () => user.value?.address ?? "",
      (address, previous) => {
        if (address === previous) return;

        // Failure here costs a banner for one's own message, or one missed for
        // somebody else's. Neither is worth interrupting anything over.
        if (address) {
          void Api.Service.SignedInAs(address).catch(() => {});
          // Whether their banners show what was written: an account setting,
          // so read once they are somebody.
          void privacy.refresh().catch(() => {});
        } else void Api.Service.SignedOut().catch(() => {});
      },
      { immediate: true },
    );
  },
});
