/**
 * The pictures people on this server chose, by their address.
 *
 * One list for the whole application, fetched once when somebody signs in
 * and again after they change their own, rather than a request per face: a
 * conversation draws the same few people dozens of times, and most people
 * have no picture at all, which a request each would only discover by failing.
 * Anybody not in the list is drawn with their initials.
 *
 * People on other servers are not in it; their initials are shown.
 */
export function useAvatars() {
  const paths = useState<Record<string, string>>("avatars", () => ({}));
  const api = useApi();

  /** Reads the list again. Signed out, or unreachable, it is left as it was. */
  async function load() {
    try {
      const { avatars } = await api.get<{ avatars: { address: string; url: string }[] }>(
        "/api/avatars",
      );
      /*
       * By address, and by username too: everybody listed is on this server,
       * so a username names them as surely, and some lists carry only that.
       */
      paths.value = Object.fromEntries(
        avatars.flatMap((each) => [
          [each.address, each.url],
          [each.address.split("@")[0]!, each.url],
        ]),
      );
    } catch {
      // Initials, until it can be read.
    }
  }

  /**
   * Where somebody's picture is, as a server path, or empty for none. By their
   * address, or by username for somebody on this server.
   */
  function pathOf(address: string | undefined): string {
    return (address && paths.value[address]) || "";
  }

  /** Records this person's own new picture, or its removal, at once. */
  function setOwn(address: string, path: string) {
    const next = { ...paths.value };
    for (const key of [address, address.split("@")[0]!]) {
      if (path) next[key] = path;
      else delete next[key];
    }
    paths.value = next;
  }

  return { load, pathOf, setOwn };
}
