<script setup lang="ts">
import { computed } from 'vue';
import { TrendingUp, TrendingDown, Minus, RefreshCw } from 'lucide-vue-next';
import type { DashboardWidget } from '@/types';
import { useTelemetryStore } from '@/stores/telemetry.store';
import { useAggregatedValue } from '@/composables/useTelemetry';

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

const machineField = computed(() => props.widget.machine?.fields?.find(f => f.key === field.value));
const label        = computed(() => machineField.value?.label ?? field.value);
const machineName  = computed(() => props.widget.machine?.name ?? '');
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
        <span
          v-if="aggLoading && !isLive"
          class="text-3xl font-bold text-gray-600 font-mono"
        >…</span>
        <span v-else class="text-3xl font-bold text-white font-mono tabular-nums">
          {{ formattedValue }}
        </span>
        <span v-if="unit" class="text-sm text-gray-400 ml-1.5">{{ unit }}</span>
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
