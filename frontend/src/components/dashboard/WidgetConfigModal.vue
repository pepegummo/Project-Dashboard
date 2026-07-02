<script setup lang="ts">
import { ref, computed, watch } from 'vue';
import { X, Save } from 'lucide-vue-next';
import { api } from '@/services/api.service';
import type { DashboardWidget, Machine, WidgetType, WidgetLayout, WidgetConfig } from '@/types';

const props = defineProps<{
  widget: DashboardWidget;
  machines: Machine[];
}>();

const emit = defineEmits<{
  save: [data: { machineId?: string; widgetType: WidgetType; title?: string; config: WidgetConfig; layout: WidgetLayout }];
  close: [];
}>();

const ALL_WIDGET_TYPES: { value: WidgetType; label: string }[] = [
  { value: 'line-chart',  label: 'Line Chart' },
  { value: 'gauge',       label: 'Gauge' },
  { value: 'kpi-card',    label: 'KPI Card' },
  { value: 'status-card', label: 'Status Card' },
  { value: 'table',       label: 'Data Table' },
  { value: 'alarm-panel', label: 'Alarm Panel' },
  { value: 'daily-count', label: 'Count' },
  { value: 'chart',       label: 'Custom Chart' },
];

const title = ref(props.widget.title ?? '');
const selectedWidgetType = ref<WidgetType>(props.widget.widgetType);
const selectedMachineId = ref(props.widget.machineId ?? '');
const selectedField = ref((props.widget.config?.field as string) ?? '');
const color = ref((props.widget.config?.color as string) ?? '#3b82f6');
const min = ref<number>((props.widget.config?.min as number) ?? 0);
const max = ref<number>((props.widget.config?.max as number) ?? 100);

// Count widget: bucket is stored as '<n><m|h|d>' (e.g. '30m'); split into value + unit for the form.
const _bucketMatch = /^(\d+)(m|h|d)$/.exec((props.widget.config?.bucket as string) ?? '');
const bucketValue = ref<number>(_bucketMatch ? Number(_bucketMatch[1]) : 1);
const bucketUnit  = ref<'m' | 'h' | 'd'>(_bucketMatch ? (_bucketMatch[2] as 'm' | 'h' | 'd') : 'h');

// Count widget: SKU + status filters. SKU options are fetched from the machine's recent data.
const sku = ref<string>((props.widget.config?.sku as string) ?? '');
const status = ref<'all' | 'good' | 'reject'>((props.widget.config?.status as 'all' | 'good' | 'reject') ?? 'all');
const skuOptions = ref<string[]>([]);
const statusOptions: Array<'all' | 'good' | 'reject'> = ['all', 'good', 'reject'];

// Custom chart: overlaid fields, render style, window (bucket × bars), and y-axis scaling.
// Reuses bucketValue/bucketUnit above (same '<n><m|h|d>' encoding as the Count widget).
const selectedFields = ref<string[]>((props.widget.config?.fields as string[]) ?? []);
const chartType = ref<'line' | 'bar' | 'area'>((props.widget.config?.chartType as 'line' | 'bar' | 'area') ?? 'line');
const chartPoints = ref<number>((props.widget.config?.points as number) ?? 20);
const chartScaling = ref<'shared' | 'dual' | 'normalized'>(
  (props.widget.config?.scaling as any) ?? (props.widget.config?.normalize ? 'normalized' : 'shared'),
);
const chartTypeOptions: Array<'line' | 'bar' | 'area'> = ['line', 'bar', 'area'];
const scalingOptions: Array<'shared' | 'dual' | 'normalized'> = ['shared', 'dual', 'normalized'];

const selectedMachine = computed(() => props.machines.find(m => m.id === selectedMachineId.value));
const availableFields = computed(() => selectedMachine.value?.fields ?? []);

watch(() => selectedMachineId.value, () => { selectedField.value = ''; sku.value = ''; selectedFields.value = []; });
watch(selectedWidgetType, () => { selectedField.value = ''; selectedFields.value = []; });

// 3+ fields can't be read on a shared or 2-axis chart — force Normalized and lock it.
const scalingLocked = computed(() => selectedFields.value.length >= 3);
watch(scalingLocked, locked => { if (locked) chartScaling.value = 'normalized'; });

const needsMachine    = (type: WidgetType) => ['line-chart', 'gauge', 'kpi-card', 'status-card', 'table', 'daily-count', 'alarm-panel', 'chart'].includes(type);
const needsField      = (type: WidgetType) => ['line-chart', 'gauge', 'kpi-card'].includes(type);
const needsMinMax     = (type: WidgetType) => type === 'gauge';
const needsBucket     = (type: WidgetType) => type === 'daily-count';
const needsSku        = (type: WidgetType) => type === 'daily-count';
const needsMultiField = (type: WidgetType) => type === 'chart';

