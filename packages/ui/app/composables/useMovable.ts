import type { ShallowRef } from "vue";
import { useDraggable, useLocalStorage, useWindowSize } from "@vueuse/core";

/**
 * A small floating window somebody can put anywhere.
 *
 * Dragged by its grip, or moved from the keyboard with the arrow keys on the
 * grip (Shift for bigger steps); kept inside the window when the window
 * shrinks; and remembered on this device under the given name. Until it is
 * moved it sits where its default classes put it, which `corner` returns while
 * there is no remembered position.
 */
export function useMovable(
  surface: Readonly<ShallowRef<HTMLElement | null>>,
  grip: Readonly<ShallowRef<HTMLElement | null>>,
  name: string,
  defaultCorner: string,
) {
  const placed = useLocalStorage<{ x: number; y: number } | null>(`tdc.${name}.position`, null, {
    serializer: {
      read: (raw) => (raw ? JSON.parse(raw) : null),
      write: (value) => JSON.stringify(value),
    },
  });
  const { width, height } = useWindowSize();

  function clamped(x: number, y: number) {
    const box = surface.value?.getBoundingClientRect();
    const w = box?.width ?? 240;
    const h = box?.height ?? 48;
    return {
      x: Math.min(Math.max(8, x), Math.max(8, width.value - w - 8)),
      y: Math.min(Math.max(8, y), Math.max(8, height.value - h - 8)),
    };
  }

  const { isDragging } = useDraggable(surface, {
    handle: grip,
    preventDefault: true,
    onStart: () => {
      const box = surface.value?.getBoundingClientRect();
      if (box && !placed.value) placed.value = { x: box.left, y: box.top };
    },
    onMove: (position) => {
      placed.value = clamped(position.x, position.y);
    },
  });

  // A window made smaller must not leave it off-screen.
  watch([width, height], () => {
    if (placed.value) placed.value = clamped(placed.value.x, placed.value.y);
  });

  function nudge(event: KeyboardEvent) {
    const step = event.shiftKey ? 64 : 16;
    const moves: Record<string, [number, number]> = {
      ArrowLeft: [-step, 0],
      ArrowRight: [step, 0],
      ArrowUp: [0, -step],
      ArrowDown: [0, step],
    };
    const move = moves[event.key];
    if (!move) return;
    event.preventDefault();
    const box = surface.value?.getBoundingClientRect();
    const from = placed.value ?? { x: box?.left ?? 0, y: box?.top ?? 0 };
    placed.value = clamped(from.x + move[0], from.y + move[1]);
  }

  const style = computed(() =>
    placed.value ? { left: `${placed.value.x}px`, top: `${placed.value.y}px` } : undefined,
  );
  const corner = computed(() => (placed.value ? "" : defaultCorner));

  return { isDragging, nudge, style, corner };
}
