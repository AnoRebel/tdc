<script setup lang="ts">
/**
 * Everything about calls that has to be there wherever somebody is.
 *
 * Loaded only on servers that offer calls. Holds the one live connection calls
 * need — rings arrive on it, and a call's negotiation travels on it — and shows
 * whoever is ringing and the call window. Pages only offer to start or join.
 */

const c = useCall();

const live = useLiveConnection({
  onEvent: () => {},
  onResync: () => {},
  onCallState: (state) => c.onState(state),
  onCallSignal: (signal) => void c.onSignal(signal),
  onCallRing: (ring) => c.onRing(ring),
  onCallRingEnded: (call) => {
    c.onRingEnded(call);
    // For whatever else showed the ring, such as a browser's notification.
    window.dispatchEvent(new CustomEvent("tdc:ring-ended", { detail: call }));
  },
  onCallDeclined: (declined) => c.onDeclined(declined),
  onCallLobby: (lobby) => c.onLobby(lobby),
  onCallAdmitted: (use) => void c.onAdmitted(use),
  onCallTurnedAway: (use) => c.onTurnedAway(use),
  onCallReaction: (reaction) => c.onReaction(reaction),
});
c.setSignaller((call, to, body) => live.signal(call, to, body));

// Every call ringing at once, in one window: a stack of windows would be worse.
const ringing = computed(() => c.incoming.value);
</script>

<template>
  <CallIncoming v-if="ringing.length" :rings="ringing" />
  <CallWindow />
</template>
