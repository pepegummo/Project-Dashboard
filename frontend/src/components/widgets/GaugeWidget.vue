<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, nextTick } from 'vue';
import VChart from 'vue-echarts';
import type { EChartsOption } from 'echarts';
import type { DashboardWidget } from '@/types';
import { useTelemetryStore } from '@/stores/telemetry.store';
import { useAggregatedValue } from '@/composables/useTelemetry';
import { wsService } from '@/services/ws.service';
import { api } from '@/services/api.service';
import { useWidgetViewStateStore } from '@/stores/widget-view-state.store';

const props = defineProps<{ widget: DashboardWidget }>();

const chartRef  = ref<InstanceType<typeof VChart>>();
const store     = useTelemetryStore();
const widgetViewStateStore = useWidgetViewStateStore();
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

// ── Live mode: WebSocket subscription + one-time REST seed ───────────────────
async function fetchLatest() {
  if (!machineId.value) return;
  try {
    const latest = await api.getLatestTelemetry(machineId.value);
    if (latest) store.updateSnapshot(machineId.value, latest.timestamp as unknown as string, latest.data as Record<string, any>);
  } catch { /* ok */ }
}

onMounted(() => {
  if (isLive && machineId.value) {
    wsService.subscribe([machineId.value]);
    fetchLatest(); // seed store immediately from DB; WS handles all subsequent updates
  }
  // Defer resize to the next paint so ECharts recalculates center/radius from the
  // container's settled layout dimensions — fixes misalignment inside CSS-scaled wrappers.
  nextTick(() => {
    chartRef.value?.chart?.resize();
  });
});

onUnmounted(() => {
  if (isLive && machineId.value) wsService.unsubscribe([machineId.value]);
});

// ── Threshold / limits from machine_field ─────────────────────────────────
const machineField = computed(() => props.widget.machine?.fields?.find(f => f.key === field.value));
const threshold    = computed(() => machineField.value?.threshold  ?? null);
const upperLimit   = computed(() => machineField.value?.upperLimit ?? null);
const lowerLimit   = computed(() => machineField.value?.lowerLimit ?? null);

const arcColors: [number, string][] = [[1, '#374151']];

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

// The unit and threshold labels are separate HTML overlays (tagged data-ai-el) — the value
// overlay below covers the rest of the canvas (dial + number) as "value", detail computed
// here so the template overlay div can read it directly.
const valueDetail = computed(() => `${currentValue.value}${unit.value ? ' ' + unit.value : ''}`);

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
      axisLine: { lineStyle: { width: 12, color: arcColors } },
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
        formatter: isLoading ? 'loading…' : '{value}',
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
      <VChart ref="chartRef" :option="option" :update-options="{ notMerge: true }" autoresize />

      <!-- Element-pick mode (/ai): whole-canvas overlay for "value" — placed BEFORE the
           unit/threshold overlays below so those stack on top and stay individually
           clickable, while the rest of the dial/number falls through to this one. -->
      <div
        v-if="widgetViewStateStore.elementPickMode"
        class="absolute inset-0"
        data-ai-el="value"
        :data-ai-detail="valueDetail"
      />

      <!-- Unit as HTML (not canvas) so it's individually clickable in AI element-pick mode -->
      <div v-if="unit" class="absolute left-0 right-0 flex justify-center pointer-events-none" style="top: calc(60% + 28% * 0.5 + 14px)">
        <span data-ai-el="unit" class="pointer-events-auto text-[11px] text-gray-400">{{ unit }}</span>
      </div>

      <!-- Threshold / limit labels -->
      <div v-if="threshold !== null || upperLimit !== null" class="absolute bottom-8 left-0 right-0 flex justify-center gap-3 text-[9px]">
        <span v-if="lowerLimit !== null" data-ai-el="threshold" :data-ai-detail="`lower ${lowerLimit}`" class="text-amber-400">↓ {{ lowerLimit }}</span>
        <span v-if="threshold !== null" data-ai-el="threshold" :data-ai-detail="`target ${threshold}`" class="text-indigo-400">◎ {{ threshold }}</span>
        <span v-if="upperLimit !== null" data-ai-el="threshold" :data-ai-detail="`upper ${upperLimit}`" class="text-amber-400">↑ {{ upperLimit }}</span>
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
