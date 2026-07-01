<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted } from "vue";
import VChart from "vue-echarts";
import type { EChartsOption } from "echarts";
import type { DashboardWidget } from "@/types";
import { api } from "@/services/api.service";
import { wsService } from "@/services/ws.service";
import { useWidgetViewStateStore } from "@/stores/widget-view-state.store";

// ── LED mode data shape ───────────────────────────────────────────────────────
export interface LedWeekDay {
  day: string; // 'MON' | 'TUE' | 'WED' | 'THU' | 'FRI' | 'SAT' | 'SUN'
  count: number;
  isToday: boolean;
  isFuture: boolean;
}

export interface LedDailyData {
  machineName: string;
  todayCount: number;
  avgPerDay: number;
  weeklyData: LedWeekDay[];
  hourlyData?: Array<{ hour: string; count: number }>;
}

const props = defineProps<{
  widget: DashboardWidget;
  ledMode?: boolean; // defaults to false
  data?: LedDailyData; // only required when ledMode = true
}>();

const machineId = computed(() => props.widget.machineId ?? "");
const machineName = computed(() => props.widget.machine?.name ?? "");

// SKU + status filter for piece counting (empty sku = all SKUs).
const skuFilter = computed(() => (props.widget.config?.sku as string) || "");
const statusFilter = computed(
  () => (props.widget.config?.status as "all" | "good" | "reject") || "all",
);

// User-selectable bucket size. Presets act as quick toggles; a custom config value (e.g. '7m')
// is shown as an extra chip alongside the presets.
const BUCKET_PRESETS = ["1m", "5m", "15m", "30m", "1h", "1d"];
const POINTS = 48; // number of recent buckets to fetch
const configBucket = (props.widget.config?.bucket as string) || "";
const selectedBucket = ref<string>(configBucket || "1h");
// Bucket choice is local-only (not persisted to config) — mirror it into widget-view-state
// so the AI assistant's context can see what the widget is actually showing (see LineChartWidget's
// startDateTime/endDateTime sync for the same pattern).
const widgetViewStateStore = useWidgetViewStateStore();
const bucketChips = computed(() =>
  configBucket && !BUCKET_PRESETS.includes(configBucket)
    ? [configBucket, ...BUCKET_PRESETS]
    : BUCKET_PRESETS,
);

// ── Data fetching ─────────────────────────────────────────────────────────────
interface BucketPoint {
  bucket: string;
  count: number;
}
const rows = ref<BucketPoint[]>([]);
const loading = ref(false);
const error = ref<string | null>(null);

async function load() {
  if (!machineId.value) return;
  // Spinner only on the first load — the 60s refetch should be silent.
  if (!rows.value.length) loading.value = true;
  error.value = null;
  try {
    const res = await api.getTelemetryCount(machineId.value, selectedBucket.value, {
      sku: skuFilter.value,
      status: statusFilter.value,
      points: POINTS,
    });
    rows.value = (res?.data ?? []).map((r) => ({ bucket: r.bucket, count: r.count }));
    // Publish the on-screen data (compacted) so the AI can read exactly what this
    // count widget shows when it's focused — same columns/data shape the backend uses.
    widgetViewStateStore.setSeries(props.widget.id, {
      columns: ["time", "count"],
      data: rows.value.map((r) => [r.bucket, r.count]),
    });
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}

// Refresh the buckets periodically. A WS per-row increment is dropped here — for bucket sums a
// quiet 60s refetch is simpler and correct, and the live view below still gives a real-time feel.
let refetchTimer: ReturnType<typeof setInterval> | null = null;

onMounted(async () => {
  widgetViewStateStore.setBucket(props.widget.id, selectedBucket.value);
  // In LED mode LedView owns all data fetching.
  if (props.ledMode) return;
  await load();
  refetchTimer = setInterval(load, 60_000);
});

onUnmounted(() => {
  widgetViewStateStore.remove(props.widget.id);
});

watch([selectedBucket, skuFilter, statusFilter], load);
watch(selectedBucket, (b) => widgetViewStateStore.setBucket(props.widget.id, b));

// ── Live mode — 30-minute rolling window ─────────────────────────────────────
const liveMode = ref(false);
const liveLoading = ref(false);
const livePoints = ref<Array<{ ts: string; count: number }>>([]);
const LIVE_WINDOW_MS = 30 * 60 * 1000;

let offTelemetry: (() => void) | null = null;
let liveTimer: ReturnType<typeof setInterval> | null = null;

// ── ResizeObserver — keeps ECharts in sync with CSS-grid cell size changes ────
const chartContainerRef = ref<HTMLElement | null>(null);
const vchart = ref<InstanceType<typeof VChart> | null>(null);
let resizeObserver: ResizeObserver | null = null;

watch(chartContainerRef, (el) => {
  resizeObserver?.disconnect();
  resizeObserver = null;
  if (el) {
    resizeObserver = new ResizeObserver(() => {
      vchart.value?.chart?.resize();
    });
    resizeObserver.observe(el);
  }
});

async function loadLiveSeed() {
  const seedField = props.widget.machine?.fields?.[0]?.key;
  if (!machineId.value || !seedField) return;
  liveLoading.value = true;
  try {
    const series = await api.getTelemetrySeries(machineId.value, seedField, {
      timeRange: "30m",
    });
    livePoints.value = (series?.data ?? []).map((p: any) => ({
      ts: p.bucket ?? p.ts,
      count: (p.avg ?? p.value) != null ? 1 : 0,
    }));
  } catch {
    /* ok */
  } finally {
    liveLoading.value = false;
  }
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
    const existing = livePoints.value.find((p) => p.ts === bucket);
    if (existing) {
      existing.count++;
    } else {
      livePoints.value.push({ ts: bucket, count: 1 });
    }
    livePoints.value = livePoints.value.filter(
      (p) => new Date(p.ts).getTime() > cutoff,
    );
  });
  liveTimer = setInterval(loadLiveSeed, 5 * 60_000);
}

