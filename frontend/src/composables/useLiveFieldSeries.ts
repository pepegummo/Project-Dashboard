import { ref, onMounted, onUnmounted } from 'vue';
import type { Ref } from 'vue';
import { api } from '@/services/api.service';
import { wsService } from '@/services/ws.service';

const WINDOW_MS = 30 * 60 * 1000;

export function useLiveFieldSeries(machineId: Ref<string>, field: Ref<string>) {
  const liveData = ref<Array<{ ts: string; value: number }>>([]);
  const loading  = ref(false);

  async function loadInitial() {
    if (!machineId.value || !field.value) return;
    loading.value = true;
    try {
      const series = await api.getTelemetrySeries(machineId.value, field.value, { timeRange: '30m' });
      liveData.value = (series?.data ?? []).map(p => {
        const raw = p as any;
        return { ts: raw.bucket ?? p.ts, value: Number(raw.avg ?? p.value) };
      });
    } finally {
      loading.value = false;
    }
  }

  let offTelemetry: (() => void) | null = null;
  let refreshTimer: ReturnType<typeof setInterval> | null = null;

  onMounted(() => {
    loadInitial();
    if (machineId.value) wsService.subscribe([machineId.value]);
    offTelemetry = wsService.onTelemetry(machineId.value || '*', (payload) => {
      const val = payload.data[field.value];
      if (val == null) return;
      const cutoff = Date.now() - WINDOW_MS;
      liveData.value.push({ ts: payload.timestamp, value: Number(val) });
      liveData.value = liveData.value.filter(p => new Date(p.ts).getTime() > cutoff);
    });
    // Re-anchor from REST every 5 min to keep buckets accurate after raw WS appends
    refreshTimer = setInterval(loadInitial, 5 * 60 * 1000);
  });

  onUnmounted(() => {
    offTelemetry?.();
    if (refreshTimer) clearInterval(refreshTimer);
    if (machineId.value) wsService.unsubscribe([machineId.value]);
  });

  return { liveData, loading };
}