function toggleField(key: string) {
  const i = selectedFields.value.indexOf(key);
  if (i === -1) selectedFields.value.push(key);
  else selectedFields.value.splice(i, 1);
}

// Load the machine's distinct SKUs for the Count widget's dropdown.
async function loadSkus() {
  skuOptions.value = [];
  if (!needsSku(selectedWidgetType.value) || !selectedMachineId.value) return;
  try { skuOptions.value = await api.getMachineSkus(selectedMachineId.value); } catch { /* ignore */ }
}
watch([selectedMachineId, selectedWidgetType], loadSkus, { immediate: true });

function save() {
  const config: WidgetConfig = {};
  if (selectedField.value) config.field = selectedField.value;
  if (color.value) config.color = color.value;
  if (needsMinMax(selectedWidgetType.value)) {
    config.min = min.value;
    config.max = max.value;
    const field = availableFields.value.find(f => f.key === selectedField.value);
    if (field?.unit) config.unit = field.unit;
  }
  if (needsBucket(selectedWidgetType.value)) {
    config.bucket = `${Math.max(1, Math.floor(bucketValue.value || 1))}${bucketUnit.value}`;
  }
  if (needsSku(selectedWidgetType.value)) {
    if (sku.value) config.sku = sku.value;
    config.status = status.value;
  }
  if (needsMultiField(selectedWidgetType.value)) {
    config.fields = selectedFields.value;
    config.chartType = chartType.value;
    config.bucket = `${Math.max(1, Math.floor(bucketValue.value || 1))}${bucketUnit.value}`;
    config.points = Math.min(500, Math.max(1, Math.floor(chartPoints.value || 20)));
    config.scaling = chartScaling.value;
  }

  emit('save', {
    machineId: selectedMachineId.value || undefined,
    widgetType: selectedWidgetType.value,
    title: title.value || undefined,
    config,
    layout: props.widget.layout,
  });
}
</script>

