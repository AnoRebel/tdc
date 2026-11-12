// A named export in v14; the default export was removed.
import { VueDatePicker } from "@vuepic/vue-datepicker";

/**
 * Registers the date picker.
 *
 * Client-only: it reads the browser's locale and time zone to decide how to
 * present a date, which the server does not have and would guess wrongly —
 * rendering one date on the server and another in the browser.
 *
 * Its stylesheet is imported in the application's own CSS rather than here, so
 * the theme variables that override it load in a predictable order.
 */
export default defineNuxtPlugin((nuxtApp) => {
  nuxtApp.vueApp.component("VueDatePicker", VueDatePicker);
});
