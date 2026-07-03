<script setup lang="ts">
import { ref, onMounted, onUnmounted, watch, nextTick, toRef } from 'vue';
import { GridStack } from 'gridstack';
import 'gridstack/dist/gridstack.min.css';
import type { DashboardWidget, WidgetLayout } from '@/types';
import WidgetWrapper from '@/components/widgets/WidgetWrapper.vue';
import { createApp, type App } from 'vue';
import { getActivePinia } from 'pinia';
import 'gridstack/dist/gridstack-extra.min.css';

const props = withDefaults(defineProps<{
  widgets: DashboardWidget[];
  readonly?: boolean;
  highlightedId?: string;
  selectedIds?: string[];
  /** Sync widget.layout prop changes into mounted grid nodes (enables prop-driven layout restore). */
  syncLayout?: boolean;
}>(), { readonly: false, syncLayout: false });
const emit = defineEmits<{
  'layout-change': [layouts: Array<{ id: string; layout: WidgetLayout }>, programmatic: boolean];
  'edit-widget': [widget: DashboardWidget];
  'remove-widget': [widgetId: string];
  'select-widget': [widget: DashboardWidget];
}>();

const gridRef = ref<HTMLElement | null>(null);
let grid: GridStack | null = null;
const mountedApps = new Map<string, App>();

// GridStack fires 'change' on programmatic updates (grid.update / removeWidget compaction)
// too — this flag lets listeners tell those apart from user drags/resizes.
let programmatic = false;

// Fingerprint tracks non-layout props so we know when to remount a widget
const fingerprints = new Map<string, string>();

// Grab the active Pinia instance so child apps share stores
const pinia = getActivePinia();

function getFingerprint(w: DashboardWidget): string {
  return JSON.stringify({
    machineId: w.machineId,
    widgetType: w.widgetType,
    title: w.title,
    config: w.config,
  });
}

onMounted(async () => {
  if (!gridRef.value) return;

  grid = GridStack.init({
  column: 12,
  columnOpts: {
    breakpointForWindow: true,
    breakpoints: [{ w: 768, c: 1 }]
  },
  cellHeight: 80,
  margin: 8,
  animate: true,
  staticGrid: props.readonly,
  resizable: { handles: 'se' },
  handle: '.gs-drag-handle',
}, gridRef.value);

  for (const widget of props.widgets) {
    addWidgetToGrid(widget);
    fingerprints.set(widget.id, getFingerprint(widget));
  }

  grid.on('change', () => {
    if (!grid || !grid.engine || !grid.engine.nodes) return;
    const layouts = grid.engine.nodes
      .filter(node => node.id !== undefined && node.id !== null)
      .map(node => ({
        id: String(node.id),
        layout: { x: node.x ?? 0, y: node.y ?? 0, w: node.w ?? 6, h: node.h ?? 4 },
      }));
    emit('layout-change', layouts, programmatic);
  });
});

onUnmounted(() => {
  mountedApps.forEach(app => app.unmount());
  mountedApps.clear();
  grid?.destroy(false);
});

function addWidgetToGrid(widget: DashboardWidget) {
  if (!grid) return;

  const el = document.createElement('div');
  el.setAttribute('gs-id', widget.id);
  el.setAttribute('gs-x', String(widget.layout.x));
  el.setAttribute('gs-y', String(widget.layout.y));
  el.setAttribute('gs-w', String(widget.layout.w));
  el.setAttribute('gs-h', String(widget.layout.h));
  el.classList.add('grid-stack-item');

  const content = document.createElement('div');
  content.classList.add('grid-stack-item-content');
  if (props.selectedIds?.includes(widget.id)) el.classList.add('widget-selected');
  el.appendChild(content);

  grid.el.appendChild(el);
  grid.makeWidget(el);

  // Mount widget app, sharing the parent's Pinia instance
  const app = createApp(WidgetWrapper, {
    widget,
    ...(props.readonly ? {} : {
      onEdit: () => emit('edit-widget', widget),
      onRemove: () => emit('remove-widget', widget.id),
      onSelect: () => emit('select-widget', widget),
    }),
  });
  if (pinia) app.use(pinia);
  app.mount(content);
  mountedApps.set(widget.id, app);
}

function getCurrentLayouts() {
  if (!grid || !grid.engine || !grid.engine.nodes) return [];
  return grid.engine.nodes
    .filter(node => node.id !== undefined && node.id !== null)
    .map(node => ({
      id: String(node.id),
      layout: { x: node.x ?? 0, y: node.y ?? 0, w: node.w ?? 6, h: node.h ?? 4 },
    }));
}

