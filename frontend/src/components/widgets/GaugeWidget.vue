<script setup lang="ts">
import { computed, onMounted, onUnmounted } from 'vue';
import VChart from 'vue-echarts';
import type { EChartsOption } from 'echarts';
import type { DashboardWidget } from '@/types';
import { useTelemetryStore } from '@/stores/telemetry.store';
import { useAggregatedValue } from '@/composables/useTelemetry';
import { wsService } from '@/services/ws.service';
import { api } from '@/services/api.service';

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

const liveValue    = computed(() => store.getFieldValue(machineId.value, field.value) ?? 0);
const currentValue = computed(() => (!isLive && summary.value != null) ? summary.value.avg : liveValue.value);

// ── Live mode: WebSocket subscription + 2-second polling fallback ─────────────
let pollTimer: ReturnType<typeof setInterval> | null = null;

async function fetchLatest() {
  if (!machineId.value) return;
  try {
    const latest = await api.getLatestTelemetry(machineId.value);
    if (latest) store.updateSnapshot(machineId.value, latest.timestamp as unknown as string, latest.data as Record<string, any>);
  } catch { /* ok */ }
}

onMounted(() => {
  if (isLive && machineId.value) {
    wsService.subscribe([machineId.value]);   // live data from simulator (if running)
    fetchLatest();                             // seed store immediately from DB
    pollTimer = setInterval(fetchLatest, 2000); // refresh every 2 s
  }
});

onUnmounted(() => {
  if (isLive && machineId.value) wsService.unsubscribe([machineId.value]);
  if (pollTimer) { clearInterval(pollTimer); pollTimer = null; }
});

// ── Threshold / limits from machine_field ─────────────────────────────────
const machineField = computed(() => props.widget.machine?.fields?.find(f => f.key === field.value));
const threshold    = computed(() => machineField.value?.threshold  ?? null);
const upperLimit   = computed(() => machineField.value?.upperLimit ?? null);
const lowerLimit   = computed(() => machineField.value?.lowerLimit ?? null);

// Arc color stops: gray | green (good zone) | gray
// Each stop is [endPercent, color] where percent = (value - min) / (max - min)
const arcColors = computed<[number, string][]>(() => {
  const range = maxVal.value - minVal.value;
  if (range === 0 || lowerLimit.value === null || upperLimit.value === null) {
    return [[1, '#374151']];
  }
  const lo = Math.max(0, Math.min(1, (lowerLimit.value - minVal.value) / range));
  const up = Math.max(0, Math.min(1, (upperLimit.value - minVal.value) / range));
  return [
    [lo, '#374151'],   // below lower → gray
    [up, '#10b981'],   // lower → upper → green (good zone)
    [1,  '#374151'],   // above upper → gray
  ];
});

// Needle color: green if in range, red if out
const inRange = computed(() => {
  if (lowerLimit.value === null || upperLimit.value === null) return true;
  return currentValue.value >= lowerLimit.value && currentValue.value <= upperLimit.value;
});
const needleColor = computed(() => {
  const isLoading = aggLoading.value && !isLive;
  if (isLoading) return '#374151';
  return inRange.value ? '#10b981' : '#ef4444';
});

const option = computed<EChartsOption>(() => {
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
      progress: { show: true, width: 12, itemStyle: { color: isLoading ? '#374151' : needleColor.value } },
      axisLine: { lineStyle: { width: 12, color: arcColors.value } },
      axisTick: { show: false },
      splitLine: { length: 8, distance: 4, lineStyle: { width: 2, color: '#374151' } },
      axisLabel: {
        distance: 20,
        color: '#6b7280',
        fontSize: 9,
        formatter: (val: number) => val >= 1000 ? `${(val / 1000).toFixed(1)}k` : val.toFixed(0),
      },
      pointer: {
        icon: 'path://M12.8,0.7l12.3,0.3L25,29.5l-12.3,0.3z',
        length: '55%', width: 5, offsetCenter: [0, '5%'],
        itemStyle: { color: isLoading ? '#374151' : needleColor.value },
      },
      anchor: {
        show: true, showAbove: true, size: 16,
        itemStyle: { color: '#1f2937', borderColor: isLoading ? '#374151' : needleColor.value, borderWidth: 3 },
      },
      detail: {
        valueAnimation: true,
        fontSize: 18, fontWeight: 'bold',
        color: isLoading ? '#4b5563' : '#f3f4f6',
        formatter: isLoading ? 'loading…' : `{value} ${unit.value}`,
        offsetCenter: [0, '28%'],
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

      <!-- Threshold / limit labels -->
      <div v-if="threshold !== null || upperLimit !== null" class="absolute bottom-8 left-0 right-0 flex justify-center gap-3 text-[9px]">
        <span v-if="lowerLimit !== null" class="text-amber-400">↓ {{ lowerLimit }}</span>
        <span v-if="threshold !== null" class="text-indigo-400">◎ {{ threshold }}</span>
        <span v-if="upperLimit !== null" class="text-amber-400">↑ {{ upperLimit }}</span>
      </div>

      <!-- Period badge -->
      <div v-if="!isLive" class="absolute bottom-2 left-0 right-0 flex justify-center">
        <span class="text-[10px] font-medium px-2 py-0.5 rounded-full bg-blue-500/15 text-blue-400 border border-blue-500/20">
          avg per {{ periodLabel }}
        </span>
      </div>
    </template>
  </div>
</template>
