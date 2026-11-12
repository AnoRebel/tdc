import { callSupportKey } from "#layers/ui/app/composables/useCallSupport";
import * as Shell from "#bindings/shell";

/**
 * What this machine lacks for calls, as only Go can tell — GStreamer's WebRTC
 * plugins on Linux — so the window can say why it cannot call rather than
 * offering a call that would fail.
 */
export default defineNuxtPlugin({
  name: "tdc:calls",
  setup(nuxtApp) {
    nuxtApp.vueApp.provide(callSupportKey, () => Shell.CallSupport.Missing());
  },
});
