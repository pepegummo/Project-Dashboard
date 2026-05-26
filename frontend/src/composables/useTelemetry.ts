import { ref, computed, onMounted, onUnmounted, watch, isRef } from 'vue';
import type { Ref } from 'vue';
import { wsService } from '@/services/ws.service';
import { api } from '@/services/api.service';


/** Composable for a single field series within a chart widget */
export function useFieldSeries(machineId: string, field: string, timeRange: string | Ref<string> = '1h') {
  const apiData = ref<Array<{ ts: string; value: number; min?: number; max?: number }>>([]);
  const loading = ref(false);

  // Support both plain strings and reactive refs
  const timeRangeRef = isRef(timeRange) ? timeRange : ref(timeRange);

  async function loadFromApi() {
    loading.value = true;
    try {
      const series = await api.getTelemetrySeries(machineId, field, timeRangeRef.value);
      apiData.value = (series?.data ?? []).map(p => {
        const raw = p as any;
        return {
          ts:    raw.bucket ?? p.ts,
          value: raw.avg    ?? p.value,
          // Explicitly check for null/undefined — Number(null)===0 would corrupt band
          min:   (raw.min != null) ? Number(raw.min) : undefined,
          max:   (raw.max != null) ? Number(raw.max) : undefined,
        };
      });
    } catch { /* ok */ }
    loading.value = false;
  }

  // Auto-refresh every 60 s to stay current without merging raw store history.
  // Gauge/KPI widgets poll every 2 s and write raw-second points into the shared
  // store history. If we merged those points here, 200+ raw-second entries would
  // be appended after the last API bucket, completely distorting the aggregated
  // chart shape (this is why CW-01/TS-01 looked wrong while CB-01/VC-01 were fine).
  let refreshTimer: ReturnType<typeof setInterval> | null = null;

  onMounted(() => {
    loadFromApi();
    wsService.subscribe([machineId]);
    refreshTimer = setInterval(loadFromApi, 60_000);
  });
  onUnmounted(() => {
    wsService.unsubscribe([machineId]);
    if (refreshTimer) { clearInterval(refreshTimer); refreshTimer = null; }
  });

  // Reload whenever timeRange changes — clear stale data first so the old
  // range never bleeds through while the new request is in-flight.
  watch(timeRangeRef, () => {
    apiData.value = [];
    loadFromApi();
  });

  // Use only API bucket data — never merge raw store history.
  // Gauge/KPI polling accumulates up to 300 raw-second points in the store for
  // every machine that has a live widget. Merging those into a 30-min-bucket
  // 7-day chart would cluster hundreds of raw points at the right edge and
  // make the chart look like a flat spike rather than a sine wave.
  const mergedData = computed(() => apiData.value);

  return { mergedData, loading, reload: loadFromApi };
}

/**
 * Composable for a single aggregated KPI value (avg / min / max over a period).
 * Fetches once on mount then auto-refreshes every 30 seconds.
 * Pass period = 'live' to skip aggregation (returns undefined).
 */
export function useAggregatedValue(machineId: string, field: string, period: string) {
  const loading  = ref(false);
  const summary  = ref<{ avg: number; min: number; max: number; count: number } | null>(null);
  const error    = ref<string | null>(null);
  let   timer: ReturnType<typeof setInterval> | null = null;

  const isLive = period === 'live' || !period;

  const PERIOD_LABELS: Record<string, string> = {
    '5m':  '5 min',
    '15m': '15 min',
    '30m': '30 min',
    '1h':  '1 hr',
    '6h':  '6 hrs',
    '24h': '24 hrs',
    '7d':  '7 days',
    '15d': '15 days',
    '30d': '30 days',
    '3mo': '3 months',
    '6mo': '6 months',
    '1y':  '1 year',
  };
  const periodLabel = PERIOD_LABELS[period] ?? period;

  async function fetch() {
    if (isLive || !machineId || !field) return;
    loading.value = true;
    error.value   = null;
    try {
      const result = await api.getTelemetryAggregate(machineId, field, period);
      summary.value = result?.summary ?? null;
    } catch (e) {
      error.value = (e as Error).message;
    } finally {
      loading.value = false;
    }
  }

  onMounted(() => {
    fetch();
    if (!isLive) timer = setInterval(fetch, 30_000);
  });

  onUnmounted(() => {
    if (timer) clearInterval(timer);
  });

  return { summary, loading, error, periodLabel, isLive, refresh: fetch };
}