function exitLive() {
  offTelemetry?.();
  offTelemetry = null;
  if (liveTimer) {
    clearInterval(liveTimer);
    liveTimer = null;
  }
  if (machineId.value) wsService.unsubscribe([machineId.value]);
  livePoints.value = [];
}

watch(liveMode, (isLive) => {
  if (isLive) enterLive();
  else exitLive();
});
onUnmounted(() => {
  if (refetchTimer) clearInterval(refetchTimer);
  exitLive();
  resizeObserver?.disconnect();
});

// ── Sample data — shown only when a configured widget has no rows yet ─────────
const SAMPLE_COUNTS = [1200, 1300, 1400, 1800, 1100, 1200, 900];

const mockRows = computed<BucketPoint[]>(() => {
  const now = Date.now();
  return SAMPLE_COUNTS.map((count, i) => ({
    bucket: new Date(now - (SAMPLE_COUNTS.length - 1 - i) * 3_600_000).toISOString(),
    count,
  }));
});

const isMockData = computed(() => !loading.value && rows.value.length === 0);
const displayRows = computed(() =>
  isMockData.value ? mockRows.value : rows.value,
);

// ── Derived stats (per bucket) ────────────────────────────────────────────────
const totalCount = computed(() =>
  displayRows.value.reduce((s, r) => s + r.count, 0),
);
const avgPerBucket = computed(() =>
  displayRows.value.length
    ? Math.round(totalCount.value / displayRows.value.length)
    : 0,
);
const peakBucket = computed(() => {
  if (!displayRows.value.length) return null;
  return displayRows.value.reduce((a, b) => (a.count >= b.count ? a : b));
});

// ── Format helpers ────────────────────────────────────────────────────────────
// Day buckets read as a date; sub-day buckets read as a clock time.
function fmtBucketLabel(iso: string) {
  const d = new Date(iso);
  if (selectedBucket.value.endsWith("d")) {
    return d.toLocaleDateString("en-US", { month: "short", day: "numeric" });
  }
  return d.toLocaleTimeString("en-US", { hour: "2-digit", minute: "2-digit", hour12: false });
}

function fmtCount(n: number) {
  return n >= 1_000_000
    ? `${(n / 1_000_000).toFixed(1)}M`
    : n >= 1_000
      ? `${(n / 1_000).toFixed(1)}k`
      : String(Math.round(n * 10) / 10);
}

// ── LED mode — 8-hour display ─────────────────────────────────────────────────
// Derive per-hour production from the daily avg; distribute across 8 hourly
// buckets with a fixed spread so the bars look realistic without a new endpoint.
const HOUR_SPREADS = [1, 1, 1, 1, 1, 1, 1, 1] as const