<template>
  <Teleport to="body">
    <div
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/70 backdrop-blur-sm"
      @click.self="$emit('close')"
    >
      <div class="bg-surface-100 border border-white/10 rounded-xl w-full max-w-lg shadow-2xl animate-fade-in">
        <!-- Header -->
        <div class="flex items-center justify-between px-6 py-4 border-b border-white/5">
          <div>
            <h2 class="text-base font-semibold text-white">Configure Widget</h2>
          </div>
          <button class="btn-ghost btn-icon" @click="$emit('close')">
            <X class="w-4 h-4" />
          </button>
        </div>

        <!-- Body -->
        <div class="px-6 py-5 space-y-4">
          <!-- Widget Type -->
          <div>
            <label class="label">Widget Type</label>
            <select v-model="selectedWidgetType" class="input">
              <option v-for="t in ALL_WIDGET_TYPES" :key="t.value" :value="t.value">{{ t.label }}</option>
            </select>
          </div>

          <!-- Title -->
          <div>
            <label class="label">Widget Title</label>
            <input v-model="title" class="input" placeholder="Auto-generated from field" />
          </div>

          <!-- Machine selector -->
          <div v-if="needsMachine(selectedWidgetType)">
            <label class="label">Machine</label>
            <select v-model="selectedMachineId" class="input">
              <option value="">— Select machine —</option>
              <option v-for="m in machines" :key="m.id" :value="m.id">{{ m.name }}</option>
            </select>
          </div>

          <!-- Field selector -->
          <div v-if="needsField(selectedWidgetType) && selectedMachineId">
            <label class="label">Data Field</label>
            <select v-model="selectedField" class="input">
              <option value="">— Select field —</option>
              <option v-for="f in availableFields" :key="f.key" :value="f.key">
                {{ f.label }} ({{ f.unit ?? 'no unit' }})
              </option>
            </select>
          </div>

          <!-- Custom Chart: multiple fields + chart type + range -->
          <template v-if="needsMultiField(selectedWidgetType) && selectedMachineId">
            <div>
              <label class="label">Data Fields</label>
              <div class="max-h-40 overflow-y-auto space-y-1 rounded border border-white/10 bg-surface-300/40 p-2">
                <label
                  v-for="f in availableFields"
                  :key="f.key"
                  class="flex items-center gap-2 px-1 py-0.5 rounded cursor-pointer hover:bg-white/5 text-sm text-gray-200"
                >
                  <input
                    type="checkbox"
                    :checked="selectedFields.includes(f.key)"
                    @change="toggleField(f.key)"
                  />
                  {{ f.label }} <span class="text-gray-500 text-xs">({{ f.unit ?? 'no unit' }})</span>
                </label>
                <p v-if="availableFields.length === 0" class="text-xs text-gray-500">No fields for this machine.</p>
              </div>
            </div>
            <div>
              <label class="label">Chart Type</label>
              <div class="flex gap-2">
                <button
                  v-for="opt in chartTypeOptions"
                  :key="opt"
                  type="button"
                  class="flex-1 px-2 py-1.5 rounded text-xs font-medium border capitalize transition-colors"
                  :class="chartType === opt
                    ? 'bg-blue-600 text-white border-blue-500'
                    : 'bg-surface-300 text-gray-400 border-gray-700 hover:text-gray-200'"
                  @click="chartType = opt"
                >{{ opt }}</button>
              </div>
            </div>
            <div class="grid grid-cols-2 gap-3">
              <div>
                <label class="label">Bucket Size</label>
                <div class="flex gap-2">
                  <input v-model.number="bucketValue" type="number" min="1" max="1000" class="input w-16" />
                  <select v-model="bucketUnit" class="input flex-1">
                    <option value="m">min</option>
                    <option value="h">hours</option>
                    <option value="d">days</option>
                  </select>
                </div>
              </div>
              <div>
                <label class="label">Bars</label>
                <input v-model.number="chartPoints" type="number" min="1" max="500" class="input" />
              </div>
            </div>
            <p class="text-[11px] text-gray-500 -mt-2">Window = bucket × bars (e.g. 1h × 20 = last 20 hours).</p>
            <div>
              <label class="label">Scaling</label>
              <div class="flex gap-2">
                <button
                  v-for="opt in scalingOptions"
                  :key="opt"
                  type="button"
                  :disabled="scalingLocked && opt !== 'normalized'"
                  class="flex-1 px-2 py-1.5 rounded text-xs font-medium border capitalize transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
                  :class="chartScaling === opt
                    ? 'bg-blue-600 text-white border-blue-500'
                    : 'bg-surface-300 text-gray-400 border-gray-700 hover:text-gray-200'"
                  @click="chartScaling = opt"
                >{{ opt }}</button>
              </div>
              <p v-if="scalingLocked" class="text-[11px] text-amber-400/80 mt-1">
                3+ fields → locked to Normalized (Shared/Dual can't display more than 2 scales).
              </p>
              <p v-else class="text-[11px] text-gray-500 mt-1">
                Shared: one axis · Dual: left/right axes grouped by unit (max 2 units) · Normalized: 0–100% (any number).
              </p>
            </div>
          </template>

          <!-- SKU + status (Count widget) -->
          <div v-if="needsSku(selectedWidgetType) && selectedMachineId" class="space-y-3">
            <div>
              <label class="label">SKU</label>
              <select v-model="sku" class="input">
                <option value="">All SKUs</option>
                <option v-for="s in skuOptions" :key="s" :value="s">{{ s }}</option>
              </select>
            </div>
            <div>
              <label class="label">Count</label>
              <div class="flex gap-2">
                <button
                  v-for="opt in statusOptions"
                  :key="opt"
                  type="button"
                  class="flex-1 px-2 py-1.5 rounded text-xs font-medium border capitalize transition-colors"
                  :class="status === opt
                    ? 'bg-blue-600 text-white border-blue-500'
                    : 'bg-surface-300 text-gray-400 border-gray-700 hover:text-gray-200'"
                  @click="status = opt"
                >{{ opt }}</button>
              </div>
            </div>
          </div>

          <!-- Count bucket (Count widget) -->
          <div v-if="needsBucket(selectedWidgetType)">
            <label class="label">Count Bucket</label>
            <div class="flex gap-2">
              <input v-model.number="bucketValue" type="number" min="1" max="1000" class="input w-24" />
              <select v-model="bucketUnit" class="input flex-1">
                <option value="m">minutes</option>
                <option value="h">hours</option>
                <option value="d">days</option>
              </select>
            </div>
            <p class="text-[11px] text-gray-500 mt-1">
              Counts pieces per bucket (e.g. pcs per 30 minutes) for the selected SKU and status.
            </p>
          </div>

          <!-- Min / Max (gauge) -->
          <div v-if="needsMinMax(selectedWidgetType)" class="grid grid-cols-2 gap-3">
            <div>
              <label class="label">Min Value</label>
              <input v-model.number="min" type="number" class="input" />
            </div>
            <div>
              <label class="label">Max Value</label>
              <input v-model.number="max" type="number" class="input" />
            </div>
          </div>

          <!-- Color -->
          <div v-if="selectedWidgetType === 'line-chart'">
            <label class="label">Chart Color</label>
            <div class="flex items-center gap-3">
              <input v-model="color" type="color" class="w-10 h-10 rounded cursor-pointer border border-white/10 bg-transparent" />
              <span class="text-sm text-gray-400 font-mono">{{ color }}</span>
            </div>
          </div>
        </div>

        <!-- Footer -->
        <div class="flex gap-3 px-6 py-4 border-t border-white/5">
          <button class="btn-secondary flex-1" @click="$emit('close')">Cancel</button>
          <button class="btn-primary flex-1 justify-center" @click="save">
            <Save class="w-4 h-4" />
            Save Widget
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>
