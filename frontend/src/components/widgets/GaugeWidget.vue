<script setup lang="ts">
import { computed } from 'vue';
import VChart from 'vue-echarts';
import type { EChartsOption } from 'echarts';
import type { DashboardWidget } from '@/types';
import { useTelemetryStore } from '@/stores/telemetry.store';
import { useAggregatedValue } from '@/composables/useTelemetry';

const props = defineProps<{ widget: DashboardWidget }>();

const store     = useTelemetryStore();
const machineId = computed(() => props.widget.machineId ?? '');
const field     = computed(() => (props.widget.config?.field as string) ?? '');
const minVal    = computed(() => (props.widget.config?.min as number) ?? 0);
const maxVal    = computed(() => (props.widget.config?.max as number) ?? 100);
const unit      = computed(() => (props.widget.config?.unit as string) ?? '');
const aggPeriod = computed(() => (props.widget.config?.aggregationPeriod as string) ?? 'live');

const { summary, loading: aggLoading, periodLabel, isLive } =
  useAggregatedValue(machineId.value, field.value, aggPeriod.value);

// Live value from WebSocket store
const liveValue = computed(() => store.getFieldValue(machineId.value, field.value) ?? 0);

// Use aggregate avg when available, otherwise live
const currentValue = computed(() => {
  if (!isLive && summary.value != null) return summary.value.avg;
  return liveValue.value;
});

const valuePercent = computed(() => {
  const range = maxVal.value - minVal.value;
  if (range === 0) return 0;
  return ((currentValue.value - minVal.value) / range) * 100;
});

const gaugeColor = computed(() => {
  const pct = valuePercent.value;
  if (pct > 90) return '#ef4444';
  if (pct > 75) return '#f59e0b';
  return '#10b981';
});

const detailFormatter = computed(() =>
  isLive || !unit.value
    ? `{value} ${unit.value}`
    : `{value} ${unit.value}\n{per|per ${periodLabel}}`,
);

const option = computed<EChartsOption>(() => {
  // ⚠ aggLoading is a Ref<boolean> — must use .value inside computed()
  const isLoading = aggLoading.value && !isLive;

  return {
    backgroundColor: 'transparent',
    series: [{
      type: 'gauge',
      min: minVal.value,
      max: maxVal.value,
      startAngle: 205,
      endAngle: -25,
      radius: '90%',
      center: ['50%', '60%'],
      progress: { show: true, width: 12, itemStyle: { color: isLoading ? '#374151' : gaugeColor.value } },
      axisLine: { lineStyle: { width: 12, color: [[1, '#1f2937']] } },
      axisTick: { show: false },
      splitLine: {
        length: 8,
        distance: 4,
        lineStyle: { width: 2, color: '#374151' },
      },
      axisLabel: {
        distance: 20,
        color: '#6b7280',
        fontSize: 9,
        formatter: (val: number) => val >= 1000 ? `${(val / 1000).toFixed(1)}k` : val.toFixed(0),
      },
      pointer: {
        icon: 'path://M12.8,0.7l12.3,0.3L25,29.5l-12.3,0.3z',
        length: '55%',
        width: 5,
        offsetCenter: [0, '5%'],
        itemStyle: { color: isLoading ? '#374151' : gaugeColor.value },
      },
      anchor: {
        show: true,
        showAbove: true,
        size: 16,
        itemStyle: { color: '#1f2937', borderColor: isLoading ? '#374151' : gaugeColor.value, borderWidth: 3 },
      },
      detail: {
        valueAnimation: true,
        fontSize: 20,
        fontWeight: 'bold',
        color: isLoading ? '#4b5563' : '#f3f4f6',
        formatter: isLoading ? 'loading…' : `{value} ${unit.value}`,
        offsetCenter: [0, '30%'],
      },
      data: [{ value: isLoading ? 0 : currentValue.value }],
    }],
  };
});
</script>

<template>
  <div class="w-full h-full relative">
    <div v-if="!machineId || !field" class="flex items-center justify-center h-full text-xs text-gray-600">
      Configure machine &amp; field
    </div>
    <template v-else>
      <VChart :option="option" autoresize />

      <!-- Period badge (aggregated mode) -->
      <div
        v-if="!isLive"
        class="absolute bottom-2 left-0 right-0 flex justify-center"
      >
        <span class="text-[10px] font-medium px-2 py-0.5 rounded-full bg-blue-500/15 text-blue-400 border border-blue-500/20">
          avg per {{ periodLabel }}
        </span>
      </div>
    </template>
  </div>
</template>
