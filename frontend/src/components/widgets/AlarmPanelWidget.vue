<script setup lang="ts">
import { computed, onMounted } from 'vue';
import type { DashboardWidget } from '@/types';
import { useAlertStore } from '@/stores/alert.store';
import { AlertTriangle, ShieldAlert, Bell, CheckCircle2 } from 'lucide-vue-next';

const props = defineProps<{ widget: DashboardWidget }>();
const alertStore = useAlertStore();

const maxItems = computed(() => (props.widget.config?.maxItems as number) ?? 10);
const severities = computed(() => (props.widget.config?.severities as string[]) ?? ['info', 'warning', 'critical']);

const displayAlerts = computed(() => {
  return alertStore.liveAlerts
    .filter(a => severities.value.includes(a.severity))
    .filter(a => !props.widget.machineId || a.machineId === props.widget.machineId)
    .slice(0, maxItems.value);
});

const severityIcon = (s: string) => {
  if (s === 'critical') return ShieldAlert;
  if (s === 'warning') return AlertTriangle;
  return Bell;
};

const severityColor = (s: string) => {
  if (s === 'critical') return 'text-red-400 bg-red-500/10';
  if (s === 'warning') return 'text-amber-400 bg-amber-500/10';
  return 'text-blue-400 bg-blue-500/10';
};

onMounted(() => {
  alertStore.fetchActiveEvents();
});
</script>

<template>
  <div class="h-full overflow-auto p-2">
    <!-- Header -->
    <div class="flex items-center justify-between mb-2 px-1">
      <div class="flex items-center gap-1.5">
        <span :class="displayAlerts.length > 0 ? 'status-dot-error' : 'status-dot-online'" />
        <span class="text-[10px] text-gray-500 uppercase font-medium">
          {{ displayAlerts.length > 0 ? `${displayAlerts.length} Active` : 'All Clear' }}
        </span>
      </div>
    </div>

    <!-- Empty state -->
    <div v-if="!displayAlerts.length" class="flex flex-col items-center justify-center h-3/4 text-center">
      <CheckCircle2 class="w-7 h-7 text-emerald-500/40 mb-2" />
      <p class="text-xs text-gray-600">No active alerts</p>
    </div>

    <!-- Alert list -->
    <div v-else class="space-y-1.5">
      <div
        v-for="alert in displayAlerts"
        :key="alert.alertId + alert.timestamp"
        class="flex items-start gap-2 p-2 rounded-lg animate-fade-in"
        :class="severityColor(alert.severity)"
      >
        <component :is="severityIcon(alert.severity)" class="w-3.5 h-3.5 mt-0.5 flex-shrink-0" />
        <div class="flex-1 min-w-0">
          <p class="text-xs font-medium truncate">{{ alert.alertName }}</p>
          <p class="text-[10px] opacity-70 truncate">
            {{ alert.machineName }} · {{ alert.field }} = <span class="font-mono">{{ alert.value }}</span>
          </p>
        </div>
        <span class="text-[9px] opacity-50 flex-shrink-0">
          {{ new Date(alert.timestamp).toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit' }) }}
        </span>
      </div>
    </div>
  </div>
</template>
