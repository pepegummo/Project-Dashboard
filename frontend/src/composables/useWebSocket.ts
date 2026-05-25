import { onMounted, onUnmounted, ref } from 'vue';
import { wsService } from '@/services/ws.service';
import { useTelemetryStore } from '@/stores/telemetry.store';
import { useAlertStore } from '@/stores/alert.store';
import { useMachineStore } from '@/stores/machine.store';

export function useWebSocket() {
  const isConnected = ref(wsService.isConnected);

  const telemetryStore = useTelemetryStore();
  const alertStore = useAlertStore();
  const machineStore = useMachineStore();

  let offTelemetry: (() => void) | null = null;
  let offAlert: (() => void) | null = null;
  let offStatus: (() => void) | null = null;
  let offConnect: (() => void) | null = null;
  let offDisconnect: (() => void) | null = null;

  onMounted(() => {
    offConnect = wsService.onConnect(() => { isConnected.value = true; });
    offDisconnect = wsService.onDisconnect(() => { isConnected.value = false; });

    // All machines telemetry → store
    offTelemetry = wsService.onTelemetry('*', (payload) => {
      telemetryStore.updateSnapshot(payload.machineId, payload.timestamp, payload.data as any);
    });

    // Alerts → store
    offAlert = wsService.onAlert((payload) => {
      alertStore.addLiveAlert(payload);
    });

    // Machine status changes
    offStatus = wsService.onMachineStatus((payload) => {
      machineStore.updateMachineStatus(payload.machineId, payload.status);
    });
  });

  onUnmounted(() => {
    offTelemetry?.();
    offAlert?.();
    offStatus?.();
    offConnect?.();
    offDisconnect?.();
  });

  return { isConnected };
}
