import { shallowRef, computed, onMounted, onUnmounted } from 'vue';

export interface UndoHistoryOptions<T> {
  /** Perform the inverse of the entry; return the entry to place on the redo stack. */
  applyUndo: (entry: T) => T | Promise<T>;
  /** Perform the entry forward; return the entry to place on the undo stack. */
  applyRedo: (entry: T) => T | Promise<T>;
  limit?: number;
  /** Called when apply fails; the failed entry is pushed back onto its stack. */
  onError?: (err: unknown) => void;
}

function isTypingTarget(e: KeyboardEvent): boolean {
  const el = e.target as HTMLElement | null;
  if (!el) return false;
  return ['INPUT', 'TEXTAREA', 'SELECT'].includes(el.tagName) || el.isContentEditable;
}

export function useUndoHistory<T>(opts: UndoHistoryOptions<T>) {
  const limit = opts.limit ?? 50;
  const undoStack = shallowRef<T[]>([]);
  const redoStack = shallowRef<T[]>([]);
  const busy = shallowRef(false);

  const canUndo = computed(() => undoStack.value.length > 0 && !busy.value);
  const canRedo = computed(() => redoStack.value.length > 0 && !busy.value);

  function push(entry: T) {
    undoStack.value = [...undoStack.value, entry].slice(-limit);
    redoStack.value = [];
  }

  function clear() {
    undoStack.value = [];
    redoStack.value = [];
  }

  async function run(from: typeof undoStack, to: typeof redoStack, apply: (e: T) => T | Promise<T>) {
    if (busy.value || from.value.length === 0) return;
    busy.value = true;
    const entry = from.value[from.value.length - 1];
    from.value = from.value.slice(0, -1);
    try {
      const inverse = await apply(entry);
      to.value = [...to.value, inverse];
    } catch (err) {
      from.value = [...from.value, entry]; // push back so the user can retry
      opts.onError?.(err);
    } finally {
      busy.value = false;
    }
  }

  const undo = () => run(undoStack, redoStack, opts.applyUndo);
  const redo = () => run(redoStack, undoStack, opts.applyRedo);

  function onKeydown(e: KeyboardEvent) {
    if (!(e.ctrlKey || e.metaKey) || isTypingTarget(e)) return;
    const key = e.key.toLowerCase();
    if (key === 'z' && !e.shiftKey) {
      e.preventDefault();
      undo();
    } else if (key === 'y' || (key === 'z' && e.shiftKey)) {
      e.preventDefault();
      redo();
    }
  }

  onMounted(() => document.addEventListener('keydown', onKeydown));
  onUnmounted(() => document.removeEventListener('keydown', onKeydown));

  return { canUndo, canRedo, busy, undoStack, redoStack, push, undo, redo, clear };
}
