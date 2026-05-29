<script setup lang="ts">
import { ref, computed, onMounted } from 'vue';
import { useMachineStore } from '@/stores/machine.store';
import { useWebSocket } from '@/composables/useWebSocket';
import { Plus, Search, Activity, Cpu, Thermometer, Eye, MoveRight, Pencil, Wrench, Power } from 'lucide-vue-next';
import type { Machine, MachineType } from '@/types';
import AddMachineModal from '@/components/machines/AddMachineModal.vue';
import EditMachineModal from '@/components/machines/EditMachineModal.vue';

const machineStore = useMachineStore();
useWebSocket();

const search = ref('');
const typeFilter = ref<MachineType | ''>('');
const showAddModal = ref(false);
const showEditModal = ref(false);
const editingMachine = ref<Machine | null>(null);
const deletingId = ref<string | null>(null);

function openEdit(machine: Machine) {
  editingMachine.value = machine;
  showEditModal.value = true;
}

async function setMaintenance(machine: Machine) {
  if (!confirm(`Set "${machine.name}" to maintenance mode?`)) return;
  deletingId.value = machine.id;
  try {
    await machineStore.updateMachine(machine.id, { status: 'maintenance' });
  } finally {
    deletingId.value = null;
  }
}

async function setOnline(machine: Machine) {
  deletingId.value = machine.id;
  try {
    await machineStore.updateMachine(machine.id, { status: 'online' });
  } finally {
    deletingId.value = null;
  }
}


onMounted(async () => {
  await Promise.all([
    machineStore.fetchMachines(),
    machineStore.fetchProductionLines(),
  ]);
});

const filtered = computed(() => {
  let list = machineStore.machines;
  if (search.value) {
    const q = search.value.toLowerCase();
    list = list.filter(m => m.name.toLowerCase().includes(q) || m.serialNumber?.toLowerCase().includes(q));
  }
  if (typeFilter.value) {
    list = list.filter(m => m.type === typeFilter.value);
  }
  return list;
});

const machineTypeIcon = (type: MachineType) => {
  const map: Record<MachineType, any> = {
    checkweigher: Activity,
    temperature_sensor: Thermometer,
    conveyor: MoveRight,
    vision_camera: Eye,
  };
  return map[type] ?? Cpu;
};

const machineTypeName = (type: MachineType) => {
  const map: Record<MachineType, string> = {
    checkweigher: 'Checkweigher',
    temperature_sensor: 'Temp Sensor',
    conveyor: 'Conveyor',
    vision_camera: 'Vision AI',
  };
  return map[type] ?? type;
};

const statusClass = (status: string) => {
  const map: Record<string, string> = {
    online: 'badge-green', offline: 'badge-gray',
    maintenance: 'badge-yellow', error: 'badge-red',
  };
  return map[status] ?? 'badge-gray';
};

const statusDotClass = (status: string) => `status-dot-${status}`;


// Summary stats
const stats = computed(() => ({
  total: machineStore.machines.length,
  online: machineStore.onlineMachines.length,
  offline: machineStore.offlineMachines.length,
  maintenance: machineStore.machines.filter(m => m.status === 'maintenance').length,
}));
</script>

