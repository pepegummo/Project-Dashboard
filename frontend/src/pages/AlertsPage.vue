<script setup lang="ts">
import { onMounted, computed, ref } from 'vue';
import { useAlertStore } from '@/stores/alert.store';
import { useMachineStore } from '@/stores/machine.store';
import { Bell, AlertTriangle, CheckCircle2, Clock, ShieldAlert, Plus, Pencil, Trash2 } from 'lucide-vue-next';
import type { Alert, AlertSeverity } from '@/types';
import AlertRuleModal from '@/components/alerts/AlertRuleModal.vue';

const alertStore = useAlertStore();
const machineStore = useMachineStore();

const activeTab = ref<'events' | 'rules' | 'live'>('events');

const showModal = ref(false);
const editingAlert = ref<Alert | null>(null);
const deletingId = ref<string | null>(null);

function openCreate() {
  editingAlert.value = null;
  showModal.value = true;
}

function openEdit(alert: Alert) {
  editingAlert.value = alert;
  showModal.value = true;
}

async function confirmDelete(alert: Alert) {
  if (!confirm(`Delete rule "${alert.name}"? This cannot be undone.`)) return;
  deletingId.value = alert.id;
  try {
    await alertStore.deleteAlert(alert.id);
  } finally {
    deletingId.value = null;
  }
}

onMounted(async () => {
  await Promise.all([
    alertStore.fetchActiveEvents(),
    alertStore.fetchAlerts(),
    machineStore.fetchMachines(),
  ]);
});

const severityClass = (s: AlertSeverity) => {
  const map: Record<AlertSeverity, string> = {
    info: 'badge-blue', warning: 'badge-yellow', critical: 'badge-red',
  };
  return map[s] ?? 'badge-gray';
};

const severityIcon = (s: AlertSeverity) => {
  const map: Record<AlertSeverity, any> = {
    info: Bell, warning: AlertTriangle, critical: ShieldAlert,
  };
  return map[s] ?? Bell;
};

const statusBadge = (status: string) => {
  const map: Record<string, string> = { open: 'badge-red', acknowledged: 'badge-yellow', resolved: 'badge-green' };
  return map[status] ?? 'badge-gray';
};

