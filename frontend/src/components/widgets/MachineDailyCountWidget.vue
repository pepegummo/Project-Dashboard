<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted } from 'vue';
import VChart from 'vue-echarts';
import type { EChartsOption } from 'echarts';
import type { DashboardWidget } from '@/types';
import { api } from '@/services/api.service';
import { wsService } from '@/services/ws.service';

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
const rows         = ref<DayPoint[]>([]);
const loading      = ref(false);
const error        = ref<string | null>(null);
const allTimeTotal = ref<number | null>(null);
const allTimeSince = ref<string | null>(null);

async function load() {
  if (!machineId.value) return;
  loading.value = true;
  error.value   = null;
  try {
    const [rangeResult, totalResult] = await Promise.all([
      api.getTelemetryDailyCount(machineId.value, selectedDays.value),
      api.getTotalCount(machineId.value),
    ]);
    rows.value = (rangeResult?.data ?? []).map(r => ({
      date:  r.date,
      count: r.count,
    }));
    allTimeTotal.value = totalResult.total;
    allTimeSince.value = totalResult.since;
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}

onMounted(load);
watch(selectedDays, load);

// ── Live mode — 30-minute rolling window ─────────────────────────────────────
const liveMode    = ref(false);
const liveLoading = ref(false);
const livePoints  = ref<Array<{ ts: string; count: number }>>([]);
const LIVE_WINDOW_MS = 30 * 60 * 1000;

let offTelemetry: (() => void) | null = null;
let liveTimer: ReturnType<typeof setInterval> | null = null;

async function loadLiveSeed() {
  const seedField = props.widget.machine?.fields?.[0]?.key;
  if (!machineId.value || !seedField) return;
  liveLoading.value = true;
  try {
    const series = await api.getTelemetrySeries(machineId.value, seedField, { timeRange: '30m' });
    livePoints.value = (series?.data ?? []).map((p: any) => ({
      ts:    p.bucket ?? p.ts,
      count: (p.avg ?? p.value) != null ? 1 : 0,
    }));
  } catch { /* ok */ }
  finally { liveLoading.value = false; }
}

function enterLive() {
  if (!machineId.value) return;
  livePoints.value = [];
  loadLiveSeed();
  wsService.subscribe([machineId.value]);
  offTelemetry = wsService.onTelemetry(machineId.value, (payload) => {
    const d = new Date(payload.timestamp);
    d.setSeconds(0, 0);
    const bucket = d.toISOString();
    const cutoff = Date.now() - LIVE_WINDOW_MS;
    const existing = livePoints.value.find(p => p.ts === bucket);
    if (existing) {
      existing.count++;
    } else {
      livePoints.value.push({ ts: bucket, count: 1 });
    }
    livePoints.value = livePoints.value.filter(p => new Date(p.ts).getTime() > cutoff);
  });
  liveTimer = setInterval(loadLiveSeed, 5 * 60_000);
}

function exitLive() {
  offTelemetry?.(); offTelemetry = null;
  if (liveTimer) { clearInterval(liveTimer); liveTimer = null; }
  if (machineId.value) wsService.unsubscribe([machineId.value]);
  livePoints.value = [];
}

watch(liveMode, isLive => { if (isLive) enterLive(); else exitLive(); });
onUnmounted(exitLive);

// ── Sample data (Adjusted to match 8.9k total, 1.3k avg, 1.8k peak) ───────────
const SAMPLE_COUNTS = [1200, 1300, 1400, 1800, 1100, 1200, 900];

const mockRows = computed<DayPoint[]>(() => {
  const today = new Date('2026-05-29T00:00:00Z'); // Fixed to match the design context
  return SAMPLE_COUNTS.map((count, i) => {
    const d = new Date(today);
    d.setDate(today.getDate() - (SAMPLE_COUNTS.length - 1 - i));
    return { date: d.toISOString(), count };
  });
});

const isMockData  = computed(() => !loading.value && rows.value.length === 0);
const displayRows = computed(() => isMockData.value ? mockRows.value : rows.value);

// ── Cumulative Calculation ────────────────────────────────────────────────────
const cumulativeRows = computed(() => {
  let runningTotal = 0;
  return displayRows.value.map(r => {
    runningTotal += r.count;
    return {
      date: r.date,
      dailyCount: r.count,
      cumulative: runningTotal
    };
  });
});

// ── Derived stats ─────────────────────────────────────────────────────────────
const totalCount = computed(() => cumulativeRows.value.length ? cumulativeRows.value[cumulativeRows.value.length - 1].cumulative : 0);
const avgPerDay  = computed(() =>
  displayRows.value.length ? Math.round(totalCount.value / displayRows.value.length) : 0,
);
const peakDay = computed(() => {
  if (!displayRows.value.length) return null;
  return displayRows.value.reduce((a, b) => (a.count >= b.count ? a : b));
});

// ── Format helpers ────────────────────────────────────────────────────────────
function fmtDate(iso: string) {
  const d = new Date(iso);
  return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
}

function fmtSince(iso: string | null) {
  if (!iso) return '';
  return new Date(iso).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' });
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

// ── ECharts option (Cumulative Area Chart) ────────────────────────────────────
const option = computed<EChartsOption>(() => {
  const labels = cumulativeRows.value.map(r => fmtDate(r.date));
  const cumulativeData = cumulativeRows.value.map(r => r.cumulative);
  const finalValue = cumulativeData[cumulativeData.length - 1] || 0;

  return {
    backgroundColor: 'transparent',
    grid: { left: 60, right: 40, top: 24, bottom: 28, containLabel: false },

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
      name: 'Cumulative Production (Units)',
      nameLocation: 'middle',
      nameGap: 40,
      nameTextStyle: {
        color: '#9ca3af',
        fontSize: 10,
      },
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
        const dataIndex = p.dataIndex;
        const dailyVal = cumulativeRows.value[dataIndex].dailyCount;
        return `<div style="font-family:monospace;line-height:1.6">
          ${p.name}<br/>
          Cumulative: <b style="color:#60a5fa">${fmtCount(p.value as number)}</b><br/>
          Daily Output: <b>${dailyVal.toLocaleString()}</b>
        </div>`;
      },
    },

    series: [{
      type: 'line',
      data: cumulativeData,
      smooth: true,
      symbol: 'circle',
      symbolSize: 5,
      showSymbol: false, 
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
            { offset: 0,   color: 'rgba(59,130,246,0.6)' },
            { offset: 1,   color: 'rgba(59,130,246,0.05)' },
          ],
        },
      },
      markPoint: {
        data: [
          {
            name: 'Total',
            coord: [labels.length - 1, finalValue],
            value: `Total\n${fmtCount(finalValue)}`,
            symbol: 'circle',
            symbolSize: 1,
            label: {
              show: true,
              position: 'right',
              color: '#60a5fa',
              fontSize: 10,
              lineHeight: 12,
              distance: 5
            }
          }
        ]
      },
      markLine: {
        silent: true,
        symbol: 'none',
        animation: false,
        data: avgPerDay.value > 0 ? [{
          yAxis: avgPerDay.value,
          lineStyle: { color: '#6b7280', type: 'dashed', width: 1 },
          label: {
            formatter: `avg`,
            color: '#9ca3af',
            fontSize: 9,
            position: 'end',
          },
        }] : [],
      },
    }],
  };
});

