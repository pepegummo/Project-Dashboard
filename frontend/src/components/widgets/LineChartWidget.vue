<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted, type Ref } from 'vue';
import VChart from 'vue-echarts';
import type { EChartsOption } from 'echarts';
import type { DashboardWidget } from '@/types';
import { useFieldSeries } from '@/composables/useTelemetry';
import { useLiveFieldSeries } from '@/composables/useLiveFieldSeries';
import { useWidgetViewStateStore } from '@/stores/widget-view-state.store';

const props = defineProps<{ widget: DashboardWidget }>();

const machineId = computed(() => props.widget.machineId ?? '');
const field     = computed(() => (props.widget.config?.field as string) ?? '');
const color     = computed(() => (props.widget.config?.color as string) ?? '#3b82f6');

const widgetViewStateStore = useWidgetViewStateStore();

// ─────────────────────────────────────────────────────────────────────────────
// Date/time helpers
// ─────────────────────────────────────────────────────────────────────────────
function toDatetimeLocal(iso: string): string {
  const d = new Date(iso);
  const p = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`;
}

const storageKeyStart = `widget_dt_start_${props.widget.id}`;
const storageKeyEnd   = `widget_dt_end_${props.widget.id}`;

const startDateTime = ref(
  props.widget.config?.startDateTime
    ?? localStorage.getItem(storageKeyStart)
    ?? toDatetimeLocal(new Date(Date.now() - 86_400_000).toISOString()),
);

const endDateTime = ref(
  props.widget.config?.endDateTime
    ?? localStorage.getItem(storageKeyEnd)
    ?? toDatetimeLocal(new Date().toISOString()),
);

watch(startDateTime, v => {
  localStorage.setItem(storageKeyStart, v);
  widgetViewStateStore.setDatetime(props.widget.id, v, endDateTime.value);
});

watch(endDateTime, v => {
  localStorage.setItem(storageKeyEnd, v);
  widgetViewStateStore.setDatetime(props.widget.id, startDateTime.value, v);
});

// invalidRange drives the warning. Native min/max on the inputs blocks bad dates,
// but not times on the same day (the native picker can't constrain time), so a
// user can still pick start >= end — show a warning rather than silently reverting.
// String compare is chronological for the fixed-width "YYYY-MM-DDTHH:mm" format.
const invalidRange = computed(() =>
  !liveMode.value && !!startDateTime.value && !!endDateTime.value && startDateTime.value >= endDateTime.value,
);

// ── Split date/time controls ─────────────────────────────────────────────────
// Native datetime-local shows locale 12h time (midnight = "12 AM"). Use a native
// date input + custom 24h hour/minute <select>s (00–23 / 00–59) instead.
// startDateTime/endDateTime stay canonical; these read/write their date/hour/minute
// parts. Same-day overlaps aren't blocked here — invalidRange flags them.
const HOURS = Array.from({ length: 24 }, (_, i) => String(i).padStart(2, '0'));
const MINUTES = Array.from({ length: 60 }, (_, i) => String(i).padStart(2, '0'));

function dtParts(dt: string) {
  const [date, time = '00:00'] = dt.split('T');
  const [h = '00', m = '00'] = time.split(':');
  return { date, h, m };
}
const splitField = (r: Ref<string>, key: 'date' | 'h' | 'm') => computed({
  get: () => dtParts(r.value)[key],
  set: (v: string) => { const p = dtParts(r.value); p[key] = v; r.value = `${p.date}T${p.h}:${p.m}`; },
});
const startDate = splitField(startDateTime, 'date');
const startHour = splitField(startDateTime, 'h');
const startMin  = splitField(startDateTime, 'm');
const endDate = splitField(endDateTime, 'date');
const endHour = splitField(endDateTime, 'h');
const endMin  = splitField(endDateTime, 'm');

onMounted(() => {
  widgetViewStateStore.setDatetime(props.widget.id, startDateTime.value, endDateTime.value);
});

onUnmounted(() => {
  widgetViewStateStore.remove(props.widget.id);
});

// ─────────────────────────────────────────────────────────────────────────────
// Duration
// ─────────────────────────────────────────────────────────────────────────────
const rangeDurationMs = computed(() => {
  if (liveMode.value) return 30 * 60 * 1000; // live mode = 30-min window → time-only axis labels
  return new Date(endDateTime.value).getTime() - new Date(startDateTime.value).getTime();
});

const bucketLabel = computed(() => {
  if (liveMode.value) return '1 min';
  const ms = rangeDurationMs.value;
  if (ms <=  1 * 60 * 60 * 1000) return '1 min';
  if (ms <=  6 * 60 * 60 * 1000) return '5 min';
  if (ms <= 24 * 60 * 60 * 1000) return '15 min';
  if (ms <= 15 * 24 * 60 * 60 * 1000) return '30 min';
  return '1 hr';
});

// ─────────────────────────────────────────────────────────────────────────────
// Data — historical (REST) and live (REST seed + WS append)
// ─────────────────────────────────────────────────────────────────────────────
const liveMode = ref((props.widget.config?.liveMode as boolean) ?? false);

const { mergedData, loading: histLoading } = useFieldSeries(
  machineId.value,
  field.value,
  startDateTime,
  endDateTime,
);

const { liveData, loading: liveLoading } = useLiveFieldSeries(machineId, field);

const chartData = computed<Array<{ ts: string; value: number; min?: number; max?: number }>>(
  () => liveMode.value ? liveData.value : mergedData.value,
);
const loading = computed<boolean>(() => liveMode.value ? liveLoading.value : histLoading.value);

// Publish the on-screen series (compacted, last ~48 points) so the AI can read
// exactly what this chart shows when it's focused — no extra fetch. Same
// columns/data shape the backend uses in compactSeriesResult.
watch(chartData, (pts) => {
  const recent = pts.slice(-48);
  const hasBand = recent.some(p => p.min != null || p.max != null);
  widgetViewStateStore.setSeries(props.widget.id, {
    columns: hasBand ? ['time', 'value', 'min', 'max'] : ['time', 'value'],
    data: recent.map(p => hasBand
      ? [p.ts, p.value, p.min ?? p.value, p.max ?? p.value]
      : [p.ts, p.value]),
  });
}, { immediate: true });

// ─────────────────────────────────────────────────────────────────────────────
// Limits / Thresholds
// ─────────────────────────────────────────────────────────────────────────────
const machineField = computed(() =>
  props.widget.machine?.fields?.find(f => f.key === field.value),
);

const threshold  = computed(() => machineField.value?.threshold  ?? null);
const upperLimit = computed(() => machineField.value?.upperLimit ?? null);
const lowerLimit = computed(() => machineField.value?.lowerLimit ?? null);

const hasProblems = computed(() => {
  if (lowerLimit.value === null && upperLimit.value === null) return false;
  return chartData.value.some(p => {
    if (upperLimit.value !== null && p.value > upperLimit.value) return true;
    if (lowerLimit.value !== null && p.value < lowerLimit.value) return true;
    return false;
  });
});

// ─────────────────────────────────────────────────────────────────────────────
// Chart refs / zoom state
// ─────────────────────────────────────────────────────────────────────────────
const chartRef = ref<InstanceType<typeof VChart> | null>(null);
const isZoomed  = ref(false);
const zoomStart = ref(0);
const zoomEnd   = ref(100);

const visibleYMin = ref<number | undefined>(undefined);
const visibleYMax = ref<number | undefined>(undefined);

// ─────────────────────────────────────────────────────────────────────────────
// Zoom handling (THROTTLED)
// ─────────────────────────────────────────────────────────────────────────────
let zoomTimer: number | null = null;

function handleZoom(e: any) {
  const batch = e?.batch?.[0] ?? e;
  const start = batch?.start ?? 0;
  const end   = batch?.end   ?? 100;
  zoomStart.value = start;
  zoomEnd.value   = end;
  isZoomed.value  = !(start === 0 && end === 100);

  if (!isZoomed.value) {
    visibleYMin.value = undefined;
    visibleYMax.value = undefined;
    chartRef.value?.chart?.setOption({ yAxis: { min: null, max: null } });
    return;
  }

  const total    = chartData.value.length;
  const startIdx = Math.floor(start / 100 * total);
  const endIdx   = Math.ceil(end   / 100 * total);
  const visible  = chartData.value.slice(startIdx, endIdx);
  const values   = visible.map(p => p.value).filter(v => v != null);
  if (!values.length) return;

  const lo  = Math.min(...values);
  const hi  = Math.max(...values);
  const pad = Math.max((hi - lo) * 0.1, hi * 0.05, 10);

  visibleYMin.value = Math.floor(lo - pad);
  visibleYMax.value = Math.ceil(hi + pad);

  chartRef.value?.chart?.setOption({
    yAxis: { min: visibleYMin.value, max: visibleYMax.value },
  });
}

function onDataZoom(e: any) {
  if (zoomTimer) clearTimeout(zoomTimer);
  zoomTimer = window.setTimeout(() => handleZoom(e), 100);
}

function resetZoom() {
  zoomStart.value   = 0;
  zoomEnd.value     = 100;
  chartRef.value?.chart?.dispatchAction({ type: 'dataZoom', start: 0, end: 100 });
  isZoomed.value    = false;
  visibleYMin.value = undefined;
  visibleYMax.value = undefined;
  chartRef.value?.chart?.setOption({ yAxis: { min: null, max: null } });
}

// Clear zoom state when entering live mode (streaming data breaks any fixed zoom window)
watch(liveMode, (isLive) => { if (isLive) resetZoom(); });

// ─────────────────────────────────────────────────────────────────────────────
// Time formatting
// ─────────────────────────────────────────────────────────────────────────────
function formatTooltipTime(ts: string): string {
  if (!ts) return '';
  const d = new Date(ts);
  return d.toLocaleString('en-US', {
    year: 'numeric', month: 'short', day: 'numeric',
    hour: '2-digit', minute: '2-digit', hour12: false,
  });
}

function formatLabel(ts: string): string {
  const d  = new Date(ts);
  const ms = rangeDurationMs.value;
  if (ms >= 90 * 24 * 60 * 60 * 1000)
    return d.toLocaleDateString('en-US', { year: '2-digit', month: 'short', day: 'numeric' });
  if (ms >= 7 * 24 * 60 * 60 * 1000)
    return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
      + ' ' + d.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', hour12: false });
  return d.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', hour12: false });
}

const shouldRotate = computed(() => !liveMode.value && rangeDurationMs.value >= 7 * 24 * 60 * 60 * 1000);

// ─────────────────────────────────────────────────────────────────────────────
// ECharts option
// ─────────────────────────────────────────────────────────────────────────────
const option = computed<EChartsOption>(() => {
  const hasLimits = lowerLimit.value !== null || upperLimit.value !== null;

  const avgSeries: any = {
    type: 'line',
    name: field.value,
    data: chartData.value.map(p => p.value),
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
  };

  // Separate series for markLine so visualMap on seriesIndex:0 never overrides line colours.
  const refSeries: any = {
    type: 'line',
    data: new Array(chartData.value.length).fill(null),
    lineStyle: { width: 0 },
    symbol: 'none',
    silent: true,
    tooltip: { show: false },
    z: 10,
    markLine: {
      silent: true,
      symbol: 'none',
      animation: false,
      data: [
        ...(upperLimit.value !== null ? [{
          yAxis: upperLimit.value,
          lineStyle: { color: '#f59e0b', type: 'solid' as const, width: 2, opacity: 0.4 },
          label: { formatter: `↑ ${upperLimit.value}`, color: '#f59e0b', fontSize: 10, fontWeight: 'bold' as const, position: 'end' as const },
        }] : []),
        ...(threshold.value !== null ? [{
          yAxis: threshold.value,
          lineStyle: { color: '#6366f1', type: 'dashed' as const, width: 1.5 },
          label: { formatter: `◎ ${threshold.value}`, color: '#6366f1', fontSize: 9, position: 'end' as const },
        }] : []),
        ...(lowerLimit.value !== null ? [{
          yAxis: lowerLimit.value,
          lineStyle: { color: '#f59e0b', type: 'solid' as const, width: 2, opacity: 0.4 },
          label: { formatter: `↓ ${lowerLimit.value}`, color: '#f59e0b', fontSize: 10, fontWeight: 'bold' as const, position: 'end' as const },
        }] : []),
      ],
    },
  };

  return {
    backgroundColor: 'transparent',
    grid: { left: 42, right: 60, top: 16, bottom: shouldRotate.value ? 52 : 30, containLabel: false },

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
      data: chartData.value.map(p => formatLabel(p.ts)),
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
      scale: true,
      min: isZoomed.value
        ? visibleYMin.value
        : (hasLimits && lowerLimit.value !== null)
          ? (v: { min: number }) => Math.floor(Math.min(v.min, lowerLimit.value!) * 0.98)
          : undefined,
      max: isZoomed.value
        ? visibleYMax.value
        : (hasLimits && upperLimit.value !== null)
          ? (v: { max: number }) => Math.ceil(Math.max(v.max, upperLimit.value!) * 1.02)
          : undefined,
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
        const p = Array.isArray(params) ? params[0] : params;
        const idx = p.dataIndex as number;
        const val = p.value as number;
        const pt  = chartData.value[idx];

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

    series: [avgSeries, refSeries],

    dataZoom: liveMode.value ? [] : [{
      type: 'inside',
      xAxisIndex: 0,
      filterMode: 'filter',
      zoomOnMouseWheel: true,
      moveOnMouseMove: true,
      moveOnMouseWheel: false,
      minValueSpan: 2,
      throttle: 60,
      start: zoomStart.value,
      end: zoomEnd.value,
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
      <!-- Header -->
      <div class="flex items-center justify-between px-1 pt-0.5 flex-shrink-0 gap-1">
        <span
          v-if="hasProblems"
          class="flex-shrink-0 flex items-center gap-1 px-1.5 py-0.5 rounded-full text-[9px] font-bold bg-red-500/20 text-red-400 border border-red-500/30 animate-pulse"
        >⚠ PROBLEM</span>
        <span
          v-else-if="lowerLimit !== null || upperLimit !== null"
          class="flex-shrink-0 flex items-center gap-1 px-1.5 py-0.5 rounded-full text-[9px] font-medium bg-emerald-500/15 text-emerald-400 border border-emerald-500/20"
        >✓ IN RANGE</span>
        <span v-else class="flex-shrink-0" />

        <div class="flex items-center gap-1.5 ml-auto">
          <span class="text-[9px] text-gray-600 hidden sm:block">scroll: zoom · drag: pan</span>
          <button
            v-if="isZoomed"
            class="flex items-center gap-0.5 px-1.5 py-0.5 rounded text-[9px] font-medium bg-blue-600/20 text-blue-400 border border-blue-500/30 hover:bg-blue-600/40 transition-colors"
            @click="resetZoom"
          >↺ reset</button>
        </div>
      </div>

      <!-- Date range / Live toggle row -->
      <div class="flex items-center gap-1 px-1 pb-0.5 flex-shrink-0 flex-wrap text-[9px] text-gray-400">
        <!-- Historical date picker (native) + 24h time selects (custom) -->
        <template v-if="!liveMode">
          <span>From</span>
          <input
            v-model="startDate"
            type="date"
            :max="endDate"
            class="bg-surface-300 text-gray-200 rounded px-1 py-0.5 border border-gray-700 focus:outline-none focus:border-blue-500 text-[9px]"
            style="color-scheme: dark"
          />
          <select v-model="startHour" class="bg-surface-300 text-gray-200 rounded px-1 py-0.5 border border-gray-700 focus:outline-none focus:border-blue-500 text-[9px]">
            <option v-for="h in HOURS" :key="h" :value="h">{{ h }}</option>
          </select>
          <span>:</span>
          <select v-model="startMin" class="bg-surface-300 text-gray-200 rounded px-1 py-0.5 border border-gray-700 focus:outline-none focus:border-blue-500 text-[9px]">
            <option v-for="m in MINUTES" :key="m" :value="m">{{ m }}</option>
          </select>
          <span>To</span>
          <input
            v-model="endDate"
            type="date"
            :min="startDate"
            class="bg-surface-300 text-gray-200 rounded px-1 py-0.5 border border-gray-700 focus:outline-none focus:border-blue-500 text-[9px]"
            style="color-scheme: dark"
          />
          <select v-model="endHour" class="bg-surface-300 text-gray-200 rounded px-1 py-0.5 border border-gray-700 focus:outline-none focus:border-blue-500 text-[9px]">
            <option v-for="h in HOURS" :key="h" :value="h">{{ h }}</option>
          </select>
          <span>:</span>
          <select v-model="endMin" class="bg-surface-300 text-gray-200 rounded px-1 py-0.5 border border-gray-700 focus:outline-none focus:border-blue-500 text-[9px]">
            <option v-for="m in MINUTES" :key="m" :value="m">{{ m }}</option>
          </select>
          <span
            v-if="invalidRange"
            class="flex items-center gap-1 px-1.5 py-0.5 rounded text-red-400 font-medium bg-red-500/15 border border-red-500/30"
          >⚠ Start must be before End</span>
        </template>

        <!-- Live mode badge -->
        <span
          v-else
          class="flex items-center gap-1 px-1.5 py-0.5 rounded-full font-bold bg-emerald-500/15 text-emerald-400 border border-emerald-500/20"
        >
          <span class="w-1.5 h-1.5 rounded-full bg-emerald-400 animate-pulse inline-block" />
          LIVE · 30 min
        </span>

        <!-- Toggle button (always visible) -->
        <button
          class="ml-auto px-1.5 py-0.5 rounded font-medium border transition-colors"
          :class="liveMode
            ? 'bg-emerald-600/20 text-emerald-400 border-emerald-500/30 hover:bg-emerald-600/30'
            : 'bg-surface-300 text-gray-400 border-gray-700 hover:text-emerald-400 hover:border-emerald-500/30'"
          @click="liveMode = !liveMode"
        >
          {{ liveMode ? 'Exit Live' : '⊙ Live' }}
        </button>
      </div>

      <!-- Chart -->
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
