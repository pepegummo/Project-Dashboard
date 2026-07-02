import { ref, computed, onMounted, onUnmounted, watch } from 'vue';
import type { Ref } from 'vue';
import { wsService } from '@/services/ws.service';
import { api } from '@/services/api.service';
import { mergeSeries, type MergedSeries } from './mergeSeries';

/**
 * Multi-field version of useFieldSeries (useTelemetry.ts): fetch several fields
 * of one machine over a preset timeRange and merge them onto a shared time axis
 * for a multi-series chart. Auto-refreshes every 60s.
 *
 * Window = bucket size × points (exactly `points` gap-filled buckets from the
 * backend). Like useFieldSeries, it deliberately does NOT merge raw WS points
 * into the aggregated buckets (see the comment at useTelemetry.ts:39-68).
 */
export function useMultiFieldSeries(
  machineId: string,
  fields: Ref<string[]>,
  bucket: Ref<string>,
  points: Ref<number>,
) {
  const merged = ref<MergedSeries>({ categories: [], series: [] });
  const loading = ref(false);

  async function loadFromApi() {
    if (!machineId || fields.value.length === 0) {
      merged.value = { categories: [], series: [] };
      return;
    }
    loading.value = true;
    try {
      const results = await Promise.all(
        fields.value.map(async field => {
          const series = await api.getTelemetrySeries(machineId, field, { bucket: bucket.value, points: points.value });
          const pts = (series?.data ?? []).map(p => {
            const raw = p as any;
            // avg is null on empty gap-filled buckets → keep null so the chart shows a gap.
            const value = raw.avg ?? p.value;
            return { ts: raw.bucket ?? p.ts, value: value == null ? null : Number(value) };
          });
          return { field, points: pts };
        }),
      );
      merged.value = mergeSeries(results);
    } catch { /* ok */ }
    loading.value = false;
  }

  let refreshTimer: ReturnType<typeof setInterval> | null = null;

  onMounted(() => {
    loadFromApi();
    if (machineId) wsService.subscribe([machineId]);
    refreshTimer = setInterval(loadFromApi, 60_000);
  });
  onUnmounted(() => {
    if (machineId) wsService.unsubscribe([machineId]);
    if (refreshTimer) { clearInterval(refreshTimer); refreshTimer = null; }
  });

  // Reload when the field selection or window changes; clear stale data first so
  // the previous config never bleeds through while the new request is in-flight.
  watch([fields, bucket, points], () => {
    merged.value = { categories: [], series: [] };
    loadFromApi();
  }, { deep: true });

  return {
    categories: computed(() => merged.value.categories),
    series: computed(() => merged.value.series),
    loading,
    reload: loadFromApi,
  };
}
