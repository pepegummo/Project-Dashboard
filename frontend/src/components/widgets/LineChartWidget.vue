<script setup lang="ts">
import { ref, computed } from 'vue';
import VChart from 'vue-echarts';
import type { EChartsOption } from 'echarts';
import type { DashboardWidget } from '@/types';
import { useFieldSeries } from '@/composables/useTelemetry';

const props = defineProps<{ widget: DashboardWidget }>();

const machineId = computed(() => props.widget.machineId ?? '');
const field     = computed(() => (props.widget.config?.field as string) ?? '');
const color     = computed(() => (props.widget.config?.color as string) ?? '#3b82f6');

// ── All available time ranges ─────────────────────────────────────────────────
const TIME_RANGES = ['5m', '15m', '30m', '1h', '6h', '24h', '7d', '15d', '30d', '3mo', '6mo', '1y'] as const;
type TimeRange = typeof TIME_RANGES[number];

// Persist selected range in localStorage so it survives page refresh.
// Priority: localStorage (user's last choice) → widget config default → '1h'
const RANGE_KEY = `widget_range_${props.widget.id}`;
const selectedRange = ref<TimeRange>(
  (localStorage.getItem(RANGE_KEY) as TimeRange | null)
    ?? (props.widget.config?.timeRange as TimeRange)
    ?? '1h',
);

const { mergedData, loading } = useFieldSeries(machineId.value, field.value, selectedRange);

// ── Threshold / limits ────────────────────────────────────────────────────────
const machineField = computed(() => props.widget.machine?.fields?.find(f => f.key === field.value));
const threshold    = computed(() => machineField.value?.threshold  ?? null);
const upperLimit   = computed(() => machineField.value?.upperLimit ?? null);
const lowerLimit   = computed(() => machineField.value?.lowerLimit ?? null);

const hasProblems = computed(() => {
  if (lowerLimit.value === null && upperLimit.value === null) return false;
  return mergedData.value.some(p => {
    if (upperLimit.value !== null && p.value > upperLimit.value) return true;
    if (lowerLimit.value !== null && p.value < lowerLimit.value) return true;
    return false;
  });
});

// ── Zoom state ────────────────────────────────────────────────────────────────
const chartRef = ref<InstanceType<typeof VChart> | null>(null);
const isZoomed = ref(false);

function onDataZoom(e: any) {
  const batch = e?.batch?.[0] ?? e;
  const start = batch?.start ?? 0;
  const end   = batch?.end   ?? 100;
  isZoomed.value = !(start === 0 && end === 100);
}

function resetZoom() {
  chartRef.value?.chart?.dispatchAction({ type: 'dataZoom', start: 0, end: 100 });
  isZoomed.value = false;
}

function selectRange(r: TimeRange) {
  selectedRange.value = r;
  localStorage.setItem(RANGE_KEY, r);
  isZoomed.value = false;
}

// ── X-axis label format ───────────────────────────────────────────────────────
function formatLabel(ts: string): string {
  const d = new Date(ts);
  if (['3mo', '6mo', '1y'].includes(selectedRange.value))
    return d.toLocaleDateString('en-US', { year: '2-digit', month: 'short', day: 'numeric' });
  if (['7d', '15d', '30d'].includes(selectedRange.value))
    return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
      + ' ' + d.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', hour12: false });
  return d.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', hour12: false });
}

const shouldRotate = computed(() =>
  ['7d', '15d', '30d', '3mo', '6mo', '1y'].includes(selectedRange.value),
);