defineExpose({ getCurrentLayouts });

function removeWidgetFromGrid(widgetId: string) {
  // 1. Unmount Vue app first (while DOM still exists)
  const app = mountedApps.get(widgetId);
  if (app) {
    app.unmount();
    mountedApps.delete(widgetId);
  }
  fingerprints.delete(widgetId);

  // 2. Remove from GridStack (also removes DOM element)
  if (grid && gridRef.value) {
    const el = gridRef.value.querySelector(`[gs-id="${widgetId}"]`) as HTMLElement | null;
    if (el) {
      grid.removeWidget(el, true);
    }
  }
}

// Spotlight: when highlightedId changes, flash the target cell.
// A mentioned widget already carries a persistent .widget-selected ring that looks
// identical to .widget-highlight, so just adding the highlight class on top is a
// no-op — nothing visibly changes. Strip both classes first so re-adding the
// highlight is a real state transition, then restore selection from live props.
watch(
  () => props.highlightedId,
  (id) => {
    if (!id || !gridRef.value) return;
    const el = gridRef.value.querySelector(`[gs-id="${id}"]`) as HTMLElement | null;
    if (!el) return;
    el.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
    el.classList.remove('widget-highlight', 'widget-selected');
    void el.offsetWidth; // force reflow so the removal is applied before re-adding
    el.classList.add('widget-highlight');
    setTimeout(() => {
      el.classList.remove('widget-highlight');
      if (props.selectedIds?.includes(id)) el.classList.add('widget-selected');
    }, 3200);
  },
);

// Selection: toggle a persistent ring on the selected cells
watch(
  () => props.selectedIds,
  (ids) => {
    if (!gridRef.value) return;
    const set = new Set(ids ?? []);
    gridRef.value.querySelectorAll('[gs-id]').forEach((el) => {
      const id = el.getAttribute('gs-id');
      el.classList.toggle('widget-selected', !!id && set.has(id));
    });
  },
  { deep: true },
);

// Deep watch handles additions, removals, and config edits
watch(
  () => props.widgets,
  async (newWidgets, oldWidgets) => {
    if (!grid) return;
    programmatic = true;

    const newMap = new Map(newWidgets.map(w => [w.id, w]));

    // ── Removals ────────────────────────────────────────────────────────────────
    // Diff against mountedApps (ground truth of what's rendered) not oldWidgets —
    // Vue 3's deep watcher passes the same array reference for both args when the
    // source computed returns a freshly-assigned array, making oldWidgets unusable.
    for (const [id] of mountedApps) {
      if (!newMap.has(id)) {
        removeWidgetFromGrid(id);
      }
    }

    // ── Additions & config-change remounts ───────────────────────────────────
    await nextTick();
    for (const widget of newWidgets) {
      if (!mountedApps.has(widget.id)) {
        // Brand new widget
        addWidgetToGrid(widget);
        fingerprints.set(widget.id, getFingerprint(widget));
      } else {
        // Already mounted — remount only if non-layout props changed
        const fp = getFingerprint(widget);
        if (fingerprints.get(widget.id) !== fp) {
          removeWidgetFromGrid(widget.id);
          await nextTick();
          addWidgetToGrid(widget);
          fingerprints.set(widget.id, fp);
        }
      }
    }

    // ── Layout sync (opt-in) ──────────────────────────────────────────────
    // Push widget.layout prop changes into mounted nodes so restoring layouts
    // in reactive state (undo/redo) visibly moves the grid.
    if (props.syncLayout) {
      const updates: Array<[HTMLElement, WidgetLayout]> = [];
      for (const widget of newWidgets) {
        const node = grid.engine?.nodes?.find(n => String(n.id) === widget.id);
        if (!node) continue;
        const l = widget.layout;
        if (node.x !== l.x || node.y !== l.y || node.w !== l.w || node.h !== l.h) {
          const el = gridRef.value?.querySelector(`[gs-id="${widget.id}"]`) as HTMLElement | null;
          if (el) updates.push([el, l]);
        }
      }
      if (updates.length) {
        grid.batchUpdate();
        for (const [el, l] of updates) grid.update(el, { ...l });
        grid.batchUpdate(false);
      }
    }

    setTimeout(() => { programmatic = false; });
  },
  { deep: true },
);
</script>

<template>
  <div ref="gridRef" class="grid-stack w-full" />
</template>