// ── Live chart option (30-min rolling bar chart) ──────────────────────────────
const liveOption = computed<EChartsOption>(() => {
  const labels = livePoints.value.map(p => {
    const d = new Date(p.ts);
    return d.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', hour12: false });
  });
  const counts = livePoints.value.map(p => p.count);

  return {
    backgroundColor: 'transparent',
    grid: { left: 36, right: 16, top: 16, bottom: 28, containLabel: false },

    xAxis: {
      type: 'category',
      data: labels,
      axisLabel: { color: '#6b7280', fontSize: 9, interval: 'auto' },
      axisLine: { lineStyle: { color: '#374151' } },
      splitLine: { show: false },
    },

    yAxis: {
      type: 'value',
      minInterval: 1,
      axisLabel: { color: '#6b7280', fontSize: 9 },
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
          ${p.name}<br/>Events: <b style="color:#34d399">${p.value}</b>
        </div>`;
      },
    },

    series: [{
      type: 'bar',
      data: counts,
      itemStyle: {
        color: {
          type: 'linear', x: 0, y: 0, x2: 0, y2: 1,
          colorStops: [
            { offset: 0, color: 'rgba(52,211,153,0.9)' },
            { offset: 1, color: 'rgba(52,211,153,0.3)' },
          ],
        },
        borderRadius: [2, 2, 0, 0],
      },
      emphasis: { itemStyle: { color: '#6ee7b7' } },
    }],
  };
});
</script>

<template>
  <div v-if="ledMode" class="led-daily-count">
    <div class="led-left">
      <div class="led-label">{{ data?.machineName }} DAILY OUTPUT</div>
      <div class="led-value">{{ formattedCount }}</div>
      <div class="led-unit">PCS TODAY</div>
      <div class="led-sub">AVG/DAY · {{ formattedAvg }} PCS</div>
    </div>

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

  <div v-else class="flex flex-col h-full">
    <!-- Unconfigured -->
    <div v-if="!machineId" class="flex items-center justify-center h-full text-xs text-gray-600">
      Configure machine
    </div>

    <template v-else>
      <!-- Stats row + day selector -->
      <div class="flex items-center justify-between px-1 pt-0.5 pb-1 flex-shrink-0">
        <div class="flex gap-3 text-[10px] text-gray-500 items-center">
          <!-- Live mode stats -->
          <template v-if="liveMode">
            <span class="flex items-center gap-1 px-1.5 py-0.5 rounded-full font-bold bg-emerald-500/15 text-emerald-400 border border-emerald-500/20">
              <span class="w-1.5 h-1.5 rounded-full bg-emerald-400 animate-pulse inline-block" />
              LIVE · 30 min
            </span>
            <span>
              <span class="text-emerald-400 font-mono">{{ livePoints.reduce((s, p) => s + p.count, 0) }}</span>
              <span class="ml-1">events</span>
            </span>
          </template>
          <!-- Historical stats -->
          <template v-else>
            <span>
              <span class="text-blue-400 font-mono">{{ fmtCount(totalCount) }}</span>
              <span class="ml-1">{{ selectedDays }}d</span>
            </span>
            <span>
              <span class="text-indigo-400 font-mono">{{ fmtCount(avgPerDay) }}</span>
              <span class="ml-1">avg/day</span>
            </span>
            <span v-if="peakDay">
              <span class="text-violet-400 font-mono">{{ fmtCount(peakDay.count) }}</span>
              <span class="ml-1">peak</span>
            </span>
          </template>
        </div>

        <div class="flex gap-0.5 items-center">
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
          <button
            class="ml-1 px-1.5 py-0.5 rounded font-medium border transition-colors text-[9px]"
            :class="liveMode
              ? 'bg-emerald-600/20 text-emerald-400 border-emerald-500/30 hover:bg-emerald-600/30'
              : 'bg-surface-300 text-gray-400 border-gray-700 hover:text-emerald-400 hover:border-emerald-500/30'"
            @click="liveMode = !liveMode"
          >
            {{ liveMode ? 'Exit Live' : '⊙ Live' }}
          </button>
        </div>
      </div>

      <!-- Loading -->
      <div v-if="liveMode ? liveLoading : loading" class="flex-1 flex items-center justify-center">
        <div class="spinner" />
      </div>

      <!-- Error (historical only) -->
      <div v-else-if="!liveMode && error" class="flex-1 flex items-center justify-center text-xs text-red-400 px-3 text-center">
        {{ error }}
      </div>

      <!-- Chart -->
      <div v-else class="relative flex-1 min-h-0">
        <VChart :option="liveMode ? liveOption : option" autoresize class="w-full h-full" />

        <div
          v-if="!liveMode && isMockData"
          class="absolute inset-0 flex items-center justify-center pointer-events-none"
        >
          <span class="text-[10px] font-medium tracking-widest uppercase text-gray-600 opacity-40">
            SAMPLE DATA
          </span>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
@import url('https://fonts.googleapis.com/css2?family=Share+Tech+Mono&family=Orbitron:wght@700&display=swap');

/* ... (LED CSS remains identical to original) ... */
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