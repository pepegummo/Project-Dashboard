<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted } from "vue";
import VChart from "vue-echarts";
import type { EChartsOption } from "echarts";
import type { DashboardWidget } from "@/types";
import { api } from "@/services/api.service";
import { useWidgetViewStateStore } from "@/stores/widget-view-state.store";
import { useDashboardStore } from "@/stores/dashboard.store";

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
const POINTS = 20; // number of recent buckets to fetch
const configBucket = (props.widget.config?.bucket as string) || "";
const selectedBucket = ref<string>(configBucket || "1h");
// Bucket choice is local-only (not persisted to config) — mirror it into widget-view-state
// so the AI assistant's context can see what the widget is actually showing (see LineChartWidget's
// startDateTime/endDateTime sync for the same pattern).
const widgetViewStateStore = useWidgetViewStateStore();
const dashboardStore = useDashboardStore();

// Picking a bucket chip should also update the saved widget setting — but only for a real,
// editable dashboard widget (preview/focus cards in the AI page use "preview-N" ids).
const persistable = !props.ledMode && !!props.widget.id && !props.widget.id.startsWith("preview-");
async function persistBucket(b: string) {
  if (!persistable || (props.widget.config?.bucket ?? "") === b) return;
  try {
    await dashboardStore.updateWidget(props.widget.id, {
      config: { ...props.widget.config, bucket: b },
    });
  } catch { /* keep the local selection even if the save fails */ }
}
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
watch(selectedBucket, (b) => {
  widgetViewStateStore.setBucket(props.widget.id, b);
  persistBucket(b);
});

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

onUnmounted(() => {
  if (refetchTimer) clearInterval(refetchTimer);
  resizeObserver?.disconnect();
});

// ── Derived stats (per bucket) ────────────────────────────────────────────────
const totalCount = computed(() =>
  rows.value.reduce((s, r) => s + r.count, 0),
);
const avgPerBucket = computed(() =>
  rows.value.length
    ? Math.round(totalCount.value / rows.value.length)
    : 0,
);
const peakBucket = computed(() => {
  if (!rows.value.length) return null;
  return rows.value.reduce((a, b) => (a.count >= b.count ? a : b));
});

// With gap-filled buckets the window is never "missing" — it's just all zeros when this
// SKU/status produced nothing. Report that in the SKU's own terms rather than "no data".
const nothingProduced = computed(() => !rows.value.length || totalCount.value === 0);
const emptyMessage = computed(() =>
  skuFilter.value
    ? `No ${skuFilter.value} was produced during this period.`
    : "Nothing was produced during this period.",
);

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
  const labels = rows.value.map((r) => fmtBucketLabel(r.bucket));
  const data = rows.value.map((r) => r.count);

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
        </div>
      </div>

      <!-- Loading -->
      <div
        v-if="loading"
        class="flex-1 flex items-center justify-center"
      >
        <div class="spinner" />
      </div>

      <!-- Error -->
      <div
        v-else-if="error"
        class="flex-1 flex items-center justify-center text-xs text-red-400 px-3 text-center"
      >
        {{ error }}
      </div>

      <!-- Empty — every bucket in the window is 0 (this SKU/status produced nothing) -->
      <div
        v-else-if="nothingProduced"
        class="flex-1 flex flex-col items-center justify-center gap-1 px-3 text-center"
      >
        <span class="text-xs text-gray-400">{{ emptyMessage }}</span>
        <span class="text-[10px] text-gray-600">Try a larger bucket or a different SKU.</span>
      </div>

      <!-- Chart -->
      <div v-else ref="chartContainerRef" class="relative flex-1 min-h-0" style="min-height:80px">
        <VChart
          ref="vchart"
          :option="option"
          class="w-full h-full"
        />
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
