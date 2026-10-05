<script lang="ts">
/**
 * The settings only one kind of device has, on the shared settings page.
 *
 * Nothing here, deliberately. Each client replaces this component with its own
 * of the same name — an application's components win over a layer's — because
 * what belongs here is exactly what the two disagree about: the web offers
 * notifications through its service worker, which a desktop webview does not
 * have, and the desktop offers its window, its updates and its servers, which a
 * browser tab has no use for. A client that adds nothing gets this, and the
 * page simply has one section fewer.
 */
export default defineComponent({
  name: "DeviceSettings",
  render: () => null,
});
</script>