// ── ECharts option ────────────────────────────────────────────────────────────
const option = computed<EChartsOption>(() => {
  const hasLimits = lowerLimit.value !== null || upperLimit.value !== null;

  // ── Avg / main series ────────────────────────────────────────────────────
  const avgSeries: any = {
    type: 'line',
    name: field.value,
    data: mergedData.value.map(p => p.value),
    smooth: 0.3,
    symbol: 'none',
    lineStyle: { width: 2 },
    z: 3,

    markArea: hasLimits && lowerLimit.value !== null && upperLimit.value !== null ? {
      silent: true,
      data: [[
        {
          yAxis: lowerLimit.value,
          itemStyle: { color: 'rgba(16,185,129,0.07)', borderColor: 'rgba(16,185,129,0.25)', borderWidth: 1 },
        },
        { yAxis: upperLimit.value },
      ]],
    } : undefined,

    markLine: {
      silent: true,
      symbol: 'none',
      animation: false,
      data: [
        ...(upperLimit.value !== null ? [{
          yAxis: upperLimit.value,
          lineStyle: { color: '#f59e0b', type: 'dashed' as const, width: 1 },
          label: { formatter: `↑ ${upperLimit.value}`, color: '#f59e0b', fontSize: 9, position: 'end' as const },
        }] : []),
        ...(threshold.value !== null ? [{
          yAxis: threshold.value,
          lineStyle: { color: '#6366f1', type: 'dashed' as const, width: 1 },
          label: { formatter: `◎ ${threshold.value}`, color: '#6366f1', fontSize: 9, position: 'end' as const },
        }] : []),
        ...(lowerLimit.value !== null ? [{
          yAxis: lowerLimit.value,
          lineStyle: { color: '#f59e0b', type: 'dashed' as const, width: 1 },
          label: { formatter: `↓ ${lowerLimit.value}`, color: '#f59e0b', fontSize: 9, position: 'end' as const },
        }] : []),
      ],
    },
  };

  const seriesList = [avgSeries];

  return {
    backgroundColor: 'transparent',
    grid: { left: 42, right: 60, top: 16, bottom: shouldRotate.value ? 52 : 30, containLabel: false },

    // visualMap always targets seriesIndex 0 (the avg line)
    visualMap: hasLimits ? [{
      show: false,
      type: 'piecewise',
      seriesIndex: 0,
      dimension: 0,
      pieces: [
        ...(lowerLimit.value !== null ? [{ lt: lowerLimit.value, color: '#ef4444' }] : []),
        {
          gte: lowerLimit.value ?? -Infinity,
          lte: upperLimit.value ??  Infinity,
          color: color.value,
        },
        ...(upperLimit.value !== null ? [{ gt: upperLimit.value, color: '#ef4444' }] : []),
      ],
    }] : undefined,

    xAxis: {
      type: 'category',
      data: mergedData.value.map(p => formatLabel(p.ts)),
      axisLabel: {
        color: '#6b7280',
        fontSize: 10,
        interval: 'auto',
        rotate: shouldRotate.value ? 30 : 0,
      },
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
        // params[0] = avg series (index 0); band series are silent so excluded
        const p = Array.isArray(params) ? params[0] : params;
        const idx = p.dataIndex as number;
        const val = p.value as number;
        const pt  = mergedData.value[idx];

        const outOfRange =
          (upperLimit.value !== null && val > upperLimit.value) ||
          (lowerLimit.value !== null && val < lowerLimit.value);
        const statusBadge = outOfRange
          ? `<span style="color:#ef4444;font-weight:bold"> ⚠ OUT OF RANGE</span>`
          : lowerLimit.value !== null
            ? `<span style="color:#10b981"> ✓ IN RANGE</span>`
            : '';

        const thr = threshold.value  !== null ? `<br/><span style="color:#6366f1">◎ target: ${threshold.value}</span>` : '';
        const up  = upperLimit.value !== null ? `<br/><span style="color:#f59e0b">↑ upper: ${upperLimit.value}</span>`  : '';
        const lo  = lowerLimit.value !== null ? `<br/><span style="color:#f59e0b">↓ lower: ${lowerLimit.value}</span>`  : '';

        return `<div style="font-family:monospace;line-height:1.6">
          ${p.name}<br/>
          ${field.value}: <b>${val}</b>${statusBadge}${thr}${up}${lo}
        </div>`;
      },
    },

    series: seriesList,

    dataZoom: [{
      type: 'inside',
      xAxisIndex: 0,
      filterMode: 'filter',
      zoomOnMouseWheel: true,
      moveOnMouseMove: false,
      moveOnMouseWheel: false,
    }],
  };
});
</script>

