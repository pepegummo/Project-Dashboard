<script setup lang="ts">
import { ref, onMounted, computed } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useDashboardStore } from '@/stores/dashboard.store';
import { useMachineStore } from '@/stores/machine.store';
import { useWidgetViewStateStore } from '@/stores/widget-view-state.store';
import { useLedExport } from '@/composables/useLedExport';
import { Save, Plus, ArrowLeft, Loader2, LayoutGrid, Monitor, ExternalLink } from 'lucide-vue-next';
import GridStackCanvas from '@/components/dashboard/GridStackCanvas.vue';
import WidgetToolbox from '@/components/dashboard/WidgetToolbox.vue';
import WidgetConfigModal from '@/components/dashboard/WidgetConfigModal.vue';
import type { DashboardWidget, WidgetType, WidgetLayout, WidgetConfig } from '@/types';

const route = useRoute();
const router = useRouter();
const dashboardStore = useDashboardStore();
const machineStore = useMachineStore();
const widgetViewStateStore = useWidgetViewStateStore();

// ── LED Export ─────────────────────────────────────────────────────────────────
const { exportLedLink, openLedPreview, exportLabel, copied } = useLedExport();

const showToolbox = ref(false);
const showConfigModal = ref(false);
const editingWidget = ref<DashboardWidget | null>(null);
const saving = ref(false);
const gridCanvasRef = ref<InstanceType<typeof GridStackCanvas> | null>(null);

const dashboardId = computed(() => route.params.id as string);

onMounted(async () => {
  await Promise.all([
    dashboardStore.fetchDashboard(dashboardId.value),
    machineStore.fetchMachines(),
  ]);
});

async function saveLayout(layouts: Array<{ id: string; layout: WidgetLayout }>) {
  await dashboardStore.saveLayout(layouts);
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
  } finally {
    saving.value = false;
  }
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
  if (editingWidget.value.id) {
    // Update existing — include machineId so machine changes take effect
    await dashboardStore.updateWidget(editingWidget.value.id, {
      machineId: widget.machineId,
      title: widget.title,
      config: widget.config,
    });
  } else {
    // Add new
    await dashboardStore.addWidget(widget);
  }
  showConfigModal.value = false;
  editingWidget.value = null;
}

function onEditWidget(widget: DashboardWidget) {
  editingWidget.value = widget;
  showConfigModal.value = true;
}

async function onRemoveWidget(widgetId: string) {
  if (!confirm('Remove this widget?')) return;
  await dashboardStore.removeWidget(widgetId);
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
          <h1 class="text-lg font-bold text-white">
            {{ dashboardStore.currentDashboard?.name ?? 'Dashboard' }}
          </h1>
          <p class="text-xs text-gray-500">
            {{ dashboardStore.widgets.length }} widgets
          </p>
        </div>
      </div>

      <div class="flex items-center gap-2">
        <button class="btn-secondary" @click="showToolbox = !showToolbox">
          <Plus class="w-4 h-4" />
          Add Widget
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
          {{ saving ? 'Saving…' : 'Saved' }}
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
          @layout-change="saveLayout"
          @edit-widget="onEditWidget"
          @remove-widget="onRemoveWidget"
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
