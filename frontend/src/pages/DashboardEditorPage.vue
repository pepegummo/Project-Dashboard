<script setup lang="ts">
import { ref, onMounted, computed } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useDashboardStore } from '@/stores/dashboard.store';
import { useMachineStore } from '@/stores/machine.store';
import { useAuthStore } from '@/stores/auth.store';
import { useWidgetViewStateStore } from '@/stores/widget-view-state.store';
import { useLedExport } from '@/composables/useLedExport';
import { useToast } from '@/composables/useToast';
import { useUndoHistory } from '@/composables/useUndoHistory';
import { Save, Plus, ArrowLeft, Loader2, LayoutGrid, Monitor, ExternalLink, Upload, Trash2, Edit2, Undo2, Redo2 } from 'lucide-vue-next';
import GridStackCanvas from '@/components/dashboard/GridStackCanvas.vue';
import WidgetToolbox from '@/components/dashboard/WidgetToolbox.vue';
import WidgetConfigModal from '@/components/dashboard/WidgetConfigModal.vue';
import type { DashboardWidget, WidgetType, WidgetLayout, WidgetConfig } from '@/types';

const route = useRoute();
const router = useRouter();
const dashboardStore = useDashboardStore();
const machineStore = useMachineStore();
const authStore = useAuthStore();
const widgetViewStateStore = useWidgetViewStateStore();

// ── LED Export ─────────────────────────────────────────────────────────────────
const { exportLedLink, openLedPreview, exportLabel, copied } = useLedExport();
const toast = useToast();

const showToolbox = ref(false);
const showConfigModal = ref(false);
const editingWidget = ref<DashboardWidget | null>(null);
const saving = ref(false);
const gridCanvasRef = ref<InstanceType<typeof GridStackCanvas> | null>(null);

const dashboardId = computed(() => route.params.id as string);

// ── Undo/redo (command pattern: add/remove/update replay inverse API calls,
//    layout changes stay client-buffered until Save) ──────────────────────────
type EditorCmd =
  | { type: 'add'; widget: DashboardWidget }
  | { type: 'remove'; widget: DashboardWidget; layouts: Record<string, WidgetLayout> }
  | { type: 'update'; widgetId: string; before: Partial<DashboardWidget>; after: Partial<DashboardWidget> }
  | { type: 'layout'; before: Record<string, WidgetLayout>; after: Record<string, WidgetLayout> };

const clone = <T>(v: T): T => JSON.parse(JSON.stringify(v));
const sameLayout = (a: WidgetLayout, b: WidgetLayout) =>
  a.x === b.x && a.y === b.y && a.w === b.w && a.h === b.h;

function widgetPayload(w: DashboardWidget) {
  return { machineId: w.machineId, widgetType: w.widgetType, title: w.title, layout: w.layout, config: w.config };
}

function applyLayoutsToStore(map: Record<string, WidgetLayout>) {
  for (const w of dashboardStore.widgets) {
    if (map[w.id] && !sameLayout(w.layout, map[w.id])) w.layout = { ...map[w.id] };
  }
}

// Undoing a delete / redoing an add re-creates the widget with a NEW server id;
// rewrite every stack entry that still references the old one.
function remapId(oldId: string, newId: string, inFlight: EditorCmd) {
  for (const c of [...history.undoStack.value, ...history.redoStack.value, inFlight]) {
    if ((c.type === 'add' || c.type === 'remove') && c.widget.id === oldId) c.widget = { ...c.widget, id: newId };
    if (c.type === 'update' && c.widgetId === oldId) c.widgetId = newId;
    const maps = c.type === 'remove' ? [c.layouts] : c.type === 'layout' ? [c.before, c.after] : [];
    for (const m of maps) if (m[oldId]) { m[newId] = m[oldId]; delete m[oldId]; }
  }
}

async function applyCmd(cmd: EditorCmd, dir: 'undo' | 'redo'): Promise<EditorCmd> {
  switch (cmd.type) {
    case 'add':
      if (dir === 'undo') await dashboardStore.removeWidget(cmd.widget.id);
      else {
        const w = await dashboardStore.addWidget(widgetPayload(cmd.widget));
        remapId(cmd.widget.id, w.id, cmd);
      }
      break;
    case 'remove':
      if (dir === 'undo') {
        const w = await dashboardStore.addWidget(widgetPayload(cmd.widget));
        remapId(cmd.widget.id, w.id, cmd);
        applyLayoutsToStore(cmd.layouts); // restore neighbors shifted by float compaction
      } else {
        await dashboardStore.removeWidget(cmd.widget.id);
      }
      break;
    case 'update':
      await dashboardStore.updateWidget(cmd.widgetId, dir === 'undo' ? cmd.before : cmd.after);
      break;
    case 'layout':
      applyLayoutsToStore(dir === 'undo' ? cmd.before : cmd.after); // no API call — persisted on Save
      break;
  }
  return cmd;
}

