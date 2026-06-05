import { defineStore } from 'pinia';
import { ref, computed } from 'vue';
import { api } from '@/services/api.service';
import type { Alert, AlertEvent, WsAlertPayload } from '@/types';

export const useAlertStore = defineStore('alerts', () => {
  const alerts = ref<Alert[]>([]);
  const activeEvents = ref<AlertEvent[]>([]);
  const liveAlerts = ref<WsAlertPayload[]>([]); // real-time stream
  const loading = ref(false);

  const criticalCount = computed(() => activeEvents.value.filter(e => e.alert?.severity === 'critical').length);
  const warningCount = computed(() => activeEvents.value.filter(e => e.alert?.severity === 'warning').length);
  const openCount = computed(() => activeEvents.value.filter(e => e.status === 'open').length);

  async function fetchAlerts(machineId?: string) {
    loading.value = true;
    try {
      alerts.value = await api.getAlerts(machineId) ?? [];
    } finally {
      loading.value = false;
    }
  }

  async function fetchActiveEvents() {
    loading.value = true;
    try {
      activeEvents.value = await api.getActiveAlertEvents() ?? [];
    } finally {
      loading.value = false;
    }
  }

  async function createAlert(payload: Parameters<typeof api.createAlert>[0]) {
    const alert = await api.createAlert(payload);
    alerts.value.unshift(alert);
    return alert;
  }

  async function updateAlert(id: string, payload: Partial<Alert>) {
    const updated = await api.updateAlert(id, payload);
    const idx = alerts.value.findIndex(a => a.id === id);
    if (idx >= 0) alerts.value[idx] = updated;
    return updated;
  }

  async function deleteAlert(id: string) {
    await api.deleteAlert(id);
    alerts.value = alerts.value.filter(a => a.id !== id);
  }

  async function acknowledgeEvent(eventId: string) {
    const updated = await api.acknowledgeAlertEvent(eventId);
    const idx = activeEvents.value.findIndex(e => e.id === eventId);
    if (idx >= 0) activeEvents.value[idx] = { ...activeEvents.value[idx], status: 'acknowledged' };
    return updated;
  }

  async function resolveEvent(eventId: string) {
    await api.resolveAlertEvent(eventId);
    activeEvents.value = activeEvents.value.filter(e => e.id !== eventId);
  }

  async function resolveAll() {
    const ids = activeEvents.value.map(e => e.id);
    await Promise.allSettled(ids.map(id => api.resolveAlertEvent(id)));
    activeEvents.value = [];
  }

  function addLiveAlert(alert: WsAlertPayload) {
    liveAlerts.value.unshift(alert);
    if (liveAlerts.value.length > 50) liveAlerts.value.pop();
    // The backend writes the DB event before broadcasting WS, so fetching here
    // always returns the real event with a real UUID — resolve/acknowledge work correctly.
    fetchActiveEvents();
  }

  function clearLiveAlerts() {
    liveAlerts.value = [];
  }

  return {
    alerts, activeEvents, liveAlerts, loading,
    criticalCount, warningCount, openCount,
    fetchAlerts, fetchActiveEvents,
    createAlert, updateAlert, deleteAlert,
    acknowledgeEvent, resolveEvent, resolveAll,
    addLiveAlert, clearLiveAlerts,
  };
});
