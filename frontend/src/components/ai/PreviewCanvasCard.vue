<script setup lang="ts">
import { computed } from 'vue';
import { ClipboardList, CheckCircle2, Trash2 } from 'lucide-vue-next';
import type { DashboardWidget, WidgetLayout } from '@/types';
import GridStackCanvas from '@/components/dashboard/GridStackCanvas.vue';

interface PreviewWidget {
  type: string; title: string; machine: string; machineUuid?: string;
  metric: string; unit: string; min?: number; max?: number;
}

const props = defineProps<{
  result: {
    dashboardName: string;
    widgets: PreviewWidget[];
    summary: string;
  };
}>();

const emit = defineEmits<{ confirm: [dashboardName: string]; 'remove-widget': [index: number] }>();

function flowLayout(index: number): WidgetLayout {
  const w = 6, h = 4, perRow = 2;
  return { x: (index % perRow) * w, y: Math.floor(index / perRow) * h, w, h };
}

// Convert preview widget descriptions into fake DashboardWidget objects.
// machineId is left undefined — widget components will show their empty state
// (spinner / no data) which is fine for a layout preview.
const previewWidgets = computed<DashboardWidget[]>(() =>
  props.result.widgets.map((w, i) => ({
    id: `preview-${i}`,
    dashboardId: 'preview',
    widgetType: w.type as DashboardWidget['widgetType'],
    title: w.title || (w.machine ? `${w.machine}${w.metric ? ' — ' + w.metric : ''}` : w.type),
    layout: flowLayout(i),
    config: {
      field: w.metric || '',
      unit: w.unit || '',
      ...(w.min !== undefined ? { min: w.min } : {}),
      ...(w.max !== undefined ? { max: w.max } : {}),
    },
    // Provide a stub machine so widget headers show the name instead of nothing
    machineId: w.machineUuid || undefined,
    machine: w.machine ? { id: w.machineUuid || '', name: w.machine, type: 'sensor' as any, fields: [] } : undefined,
    order: i,
  }))
);

// Container height: each widget row is h:4 cells × 80px + margins
const gridHeight = computed(() => {
  const rows = Math.ceil(props.result.widgets.length / 2);
  return rows * (4 * 80 + 8) + 24;
});
</script>

<template>
  <div class="animate-slide-in rounded-xl border border-violet-500/25 bg-violet-500/10 p-4 w-full">
    <div class="flex items-center justify-between mb-3">
      <div class="flex items-center gap-2 text-violet-400 font-semibold text-sm">
        <ClipboardList class="w-4 h-4" />
        Dashboard Plan — {{ result.dashboardName }}
      </div>
      <span class="text-[10px] text-violet-400/60 bg-violet-500/10 px-2 py-0.5 rounded-full border border-violet-500/20">
        Preview
      </span>
    </div>

    <!-- Live widget preview grid (readonly, no data fetched without real machineId) -->
    <div
      class="rounded-lg overflow-hidden bg-surface border border-white/5 mb-4"
      :style="{ height: gridHeight + 'px' }"
    >
      <GridStackCanvas :widgets="previewWidgets" :readonly="true" />
    </div>

    <!-- Widget chip list with delete buttons -->
    <div class="flex flex-wrap gap-1.5 mb-3">
      <span
        v-for="(w, i) in result.widgets"
        :key="i"
        class="inline-flex items-center gap-1 px-2 py-1 rounded-md text-xs bg-white/5 border border-white/10 text-white/70"
      >
        {{ w.title || w.type }}
        <button class="hover:text-red-400 transition-colors ml-0.5" @click="emit('remove-widget', i)">
          <Trash2 class="w-3 h-3" />
        </button>
      </span>
    </div>

    <button
      class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium bg-violet-600 hover:bg-violet-500 text-white transition-colors"
      @click="emit('confirm', result.dashboardName)"
    >
      <CheckCircle2 class="w-3.5 h-3.5" />
      Create Dashboard
    </button>
  </div>
</template>
