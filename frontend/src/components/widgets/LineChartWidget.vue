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

// ── Time range selector (local state — does NOT unmount the chart) ─────────
const TIME_RANGES = ['5m', '30m', '1h', '6h', '24h', '7d'] as const;
const selectedRange = ref<string>((props.widget.config?.timeRange as string) ?? '1h');

// Pass as reactive ref so useFieldSeries watches it and reloads automatically
const { mergedData, loading } = useFieldSeries(machineId.value, field.value, selectedRange);

// ── Threshold / limits from machine_field table ────────────────────────────
const machineField = computed(() => props.widget.machine?.fields?.find(f => f.key === field.value));
const threshold    = computed(() => machineField.value?.threshold  ?? null);
const upperLimit   = computed(() => machineField.value?.upperLimit ?? null);
const lowerLimit   = computed(() => machineField.value?.lowerLimit ?? null);

// Check if any data point is out of range
const hasProblems = computed(() => {
  if (lowerLimit.value === null && upperLimit.value === null) return false;
  return mergedData.value.some(p => {
    if (upperLimit.value !== null && p.value > upperLimit.value) return true;
    if (lowerLimit.value !== null && p.value < lowerLimit.value) return true;
    return false;
  });
});

const option = computed<EChartsOption>(() => {
  const hasLimits = lowerLimit.value !== null || upperLimit.value !== null;

  return {
    backgroundColor: 'transparent',
    grid: { left: 42, right: 60, top: 16, bottom: 30, containLabel: false },

    // ── Color line red when outside bounds ───────────────────────────────
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
      data: mergedData.value.map(p => {
        const d = new Date(p.ts);
        if (['7d', '14d', '30d'].includes(selectedRange.value)) {
          return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
            + ' ' + d.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', hour12: false });
        }
        if (['6h', '24h'].includes(selectedRange.value)) {
          return d.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', hour12: false });
        }
        return d.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });
      }),
      axisLabel: {
        color: '#6b7280',
        fontSize: 10,
        interval: 'auto',
        rotate: ['7d', '14d', '30d'].includes(selectedRange.value) ? 25 : 0,
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
        const p = Array.isArray(params) ? params[0] : params;
        const val = p.value as number;
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

    series: [{
      type: 'line',
      data: mergedData.value.map(p => p.value),
      smooth: 0.3,
      symbol: 'none',
      lineStyle: { width: 2 },

      markArea: hasLimits && lowerLimit.value !== null && upperLimit.value !== null ? {
        silent: true,
        data: [[
          {
            yAxis: lowerLimit.value,
            itemStyle: { color: 'rgba(16, 185, 129, 0.07)', borderColor: 'rgba(16, 185, 129, 0.25)', borderWidth: 1 },
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
    }],
  };
});
</script>

<template>
  <div class="relative w-full h-full flex flex-col">
    <!-- Unconfigured -->
    <div v-if="!machineId || !field" class="flex items-center justify-center h-full text-xs text-gray-600">
      Configure machine &amp; field
    </div>

    <template v-else>
      <!-- ── Time range toggle + status ──────────────────────────────────── -->
      <div class="flex items-center justify-between px-1 pt-0.5 flex-shrink-0">
        <!-- Status badge -->
        <span
          v-if="hasProblems"
          class="flex items-center gap-1 px-1.5 py-0.5 rounded-full text-[9px] font-bold bg-red-500/20 text-red-400 border border-red-500/30 animate-pulse"
        >⚠ PROBLEM</span>
        <span
          v-else-if="lowerLimit !== null || upperLimit !== null"
          class="flex items-center gap-1 px-1.5 py-0.5 rounded-full text-[9px] font-medium bg-emerald-500/15 text-emerald-400 border border-emerald-500/20"
        >✓ IN RANGE</span>
        <span v-else />

        <!-- Range buttons -->
        <div class="flex gap-0.5">
          <button
            v-for="r in TIME_RANGES"
            :key="r"
            class="px-1.5 py-0.5 rounded text-[9px] font-medium transition-colors"
            :class="selectedRange === r
              ? 'bg-blue-600 text-white'
              : 'bg-surface-300 text-gray-400 hover:text-gray-200'"
            @click="selectedRange = r"
          >{{ r }}</button>
        </div>
      </div>

      <!-- ── Chart (always mounted — spinner is an overlay) ──────────────── -->
      <div class="relative flex-1 min-h-0">
        <!-- Loading overlay — VChart stays mounted so marks don't disappear -->
        <div
          v-if="loading"
          class="absolute inset-0 z-10 flex items-center justify-center bg-surface-100/60 backdrop-blur-[1px]"
        >
          <div class="spinner" />
        </div>

        <VChart :option="option" :update-options="{ replaceMerge: ['series'] }" autoresize class="w-full h-full" />

        <!-- Reference line legend (top-right corner) -->
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
