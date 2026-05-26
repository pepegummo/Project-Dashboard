import { ref, computed, onMounted, onUnmounted, watch, isRef } from 'vue';
import type { Ref } from 'vue';
import { wsService } from '@/services/ws.service';
import { useTelemetryStore } from '@/stores/telemetry.store';
import { api } from '@/services/api.service';
import type { TelemetryHistory } from '@/stores/telemetry.store';

export function useMachineTelemetry(machineId: string, fields: string[]) {
  const store = useTelemetryStore();
  const loading = ref(false);

  // Subscribe WebSocket
  let offHandlers: Array<() => void> = [];

  onMounted(async () => {
    wsService.subscribe([machineId]);

    // Load initial historical data for each field
    loading.value = true;
    try {
      for (const field of fields) {
        const series = await api.getTelemetrySeries(machineId, field, '30m');
        if (series?.data) {
          for (const point of series.data) {
            const ts = point.ts ?? (point as any).bucket;
            const val = typeof point.value === 'number' ? point.value : (point as any).avg;
            if (ts && typeof val === 'number') {
              store.updateSnapshot(machineId, ts, { [field]: val });
            }
          }
        }
      }
    } catch { /* ignore if offline */ }
    loading.value = false;
  });

  onUnmounted(() => {
    wsService.unsubscribe([machineId]);
    offHandlers.forEach(off => off());
  });

  const snapshot = computed(() => store.getLatest(machineId));

  function getFieldHistory(field: string): TelemetryHistory | undefined {
    return store.getHistory(machineId, field);
  }

  function getFieldValue(field: string): number | undefined {
    return store.getFieldValue(machineId, field);
  }

  return { snapshot, loading, getFieldHistory, getFieldValue };
}

/** Composable for a single field series within a chart widget */
export function useFieldSeries(machineId: string, field: string, timeRange: string | Ref<string> = '1h') {
  const apiData = ref<Array<{ ts: string; value: number }>>([]);
  const loading = ref(false);

  // Support both plain strings and reactive refs
  const timeRangeRef = isRef(timeRange) ? timeRange : ref(timeRange);

  async function loadFromApi() {
    loading.value = true;
    try {
      const series = await api.getTelemetrySeries(machineId, field, timeRangeRef.value);
      apiData.value = (series?.data ?? []).map(p => ({
        ts: (p as any).bucket ?? p.ts,
        value: (p as any).avg ?? p.value,
      }));
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

  // Reload whenever timeRange changes
  watch(timeRangeRef, () => { loadFromApi(); });

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