const eightHourCount = computed(() => {
  const h = props.data?.hourlyData;
  if (h?.length) return hourlyBars.value.reduce((s, b) => s + b.count, 0);
  const daily = props.data?.todayCount ?? props.data?.avgPerDay ?? 0;
  return Math.round(daily * (8 / 24));
});
const avgPerHour = computed(() => {
  const h = props.data?.hourlyData;
  if (h?.length) return Math.round(eightHourCount.value / 8);
  return Math.round((props.data?.avgPerDay ?? 0) / 24);
});

const formattedEightHour = computed(() =>
  eightHourCount.value.toLocaleString(),
);
const formattedAvgHour = computed(() => avgPerHour.value.toLocaleString());

const hourlyBars = computed(() => {
  const curHour = new Date().getHours();
  const bars = Array.from({ length: 8 }, (_, i) => {
    const hour = (curHour - 7 + i + 24) % 24;
    return {
      label: `${String(hour).padStart(2, "0")}:00`,
      count: 0,
      isCurrent: i === 7,
    };
  });

  const hourlyData = props.data?.hourlyData;
  if (hourlyData?.length) {
    for (const p of hourlyData) {
      const localH = new Date(p.hour).getHours();
      const bar = bars.find(b => b.label === `${String(localH).padStart(2, "0")}:00`);
      if (bar) bar.count = p.count;
    }
    return bars;
  }

  // fallback: estimate from daily average
  const perHour = (props.data?.avgPerDay ?? 0) / 24;
  return bars.map((b, i) => ({ ...b, count: Math.round(perHour * HOUR_SPREADS[i]) }));
});

const maxHourly = computed(() =>
  Math.max(...hourlyBars.value.map((b) => b.count), 1),
);

// Bar height as a 0–1 fraction of the chart area. A small floor keeps a
// visible nub on the baseline even when an hour has no production yet.
function hourBarPct(count: number): number {
  if (count <= 0) return 0.05;
  return Math.max(0.05, count / maxHourly.value);
}

// ── ECharts option (per-bucket bar chart) ─────────────────────────────────────
const option = computed<EChartsOption>(() => {
  const labels = displayRows.value.map((r) => fmtBucketLabel(r.bucket));
  const data = displayRows.value.map((r) => r.count);

  return {
    backgroundColor: "transparent",
    grid: { left: 50, right: 20, top: 22, bottom: 28, containLabel: false },

    xAxis: {
      type: "category",
      data: labels,
      axisLabel: {
        color: "#6b7280",
        fontSize: 9,
        interval: "auto",
        rotate: labels.length > 16 ? 30 : 0,
      },
      axisLine: { lineStyle: { color: "#374151" } },
      splitLine: { show: false },
    },

    yAxis: {
      name: `Units per ${selectedBucket.value}`,
      nameLocation: "middle",
      nameGap: 38,
      nameTextStyle: { color: "#9ca3af", fontSize: 10 },
      type: "value",
      axisLabel: {
        color: "#6b7280",
        fontSize: 9,
        formatter: (v: number) => fmtCount(v),
      },
      splitLine: { lineStyle: { color: "#1f2937", type: "dashed" } },
      min: 0,
    },

    tooltip: {
      trigger: "axis",
      backgroundColor: "#1e2130",
      borderColor: "#374151",
      textStyle: { color: "#e5e7eb", fontSize: 12 },
      formatter: (params: any) => {
        const p = Array.isArray(params) ? params[0] : params;
        return `<div style="font-family:monospace;line-height:1.6">
          ${p.name}<br/>
          <b style="color:#60a5fa">${(p.value as number).toLocaleString()}</b> units
        </div>`;
      },
    },

    series: [
      {
        type: "bar",
        data,
        barMaxWidth: 28,
        itemStyle: {
          borderRadius: [3, 3, 0, 0],
          color: {
            type: "linear",
            x: 0,
            y: 0,
            x2: 0,
            y2: 1,
            colorStops: [
              { offset: 0, color: "rgba(96,165,250,0.95)" },
              { offset: 1, color: "rgba(59,130,246,0.35)" },
            ],
          },
        },
        markLine:
          avgPerBucket.value > 0
            ? {
                silent: true,
                symbol: "none",
                animation: false,
                data: [
                  {
                    yAxis: avgPerBucket.value,
                    lineStyle: { color: "#6b7280", type: "dashed", width: 1 },
                    label: {
                      formatter: "avg",
                      color: "#9ca3af",
                      fontSize: 9,
                      position: "end",
                    },
                  },
                ],
              }
            : undefined,
      } as any,
    ],
  };
});

