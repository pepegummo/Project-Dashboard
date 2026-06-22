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
}>(), { readonly: false });
const emit = defineEmits<{
  'layout-change': [layouts: Array<{ id: string; layout: WidgetLayout }>];
  'edit-widget': [widget: DashboardWidget];
  'remove-widget': [widgetId: string];
}>();

const gridRef = ref<HTMLElement | null>(null);
let grid: GridStack | null = null;
const mountedApps = new Map<string, App>();

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
    if (!grid) return;
    const items = grid.save(false) as Array<{ id?: string; x: number; y: number; w: number; h: number }>;
    const layouts = items
      .filter(item => item.id)
      .map(item => ({
        id: item.id!,
        layout: { x: item.x, y: item.y, w: item.w, h: item.h },
      }));
    emit('layout-change', layouts);
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
  el.appendChild(content);

  grid.el.appendChild(el);
  grid.makeWidget(el);

  // Mount widget app, sharing the parent's Pinia instance
  const app = createApp(WidgetWrapper, {
    widget,
    ...(props.readonly ? {} : {
      onEdit: () => emit('edit-widget', widget),
      onRemove: () => emit('remove-widget', widget.id),
    }),
  });
  if (pinia) app.use(pinia);
  app.mount(content);
  mountedApps.set(widget.id, app);
}

function getCurrentLayouts() {
  if (!grid) return [];
  const items = grid.save(false) as Array<{ id?: string; x: number; y: number; w: number; h: number }>;
  return items
    .filter(item => item.id)
    .map(item => ({ id: item.id!, layout: { x: item.x, y: item.y, w: item.w, h: item.h } }));
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

// Spotlight: when highlightedId changes, add animation class to the target cell
watch(
  () => props.highlightedId,
  (id) => {
    if (!id || !gridRef.value) return;
    const el = gridRef.value.querySelector(`[gs-id="${id}"]`) as HTMLElement | null;
    if (!el) return;
    el.classList.remove('widget-highlight');
    // Force reflow to restart animation if same widget is highlighted again
    void el.offsetWidth;
    el.classList.add('widget-highlight');
    setTimeout(() => el.classList.remove('widget-highlight'), 3200);
  },
);

// Deep watch handles additions, removals, and config edits
watch(
  () => props.widgets,
  async (newWidgets, oldWidgets) => {
    if (!grid) return;

    const newMap = new Map(newWidgets.map(w => [w.id, w]));
    const oldMap = new Map(oldWidgets.map(w => [w.id, w]));

    // ── Removals ────────────────────────────────────────────────────────────────
    for (const [id] of oldMap) {
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
  },
  { deep: true },
);
</script>

<template>
  <div ref="gridRef" class="grid-stack w-full" />
</template>