<template>
  <div>
    <!-- Header -->
    <div class="page-header">
      <div>
        <h1 class="page-title">Machines</h1>
        <p class="page-subtitle">Monitor and manage production equipment</p>
      </div>
      <button class="btn-primary" @click="showAddModal = true">
        <Plus class="w-4 h-4" />
        Add Machine
      </button>
    </div>

    <!-- Stats row -->
    <div class="grid grid-cols-4 gap-4 mb-6">
      <div class="card text-center">
        <div class="text-2xl font-bold text-white">{{ stats.total }}</div>
        <div class="text-xs text-gray-500 mt-1">Total</div>
      </div>
      <div class="card text-center">
        <div class="text-2xl font-bold text-emerald-400">{{ stats.online }}</div>
        <div class="text-xs text-gray-500 mt-1">Online</div>
      </div>
      <div class="card text-center">
        <div class="text-2xl font-bold text-amber-400">{{ stats.maintenance }}</div>
        <div class="text-xs text-gray-500 mt-1">Maintenance</div>
      </div>
      <div class="card text-center">
        <div class="text-2xl font-bold text-gray-400">{{ stats.offline }}</div>
        <div class="text-xs text-gray-500 mt-1">Offline</div>
      </div>
    </div>

    <!-- Filters -->
    <div class="flex gap-3 mb-4">
      <div class="relative flex-1 max-w-sm">
        <Search class="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-500" />
        <input v-model="search" class="input pl-9" placeholder="Search machines…" />
      </div>
      <select v-model="typeFilter" class="input w-44">
        <option value="">All types</option>
        <option value="checkweigher">Checkweigher</option>
        <option value="temperature_sensor">Temp Sensor</option>
        <option value="conveyor">Conveyor</option>
        <option value="vision_camera">Vision AI</option>
      </select>
    </div>

    <!-- Loading -->
    <div v-if="machineStore.loading" class="flex items-center justify-center h-48">
      <div class="spinner" />
    </div>

    <!-- Table -->
    <div v-else class="table-container">
      <table class="table">
        <thead>
          <tr>
            <th>Machine</th>
            <th>Type</th>
            <th>Status</th>
            <th>Production Line</th>
            <th>Fields</th>
            <th>Alerts</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="machine in filtered" :key="machine.id">
            <td>
              <div class="flex items-center gap-3">
                <div class="w-8 h-8 rounded-lg bg-surface-300 flex items-center justify-center flex-shrink-0">
                  <component :is="machineTypeIcon(machine.type)" class="w-4 h-4 text-gray-400" />
                </div>
                <div>
                  <div class="font-medium text-white">{{ machine.name }}</div>
                  <div class="text-xs text-gray-500">{{ machine.serialNumber ?? '—' }}</div>
                </div>
              </div>
            </td>
            <td>
              <span class="badge badge-gray">{{ machineTypeName(machine.type) }}</span>
            </td>
            <td>
              <div class="flex items-center gap-2">
                <span :class="statusDotClass(machine.status)" />
                <span :class="statusClass(machine.status)">{{ machine.status }}</span>
              </div>
            </td>
            <td class="text-gray-400">{{ machine.productionLine?.name ?? '—' }}</td>
            <td class="text-gray-400">{{ machine.fields?.length ?? 0 }} fields</td>
            <td>
              <span
                class="badge"
                :class="(machine._count?.alerts ?? 0) > 0 ? 'badge-red' : 'badge-gray'"
              >
                {{ machine._count?.alerts ?? 0 }}
              </span>
            </td>
            <td>
              <div class="flex items-center gap-1">
                <button
                  class="p-1.5 rounded-lg text-gray-500 hover:text-white hover:bg-white/10 transition-colors"
                  title="Edit machine"
                  @click="openEdit(machine)"
                >
                  <Pencil class="w-3.5 h-3.5" />
                </button>
                <button
                  v-if="machine.status !== 'maintenance'"
                  class="p-1.5 rounded-lg text-gray-500 hover:text-amber-400 hover:bg-amber-500/10 transition-colors"
                  title="Set to maintenance"
                  :disabled="deletingId === machine.id"
                  @click="setMaintenance(machine)"
                >
                  <Wrench class="w-3.5 h-3.5" />
                </button>
                <button
                  v-else
                  class="p-1.5 rounded-lg text-amber-400 hover:text-emerald-400 hover:bg-emerald-500/10 transition-colors"
                  title="Set back to online"
                  :disabled="deletingId === machine.id"
                  @click="setOnline(machine)"
                >
                  <Power class="w-3.5 h-3.5" />
                </button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
      <div v-if="!filtered.length" class="py-12 text-center text-gray-500">
        No machines match your search
      </div>
    </div>
  </div>

  <AddMachineModal
    v-if="showAddModal"
    :production-lines="machineStore.productionLines"
    @close="showAddModal = false"
    @created="machineStore.fetchMachines()"
  />

  <EditMachineModal
    v-if="showEditModal && editingMachine"
    :machine="editingMachine"
    :production-lines="machineStore.productionLines"
    @close="showEditModal = false; editingMachine = null"
    @updated="machineStore.fetchMachines()"
  />
</template>
