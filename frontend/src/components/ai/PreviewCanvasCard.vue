<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue';
import { ClipboardList, CheckCircle2, Plus } from 'lucide-vue-next';
import type { DashboardWidget, WidgetLayout, WidgetType, WidgetConfig } from '@/types';
import GridStackCanvas from '@/components/dashboard/GridStackCanvas.vue';
import WidgetConfigModal from '@/components/dashboard/WidgetConfigModal.vue';
import WidgetToolbox from '@/components/dashboard/WidgetToolbox.vue';
import { useMachineStore } from '@/stores/machine.store';

interface PreviewWidget {
  type: string; title: string; machine: string; machineUuid?: string;
  metric: string; unit: string; min?: number; max?: number;
  startDateTime?: string; endDateTime?: string;
}

const props = defineProps<{
  result: {
    dashboardName: string;
    widgets: PreviewWidget[];
    summary: string;
  };
  highlightId?: string;
  resetToken?: number;
}>();

const emit = defineEmits<{
  confirm: [dashboardName: string];
  'remove-widget': [index: number];
  'add-widget': [widget: PreviewWidget];
  'update-widget': [index: number, data: Partial<PreviewWidget>];
  'mention-widget': [payload: { text: string; selected: boolean }];
}>();

const machineStore = useMachineStore();
onMounted(() => machineStore.fetchMachines());

const localName = ref(props.result.dashboardName);

const localLayouts = ref<Record<string, WidgetLayout>>({});
// preview widget id -> the exact mention token appended to the AI input (so removal is exact)
const selected = ref<Record<string, string>>({});
const selectedIds = computed(() => Object.keys(selected.value));
// Parent bumps resetToken after a message is sent → clear the mention rings.
watch(() => props.resetToken, () => { selected.value = {}; });
const showToolbox = ref(false);
const showConfigModal = ref(false);
const editingPreviewIdx = ref(-1);
const editingWidget = ref<DashboardWidget | null>(null);
const gridRef = ref<InstanceType<typeof GridStackCanvas> | null>(null);

function flowLayout(index: number): WidgetLayout {
  const w = 6, h = 4, perRow = 2;
  return { x: (index % perRow) * w, y: Math.floor(index / perRow) * h, w, h };
}

const previewWidgets = computed<DashboardWidget[]>(() =>
  props.result.widgets.map((w, i) => {
    const id = `preview-${i}`;
    return {
      id,
      dashboardId: 'preview',
      widgetType: w.type as DashboardWidget['widgetType'],
      title: w.title || (w.machine ? `${w.machine}${w.metric ? ' — ' + w.metric : ''}` : w.type),
      layout: localLayouts.value[id] ?? flowLayout(i),
      config: {
        field: w.metric || '',
        unit: w.unit || '',
        ...(w.min !== undefined ? { min: w.min } : {}),
        ...(w.max !== undefined ? { max: w.max } : {}),
        ...(w.startDateTime ? { startDateTime: w.startDateTime } : {}),
        ...(w.endDateTime ? { endDateTime: w.endDateTime } : {}),
      },
      machineId: w.machineUuid || undefined,
      machine: w.machine ? { id: w.machineUuid || '', name: w.machine, type: 'sensor' as any, fields: [] } : undefined,
      order: i,
    };
  })
);

function onLayoutChange(layouts: Array<{ id: string; layout: WidgetLayout }>) {
  for (const { id, layout } of layouts) localLayouts.value[id] = layout;
}

function onSelectPreviewWidget(widget: DashboardWidget) {
  if (selected.value[widget.id]) {
    const token = selected.value[widget.id];
    const { [widget.id]: _, ...rest } = selected.value;
    selected.value = rest;
    emit('mention-widget', { text: token, selected: false });
  } else {
    const token = `@${widget.title} `;
    selected.value = { ...selected.value, [widget.id]: token };
    emit('mention-widget', { text: token, selected: true });
  }
}

function onEditPreviewWidget(widget: DashboardWidget) {
  const idx = parseInt(widget.id.replace('preview-', ''), 10);
  if (isNaN(idx)) return;
  editingPreviewIdx.value = idx;
  editingWidget.value = { ...widget };
  showConfigModal.value = true;
}

