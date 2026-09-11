import type { MenuItem } from "#layers/ui/app/components/ContextMenu.vue";

/**
 * Opens a context menu by right-click or by long press.
 *
 * Both gestures are handled here rather than at each call site, because getting
 * long press right takes more care than it looks: a press that becomes a scroll
 * must not open a menu, and one that opens a menu must not also fire the click
 * underneath it.
 */

/** How long a press must be held before it counts as a long press. */
const longPressMs = 500;

/**
 * How far a finger may travel and still count as a press rather than a scroll.
 *
 * A finger never holds perfectly still, so zero would make long press
 * impossible; too generous and every scroll opens a menu.
 */
const moveToleranceP = 10;

export function useContextMenu() {
  const open = ref(false);
  const position = ref({ x: 0, y: 0 });
  const items = ref<MenuItem[]>([]);

  let timer: ReturnType<typeof setTimeout> | null = null;
  let startedAt = { x: 0, y: 0 };
  // Set when a long press fires, so the click it would otherwise produce can be
  // suppressed — without this, releasing after a long press also activates
  // whatever was underneath.
  let suppressNextClick = false;

  function show(x: number, y: number, menu: MenuItem[]) {
    position.value = { x, y };
    items.value = menu;
    open.value = true;
  }

  function close() {
    open.value = false;
  }

  function cancelTimer() {
    if (timer !== null) {
      clearTimeout(timer);
      timer = null;
    }
  }

  /** Handlers to spread onto whatever the menu belongs to. */
  function handlers(menu: () => MenuItem[]) {
    return {
      onContextmenu(event: MouseEvent) {
        event.preventDefault();
        show(event.clientX, event.clientY, menu());
      },

      onTouchstart(event: TouchEvent) {
        const touch = event.touches[0];
        if (!touch) return;

        startedAt = { x: touch.clientX, y: touch.clientY };
        suppressNextClick = false;

        timer = setTimeout(() => {
          suppressNextClick = true;
          // A short buzz confirms the press registered, which is the only
          // feedback a finger gets — there is no cursor to change shape.
          navigator.vibrate?.(10);
          show(startedAt.x, startedAt.y, menu());
        }, longPressMs);
      },

      onTouchmove(event: TouchEvent) {
        const touch = event.touches[0];
        if (!touch) return;

        // Moved far enough to be a scroll rather than a press.
        const travelled =
          Math.abs(touch.clientX - startedAt.x) + Math.abs(touch.clientY - startedAt.y);
        if (travelled > moveToleranceP) cancelTimer();
      },

      onTouchend() {
        cancelTimer();
      },

      onTouchcancel() {
        cancelTimer();
      },

      onClick(event: MouseEvent) {
        // The click that follows the release of a long press would otherwise
        // activate whatever the menu was opened on.
        if (suppressNextClick) {
          event.preventDefault();
          event.stopPropagation();
          suppressNextClick = false;
        }
      },
    };
  }

  onScopeDispose(cancelTimer);

  return { open, position, items, show, close, handlers };
}