// ── Live chart option (30-min rolling cumulative area chart) ─────────────────
const liveOption = computed<EChartsOption>(() => {
  const labels = livePoints.value.map((p) => {
    const d = new Date(p.ts);
    return d.toLocaleTimeString("en-US", {
      hour: "2-digit",
      minute: "2-digit",
      hour12: false,
    });
  });

  // Running cumulative total across the 30-min window
  let running = 0;
  const cumul = livePoints.value.map((p) => {
    running += p.count;
    return running;
  });
  const finalVal = cumul[cumul.length - 1] ?? 0;
  const liveAvg = livePoints.value.length
    ? Math.round(running / livePoints.value.length)
    : 0;

  return {
    backgroundColor: "transparent",
    grid: { left: 36, right: 20, top: 16, bottom: 28, containLabel: false },

    xAxis: {
      type: "category",
      data: labels,
      axisLabel: { color: "#6b7280", fontSize: 9, interval: "auto" },
      axisLine: { lineStyle: { color: "#374151" } },
      splitLine: { show: false },
    },

    yAxis: {
      type: "value",
      minInterval: 1,
      axisLabel: {
        color: "#6b7280",
        fontSize: 9,
        formatter: (v: number) => fmtCount(v),
      },
      splitLine: { lineStyle: { color: "#1f2937", type: "dashed" } },
      min: 0,
    },

    tooltip: {
      trigger: "axis",
      backgroundColor: "#1e2130",
      borderColor: "#374151",
      textStyle: { color: "#e5e7eb", fontSize: 12 },
      formatter: (params: any) => {
        const arr = Array.isArray(params) ? params : [params];
        const time = arr[0]?.name ?? "";
        const cumulVal = arr[0]?.value ?? 0;
        const idx = arr[0]?.dataIndex ?? 0;
        const perMin = livePoints.value[idx]?.count ?? 0;
        return `<div style="font-family:monospace;line-height:1.8;font-size:11px">
          <span style="color:#9ca3af">${time}</span><br/>
          <span style="color:#60a5fa">Cumulative</span>: <b>${(cumulVal as number).toLocaleString()}</b><br/>
          <span style="color:#6b7280">This min</span>: +${perMin}
        </div>`;
      },
    },

    series: [
      {
        type: "line",
        data: cumul,
        smooth: 0.3,
        symbol: "circle",
        symbolSize: 4,
        showSymbol: false,
        lineStyle: { color: "#3b82f6", width: 2 },
        itemStyle: { color: "#3b82f6", borderColor: "#1e40af", borderWidth: 2 },
        emphasis: {
          showSymbol: true,
          itemStyle: {
            color: "#60a5fa",
            borderColor: "#93c5fd",
            borderWidth: 2,
          },
        },
        areaStyle: {
          color: {
            type: "linear",
            x: 0,
            y: 0,
            x2: 0,
            y2: 1,
            colorStops: [
              { offset: 0, color: "rgba(59,130,246,0.35)" },
              { offset: 0.6, color: "rgba(59,130,246,0.08)" },
              { offset: 1, color: "rgba(59,130,246,0.00)" },
            ],
          },
        },
        markLine:
          liveAvg > 0
            ? {
                silent: true,
                symbol: "none",
                animation: false,
                data: [
                  {
                    yAxis: liveAvg,
                    lineStyle: { color: "#6366f1", type: "dashed", width: 1 },
                    label: {
                      formatter: `avg ${fmtCount(liveAvg)}`,
                      color: "#6366f1",
                      fontSize: 9,
                      position: "end",
                    },
                  },
                ],
              }
            : undefined,
        markPoint: cumul.length
          ? {
              symbol: "circle",
              symbolSize: 6,
              data: [{ type: "max" as const, name: "Total" }],
              label: {
                formatter: (p: any) => `Total ${fmtCount(p.value)}`,
                color: "#93c5fd",
                fontSize: 9,
                position: "top",
                distance: 8,
                backgroundColor: "rgba(14,17,30,0.85)",
                padding: [2, 5] as [number, number],
                borderRadius: 3,
              },
              itemStyle: {
                color: "#3b82f6",
                borderColor: "#93c5fd",
                borderWidth: 1,
              },
            }
          : undefined,
      } as any,
    ],
  };
});
</script>

