<script setup lang="ts">
import { computed } from 'vue';
import type { DashboardWidget, MachineStatus } from '@/types';
import { useMachineStore } from '@/stores/machine.store';
import { useTelemetryStore } from '@/stores/telemetry.store';
import { Activity } from 'lucide-vue-next';

const props = defineProps<{ widget: DashboardWidget }>();

const machineStore = useMachineStore();
const telemetryStore = useTelemetryStore();

const machineId = computed(() => props.widget.machineId ?? '');
const machine = computed(() => machineStore.machineById(machineId.value));
const snapshot = computed(() => telemetryStore.getLatest(machineId.value));

const statusColors: Record<MachineStatus, string> = {
  online:      'text-emerald-400 bg-emerald-500/10 border-emerald-500/30',
  offline:     'text-gray-400 bg-gray-500/10 border-gray-500/30',
  maintenance: 'text-amber-400 bg-amber-500/10 border-amber-500/30',
  error:       'text-red-400 bg-red-500/10 border-red-500/30',
};

const dotClass: Record<MachineStatus, string> = {
  online: 'status-dot-online', offline: 'status-dot-offline',
  maintenance: 'status-dot-maintenance', error: 'status-dot-error',
};

const keyFields = computed(() => machine.value?.fields?.filter(f => f.isKey) ?? []);
</script>

<template>
  <div class="flex flex-col h-full px-4 py-3">
    <div v-if="!machine" class="flex items-center justify-center h-full text-xs text-gray-600">
      Configure machine
    </div>
    <template v-else>
      <!-- Machine info -->
      <div class="flex items-center justify-between mb-3">
        <div class="flex items-center gap-2">
          <Activity class="w-4 h-4 text-gray-500" />
          <div>
            <p class="text-sm font-semibold text-white">{{ machine.name }}</p>
            <p class="text-xs text-gray-500">{{ machine.type.replace('_', ' ') }}</p>
          </div>
        </div>
        <div
          class="flex items-center gap-1.5 px-2.5 py-1 rounded-full border text-xs font-medium"
          :class="statusColors[machine.status as MachineStatus]"
        >
          <span :class="dotClass[machine.status as MachineStatus]" />
          {{ machine.status }}
        </div>
      </div>

      <!-- Key metrics -->
      <div class="flex-1 grid grid-cols-2 gap-2">
        <div
          v-for="kf in keyFields"
          :key="kf.key"
          class="bg-surface-200 rounded-lg p-2 flex flex-col"
        >
          <p class="text-[10px] text-gray-500 uppercase tracking-wide">{{ kf.label }}</p>
          <p class="text-lg font-bold text-white font-mono tabular-nums mt-auto">
            {{ telemetryStore.getFieldValue(machineId, kf.key)?.toFixed(kf.precision ?? 1) ?? '—' }}
            <span class="text-xs text-gray-500 font-normal">{{ kf.unit }}</span>
          </p>
        </div>
      </div>

      <!-- Last seen -->
      <p v-if="snapshot" class="text-[10px] text-gray-600 mt-2">
        Updated: {{ new Date(snapshot.timestamp).toLocaleTimeString() }}
      </p>
    </template>
  </div>
</template>
