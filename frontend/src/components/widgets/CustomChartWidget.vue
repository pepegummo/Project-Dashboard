<script setup lang="ts">
import { ref, computed } from 'vue';
import VChart from 'vue-echarts';
import type { EChartsOption } from 'echarts';
import type { DashboardWidget } from '@/types';
import { useMultiFieldSeries } from '@/composables/useMultiFieldSeries';

const props = defineProps<{ widget: DashboardWidget }>();

const machineId = computed(() => props.widget.machineId ?? '');
const fields    = computed<string[]>(() => (props.widget.config?.fields as string[]) ?? []);
const chartType = computed<'line' | 'bar' | 'area'>(() => (props.widget.config?.chartType as any) ?? 'line');

// Window: exactly `points` buckets of `bucket` size (backend gap-fills empties).
const bucket = computed<string>(() => (props.widget.config?.bucket as string) ?? '1h');
const points = computed<number>(() => (props.widget.config?.points as number) ?? 20);

// Scaling mode for mixed-magnitude metrics. Back-compat: old normalize flag → 'normalized'.
const scaling = computed<'shared' | 'dual' | 'normalized'>(
  () => (props.widget.config?.scaling as any) ?? (props.widget.config?.normalize ? 'normalized' : 'shared'),
);

// Normalize each series to its own 0–100% range so metrics at very different
// scales (e.g. ~10 vs ~500) are shape-comparable. Real values stay in the tooltip.
function normalizeData(data: Array<number | null>): Array<number | null> {
  const vals = data.filter((v): v is number => v != null);
  if (vals.length === 0) return data;
  const min = Math.min(...vals);
  const span = Math.max(...vals) - min;
  return data.map(v => v == null ? null : span === 0 ? 0.5 : (v - min) / span);
}

// Default palette; overridable per-series via config.colors.
const PALETTE = ['#3b82f6', '#10b981', '#f59e0b', '#ef4444', '#8b5cf6', '#06b6d4', '#ec4899', '#84cc16'];
const colors = computed<string[]>(() => (props.widget.config?.colors as string[]) ?? PALETTE);

const { categories, series: rawSeries, loading } = useMultiFieldSeries(machineId.value, fields, bucket, points);

// key → label / unit from the machine's field schema, for legend/tooltip/axes.
function fieldLabel(key: string): string {
  return props.widget.machine?.fields?.find(f => f.key === key)?.label ?? key;
}
function fieldUnit(key: string): string {
  return props.widget.machine?.fields?.find(f => f.key === key)?.unit ?? '';
}

// Distinct units (first-appearance order) across the rendered series — drives dual axes.
const dualUnits = computed<string[]>(() => {
  const seen: string[] = [];
  for (const s of rawSeries.value) {
    const u = fieldUnit(s.field);
    if (!seen.includes(u)) seen.push(u);
  }
  return seen;
});

// 3+ fields can't be read on a shared/2-axis chart → always Normalized.
// For 1–2 fields: dual only works with exactly 2 units (1 → shared, else normalized).
const effectiveScaling = computed<'shared' | 'dual' | 'normalized'>(() => {
  if (rawSeries.value.length >= 3) return 'normalized';
  if (scaling.value !== 'dual') return scaling.value;
  if (dualUnits.value.length === 2) return 'dual';
  if (dualUnits.value.length > 2) return 'normalized';
  return 'shared';
});

// Note shown when the requested scaling was downgraded.
const scalingNote = computed<string>(() => {
  if (effectiveScaling.value === scaling.value) return '';
  if (rawSeries.value.length >= 3) return '3+ fields → normalized';
  return effectiveScaling.value === 'normalized' ? '3+ units → normalized' : 'same unit → shared axis';
});

function formatLabel(ts: string): string {
  const d = new Date(ts);
  if (isNaN(d.getTime())) return ts;
  return d.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', hour12: false });
}

