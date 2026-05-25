<script setup lang="ts">
import { computed, ref } from 'vue';
import { use } from 'echarts/core';
import VChart from 'vue-echarts';
import type { EChartsOption } from 'echarts';
import type { DashboardWidget } from '@/types';
import { useFieldSeries } from '@/composables/useTelemetry';
import { useTelemetryStore } from '@/stores/telemetry.store';

const props = defineProps<{ widget: DashboardWidget }>();

const machineId = computed(() => props.widget.machineId ?? '');
const field     = computed(() => (props.widget.config?.field as string) ?? '');
const color     = computed(() => (props.widget.config?.color as string) ?? '#3b82f6');
const timeRange = computed(() => (props.widget.config?.timeRange as string) ?? '1h');

const { mergedData, loading } = useFieldSeries(machineId.value, field.value, timeRange.value);

const option = computed<EChartsOption>(() => ({
  backgroundColor: 'transparent',
  grid: { left: 42, right: 12, top: 12, bottom: 30, containLabel: false },
  xAxis: {
    type: 'category',
    data: mergedData.value.map(p => {
      const d = new Date(p.ts);
      return d.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });
    }),
    axisLabel: { color: '#6b7280', fontSize: 10, interval: 'auto', maxInterval: 20 },
    axisLine: { lineStyle: { color: '#374151' } },
    splitLine: { show: false },
  },
  yAxis: {
    type: 'value',
    axisLabel: { color: '#6b7280', fontSize: 10 },
    splitLine: { lineStyle: { color: '#1f2937', type: 'dashed' } },
  },
  tooltip: {
    trigger: 'axis',
    backgroundColor: '#1e2130',
    borderColor: '#374151',
    textStyle: { color: '#e5e7eb', fontSize: 12 },
    formatter: (params: any) => {
      const p = Array.isArray(params) ? params[0] : params;
      return `<div style="font-family:monospace">${p.name}<br/>${field.value}: <b>${p.value}</b></div>`;
    },
  },
  series: [{
    type: 'line',
    data: mergedData.value.map(p => p.value),
    smooth: 0.3,
    symbol: 'none',
    lineStyle: { color: color.value, width: 2 },
    areaStyle: {
      color: {
        type: 'linear',
        x: 0, y: 0, x2: 0, y2: 1,
        colorStops: [
          { offset: 0, color: color.value + '40' },
          { offset: 1, color: color.value + '00' },
        ],
      },
    },
  }],
}));
</script>

<template>
  <div class="relative w-full h-full">
    <div v-if="loading" class="absolute inset-0 flex items-center justify-center">
      <div class="spinner" />
    </div>
    <div v-else-if="!machineId || !field" class="flex items-center justify-center h-full text-xs text-gray-600">
      Configure machine &amp; field
    </div>
    <VChart v-else :option="option" autoresize />
  </div>
</template>
