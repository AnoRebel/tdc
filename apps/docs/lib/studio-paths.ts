/**
 * Where Studio's commits land.
 *
 * Studio (nuxt-studio 1.x) writes every page it edits to
 * `<rootDir>/content/<file>` and ignores the collection's own directory, so on
 * its own it would commit an edited guide to apps/docs/content/hosting.md — a
 * second copy beside the real one in docs/, which is exactly the drift this
 * site exists to avoid (nuxt-content/nuxt-studio#313 is the request to fix
 * that upstream).
 *
 * Studio talks to GitHub from the editor's browser, so the requests are where
 * the path can be put right: reading a file and building the commit's tree
 * both name it, and both are rewritten from where Studio thinks the guide is
 * to where it is. Everything else passes through untouched.
 */

/** Where Studio believes the guides are, given repository.rootDir "apps/docs". */
export const STUDIO_CONTENT_DIR = "apps/docs/content/";
/** Where they are. */
export const GUIDES_DIR = "docs/";

const GITHUB_API = "https://api.github.com";

function moved(path: string): string {
  return path.startsWith(STUDIO_CONTENT_DIR)
    ? GUIDES_DIR + path.slice(STUDIO_CONTENT_DIR.length)
    : path;
}

/**
 * The request Studio should have made, for one it is about to make.
 *
 * `repository` is "owner/name". Only that repository's contents and tree
 * endpoints are touched.
 */
export function redirectStudioRequest(
  url: string,
  body: unknown,
  repository: string,
): { url: string; body: unknown } {
  const base = `${GITHUB_API}/repos/${repository}`;

  const contents = `${base}/contents/`;
  if (url.startsWith(contents)) {
    return { url: contents + moved(url.slice(contents.length)), body };
  }

  if (url === `${base}/git/trees` && typeof body === "string") {
    const tree = JSON.parse(body) as { tree?: { path: string }[] };
    for (const entry of tree.tree ?? []) entry.path = moved(entry.path);
    return { url, body: JSON.stringify(tree) };
  }

  return { url, body };
}