const fmt = (ts: string) => new Date(ts).toLocaleString('en-US', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
</script>

<template>
  <div>
    <div class="page-header">
      <div>
        <h1 class="page-title">Alerts</h1>
        <p class="page-subtitle">Monitor alert rules and active events</p>
      </div>
      <button class="btn-primary flex items-center gap-2" @click="openCreate">
        <Plus class="w-4 h-4" /> New Rule
      </button>
    </div>

    <!-- Summary cards -->
    <div class="grid grid-cols-3 gap-4 mb-6">
      <div class="card border-l-2 border-l-red-500">
        <div class="text-2xl font-bold text-red-400">{{ alertStore.criticalCount }}</div>
        <div class="text-xs text-gray-500 mt-1 flex items-center gap-1">
          <ShieldAlert class="w-3 h-3" /> Critical
        </div>
      </div>
      <div class="card border-l-2 border-l-amber-500">
        <div class="text-2xl font-bold text-amber-400">{{ alertStore.warningCount }}</div>
        <div class="text-xs text-gray-500 mt-1 flex items-center gap-1">
          <AlertTriangle class="w-3 h-3" /> Warning
        </div>
      </div>
      <div class="card border-l-2 border-l-blue-500">
        <div class="text-2xl font-bold text-blue-400">{{ alertStore.alerts.length }}</div>
        <div class="text-xs text-gray-500 mt-1 flex items-center gap-1">
          <Bell class="w-3 h-3" /> Rules Defined
        </div>
      </div>
    </div>

    <!-- Tabs -->
    <div class="flex gap-1 mb-4 bg-surface-200 p-1 rounded-lg w-fit">
      <button
        v-for="tab in [{ key: 'events', label: 'Active Events' }, { key: 'rules', label: 'Alert Rules' }, { key: 'live', label: 'Live Stream' }]"
        :key="tab.key"
        class="px-4 py-2 rounded-md text-sm font-medium transition-colors"
        :class="activeTab === tab.key ? 'bg-surface-400 text-white' : 'text-gray-400 hover:text-white'"
        @click="activeTab = tab.key as any"
      >
        {{ tab.label }}
        <span
          v-if="tab.key === 'events' && alertStore.openCount"
          class="ml-1.5 px-1.5 py-0.5 rounded-full bg-red-500 text-white text-[10px]"
        >{{ alertStore.openCount }}</span>
        <span
          v-if="tab.key === 'live' && alertStore.liveAlerts.length"
          class="ml-1.5 px-1.5 py-0.5 rounded-full bg-primary-500 text-white text-[10px]"
        >{{ alertStore.liveAlerts.length }}</span>
      </button>
    </div>

    <!-- Active Events Tab -->
    <template v-if="activeTab === 'events'">
      <div v-if="alertStore.loading" class="flex justify-center py-12"><div class="spinner" /></div>
      <div v-else-if="!alertStore.activeEvents.length" class="flex flex-col items-center py-16 text-gray-500">
        <CheckCircle2 class="w-10 h-10 mb-3 text-emerald-500/50" />
        <p class="font-medium">No active alert events</p>
        <p class="text-sm text-gray-600 mt-1">All systems operating normally</p>
      </div>
      <div v-else class="space-y-2">
        <div
          v-for="event in alertStore.activeEvents"
          :key="event.id"
          class="card flex items-start gap-4"
        >
          <div class="flex-shrink-0 w-8 h-8 rounded-full flex items-center justify-center"
            :class="event.alert.severity === 'critical' ? 'bg-red-500/20 text-red-400' : event.alert.severity === 'warning' ? 'bg-amber-500/20 text-amber-400' : 'bg-blue-500/20 text-blue-400'"
          >
            <component :is="severityIcon(event.alert.severity)" class="w-4 h-4" />
          </div>

          <div class="flex-1 min-w-0">
            <div class="flex items-center gap-2 flex-wrap">
              <span class="font-medium text-white text-sm">{{ event.alert.name }}</span>
              <span :class="severityClass(event.alert.severity)">{{ event.alert.severity }}</span>
              <span :class="statusBadge(event.status)">{{ event.status }}</span>
            </div>
            <p class="text-xs text-gray-400 mt-1">
              {{ event.alert.machine?.name }} · <span class="font-mono">{{ event.alert.field }}</span> =
              <span class="text-white font-mono">{{ event.value }}</span>
              (threshold: {{ event.alert.threshold }})
            </p>
            <p class="text-xs text-gray-600 mt-0.5 flex items-center gap-1">
              <Clock class="w-3 h-3" />{{ fmt(event.createdAt) }}
            </p>
          </div>

          <div class="flex gap-2 flex-shrink-0">
            <button
              v-if="event.status === 'open'"
              class="btn-sm btn-secondary"
              @click="alertStore.acknowledgeEvent(event.id)"
            >Ack</button>
            <button
              class="btn-sm btn-ghost text-emerald-400 hover:bg-emerald-500/10"
              @click="alertStore.resolveEvent(event.id)"
            >Resolve</button>
          </div>
        </div>
      </div>
    </template>

    <!-- Rules Tab -->
    <template v-if="activeTab === 'rules'">
      <div class="table-container">
        <table class="table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Machine</th>
              <th>Field</th>
              <th>Condition</th>
              <th>Severity</th>
              <th>Status</th>
              <th>Events</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="alert in alertStore.alerts" :key="alert.id">
              <td class="font-medium text-white">{{ alert.name }}</td>
              <td class="text-gray-400">{{ alert.machine?.name ?? machineStore.machineById(alert.machineId)?.name ?? '—' }}</td>
              <td><code class="text-xs bg-surface-300 px-1.5 py-0.5 rounded text-cyan-400">{{ alert.field }}</code></td>
              <td class="text-gray-400">
                {{ alert.condition }} {{ alert.threshold }}
                <span v-if="alert.thresholdHi">– {{ alert.thresholdHi }}</span>
              </td>
              <td><span :class="severityClass(alert.severity)">{{ alert.severity }}</span></td>
              <td>
                <div class="flex items-center gap-1.5">
                  <span :class="alert.isActive ? 'status-dot-online' : 'status-dot-offline'" />
                  <span class="text-xs text-gray-400">{{ alert.isActive ? 'Active' : 'Disabled' }}</span>
                </div>
              </td>
              <td class="text-gray-400">{{ alert._count?.events ?? 0 }}</td>
              <td>
                <div class="flex items-center gap-1">
                  <button class="btn-sm btn-ghost text-gray-400 hover:text-white" title="Edit" @click="openEdit(alert)">
                    <Pencil class="w-3.5 h-3.5" />
                  </button>
                  <button
                    class="btn-sm btn-ghost text-gray-400 hover:text-red-400"
                    title="Delete"
                    :disabled="deletingId === alert.id"
                    @click="confirmDelete(alert)"
                  >
                    <Trash2 class="w-3.5 h-3.5" />
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>

    <!-- Live Stream Tab -->
    <template v-if="activeTab === 'live'">
      <div class="card">
        <div class="flex items-center gap-2 mb-3">
          <span class="status-dot-online" />
          <span class="text-xs font-medium text-emerald-400">Live alert stream</span>
          <button class="ml-auto btn-sm btn-ghost text-gray-500" @click="alertStore.clearLiveAlerts()">Clear</button>
        </div>
        <div v-if="!alertStore.liveAlerts.length" class="py-8 text-center text-gray-600 text-sm">
          No alerts received yet — they'll appear here in real-time
        </div>
        <div v-else class="space-y-2 max-h-96 overflow-y-auto">
          <div
            v-for="alert in alertStore.liveAlerts"
            :key="alert.alertId + alert.timestamp"
            class="flex items-start gap-3 p-3 rounded-lg bg-surface-200 border border-white/5 animate-fade-in"
          >
            <span
              class="w-2 h-2 mt-1.5 rounded-full flex-shrink-0"
              :class="alert.severity === 'critical' ? 'bg-red-500' : alert.severity === 'warning' ? 'bg-amber-500' : 'bg-blue-500'"
            />
            <div class="flex-1 min-w-0">
              <div class="text-xs font-medium text-white">{{ alert.alertName }}</div>
              <div class="text-xs text-gray-400 mt-0.5">
                {{ alert.machineName }} · <code class="text-cyan-400">{{ alert.field }}</code> =
                <span class="text-white font-mono">{{ alert.value }}</span>
              </div>
            </div>
            <span class="text-[10px] text-gray-600 flex-shrink-0">
              {{ new Date(alert.timestamp).toLocaleTimeString() }}
            </span>
          </div>
        </div>
      </div>
    </template>

    <!-- Create / Edit modal -->
    <AlertRuleModal
      v-if="showModal"
      :alert="editingAlert"
      @close="showModal = false"
      @saved="alertStore.fetchAlerts()"
    />
  </div>
</template>
