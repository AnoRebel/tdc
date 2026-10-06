export interface DocsVersion {
  /** The branch this version is built from, and the one its Studio commits to. */
  version: string;
  label: string;
  url: string;
}

/** The version being read, the others, and how to move between them. */
export function useDocsVersions() {
  const docs = useRuntimeConfig().public.docs as unknown as {
    version: string;
    versions: DocsVersion[];
    repository: string;
  };
  const route = useRoute();

  /*
   * Whether this deployment has Studio's credentials, so "Edit in Studio" is
   * offered only where signing in can work. Decided by the server from the
   * container's environment and carried to the browser in the page's payload.
   */
  const studio = useState("docs-studio", () =>
    import.meta.server ? Boolean(process.env.STUDIO_GITHUB_CLIENT_ID) : false,
  );

  const current = computed(
    () =>
      docs.versions.find((entry) => entry.version === docs.version) ?? {
        version: docs.version,
        label: docs.version,
        url: "",
      },
  );
  const stable = computed(() => docs.versions.find((entry) => entry.version === "main"));

  /**
   * Opens another version at the page being read, if it has that page, and at
   * its front page otherwise. Asking first, rather than landing on a "not
   * found", because guides are added on dev well before main has them.
   */
  async function switchTo(target: DocsVersion) {
    const path = route.path;
    let destination = target.url;
    try {
      const { exists } = await $fetch<{ exists: boolean }>("/api/page-exists", {
        baseURL: target.url,
        query: { path },
        timeout: 3000,
      });
      if (exists) destination = target.url + path + route.hash;
    } catch {
      // Unreachable or older than this check: its front page is still right.
    }
    window.location.assign(destination);
  }

  return { docs, current, stable, studio, switchTo };
}
