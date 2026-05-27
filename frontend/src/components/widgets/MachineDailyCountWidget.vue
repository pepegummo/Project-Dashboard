<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue';
import VChart from 'vue-echarts';
import type { EChartsOption } from 'echarts';
import type { DashboardWidget } from '@/types';
import { api } from '@/services/api.service';

// ── LED mode data shape ───────────────────────────────────────────────────────
export interface LedWeekDay {
  day:      string   // 'MON' | 'TUE' | 'WED' | 'THU' | 'FRI' | 'SAT' | 'SUN'
  count:    number
  isToday:  boolean
  isFuture: boolean
}

export interface LedDailyData {
  machineName: string
  todayCount:  number
  avgPerDay:   number
  weeklyData:  LedWeekDay[]
}

const props = defineProps<{
  widget:   DashboardWidget
  ledMode?: boolean       // defaults to false
  data?:    LedDailyData  // only required when ledMode = true
}>();

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

// ── LED mode computed values ──────────────────────────────────────────────────
const formattedCount = computed(() => props.data?.todayCount.toLocaleString() ?? '0')
const formattedAvg   = computed(() => props.data?.avgPerDay.toLocaleString()  ?? '0')
const maxCount       = computed(() =>
  Math.max(...(props.data?.weeklyData ?? []).map(d => d.count), 1)
)

function barHeight(item: LedWeekDay): number {
  if (item.isFuture || item.count === 0) return 4
  return Math.round((item.count / maxCount.value) * 60)
}

// ── ECharts option ────────────────────────────────────────────────────────────
const option = computed<EChartsOption>(() => {
  const labels = rows.value.map(r => fmtDate(r.date));
  const counts = rows.value.map(r => r.count);

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

    series: [{
      type: 'line',
      data: counts,
      smooth: true,
      symbol: 'circle',
      symbolSize: 5,
      showSymbol: false,        // only show dots on hover
      lineStyle: {
        color: '#3b82f6',
        width: 2,
      },
      itemStyle: {
        color: '#3b82f6',
        borderColor: '#1e40af',
        borderWidth: 2,
      },
      emphasis: {
        showSymbol: true,
        itemStyle: { color: '#60a5fa', borderColor: '#93c5fd', borderWidth: 2 },
      },
      areaStyle: {
        color: {
          type: 'linear',
          x: 0, y: 0, x2: 0, y2: 1,
          colorStops: [
            { offset: 0,   color: 'rgba(59,130,246,0.35)' },
            { offset: 0.6, color: 'rgba(59,130,246,0.08)' },
            { offset: 1,   color: 'rgba(59,130,246,0.00)' },
          ],
        },
      },
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
  <!-- ═══════════════════════════════════════════════════════════════════════ -->
  <!--  LED MODE                                                              -->
  <!-- ═══════════════════════════════════════════════════════════════════════ -->
  <div v-if="ledMode" class="led-daily-count">
    <!-- Left: big number readout -->
    <div class="led-left">
      <div class="led-label">{{ data?.machineName }} DAILY OUTPUT</div>
      <div class="led-value">{{ formattedCount }}</div>
      <div class="led-unit">PCS TODAY</div>
      <div class="led-sub">AVG/DAY · {{ formattedAvg }} PCS</div>
    </div>

    <!-- Right: 7-day bar chart -->
    <div class="led-right">
      <div class="led-bars">
        <div
          v-for="item in data?.weeklyData"
          :key="item.day"
          class="led-bar-col"
        >
          <div
            class="led-bar"
            :class="{
              'is-today':  item.isToday,
              'is-future': item.isFuture,
            }"
            :style="{ height: barHeight(item) + 'px' }"
          />
          <div class="led-day-label" :class="{ 'is-today': item.isToday }">
            {{ item.day }}
          </div>
        </div>
      </div>
    </div>
  </div>

  <!-- ═══════════════════════════════════════════════════════════════════════ -->
  <!--  NORMAL MODE  (unchanged)                                              -->
  <!-- ═══════════════════════════════════════════════════════════════════════ -->
  <div v-else class="flex flex-col h-full">
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

<style scoped>
@import url('https://fonts.googleapis.com/css2?family=Share+Tech+Mono&family=Orbitron:wght@700&display=swap');

.led-daily-count {
  display: flex;
  flex-direction: row;
  align-items: center;
  gap: 24px;
  background: #050f05;
  border: 1px solid #1a3a1a;
  border-radius: 2px;
  padding: 12px 16px;
  height: 100%;
  box-sizing: border-box;
}

.led-left {
  flex: 0 0 40%;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.led-label {
  font-family: 'Share Tech Mono', monospace;
  font-size: 11px;
  letter-spacing: 3px;
  color: #00cc66;
  opacity: 0.75;
  text-transform: uppercase;
}

.led-value {
  font-family: 'Orbitron', sans-serif;
  font-weight: 700;
  font-size: 48px;
  color: #00ff88;
  line-height: 1;
  text-shadow: 0 0 12px rgba(0, 255, 136, 0.5);
}

.led-unit {
  font-family: 'Share Tech Mono', monospace;
  font-size: 11px;
  letter-spacing: 2px;
  color: #00aa55;
  opacity: 0.7;
  text-transform: uppercase;
}

.led-sub {
  font-family: 'Share Tech Mono', monospace;
  font-size: 11px;
  letter-spacing: 1px;
  color: #00aa55;
  opacity: 0.5;
  margin-top: 6px;
}

.led-right {
  flex: 1;
}

.led-bars {
  display: flex;
  align-items: flex-end;
  gap: 4px;
  height: 76px;
}

.led-bar-col {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: flex-end;
  gap: 4px;
  height: 100%;
}

.led-bar {
  width: 100%;
  border-radius: 1px 1px 0 0;
  background: #00ff88;
  opacity: 0.3;
  min-height: 4px;
  transition: height 0.4s ease;
}

.led-bar.is-today {
  opacity: 1;
  box-shadow: 0 0 8px #00ff88;
}

.led-bar.is-future {
  background: #0d1a0d;
  opacity: 1;
}

.led-day-label {
  font-family: 'Share Tech Mono', monospace;
  font-size: 9px;
  letter-spacing: 1px;
  color: #00aa55;
  opacity: 0.5;
  text-transform: uppercase;
}

.led-day-label.is-today {
  color: #00ff88;
  opacity: 1;
}
</style>
