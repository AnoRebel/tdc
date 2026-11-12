import { Events } from "@wailsio/runtime";

import {
  remoteControlKey,
  type ControlState,
} from "#layers/ui/app/composables/useRemoteControlHost";
import * as RemoteControl from "#bindings/remotecontrol";

/**
 * Remote control of a screen shared in a call, through Go — which is what
 * lets the desktop application, and not a browser, be driven by somebody
 * else in the call. Go keeps the rules; this only carries the calls across.
 */
export default defineNuxtPlugin({
  name: "tdc:remote-control",
  setup(nuxtApp) {
    nuxtApp.vueApp.provide(remoteControlKey, {
      capability: () => RemoteControl.Service.Capability(),
      beginShare: (width, height) => RemoteControl.Service.BeginShare(width, height),
      endShare: () => RemoteControl.Service.EndShare(),
      grant: (controller, name, notice) => RemoteControl.Service.Grant(controller, name, notice),
      revoke: () => RemoteControl.Service.Revoke(),
      apply: (controller, input) => RemoteControl.Service.Apply(controller, input),
      onChange(listener) {
        return Events.On("tdc:remote-control", (event: { data: ControlState }) =>
          listener(event.data),
        );
      },
    });
  },
});
