import type { Appearance } from "@tdc/types";

/**
 * Keeps the interface's colours in step with the server.
 *
 * Two sources, resolved in one place: the operator's defaults, which everybody
 * gets, and this person's own choice, which overrides them. Both arrive
 * asynchronously and either may arrive first, so this reacts to whichever has
 * turned up rather than assuming an order.
 *
 * Called once, from the root component of each client. Anything that applied
 * these separately would race — which is what the web application did before,
 * clearing a person's accent whenever the operator had set none.
 */
export function useAppearanceSync() {
  const theme = useTheme();
  const { branding } = useServerInfo();
  const api = useApi();

  /** What the server has stored for this person, once it is known. */
  const mine = useState<Appearance | undefined>("appearance-mine", () => undefined);

  onMounted(async () => {
    try {
      mine.value = await api.get<Appearance>("/api/me/preferences/appearance");
    } catch {
      /*
       * Signed out, or nothing reachable. The operator's defaults still apply,
       * and this browser's own stored choice was applied before the first
       * paint — so somebody sees what they chose either way.
       */
    }
  });

  watchEffect(() => {
    theme.settle({ accent: branding.value.accent, palette: branding.value.palette }, mine.value);
  });
}