const option = computed<EChartsOption>(() => ({
  backgroundColor: 'transparent',
  grid: { left: 42, right: 16, top: 32, bottom: 28, containLabel: false },
  legend: {
    top: 2,
    textStyle: { color: '#9ca3af', fontSize: 10 },
    icon: 'roundRect',
    itemWidth: 10,
    itemHeight: 10,
  },
  xAxis: {
    type: 'category',
    data: categories.value.map(formatLabel),
    axisLabel: { color: '#6b7280', fontSize: 10, interval: 'auto' },
    axisLine: { lineStyle: { color: '#374151' } },
    splitLine: { show: false },
  },
  yAxis: effectiveScaling.value === 'dual'
    ? [
        { type: 'value', scale: true, name: dualUnits.value[0], nameTextStyle: { color: '#6b7280', fontSize: 9 }, axisLabel: { color: '#6b7280', fontSize: 10 }, splitLine: { lineStyle: { color: '#1f2937', type: 'dashed' } } },
        { type: 'value', scale: true, position: 'right', name: dualUnits.value[1], nameTextStyle: { color: '#6b7280', fontSize: 9 }, axisLabel: { color: '#6b7280', fontSize: 10 }, splitLine: { show: false } },
      ]
    : {
        type: 'value',
        scale: effectiveScaling.value !== 'normalized',
        min: effectiveScaling.value === 'normalized' ? 0 : undefined,
        max: effectiveScaling.value === 'normalized' ? 1 : undefined,
        axisLabel: {
          color: '#6b7280',
          fontSize: 10,
          formatter: effectiveScaling.value === 'normalized' ? (v: number) => `${Math.round(v * 100)}%` : undefined,
        },
        splitLine: { lineStyle: { color: '#1f2937', type: 'dashed' } },
      },
  tooltip: {
    trigger: 'axis',
    backgroundColor: '#1e2130',
    borderColor: '#374151',
    textStyle: { color: '#e5e7eb', fontSize: 12 },
    // Always show the real (un-normalized) values from rawSeries.
    formatter: (params: any) => {
      const arr = Array.isArray(params) ? params : [params];
      const idx = arr[0]?.dataIndex ?? 0;
      const time = arr[0]?.axisValue ?? '';
      const rows = arr.map((p: any) => {
        const raw = rawSeries.value[p.seriesIndex]?.data[idx];
        return `${p.marker} ${p.seriesName}: <b>${raw ?? '–'}</b>`;
      }).join('<br/>');
      return `<div style="line-height:1.6"><span style="color:#9ca3af;font-size:11px">${time}</span><br/>${rows}</div>`;
    },
  },
  series: rawSeries.value.map((s, i) => ({
    name: fieldLabel(s.field),
    type: chartType.value === 'bar' ? 'bar' : 'line',
    data: effectiveScaling.value === 'normalized' ? normalizeData(s.data) : s.data,
    // dual: axis chosen by the field's unit (fields sharing a unit share an axis).
    yAxisIndex: effectiveScaling.value === 'dual' ? Math.max(0, dualUnits.value.indexOf(fieldUnit(s.field))) : 0,
    smooth: chartType.value === 'area' ? 0.3 : false,
    symbol: 'none',
    connectNulls: false,
    lineStyle: { width: 2 },
    areaStyle: chartType.value === 'area' ? { opacity: 0.18 } : undefined,
    itemStyle: { color: colors.value[i % colors.value.length] },
  })),
}));
</script>

<template>
  <div class="relative w-full h-full flex flex-col">
    <div v-if="!machineId || fields.length === 0" class="flex items-center justify-center h-full text-xs text-gray-600">
      Configure machine &amp; fields
    </div>

    <template v-else>
      <!-- Dual-axis fallback note -->
      <div v-if="scalingNote" class="flex justify-end px-1 flex-shrink-0">
        <span class="text-[9px] text-amber-400/80">{{ scalingNote }}</span>
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
          :option="option"
          :update-options="{ replaceMerge: ['series'] }"
          autoresize
          class="w-full h-full"
        />
      </div>
    </template>
  </div>
</template>
