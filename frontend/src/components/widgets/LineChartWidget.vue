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

// ── Date/time range state ─────────────────────────────────────────────────────
function toDatetimeLocal(iso: string): string {
  const d = new Date(iso);
  const p = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`;
}

const startDateTime = ref(toDatetimeLocal(new Date(Date.now() - 86_400_000).toISOString())); // 24h ago
const endDateTime   = ref(toDatetimeLocal(new Date().toISOString()));                         // now

// Duration in ms — drives label format, shouldRotate, bucket label in tooltip
const rangeDurationMs = computed(() =>
  new Date(endDateTime.value).getTime() - new Date(startDateTime.value).getTime(),
);

// Bucket label shown in tooltip (mirrors backend calculateBucketForDuration)
const bucketLabel = computed(() => {
  const ms = rangeDurationMs.value;
  if (ms <=  1 * 60 * 60 * 1000) return '1 min';
  if (ms <=  6 * 60 * 60 * 1000) return '5 min';
  if (ms <= 24 * 60 * 60 * 1000) return '15 min';
  if (ms <= 15 * 24 * 60 * 60 * 1000) return '30 min';
  return '1 hr';
});

const { mergedData, loading } = useFieldSeries(machineId.value, field.value, startDateTime, endDateTime);

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

// ── Zoom state + Y-axis auto-scale ────────────────────────────────────────────
const chartRef = ref<InstanceType<typeof VChart> | null>(null);
const isZoomed = ref(false);

// Visible Y range — set when zoomed, cleared when not
const visibleYMin = ref<number | undefined>(undefined);
const visibleYMax = ref<number | undefined>(undefined);

function onDataZoom(e: any) {
  const batch = e?.batch?.[0] ?? e;
  const start = batch?.start ?? 0;
  const end   = batch?.end   ?? 100;
  isZoomed.value = !(start === 0 && end === 100);

  if (isZoomed.value) {
    const total    = mergedData.value.length;
    const startIdx = Math.floor(start / 100 * total);
    const endIdx   = Math.ceil(end   / 100 * total);
    const visible  = mergedData.value.slice(startIdx, endIdx);
    const values   = visible.map(p => p.value).filter(v => v != null);
    if (values.length) {
      const lo  = Math.min(...values);
      const hi  = Math.max(...values);
      const pad = Math.max((hi - lo) * 0.1, hi * 0.05, 10); // ≥10% of span, ≥5% of hi, ≥10 units
      visibleYMin.value = Math.floor(lo - pad);
      visibleYMax.value = Math.ceil(hi + pad);
    }
  } else {
    visibleYMin.value = undefined;
    visibleYMax.value = undefined;
  }
}

function resetZoom() {
  chartRef.value?.chart?.dispatchAction({ type: 'dataZoom', start: 0, end: 100 });
  isZoomed.value    = false;
  visibleYMin.value = undefined;
  visibleYMax.value = undefined;
}

// ── Tooltip timestamp (full date + time, all ranges) ─────────────────────────
function formatTooltipTime(ts: string): string {
  if (!ts) return '';
  const d = new Date(ts);
  return d.toLocaleString('en-US', {
    year: 'numeric', month: 'short', day: 'numeric',
    hour: '2-digit', minute: '2-digit', hour12: false,
  });
}

// ── X-axis label format ───────────────────────────────────────────────────────
function formatLabel(ts: string): string {
  const d  = new Date(ts);
  const ms = rangeDurationMs.value;
  if (ms >= 90 * 24 * 60 * 60 * 1000) // ≥3 months
    return d.toLocaleDateString('en-US', { year: '2-digit', month: 'short', day: 'numeric' });
  if (ms >= 7 * 24 * 60 * 60 * 1000) // ≥7 days
    return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
      + ' ' + d.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', hour12: false });
  return d.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', hour12: false });
}

const shouldRotate = computed(() => rangeDurationMs.value >= 7 * 24 * 60 * 60 * 1000);

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
          itemStyle: { color: 'rgba(16,185,129,0.12)', borderColor: 'rgba(16,185,129,0.4)', borderWidth: 1 },
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
          lineStyle: { color: '#f59e0b', type: 'solid' as const, width: 2, opacity: 0.8 },
          label: { formatter: `↑ ${upperLimit.value}`, color: '#f59e0b', fontSize: 10, fontWeight: 'bold' as const, position: 'end' as const },
        }] : []),
        ...(threshold.value !== null ? [{
          yAxis: threshold.value,
          lineStyle: { color: '#6366f1', type: 'dashed' as const, width: 1.5 },
          label: { formatter: `◎ ${threshold.value}`, color: '#6366f1', fontSize: 9, position: 'end' as const },
        }] : []),
        ...(lowerLimit.value !== null ? [{
          yAxis: lowerLimit.value,
          lineStyle: { color: '#f59e0b', type: 'solid' as const, width: 2, opacity: 0.8 },
          label: { formatter: `↓ ${lowerLimit.value}`, color: '#f59e0b', fontSize: 10, fontWeight: 'bold' as const, position: 'end' as const },
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
      scale: true,                   // don't force axis to start from 0
      min: visibleYMin.value,        // undefined = ECharts auto; set when zoomed
      max: visibleYMax.value,
      axisLabel: {
        color: '#6b7280',
        fontSize: 10,
        formatter: (val: number) => {
          const prec = machineField.value?.precision ?? 0;
          return prec > 0 ? val.toFixed(prec) : Math.round(val).toString();
        },
      },
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
          <span style="color:#9ca3af;font-size:11px">${formatTooltipTime(pt?.ts ?? '')}</span><span style="color:#6b7280;font-size:10px"> · avg/${bucketLabel.value}</span><br/>
          ${field.value}: <b>${val}</b>${statusBadge}${thr}${up}${lo}
        </div>`;
      },
    },

    series: seriesList,

    dataZoom: [{
      type: 'inside',
      xAxisIndex: 0,
      filterMode: 'filter',
      zoomOnMouseWheel: true,   // scroll = zoom
      moveOnMouseMove: true,    // drag on chart = pan (safe — GridStack only drags from .gs-drag-handle)
      moveOnMouseWheel: false,  // scroll does not pan
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
          <span class="text-[9px] text-gray-600 hidden sm:block">scroll: zoom · drag: pan</span>
          <button
            v-if="isZoomed"
            class="flex items-center gap-0.5 px-1.5 py-0.5 rounded text-[9px] font-medium bg-blue-600/20 text-blue-400 border border-blue-500/30 hover:bg-blue-600/40 transition-colors"
            @click="resetZoom"
          >↺ reset</button>
        </div>
      </div>

      <!-- ── Date/time range pickers ──────────────────────────────────────── -->
      <div class="flex items-center gap-1 px-1 pb-0.5 flex-shrink-0 flex-wrap text-[9px] text-gray-400">
        <span>From</span>
        <input
          v-model="startDateTime"
          type="datetime-local"
          class="bg-surface-300 text-gray-200 rounded px-1 py-0.5 border border-gray-700 focus:outline-none focus:border-blue-500 text-[9px]"
          style="color-scheme: dark"
        />
        <span>To</span>
        <input
          v-model="endDateTime"
          type="datetime-local"
          class="bg-surface-300 text-gray-200 rounded px-1 py-0.5 border border-gray-700 focus:outline-none focus:border-blue-500 text-[9px]"
          style="color-scheme: dark"
        />
      </div>

      <!-- ── Chart ─────────────────────────────────────────────────────────── -->
      <!-- @mousedown.stop prevents mousedown from bubbling to GridStack's drag handler -->
      <div class="relative flex-1 min-h-0" @mousedown.stop>
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
