<script setup lang="ts">
import { computed, onMounted } from 'vue';
import type { DashboardWidget } from '@/types';
import { useAlertStore } from '@/stores/alert.store';
import { AlertTriangle, ShieldAlert, Bell, CheckCircle2 } from 'lucide-vue-next';

const props = defineProps<{ widget: DashboardWidget }>();
const alertStore = useAlertStore();

const maxItems = computed(() => (props.widget.config?.maxItems as number) ?? 10);
const severities = computed(() => (props.widget.config?.severities as string[]) ?? ['info', 'warning', 'critical']);

// Use activeEvents (DB-backed, fetched on mount + polled every 30s).
// Previously used liveAlerts which starts empty on every page load → "All Clear" bug.
// AlertEvent shape: { id, alertId, value, createdAt, alert: { name, field, severity, machine: { id, name } } }
const displayAlerts = computed(() => {
  return alertStore.activeEvents
    .filter(e => severities.value.includes(e.alert?.severity ?? ''))
    .filter(e => !props.widget.machineId || e.alert?.machine?.id === props.widget.machineId)
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
  // Seed active events once from REST; subsequent updates arrive via WS → alert store.
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
        v-for="event in displayAlerts"
        :key="event.id"
        class="flex items-start gap-2 p-2 rounded-lg animate-fade-in"
        :class="severityColor(event.alert?.severity ?? '')"
      >
        <component :is="severityIcon(event.alert?.severity ?? '')" class="w-3.5 h-3.5 mt-0.5 flex-shrink-0" />
        <div class="flex-1 min-w-0">
          <p class="text-xs font-medium truncate">{{ event.alert?.name }}</p>
          <p class="text-[10px] opacity-70 truncate">
            {{ event.alert?.machine?.name }} · {{ event.alert?.field }} = <span class="font-mono">{{ event.value }}</span>
          </p>
        </div>
        <span class="text-[9px] opacity-50 flex-shrink-0">
          {{ new Date(event.createdAt).toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit' }) }}
        </span>
      </div>
    </div>
  </div>
</template>