<template>
  <div
    v-if="ledMode"
    class="w-full h-full bg-black flex flex-col overflow-hidden select-none font-mono"
    style="padding: 0.6rem 1rem 0.5rem; box-sizing: border-box"
  >
    <!-- Title -->
    <p
      class="text-gray-100 uppercase font-bold w-full text-center leading-none truncate flex-shrink-0"
      style="font-size: 0.6rem; letter-spacing: 0.18em; text-shadow: 0 1px 4px rgba(0,0,0,0.95), 0 0 10px rgba(0,0,0,0.85);"
    >
      8-HOUR OUTPUT
    </p>

    <!-- 8-hour count -->
    <p
      class="tabular-nums leading-none text-center font-black text-emerald-400 flex-shrink-0"
      style="
        font-size: 2rem;
        margin-top: 0.18em;
        text-shadow:
          0 0 2px rgba(255, 255, 255, 0.55),
          0 0 14px #10b981,
          0 0 45px #10b98155;
      "
    >
      {{ formattedEightHour }}
    </p>

    <!-- Unit -->
    <p
      class="text-gray-200 uppercase font-semibold text-center leading-none flex-shrink-0"
      style="font-size: 0.6rem; letter-spacing: 0.16em; margin-top: 0.22em; text-shadow: 0 1px 4px rgba(0,0,0,0.95), 0 0 10px rgba(0,0,0,0.85);"
    >
      PCS (LAST 8H)
    </p>

    <!-- 8-hour bar chart — fills the remaining height, full width -->
    <div
      class="flex-1 min-h-0 w-full flex flex-col justify-end"
      style="margin-top: 0.5em"
    >
      <!-- relative wrapper so the HTML label overlay sits over the SVG -->
      <div class="relative flex-1 min-h-0 w-full">
        <svg
          class="w-full h-full"
          style="display: block"
          viewBox="0 0 100 100"
          preserveAspectRatio="none"
        >
          <!-- Baseline -->
          <line x1="0" y1="99" x2="100" y2="99" stroke="rgba(255,255,255,0.12)" stroke-width="0.6" />

          <!-- Bars only — count labels are in the HTML overlay below -->
          <g v-for="(bar, i) in hourlyBars" :key="bar.label">
            <rect
              :x="i * 12.5 + 2.5"
              :y="99 - hourBarPct(bar.count) * 85"
              width="7.5"
              :height="hourBarPct(bar.count) * 85"
              rx="1.2"
              fill="#10b981"
              :fill-opacity="bar.isCurrent ? 1 : 0.3"
            />
          </g>
        </svg>

        <!-- Count labels as HTML — immune to SVG preserveAspectRatio="none" distortion -->
        <div class="absolute inset-0 pointer-events-none">
          <template v-for="(bar, i) in hourlyBars" :key="`cnt-${bar.label}`">
            <span
              v-if="bar.count > 0"
              class="absolute text-white font-bold font-mono leading-none"
              style="font-size: 0.5rem; text-align: center; transform: translateX(-50%); text-shadow: 0 0 3px rgba(0,0,0,0.9), 0 1px 2px rgba(0,0,0,0.8);"
              :style="{
                left:   `${i * 12.5 + 6.25}%`,
                bottom: `${hourBarPct(bar.count) * 85 + 1}%`,
              }"
            >{{ bar.count }}</span>
          </template>
        </div>
      </div>

      <!-- Hour labels (HTML for crisp, undistorted text) -->
      <div class="flex w-full" style="margin-top: 3px">
        <span
          v-for="bar in hourlyBars"
          :key="`lbl-${bar.label}`"
          class="text-center leading-none"
          :style="{
            flex: '1 1 0',
            fontSize: '0.42rem',
            fontWeight: 'bold',
            letterSpacing: '0.01em',
            color: bar.isCurrent ? '#10b981' : 'rgba(255,255,255,0.32)',
          }"
        >
          {{ bar.label }}
        </span>
      </div>
    </div>

    <!-- AVG/HR -->
    <p
      class="text-gray-400 text-center leading-none flex-shrink-0"
      style="font-size: 0.52rem; letter-spacing: 0.12em; margin-top: 0.4em; text-shadow: 0 1px 3px rgba(0,0,0,0.9);"
    >
      AVG {{ formattedAvgHour }} PCS/HR
    </p>
  </div>

  <div v-else class="flex flex-col h-full">
    <!-- Unconfigured -->
    <div
      v-if="!machineId"
      class="flex items-center justify-center h-full text-xs text-gray-600"
    >
      Configure machine
    </div>

    <template v-else>
      <!-- Stats row + day selector -->
      <div
        class="flex items-center justify-between px-1 pt-0.5 pb-1 flex-shrink-0"
      >
        <div class="flex gap-3 text-[10px] text-gray-500 items-center">
          <!-- Live mode stats -->
          <template v-if="liveMode">
            <span
              class="flex items-center gap-1 px-1.5 py-0.5 rounded-full font-bold bg-emerald-500/15 text-emerald-400 border border-emerald-500/20"
            >
              <span
                class="w-1.5 h-1.5 rounded-full bg-emerald-400 animate-pulse inline-block"
              />
              LIVE · 30 min
            </span>
            <span>
              <span class="text-emerald-400 font-mono">{{
                livePoints.reduce((s, p) => s + p.count, 0)
              }}</span>
              <span class="ml-1">events</span>
            </span>
          </template>
          <!-- Historical stats -->
          <template v-else>
            <span class="px-1.5 py-0.5 rounded bg-surface-300 text-gray-300 font-mono">
              {{ skuFilter || 'All SKUs' }}<span v-if="statusFilter !== 'all'" class="text-gray-500"> · {{ statusFilter }}</span>
            </span>
            <span>
              <span class="text-blue-400 font-mono">{{ fmtCount(totalCount) }}</span>
              <span class="ml-1">total</span>
            </span>
            <span>
              <span class="text-indigo-400 font-mono">{{ fmtCount(avgPerBucket) }}</span>
              <span class="ml-1">avg/{{ selectedBucket }}</span>
            </span>
            <span v-if="peakBucket">
              <span class="text-violet-400 font-mono">{{ fmtCount(peakBucket.count) }}</span>
              <span class="ml-1">peak</span>
            </span>
          </template>
        </div>

        <div class="flex gap-0.5 items-center">
          <button
            v-for="b in bucketChips"
            :key="b"
            class="px-1.5 py-0.5 rounded text-[9px] font-medium transition-colors"
            :class="
              selectedBucket === b
                ? 'bg-blue-600 text-white'
                : 'bg-surface-300 text-gray-400 hover:text-gray-200'
            "
            @click="selectedBucket = b"
          >
            {{ b }}
          </button>
          <button
            class="ml-1 px-1.5 py-0.5 rounded font-medium border transition-colors text-[9px]"
            :class="
              liveMode
                ? 'bg-emerald-600/20 text-emerald-400 border-emerald-500/30 hover:bg-emerald-600/30'
                : 'bg-surface-300 text-gray-400 border-gray-700 hover:text-emerald-400 hover:border-emerald-500/30'
            "
            @click="liveMode = !liveMode"
          >
            {{ liveMode ? "Exit Live" : "⊙ Live" }}
          </button>
        </div>
      </div>

      <!-- Loading -->
      <div
        v-if="liveMode ? liveLoading : loading"
        class="flex-1 flex items-center justify-center"
      >
        <div class="spinner" />
      </div>

      <!-- Error (historical only) -->
      <div
        v-else-if="!liveMode && error"
        class="flex-1 flex items-center justify-center text-xs text-red-400 px-3 text-center"
      >
        {{ error }}
      </div>

      <!-- Chart -->
      <div v-else ref="chartContainerRef" class="relative flex-1 min-h-0" style="min-height:80px">
        <VChart
          ref="vchart"
          :option="liveMode ? liveOption : option"
          class="w-full h-full"
        />

        <div
          v-if="!liveMode && isMockData"
          class="absolute inset-0 flex items-center justify-center pointer-events-none"
        >
          <span
            class="text-[10px] font-medium tracking-widest uppercase text-gray-600 opacity-40"
          >
            SAMPLE DATA
          </span>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.spinner {
  width: 20px;
  height: 20px;
  border: 2px solid #374151;
  border-top-color: #3b82f6;
  border-radius: 50%;
  animation: spin 0.7s linear infinite;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
