import type { Component } from "vue";
import {
  Flag,
  Keyboard,
  KeyboardOff,
  LogOut,
  Moon,
  Plus,
  ScrollText,
  Settings,
  SlidersHorizontal,
  Sun,
  Users,
  Wind,
} from "lucide-vue-next";

/** Something the palette can do, rather than somewhere it can go. */
export interface Command {
  id: string;
  /** What it does, as the row reads. */
  label: string;
  icon: Component;
  /** Extra words matched against, so "dark" finds "Appearance". */
  keywords?: string;
  run: () => void | Promise<void>;
}

/**
 * The commands this person may actually run.
 *
 * Filtered by permission and by what the server offers, not merely hidden in a
 * stylesheet — a palette listing an action that will be refused teaches people
 * the list cannot be trusted, and they stop reading it. What is absent here is
 * absent because this person genuinely cannot do it.
 *
 * The server enforces all of this regardless. This decides what is worth
 * showing, never what is allowed.
 */
export function useCommands(options: { openShortcuts: () => void; createChannel: () => void }) {
  const { $localeRoute: localeRoute, $ts } = useI18n();
  // The motion and keyboard preferences, which a dialog in the layout shows.
  const showingInterface = useState("interface.open", () => false);
  const { shortcuts: shortcutsOn } = useInterfacePreferences();
  const { can, signOut } = useAuth();
  const router = useRouter();
  const theme = useTheme();
  const { offers } = useServerInfo();

  const commands = computed<Command[]>(() => {
    const available: Command[] = [];

    // Only where this person may actually make one. A palette offering an
    // action that will be refused is one that has misled them.
    if (can("channel.create")) {
      available.push({
        id: "new-channel",
        label: "New channel",
        icon: Plus,
        keywords: "create make room",
        run: options.createChannel,
      });
    }

    available.push(
      {
        id: "settings",
        label: "Settings",
        icon: Settings,
        keywords: "account status devices privacy blocked",
        run: () => void router.push(localeRoute({ name: "settings" })),
      },
      {
        id: "shortcuts",
        label: "Keyboard shortcuts",
        icon: Keyboard,
        keywords: "keys help",
        run: options.openShortcuts,
      },
      {
        id: "interface",
        label: $ts("interface.command"),
        icon: Wind,
        keywords: "motion animation reduce density compact keyboard shortcuts",
        run: () => {
          showingInterface.value = true;
        },
      },
      {
        id: "shortcuts-toggle",
        label: shortcutsOn.value
          ? $ts("interface.turnShortcutsOff")
          : $ts("interface.turnShortcutsOn"),
        icon: shortcutsOn.value ? KeyboardOff : Keyboard,
        keywords: "keys keyboard disable enable",
        run: () => {
          shortcutsOn.value = !shortcutsOn.value;
        },
      },
      {
        id: "theme-dark",
        label: "Dark appearance",
        icon: Moon,
        keywords: "theme night colour color",
        run: () => theme.set("dark"),
      },
      {
        id: "theme-light",
        label: "Light appearance",
        icon: Sun,
        keywords: "theme day colour color",
        run: () => theme.set("light"),
      },
    );

    if (can("moderation.review")) {
      available.push({
        id: "reports",
        label: "Reports",
        icon: Flag,
        keywords: "moderation queue abuse",
        run: () => void router.push(localeRoute({ name: "reports" })),
      });
    }

    if (can("user.suspend") || can("user.invite")) {
      available.push({
        id: "people",
        label: "People",
        icon: Users,
        keywords: "members invite suspend accounts",
        run: () => void router.push(localeRoute({ name: "people" })),
      });
    }

    if (can("server.configure")) {
      available.push({
        id: "admin",
        label: "Server settings",
        icon: SlidersHorizontal,
        keywords: "administration limits policy filter branding",
        run: () => void router.push(localeRoute({ name: "admin" })),
      });
    }

    if (can("server.audit.read")) {
      available.push({
        id: "audit",
        label: "Audit log",
        icon: ScrollText,
        keywords: "history record who did",
        run: () => void router.push(localeRoute({ name: "audit" })),
      });
    }

    /*
     * A capability the operator turned off is not offered.
     *
     * Distinct from a permission: this person may be entitled to invite
     * people, on a server that is not issuing invitations at all. Both are
     * reasons the command should not appear, and neither is the other.
     */
    if (can("user.invite") && offers("invitations")) {
      available.push({
        id: "invite",
        label: "Invite somebody",
        icon: Users,
        keywords: "invitation link code join",
        run: () => void router.push(localeRoute({ name: "people" })),
      });
    }

    available.push({
      id: "sign-out",
      label: "Sign out",
      icon: LogOut,
      keywords: "leave log out",
      run: async () => {
        await signOut();
        await router.push(localeRoute({ name: "signin" }));
      },
    });

    return available;
  });

  /**
   * The commands matching what somebody has typed.
   *
   * Matched against the label and its keywords, so "dark" finds the appearance
   * command whose label says nothing about darkness. Empty text offers
   * everything, which is what makes the palette browsable rather than only
   * searchable.
   */
  function matching(query: string): Command[] {
    const text = query.trim().toLowerCase();
    if (!text) return commands.value;

    return commands.value.filter((command) =>
      `${command.label} ${command.keywords ?? ""}`.toLowerCase().includes(text),
    );
  }

  return { commands, matching };
}