const history = useUndoHistory<EditorCmd>({
  applyUndo: c => applyCmd(c, 'undo'),
  applyRedo: c => applyCmd(c, 'redo'),
  onError: () => toast.show('Undo failed', 'error'),
});
const { canUndo, canRedo } = history;

function onLayoutChange(layouts: Array<{ id: string; layout: WidgetLayout }>, programmatic: boolean) {
  const before: Record<string, WidgetLayout> = {};
  const after: Record<string, WidgetLayout> = {};
  for (const { id, layout } of layouts) {
    const w = dashboardStore.widgets.find(x => x.id === id);
    if (!w || sameLayout(w.layout, layout)) continue;
    before[id] = { ...w.layout };
    after[id] = { ...layout };
    w.layout = { ...layout }; // mirror grid → store (client-side only)
  }
  if (!programmatic && Object.keys(after).length) history.push({ type: 'layout', before, after });
}

onMounted(async () => {
  await Promise.all([
    dashboardStore.fetchDashboard(dashboardId.value),
    machineStore.fetchMachines(),
  ]);
});

async function onRename() {
  const currentName = dashboardStore.currentDashboard?.name || '';
  const newName = prompt('Rename dashboard:', currentName);
  if (newName === null) return;
  const trimmed = newName.trim();
  if (!trimmed) {
    toast.show('Dashboard name cannot be empty', 'error');
    return;
  }
  try {
    await dashboardStore.updateDashboard(dashboardId.value, { name: trimmed });
    toast.show('Dashboard renamed');
  } catch (e: any) {
    toast.show(e?.message || 'Failed to rename dashboard', 'error');
  }
}

async function onDelete() {
  if (!confirm('Are you sure you want to delete this dashboard? This cannot be undone.')) return;
  try {
    await dashboardStore.deleteDashboard(dashboardId.value);
    toast.show('Dashboard deleted');
    router.push('/dashboards');
  } catch (e: any) {
    toast.show(e?.message || 'Failed to delete dashboard', 'error');
  }
}

async function onSave() {
  saving.value = true;
  try {
    // Save current widget positions
    const layouts = gridCanvasRef.value?.getCurrentLayouts() ?? [];
    if (layouts.length) await dashboardStore.saveLayout(layouts);

    // Save datetime ranges for all mounted line-chart widgets
    const dtStates = widgetViewStateStore.datetimeStates;
    await Promise.all(
      Object.entries(dtStates).map(([widgetId, { startDateTime, endDateTime }]) => {
        const widget = dashboardStore.widgets.find(w => w.id === widgetId);
        if (!widget) return;
        return dashboardStore.updateWidget(widgetId, {
          config: { ...widget.config, startDateTime, endDateTime },
        });
      }),
    );
    toast.show('Dashboard saved');
  } catch {
    toast.show('Failed to save', 'error');
  } finally {
    saving.value = false;
  }
}

