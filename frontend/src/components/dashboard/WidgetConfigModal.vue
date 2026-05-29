<script setup lang="ts">
import { ref, computed, watch } from 'vue';
import { X, Save } from 'lucide-vue-next';
import type { DashboardWidget, Machine, WidgetType, WidgetLayout, WidgetConfig } from '@/types';

const props = defineProps<{
  widget: DashboardWidget;
  machines: Machine[];
}>();

const emit = defineEmits<{
  save: [data: { machineId?: string; widgetType: WidgetType; title?: string; config: WidgetConfig; layout: WidgetLayout }];
  close: [];
}>();

const title = ref(props.widget.title ?? '');
const selectedMachineId = ref(props.widget.machineId ?? '');
const selectedField = ref((props.widget.config?.field as string) ?? '');
const color = ref((props.widget.config?.color as string) ?? '#3b82f6');
const min = ref<number>((props.widget.config?.min as number) ?? 0);
const max = ref<number>((props.widget.config?.max as number) ?? 100);

const selectedMachine = computed(() => props.machines.find(m => m.id === selectedMachineId.value));
const availableFields = computed(() => selectedMachine.value?.fields ?? []);

watch(() => selectedMachineId.value, () => { selectedField.value = ''; });

const widgetTypeLabel = (type: WidgetType) => {
  const map: Record<WidgetType, string> = {
    'line-chart':  'Line Chart',
    'gauge':       'Gauge',
    'kpi-card':    'KPI Card',
    'status-card': 'Status Card',
    'table':       'Data Table',
    'alarm-panel': 'Alarm Panel',
    'daily-count': 'Daily Count',
  };
  return map[type] ?? type;
};

const needsMachine = (type: WidgetType) => ['line-chart', 'gauge', 'kpi-card', 'status-card', 'table', 'daily-count', 'alarm-panel'].includes(type);
const needsField   = (type: WidgetType) => ['line-chart', 'gauge', 'kpi-card'].includes(type);
const needsMinMax  = (type: WidgetType) => type === 'gauge';

function save() {
  const config: WidgetConfig = {};
  if (selectedField.value) config.field = selectedField.value;
  if (color.value) config.color = color.value;
  if (needsMinMax(props.widget.widgetType)) {
    config.min = min.value;
    config.max = max.value;
    const field = availableFields.value.find(f => f.key === selectedField.value);
    if (field?.unit) config.unit = field.unit;
  }

  emit('save', {
    machineId: selectedMachineId.value || undefined,
    widgetType: props.widget.widgetType,
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
            <p class="text-xs text-gray-500 mt-0.5">{{ widgetTypeLabel(widget.widgetType) }}</p>
          </div>
          <button class="btn-ghost btn-icon" @click="$emit('close')">
            <X class="w-4 h-4" />
          </button>
        </div>

        <!-- Body -->
        <div class="px-6 py-5 space-y-4">
          <!-- Title -->
          <div>
            <label class="label">Widget Title</label>
            <input v-model="title" class="input" placeholder="Auto-generated from field" />
          </div>

          <!-- Machine selector -->
          <div v-if="needsMachine(widget.widgetType)">
            <label class="label">Machine</label>
            <select v-model="selectedMachineId" class="input">
              <option value="">— Select machine —</option>
              <option v-for="m in machines" :key="m.id" :value="m.id">{{ m.name }}</option>
            </select>
          </div>

          <!-- Field selector -->
          <div v-if="needsField(widget.widgetType) && selectedMachineId">
            <label class="label">Data Field</label>
            <select v-model="selectedField" class="input">
              <option value="">— Select field —</option>
              <option v-for="f in availableFields" :key="f.key" :value="f.key">
                {{ f.label }} ({{ f.unit ?? 'no unit' }})
              </option>
            </select>
          </div>

          <!-- Min / Max (gauge) -->
          <div v-if="needsMinMax(widget.widgetType)" class="grid grid-cols-2 gap-3">
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
          <div v-if="['line-chart'].includes(widget.widgetType)">
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