<template>
  <div class="relative w-full h-full flex flex-col">
    <div v-if="!machineId || !field" class="flex items-center justify-center h-full text-xs text-gray-600">
      Configure machine &amp; field
    </div>

    <template v-else>
      <!-- ── Header ─────────────────────────────────────────────────────────── -->
      <div class="flex items-center justify-between px-1 pt-0.5 flex-shrink-0 gap-1">
        <!-- Status badge -->
        <span
          v-if="hasProblems"
          class="flex-shrink-0 flex items-center gap-1 px-1.5 py-0.5 rounded-full text-[9px] font-bold bg-red-500/20 text-red-400 border border-red-500/30 animate-pulse"
        >⚠ PROBLEM</span>
        <span
          v-else-if="lowerLimit !== null || upperLimit !== null"
          class="flex-shrink-0 flex items-center gap-1 px-1.5 py-0.5 rounded-full text-[9px] font-medium bg-emerald-500/15 text-emerald-400 border border-emerald-500/20"
        >✓ IN RANGE</span>
        <span v-else class="flex-shrink-0" />

        <!-- Zoom hints -->
        <div class="flex items-center gap-1.5 ml-auto">
          <span class="text-[9px] text-gray-600 hidden sm:block">scroll to zoom</span>
          <button
            v-if="isZoomed"
            class="flex items-center gap-0.5 px-1.5 py-0.5 rounded text-[9px] font-medium bg-blue-600/20 text-blue-400 border border-blue-500/30 hover:bg-blue-600/40 transition-colors"
            @click="resetZoom"
          >↺ reset</button>
        </div>
      </div>

      <!-- ── Time range buttons ────────────────────────────────────────────── -->
      <div class="flex items-center gap-0.5 px-1 pb-0.5 flex-shrink-0 flex-wrap">
        <button
          v-for="r in TIME_RANGES"
          :key="r"
          class="px-1.5 py-0.5 rounded text-[9px] font-medium transition-all"
          :class="selectedRange === r
            ? 'bg-blue-600 text-white shadow-sm shadow-blue-600/40 ring-1 ring-blue-500'
            : 'bg-surface-300 text-gray-400 hover:bg-surface-200 hover:text-gray-200'"
          @click="selectRange(r)"
        >{{ r }}</button>
      </div>

      <!-- ── Chart ─────────────────────────────────────────────────────────── -->
      <div class="relative flex-1 min-h-0">
        <div
          v-if="loading"
          class="absolute inset-0 z-10 flex items-center justify-center bg-surface-100/60 backdrop-blur-[1px]"
        >
          <div class="spinner" />
        </div>

        <VChart
          ref="chartRef"
          :option="option"
          :update-options="{ replaceMerge: ['series'] }"
          autoresize
          class="w-full h-full"
          @datazoom="onDataZoom"
        />

        <!-- Threshold legend -->
        <div
          v-if="threshold !== null || upperLimit !== null"
          class="absolute top-1 right-1 flex flex-col gap-0.5 text-[9px]"
        >
          <span v-if="upperLimit !== null" class="flex items-center gap-1 justify-end">
            <span class="text-amber-400">↑ upper {{ upperLimit }}</span>
            <span class="w-4 border-t border-dashed border-amber-400" />
          </span>
          <span v-if="threshold !== null" class="flex items-center gap-1 justify-end">
            <span class="text-indigo-400">◎ target {{ threshold }}</span>
            <span class="w-4 border-t border-dashed border-indigo-400" />
          </span>
          <span v-if="lowerLimit !== null" class="flex items-center gap-1 justify-end">
            <span class="text-amber-400">↓ lower {{ lowerLimit }}</span>
            <span class="w-4 border-t border-dashed border-amber-400" />
          </span>
        </div>
      </div>
    </template>
  </div>
</template>
