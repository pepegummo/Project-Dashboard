<script setup lang="ts">
import { computed, onMounted, onUnmounted } from 'vue';
import type { DashboardWidget } from '@/types';
import { useTelemetryStore } from '@/stores/telemetry.store';
import { useMachineStore } from '@/stores/machine.store';
import { api } from '@/services/api.service';
import { wsService } from '@/services/ws.service';

const props = defineProps<{ widget: DashboardWidget }>();

const telemetryStore = useTelemetryStore();
const machineStore = useMachineStore();

const machineId = computed(() => props.widget.machineId ?? '');
const machine = computed(() => machineStore.machineById(machineId.value));
const snapshot = computed(() => telemetryStore.getLatest(machineId.value));

let offTelemetry: (() => void) | null = null;

onMounted(async () => {
  if (!machineId.value) return;
  try {
    const snap = await api.getLatestTelemetry(machineId.value);
    if (snap) telemetryStore.updateSnapshot(machineId.value, (snap as any).timestamp, (snap as any).data ?? {});
  } catch {}
  wsService.subscribe([machineId.value]);
  offTelemetry = wsService.onTelemetry(machineId.value, (payload) => {
    telemetryStore.updateSnapshot(machineId.value, payload.timestamp, payload.data as any);
  });
});

onUnmounted(() => {
  offTelemetry?.();
  if (machineId.value) wsService.unsubscribe([machineId.value]);
});

const rows = computed(() => {
  if (!snapshot.value || !machine.value) return [];
  const fields = machine.value.fields ?? [];
  return Object.entries(snapshot.value.data).map(([key, value]) => {
    const fieldDef = fields.find(f => f.key === key);
    return {
      key,
      label: fieldDef?.label ?? key,
      value: typeof value === 'number' ? value.toFixed(fieldDef?.precision ?? 2) : String(value),
      unit: fieldDef?.unit ?? '',
      isKey: fieldDef?.isKey ?? false,
    };
  });
});
</script>

<template>
  <div class="h-full overflow-auto">
    <div v-if="!machineId" class="flex items-center justify-center h-full text-xs text-gray-600">
      Configure machine
    </div>
    <table v-else class="w-full text-xs">
      <thead class="sticky top-0 bg-surface-200">
        <tr>
          <th class="text-left px-3 py-2 text-gray-400 font-medium">Field</th>
          <th class="text-right px-3 py-2 text-gray-400 font-medium">Value</th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="row in rows"
          :key="row.key"
          class="border-t border-white/5"
          :class="row.isKey ? 'bg-primary-500/5' : ''"
        >
          <td class="px-3 py-2">
            <span class="text-gray-300" :class="row.isKey ? 'font-semibold text-white' : ''">{{ row.label }}</span>
            <span class="ml-1.5 text-gray-600 font-mono text-[10px]">{{ row.key }}</span>
          </td>
          <td class="px-3 py-2 text-right font-mono tabular-nums text-white">
            {{ row.value }}
            <span class="text-gray-500 ml-0.5">{{ row.unit }}</span>
          </td>
        </tr>
        <tr v-if="!rows.length">
          <td colspan="2" class="px-3 py-8 text-center text-gray-600">
            Waiting for telemetry data…
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