function exportDashboard() {
  const dashboard = dashboardStore.currentDashboard;
  if (!dashboard) return;

  const exportData = {
    orgId: authStore.activeOrgId,
    name: dashboard.name,
    description: dashboard.description ?? '',
    tags: dashboard.tags ?? [],
    widgets: dashboardStore.widgets.map(w => ({
      widgetType: w.widgetType,
      title: w.title,
      machineId: w.machineId,
      layout: w.layout,
      config: w.config,
    })),
  };

  const blob = new Blob([JSON.stringify(exportData, null, 2)], { type: 'application/json' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = `${dashboard.name.replace(/[^a-z0-9]/gi, '_').toLowerCase()}.json`;
  a.click();
  URL.revokeObjectURL(url);
}

async function onAddWidget(type: WidgetType) {
  showToolbox.value = false;
  // Open config modal for the chosen widget type
  editingWidget.value = {
    id: '',
    dashboardId: dashboardId.value,
    widgetType: type,
    layout: { x: 0, y: 9999, w: 6, h: 4 },
    config: {},
    order: 0,
  };
  showConfigModal.value = true;
}

async function onSaveWidget(widget: { machineId?: string; widgetType: WidgetType; title?: string; config: WidgetConfig; layout: WidgetLayout }) {
  if (!editingWidget.value) return;
  try {
    if (editingWidget.value.id) {
      // Update existing
      const id = editingWidget.value.id;
      const cur = dashboardStore.widgets.find(w => w.id === id);
      const before = cur
        ? clone({ widgetType: cur.widgetType, machineId: cur.machineId, title: cur.title, config: cur.config })
        : null;
      const after = {
        widgetType: widget.widgetType,
        machineId: widget.machineId,
        title: widget.title,
        config: widget.config,
      };
      await dashboardStore.updateWidget(id, after);
      if (before) history.push({ type: 'update', widgetId: id, before, after: clone(after) });
    } else {
      // Add new
      const w = await dashboardStore.addWidget(widget);
      history.push({ type: 'add', widget: clone(w) });
    }
  } catch (e: any) {
    toast.show(e?.message ?? 'Could not save widget', 'error');
    return;
  } finally {
    showConfigModal.value = false;
    editingWidget.value = null;
  }
}

function onEditWidget(widget: DashboardWidget) {
  editingWidget.value = widget;
  showConfigModal.value = true;
}

async function onRemoveWidget(widgetId: string) {
  if (!confirm('Remove this widget?')) return;
  const cur = dashboardStore.widgets.find(w => w.id === widgetId);
  const layouts = Object.fromEntries(dashboardStore.widgets.map(w => [w.id, { ...w.layout }]));
  await dashboardStore.removeWidget(widgetId);
  if (cur) history.push({ type: 'remove', widget: clone(cur), layouts });
}
</script>

<template>
  <div class="flex flex-col h-full">
    <!-- Toolbar -->
    <div class="flex items-center justify-between mb-4 gap-3 flex-shrink-0">
      <div class="flex items-center gap-3">
        <button class="btn-ghost btn-icon" @click="router.back()">
          <ArrowLeft class="w-4 h-4" />
        </button>
        <div>
          <div class="flex items-center gap-2">
            <h1 class="text-lg font-bold text-white leading-none">
              {{ dashboardStore.currentDashboard?.name ?? 'Dashboard' }}
            </h1>
            <button
              class="p-1 rounded hover:bg-surface-300 text-gray-400 hover:text-white transition-colors"
              title="Rename dashboard"
              @click="onRename"
            >
              <Edit2 class="w-3.5 h-3.5" />
            </button>
            <button
              class="p-1 rounded hover:bg-red-500/20 text-gray-400 hover:text-red-400 transition-colors"
              title="Delete dashboard"
              @click="onDelete"
            >
              <Trash2 class="w-3.5 h-3.5" />
            </button>
          </div>
          <p class="text-xs text-gray-500 mt-1">
            {{ dashboardStore.widgets.length }} widgets
          </p>
        </div>
      </div>

      <div class="flex items-center gap-2">
        <button
          class="btn-ghost btn-icon"
          :disabled="!canUndo"
          title="Undo (Ctrl+Z)"
          @click="history.undo()"
        >
          <Undo2 class="w-4 h-4" />
        </button>
        <button
          class="btn-ghost btn-icon"
          :disabled="!canRedo"
          title="Redo (Ctrl+Y)"
          @click="history.redo()"
        >
          <Redo2 class="w-4 h-4" />
        </button>

        <button class="btn-secondary" @click="showToolbox = !showToolbox">
          <Plus class="w-4 h-4" />
          Add Widget
        </button>

        <button
          class="btn-secondary"
          :disabled="!dashboardStore.widgets.length"
          title="Export dashboard config as JSON"
          @click="exportDashboard"
        >
          <Upload class="w-4 h-4" />
          Export
        </button>

        <!-- ── LED Export button group ──────────────────────────────────── -->
        <div class="flex items-center rounded-lg overflow-hidden border border-violet-500/25 bg-violet-500/8">

          <!-- Copy link -->
          <button
            class="led-export-btn group"
            :class="copied ? 'led-export-btn--copied' : 'led-export-btn--default'"
            :disabled="!dashboardStore.widgets.length"
            :title="dashboardStore.widgets.length ? 'Copy LED kiosk URL to clipboard' : 'Add widgets first'"
            @click="exportLedLink(dashboardStore.widgets)"
          >
            <!-- Icon: checkmark when copied, monitor otherwise -->
            <Transition name="icon-swap" mode="out-in">
              <svg
                v-if="copied"
                key="check"
                class="w-4 h-4 flex-shrink-0 text-emerald-400"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
                stroke-width="2.5"
              >
                <path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />
              </svg>
              <Monitor v-else key="monitor" class="w-4 h-4 flex-shrink-0" />
            </Transition>

            <!-- Label -->
            <Transition name="label-swap" mode="out-in">
              <span :key="exportLabel">{{ exportLabel }}</span>
            </Transition>
          </button>

          <!-- Divider -->
          <div class="w-px self-stretch bg-violet-500/20" />

          <!-- Open in new tab (preview) -->
          <button
            class="led-export-btn led-export-btn--default px-2"
            :disabled="!dashboardStore.widgets.length"
            title="Open LED kiosk in a new tab"
            @click="openLedPreview(dashboardStore.widgets)"
          >
            <ExternalLink class="w-3.5 h-3.5" />
          </button>
        </div>
        <!-- ────────────────────────────────────────────────────────────── -->

        <button
          class="btn-primary"
          :disabled="saving"
          @click="onSave"
        >
          <Loader2 v-if="saving" class="w-4 h-4 animate-spin" />
          <Save v-else class="w-4 h-4" />
          {{ saving ? 'Saving…' : 'Save' }}
        </button>
      </div>
    </div>

    <!-- Main area -->
    <div class="flex gap-4 flex-1 min-h-0">
      <!-- Toolbox -->
      <WidgetToolbox
        v-if="showToolbox"
        @select="onAddWidget"
        @close="showToolbox = false"
      />

      <!-- Grid canvas -->
      <div class="flex-1 overflow-auto">
        <div v-if="dashboardStore.loading" class="flex items-center justify-center h-64">
          <div class="spinner" />
        </div>
        <div v-else-if="!dashboardStore.widgets.length" class="flex flex-col items-center justify-center h-64 text-center border-2 border-dashed border-white/10 rounded-xl">
          <LayoutGrid class="w-10 h-10 text-gray-600 mb-3" />
          <p class="text-gray-400 font-medium">Empty dashboard</p>
          <p class="text-gray-600 text-sm mt-1">Click "Add Widget" to get started</p>
        </div>
        <GridStackCanvas
          v-else
          ref="gridCanvasRef"
          :widgets="dashboardStore.widgets"
          :sync-layout="true"
          @edit-widget="onEditWidget"
          @remove-widget="onRemoveWidget"
          @layout-change="onLayoutChange"
        />
      </div>
    </div>

    <!-- Widget Config Modal -->
    <WidgetConfigModal
      v-if="showConfigModal && editingWidget"
      :widget="editingWidget"
      :machines="machineStore.machines"
      @save="onSaveWidget"
      @close="showConfigModal = false; editingWidget = null"
    />
  </div>
</template>

<style scoped>
/* ── LED Export button ───────────────────────────────────────────────────────── */

.led-export-btn {
  display:     inline-flex;
  align-items: center;
  gap:         0.375rem;       /* gap-1.5 */
  padding:     0.375rem 0.625rem; /* py-1.5 px-2.5 */
  font-size:   0.8125rem;      /* text-[13px] */
  font-weight: 500;
  cursor:      pointer;
  transition:  color 180ms ease, background-color 180ms ease;
  white-space: nowrap;
  user-select: none;
  border:      none;
  background:  transparent;
}

.led-export-btn:disabled {
  opacity: 0.38;
  cursor:  not-allowed;
}

.led-export-btn--default {
  color: rgb(196 181 253);  /* violet-300 */
}
.led-export-btn--default:not(:disabled):hover {
  color:            rgb(237 233 254); /* violet-100 */
  background-color: rgba(139 92 246 / 0.12);
}

.led-export-btn--copied {
  color: rgb(52 211 153); /* emerald-400 */
}

/* ── Icon swap animation ────────────────────────────────────────────────────── */
.icon-swap-enter-active,
.icon-swap-leave-active {
  transition: opacity 120ms ease, transform 120ms ease;
}
.icon-swap-enter-from { opacity: 0; transform: scale(0.6) rotate(-15deg); }
.icon-swap-leave-to   { opacity: 0; transform: scale(0.6) rotate(15deg);  }

/* ── Label swap animation ───────────────────────────────────────────────────── */
.label-swap-enter-active,
.label-swap-leave-active {
  transition: opacity 100ms ease, transform 100ms ease;
}
.label-swap-enter-from { opacity: 0; transform: translateY(-4px); }
.label-swap-leave-to   { opacity: 0; transform: translateY(4px);  }
</style>
