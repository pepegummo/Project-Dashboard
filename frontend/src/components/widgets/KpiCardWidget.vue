<script setup lang="ts">
import { computed, onMounted, onUnmounted } from 'vue';
import { TrendingUp, TrendingDown, Minus, RefreshCw } from 'lucide-vue-next';
import type { DashboardWidget } from '@/types';
import { useTelemetryStore } from '@/stores/telemetry.store';
import { useAggregatedValue } from '@/composables/useTelemetry';
import { wsService } from '@/services/ws.service';
import { api } from '@/services/api.service';

const props = defineProps<{ widget: DashboardWidget }>();

const store     = useTelemetryStore();
const machineId = computed(() => props.widget.machineId ?? '');
const field     = computed(() => (props.widget.config?.field as string) ?? '');
const unit      = computed(() => (props.widget.config?.unit as string) ?? '');
const precision = computed(() => (props.widget.config?.precision as number) ?? 2);
const aggPeriod = computed(() => (props.widget.config?.aggregationPeriod as string) ?? 'live');

// ── Aggregated mode ────────────────────────────────────────────────────────────
const { summary, loading: aggLoading, periodLabel, isLive, refresh } =
  useAggregatedValue(machineId.value, field.value, aggPeriod.value);

// ── Live mode ──────────────────────────────────────────────────────────────────
const currentValue = computed(() => store.getFieldValue(machineId.value, field.value));
const history      = computed(() => store.getHistory(machineId.value, field.value));

// Which value to display
const displayValue = computed(() => {
  if (!isLive && summary.value) return summary.value.avg;
  return currentValue.value;
});

const formattedValue = computed(() => {
  if (displayValue.value === undefined || displayValue.value === null) return '—';
  return displayValue.value.toFixed(precision.value);
});

// Trend (live mode only)
const trend = computed(() => {
  if (!isLive) return 0;
  const hist = history.value;
  if (!hist || hist.values.length < 10) return 0;
  const prev = hist.values[hist.values.length - 10];
  const curr = hist.values[hist.values.length - 1];
  return curr - prev;
});

const trendPct = computed(() => {
  if (!isLive) return null;
  const hist = history.value;
  if (!hist || hist.values.length < 10) return null;
  const prev = hist.values[hist.values.length - 10];
  if (prev === 0) return null;
  return ((trend.value / Math.abs(prev)) * 100).toFixed(1);
});

// ── Live mode: WebSocket subscription + one-time REST seed ───────────────────
async function fetchLatest() {
  if (!machineId.value) return;
  try {
    const latest = await api.getLatestTelemetry(machineId.value);
    if (latest) store.updateSnapshot(machineId.value, latest.timestamp as unknown as string, latest.data as Record<string, any>);
  } catch { /* ok */ }
}

onMounted(() => {
  if (isLive && machineId.value) {
    wsService.subscribe([machineId.value]);
    fetchLatest(); // seed store immediately from DB; WS handles all subsequent updates
  }
});

onUnmounted(() => {
  if (isLive && machineId.value) wsService.unsubscribe([machineId.value]);
});

const machineField = computed(() => props.widget.machine?.fields?.find(f => f.key === field.value));
const label        = computed(() => machineField.value?.label     ?? field.value);
const machineName  = computed(() => props.widget.machine?.name    ?? '');
const threshold    = computed(() => machineField.value?.threshold  ?? null);
const upperLimit   = computed(() => machineField.value?.upperLimit ?? null);
const lowerLimit   = computed(() => machineField.value?.lowerLimit ?? null);

// Green = in range, Red = out of range, Gray = no threshold defined
const rangeStatus = computed(() => {
  const val = displayValue.value;
  if (val === undefined || val === null) return 'none';
  if (lowerLimit.value !== null && val < lowerLimit.value) return 'below';
  if (upperLimit.value !== null && val > upperLimit.value) return 'above';
  if (lowerLimit.value !== null || upperLimit.value !== null) return 'ok';
  return 'none';
});
</script>

