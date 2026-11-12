/**
 * What every page tells search engines and link previews beyond its own title
 * and description.
 *
 * The canonical address is always the stable site's: the beta and development
 * sites are copies of it for the work in progress, are not indexed (site.indexable
 * in nuxt.config.ts), and name the stable page as the one to count. Where the
 * stable site lives comes from NUXT_PUBLIC_DOCS_VERSIONS at runtime, like the
 * version switcher, so moving it needs no rebuild.
 *
 * The preview picture is one static image (public/og.png). The layer draws one
 * per page only while prerendering, which this site does not do.
 */
export function useDocsSeo() {
  const route = useRoute();
  const { current, stable } = useDocsVersions();
  const origin = useRequestURL().origin;

  const base = (url: string | undefined) => (url || origin).replace(/\/+$/, "");
  const canonical = computed(() => base(stable.value?.url ?? current.value.url) + route.path);
  const image = `${base(current.value.url)}/og.png`;

  useHead({ link: [{ rel: "canonical", href: canonical }] });
  useSeoMeta({
    ogUrl: canonical,
    ogImage: image,
    ogImageWidth: 1200,
    ogImageHeight: 630,
    ogImageType: "image/png",
    ogImageAlt: "TDC documentation",
    twitterImage: image,
    twitterImageAlt: "TDC documentation",
  });
}
