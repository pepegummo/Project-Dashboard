import { defineStore } from 'pinia';
import { ref, computed } from 'vue';
import { api } from '@/services/api.service';
import type { Machine, ProductionLine, Factory } from '@/types';

export const useMachineStore = defineStore('machines', () => {
  const machines = ref<Machine[]>([]);
  const productionLines = ref<ProductionLine[]>([]);
  const factories = ref<Factory[]>([]);
  const loading = ref(false);
  const error = ref<string | null>(null);

  const machineById = computed(() => {
    return (id: string) => machines.value.find(m => m.id === id);
  });

  const onlineMachines = computed(() => machines.value.filter(m => m.status === 'online'));
  const offlineMachines = computed(() => machines.value.filter(m => m.status === 'offline'));

  async function fetchMachines(filters?: Record<string, string>) {
    loading.value = true;
    error.value = null;
    try {
      machines.value = await api.getMachines(filters);
    } catch (err) {
      error.value = (err as Error).message;
    } finally {
      loading.value = false;
    }
  }

  async function fetchMachine(id: string) {
    const machine = await api.getMachine(id);
    const idx = machines.value.findIndex(m => m.id === id);
    if (idx >= 0) machines.value[idx] = machine;
    else machines.value.push(machine);
    return machine;
  }

  async function createMachine(payload: Parameters<typeof api.createMachine>[0]) {
    const machine = await api.createMachine(payload);
    machines.value.push(machine);
    return machine;
  }

  async function updateMachine(id: string, payload: Partial<Machine>) {
    const machine = await api.updateMachine(id, payload);
    const idx = machines.value.findIndex(m => m.id === id);
    if (idx >= 0) machines.value[idx] = machine;
    return machine;
  }

  async function deleteMachine(id: string) {
    await api.deleteMachine(id);
    machines.value = machines.value.filter(m => m.id !== id);
  }

  async function fetchProductionLines() {
    productionLines.value = await api.getProductionLines();
  }

  async function fetchFactories() {
    factories.value = await api.getFactories();
  }

  function updateMachineStatus(machineId: string, status: Machine['status']) {
    const machine = machines.value.find(m => m.id === machineId);
    if (machine) machine.status = status;
  }

  return {
    machines, productionLines, factories, loading, error,
    machineById, onlineMachines, offlineMachines,
    fetchMachines, fetchMachine, createMachine, updateMachine, deleteMachine,
    fetchProductionLines, fetchFactories, updateMachineStatus,
  };
});
