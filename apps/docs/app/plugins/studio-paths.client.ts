import { redirectStudioRequest } from "../../lib/studio-paths";

/**
 * Sends Studio's commits to docs/ rather than to a copy beside it; see
 * lib/studio-paths.ts for why this has to happen on the way out.
 *
 * Installed before Studio's editor loads (it loads only once somebody signs
 * in), because the editor keeps a reference to fetch from the moment it
 * starts.
 */
export default defineNuxtPlugin(() => {
  const { owner, repo } = useRuntimeConfig().public.studio.repository as {
    owner: string;
    repo: string;
  };
  const repository = `${owner}/${repo}`;
  const original = window.fetch.bind(window);

  window.fetch = (input, init) => {
    if (typeof input !== "string" && !(input instanceof URL)) return original(input, init);
    const { url, body } = redirectStudioRequest(String(input), init?.body, repository);
    return original(url, init ? { ...init, body: body as BodyInit } : init);
  };
});