function onRemovePreviewWidget(widgetId: string) {
  const idx = parseInt(widgetId.replace('preview-', ''), 10);
  if (!isNaN(idx)) {
    delete localLayouts.value[widgetId];
    emit('remove-widget', idx);
  }
}

function onAddWidget(type: WidgetType) {
  showToolbox.value = false;
  editingPreviewIdx.value = -1;
  editingWidget.value = {
    id: '',
    dashboardId: 'preview',
    widgetType: type,
    layout: { x: 0, y: 9999, w: 6, h: 4 },
    config: {},
    order: 0,
  };
  showConfigModal.value = true;
}

function onSaveWidget(data: { machineId?: string; widgetType: WidgetType; title?: string; config: WidgetConfig; layout: WidgetLayout }) {
  const machineName = data.machineId
    ? (machineStore.machines.find(m => m.id === data.machineId)?.name ?? '')
    : '';

  const pw: PreviewWidget = {
    type: data.widgetType,
    title: data.title ?? '',
    machine: machineName,
    machineUuid: data.machineId,
    metric: (data.config.field as string) ?? '',
    unit: (data.config.unit as string) ?? '',
    ...(data.config.min !== undefined ? { min: data.config.min as number } : {}),
    ...(data.config.max !== undefined ? { max: data.config.max as number } : {}),
    ...(data.config.startDateTime ? { startDateTime: data.config.startDateTime as string } : {}),
    ...(data.config.endDateTime ? { endDateTime: data.config.endDateTime as string } : {}),
  };

  if (editingPreviewIdx.value === -1) {
    emit('add-widget', pw);
  } else {
    emit('update-widget', editingPreviewIdx.value, pw);
    delete localLayouts.value[`preview-${editingPreviewIdx.value}`];
  }
  showConfigModal.value = false;
  editingWidget.value = null;
}
</script>

<template>
  <div class="animate-slide-in rounded-xl border border-violet-500/25 bg-violet-500/10 p-4 w-full">
    <div class="flex items-center justify-between mb-3">
      <div class="flex items-center gap-2 text-violet-400 font-semibold text-sm">
        <ClipboardList class="w-4 h-4" />
        <input
          v-model="localName"
          class="bg-transparent border-b border-violet-500/40 focus:border-violet-400 outline-none text-violet-300 font-semibold text-sm min-w-0 w-48"
          placeholder="Dashboard name"
        />
      </div>
      <span class="text-[10px] text-violet-400/60 bg-violet-500/10 px-2 py-0.5 rounded-full border border-violet-500/20">
        Preview
      </span>
    </div>

    <!-- Grid + toolbox in flex row (same pattern as DashboardEditorPage) -->
    <div class="flex gap-3 mb-4">
      <WidgetToolbox
        v-if="showToolbox"
        @select="onAddWidget"
        @close="showToolbox = false"
      />

      <!-- Editable preview grid -->
      <div class="flex-1 rounded-lg overflow-hidden bg-surface border border-white/5">
        <GridStackCanvas
          ref="gridRef"
          :widgets="previewWidgets"
          :selected-ids="selectedIds"
          :highlighted-id="props.highlightId"
          @edit-widget="onEditPreviewWidget"
          @remove-widget="onRemovePreviewWidget"
          @select-widget="onSelectPreviewWidget"
          @layout-change="onLayoutChange"
        />
      </div>
    </div>

    <!-- Widget chip list with delete buttons -->
    <div class="flex flex-wrap gap-1.5 mb-3">
      <span
        v-for="(w, i) in result.widgets"
        :key="i"
        class="inline-flex items-center px-2 py-1 rounded-md text-xs bg-white/5 border border-white/10 text-white/70"
      >
        {{ w.title || w.type }}
      </span>
    </div>

    <div class="flex items-center gap-2">
      <button
        class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium bg-white/5 hover:bg-white/10 border border-white/10 text-white/70 transition-colors"
        @click="showToolbox = !showToolbox"
      >
        <Plus class="w-3.5 h-3.5" />
        Add Widget
      </button>

      <button
        class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium bg-violet-600 hover:bg-violet-500 text-white transition-colors"
        @click="emit('confirm', localName)"
      >
        <CheckCircle2 class="w-3.5 h-3.5" />
        Create Dashboard
      </button>
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
