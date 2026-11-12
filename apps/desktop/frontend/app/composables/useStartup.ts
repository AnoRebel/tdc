import * as Shell from "#bindings/shell";

/**
 * Whether TDC opens when somebody logs in, and whether it opens to the tray.
 *
 * Two choices rather than one. Somebody may want it running quietly from
 * login — which is the point of a chat application being there at all — or
 * want a window when they open it themselves and nothing at login.
 *
 * Both are off for anybody who has not asked. An application that added itself
 * to a login, or opened invisibly, without being asked has taken a decision
 * about somebody's machine that is not its to take.
 */
export function useStartup() {
  const atLogin = useState("startup-at-login", () => false);
  const hidden = useState("startup-hidden", () => false);
  const problem = useState("startup-problem", () => "");

  async function refresh() {
    const settings = await Shell.StartupService.StartupSettings();
    atLogin.value = settings.atLogin;
    hidden.value = settings.hidden;
  }

  /**
   * Turns opening at login on or off.
   *
   * Reports a failure rather than swallowing it: a desktop with no autostart
   * directory cannot do this, and a switch that silently returns to off is a
   * bug report waiting to be written.
   */
  async function setAtLogin(wanted: boolean) {
    problem.value = "";

    try {
      await Shell.StartupService.SetOpensAtLogin(wanted);
      atLogin.value = wanted;
    } catch (error) {
      problem.value = error instanceof Error ? error.message : String(error);
      // Back to what the platform actually says, rather than what was asked.
      await refresh();
    }
  }

  async function setHidden(wanted: boolean) {
    await Shell.StartupService.SetStartsHidden(wanted);
    hidden.value = wanted;
  }

  return { atLogin, hidden, problem, refresh, setAtLogin, setHidden };
}