<template>
  <div class="flex flex-col justify-between h-full px-4 py-3">
    <!-- Top row: label + period badge -->
    <div class="flex items-start justify-between gap-2">
      <div class="min-w-0">
        <p class="text-xs text-gray-500 uppercase tracking-wide truncate">{{ label }}</p>
        <p v-if="machineName" class="text-[10px] text-gray-600 mt-0.5 truncate">{{ machineName }}</p>
      </div>

      <!-- Aggregation badge -->
      <div v-if="!isLive" class="flex items-center gap-1 flex-shrink-0">
        <span class="text-[10px] font-medium px-1.5 py-0.5 rounded bg-blue-500/15 text-blue-400 border border-blue-500/20 whitespace-nowrap">
          per {{ periodLabel }}
        </span>
        <button
          class="p-0.5 text-gray-600 hover:text-gray-300 transition-colors"
          title="Refresh"
          @click.stop="refresh()"
        >
          <RefreshCw class="w-3 h-3" :class="aggLoading ? 'animate-spin' : ''" />
        </button>
      </div>
    </div>

    <!-- Main value -->
    <div class="flex items-end justify-between mt-1">
      <div>
        <span v-if="aggLoading && !isLive" class="text-3xl font-bold text-gray-600 font-mono">…</span>
        <span
          v-else
          class="text-3xl font-bold font-mono tabular-nums"
          data-ai-el="value"
          :class="{
            'text-emerald-400': rangeStatus === 'ok',
            'text-red-400':     rangeStatus === 'above' || rangeStatus === 'below',
            'text-white':       rangeStatus === 'none',
          }"
        >{{ formattedValue }}</span>
        <span v-if="unit" class="text-sm text-gray-400 ml-1.5" data-ai-el="unit">{{ unit }}</span>
      </div>

      <!-- Trend (live only) -->
      <div v-if="trendPct !== null && isLive" class="flex items-center gap-1 text-xs font-medium pb-1">
        <component
          :is="trend > 0 ? TrendingUp : trend < 0 ? TrendingDown : Minus"
          class="w-4 h-4"
          :class="trend > 0 ? 'text-emerald-400' : trend < 0 ? 'text-red-400' : 'text-gray-400'"
        />
        <span :class="trend > 0 ? 'text-emerald-400' : trend < 0 ? 'text-red-400' : 'text-gray-400'">
          {{ Math.abs(parseFloat(trendPct)) }}%
        </span>
      </div>
    </div>

    <!-- Threshold / limit row -->
    <div v-if="threshold !== null || upperLimit !== null" class="flex gap-3 mt-1">
      <div v-if="lowerLimit !== null" class="text-[10px]">
        <span class="text-gray-600">lower </span>
        <span class="font-mono text-amber-400">{{ lowerLimit }}</span>
      </div>
      <div v-if="threshold !== null" class="text-[10px]">
        <span class="text-gray-600">target </span>
        <span class="font-mono text-indigo-400">{{ threshold }}</span>
      </div>
      <div v-if="upperLimit !== null" class="text-[10px]">
        <span class="text-gray-600">upper </span>
        <span class="font-mono text-amber-400">{{ upperLimit }}</span>
      </div>
      <div v-if="rangeStatus !== 'none'" class="text-[10px] ml-auto">
        <span
          class="px-1.5 py-0.5 rounded-full font-medium"
          :class="{
            'bg-emerald-500/15 text-emerald-400 border border-emerald-500/20': rangeStatus === 'ok',
            'bg-red-500/15 text-red-400 border border-red-500/20': rangeStatus === 'above' || rangeStatus === 'below',
          }"
        >{{ rangeStatus === 'ok' ? 'IN RANGE' : rangeStatus === 'above' ? 'OVER' : 'UNDER' }}</span>
      </div>
    </div>

    <!-- Aggregated min / max row -->
    <div v-if="!isLive && summary" class="flex gap-4 mt-1">
      <div class="text-[10px] text-gray-500">
        <span class="text-gray-600">min </span>
        <span class="font-mono text-gray-300">{{ summary.min.toFixed(precision) }}</span>
      </div>
      <div class="text-[10px] text-gray-500">
        <span class="text-gray-600">max </span>
        <span class="font-mono text-gray-300">{{ summary.max.toFixed(precision) }}</span>
      </div>
      <div class="text-[10px] text-gray-500">
        <span class="text-gray-600">n </span>
        <span class="font-mono text-gray-300">{{ summary.count.toLocaleString() }}</span>
      </div>
    </div>

    <!-- Sparkline (live mode only) -->
    <div v-if="isLive && history?.values.length" class="h-8 mt-2">
      <svg class="w-full h-full" viewBox="0 0 200 30" preserveAspectRatio="none">
        <polyline
          :points="history.values.slice(-50).map((v, i, arr) => {
            const minV = Math.min(...arr);
            const maxV = Math.max(...arr);
            const x = (i / (arr.length - 1)) * 200;
            const y = 30 - ((v - minV) / (maxV - minV || 1)) * 28;
            return `${x},${y}`;
          }).join(' ')"
          fill="none"
          stroke="#3b82f6"
          stroke-width="1.5"
          stroke-linecap="round"
          stroke-linejoin="round"
        />
      </svg>
    </div>

    <div v-if="!machineId || !field" class="flex items-center justify-center h-full text-xs text-gray-600">
      Configure machine &amp; field
    </div>
  </div>
</template>
