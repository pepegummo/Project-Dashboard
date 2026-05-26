<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue';
import VChart from 'vue-echarts';
import type { EChartsOption } from 'echarts';
import type { DashboardWidget } from '@/types';
import { api } from '@/services/api.service';

const props = defineProps<{ widget: DashboardWidget }>();

const machineId   = computed(() => props.widget.machineId ?? '');
const machineName = computed(() => props.widget.machine?.name ?? '');

// User-selectable day range (from config default or 7)
const DAY_OPTIONS = [7, 14, 30] as const;
type DayOption = typeof DAY_OPTIONS[number];

const selectedDays = ref<DayOption>(
  (props.widget.config?.days as DayOption) ?? 7,
);

// ── Data fetching ─────────────────────────────────────────────────────────────
interface DayPoint { date: string; count: number }
const rows    = ref<DayPoint[]>([]);
const loading = ref(false);
const error   = ref<string | null>(null);

async function load() {
  if (!machineId.value) return;
  loading.value = true;
  error.value   = null;
  try {
    const result = await api.getTelemetryDailyCount(machineId.value, selectedDays.value);
    rows.value = (result?.data ?? []).map(r => ({
      date:  r.date,
      count: r.count,
    }));
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}

onMounted(load);
watch(selectedDays, load);

// ── Derived stats ─────────────────────────────────────────────────────────────
const totalCount = computed(() => rows.value.reduce((s, r) => s + r.count, 0));
const avgPerDay  = computed(() =>
  rows.value.length ? Math.round(totalCount.value / rows.value.length) : 0,
);
const peakDay = computed(() => {
  if (!rows.value.length) return null;
  return rows.value.reduce((a, b) => (a.count >= b.count ? a : b));
});

// ── Format helpers ────────────────────────────────────────────────────────────
function fmtDate(iso: string) {
  const d = new Date(iso);
  return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
}

function fmtCount(n: number) {
  return n >= 1_000_000
    ? `${(n / 1_000_000).toFixed(1)}M`
    : n >= 1_000
    ? `${(n / 1_000).toFixed(1)}k`
    : String(n);
}

// ── ECharts option ────────────────────────────────────────────────────────────
const option = computed<EChartsOption>(() => {
  const labels = rows.value.map(r => fmtDate(r.date));
  const counts = rows.value.map(r => r.count);
  const maxVal = Math.max(...counts, 1);

  return {
    backgroundColor: 'transparent',
    grid: { left: 48, right: 12, top: 16, bottom: 28, containLabel: false },

    xAxis: {
      type: 'category',
      data: labels,
      axisLabel: {
        color: '#6b7280',
        fontSize: 9,
        interval: 'auto',
        rotate: labels.length > 14 ? 30 : 0,
      },
      axisLine: { lineStyle: { color: '#374151' } },
      splitLine: { show: false },
    },

    yAxis: {
      type: 'value',
      axisLabel: {
        color: '#6b7280',
        fontSize: 9,
        formatter: (v: number) => fmtCount(v),
      },
      splitLine: { lineStyle: { color: '#1f2937', type: 'dashed' } },
      min: 0,
    },

    tooltip: {
      trigger: 'axis',
      backgroundColor: '#1e2130',
      borderColor: '#374151',
      textStyle: { color: '#e5e7eb', fontSize: 12 },
      formatter: (params: any) => {
        const p = Array.isArray(params) ? params[0] : params;
        return `<div style="font-family:monospace;line-height:1.6">
          ${p.name}<br/>
          Readings: <b>${(p.value as number).toLocaleString()}</b>
        </div>`;
      },
    },

    visualMap: [{
      show: false,
      type: 'continuous',
      min: 0,
      max: maxVal,
      inRange: {
        color: ['#1d4ed8', '#3b82f6', '#60a5fa'],
      },
      seriesIndex: 0,
      dimension: 1,
    }],

    series: [{
      type: 'bar',
      data: counts,
      barMaxWidth: 32,
      itemStyle: {
        borderRadius: [3, 3, 0, 0],
      },
      emphasis: {
        itemStyle: { color: '#60a5fa' },
      },
      // Area-style line overlay for trend
      markLine: {
        silent: true,
        symbol: 'none',
        animation: false,
        data: avgPerDay.value > 0 ? [{
          yAxis: avgPerDay.value,
          lineStyle: { color: '#6366f1', type: 'dashed', width: 1 },
          label: {
            formatter: `avg ${fmtCount(avgPerDay.value)}`,
            color: '#6366f1',
            fontSize: 9,
            position: 'end',
          },
        }] : [],
      },
    }],
  };
});
</script>

<template>
  <div class="flex flex-col h-full">
    <!-- Unconfigured state -->
    <div
      v-if="!machineId"
      class="flex items-center justify-center h-full text-xs text-gray-600"
    >
      Configure machine
    </div>

    <template v-else>
      <!-- Day-range toggle + summary row -->
      <div class="flex items-center justify-between px-1 pt-0.5 pb-1 flex-shrink-0">
        <!-- Stats -->
        <div class="flex gap-3 text-[10px] text-gray-500">
          <span>
            <span class="text-gray-400 font-mono">{{ fmtCount(totalCount) }}</span>
            <span class="ml-1">total</span>
          </span>
          <span>
            <span class="text-indigo-400 font-mono">{{ fmtCount(avgPerDay) }}</span>
            <span class="ml-1">avg/day</span>
          </span>
          <span v-if="peakDay">
            <span class="text-blue-400 font-mono">{{ fmtCount(peakDay.count) }}</span>
            <span class="ml-1">peak</span>
          </span>
        </div>

        <!-- Day selector -->
        <div class="flex gap-0.5">
          <button
            v-for="d in DAY_OPTIONS"
            :key="d"
            class="px-1.5 py-0.5 rounded text-[9px] font-medium transition-colors"
            :class="selectedDays === d
              ? 'bg-blue-600 text-white'
              : 'bg-surface-300 text-gray-400 hover:text-gray-200'"
            @click="selectedDays = d"
          >
            {{ d }}d
          </button>
        </div>
      </div>

      <!-- Loading -->
      <div v-if="loading" class="flex-1 flex items-center justify-center">
        <div class="spinner" />
      </div>

      <!-- Error -->
      <div v-else-if="error" class="flex-1 flex items-center justify-center text-xs text-red-400 px-3 text-center">
        {{ error }}
      </div>

      <!-- Empty -->
      <div
        v-else-if="!rows.length"
        class="flex-1 flex items-center justify-center text-xs text-gray-600"
      >
        No data for the last {{ selectedDays }} days
      </div>

      <!-- Chart -->
      <div v-else class="flex-1 min-h-0">
        <VChart :option="option" autoresize class="w-full h-full" />
      </div>
    </template>
  </div>
</template>
