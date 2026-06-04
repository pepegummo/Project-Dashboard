<script setup lang="ts">
import { ref, reactive, computed, watch } from 'vue';
import { X } from 'lucide-vue-next';
import { useMachineStore } from '@/stores/machine.store';
import { useAlertStore } from '@/stores/alert.store';
import type { Alert, AlertCondition, AlertSeverity } from '@/types';

const props = defineProps<{ alert?: Alert | null }>();
const emit = defineEmits<{ close: []; saved: [] }>();

const machineStore = useMachineStore();
const alertStore = useAlertStore();

const isEdit = computed(() => !!props.alert);
const saving = ref(false);
const error = ref('');

const form = reactive({
  name: '',
  description: '',
  machineId: '',
  field: '',
  condition: 'gt' as AlertCondition,
  threshold: '',
  thresholdHi: '',
  severity: 'warning' as AlertSeverity,
  cooldownSec: '300',
  isActive: true,
});

watch(() => props.alert, (a) => {
  if (a) {
    form.name = a.name;
    form.description = a.description ?? '';
    form.machineId = a.machineId;
    form.field = a.field;
    form.condition = a.condition;
    form.threshold = String(a.threshold);
    form.thresholdHi = a.thresholdHi != null ? String(a.thresholdHi) : '';
    form.severity = a.severity;
    form.cooldownSec = String(a.cooldownSec);
    form.isActive = a.isActive;
  }
}, { immediate: true });

const selectedMachine = computed(() => machineStore.machines.find(m => m.id === form.machineId));
const availableFields = computed(() => selectedMachine.value?.fields.filter(f => f.dataType === 'number') ?? []);
const needsHi = computed(() => form.condition === 'between' || form.condition === 'outside');

const conditions: { value: AlertCondition; label: string }[] = [
  { value: 'gt',      label: '> Greater than' },
  { value: 'gte',     label: '>= Greater or equal' },
  { value: 'lt',      label: '< Less than' },
  { value: 'lte',     label: '<= Less or equal' },
  { value: 'eq',      label: '= Equal' },
  { value: 'neq',     label: '≠ Not equal' },
  { value: 'between', label: 'Between (lo – hi)' },
  { value: 'outside', label: 'Outside (lo – hi)' },
];

async function submit() {
  error.value = '';
  if (!form.name.trim()) { error.value = 'Name is required.'; return; }
  if (!form.machineId)   { error.value = 'Machine is required.'; return; }
  if (!form.field.trim()) { error.value = 'Field is required.'; return; }
  if (form.threshold === '') { error.value = 'Threshold is required.'; return; }
  if (needsHi.value && form.thresholdHi === '') { error.value = 'Upper threshold is required.'; return; }

  const payload: Partial<Alert> & { machineId: string } = {
    name: form.name.trim(),
    description: form.description.trim() || undefined,
    machineId: form.machineId,
    field: form.field.trim(),
    condition: form.condition,
    threshold: Number(form.threshold),
    thresholdHi: needsHi.value ? Number(form.thresholdHi) : undefined,
    severity: form.severity,
    cooldownSec: Number(form.cooldownSec) || 300,
    isActive: form.isActive,
  };

  saving.value = true;
  try {
    if (isEdit.value && props.alert) {
      await alertStore.updateAlert(props.alert.id, payload);
    } else {
      await alertStore.createAlert(payload);
    }
    emit('saved');
    emit('close');
  } catch (e: any) {
    error.value = e?.response?.data?.message ?? e?.message ?? 'Failed to save.';
  } finally {
    saving.value = false;
  }
}
</script>

<template>
  <div class="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60 backdrop-blur-sm">
    <div class="bg-surface-100 border border-white/10 rounded-xl shadow-2xl w-full max-w-lg">
      <!-- Header -->
      <div class="flex items-center justify-between px-6 py-4 border-b border-white/10">
        <h2 class="text-base font-semibold text-white">{{ isEdit ? 'Edit Alert Rule' : 'New Alert Rule' }}</h2>
        <button class="text-gray-400 hover:text-white" @click="emit('close')"><X class="w-5 h-5" /></button>
      </div>

      <!-- Body -->
      <form class="px-6 py-5 space-y-4" @submit.prevent="submit">
        <!-- Name -->
        <div>
          <label class="form-label">Name</label>
          <input v-model="form.name" class="input" placeholder="e.g. High Temperature Alert" />
        </div>

        <!-- Description -->
        <div>
          <label class="form-label">Description <span class="text-gray-600">(optional)</span></label>
          <input v-model="form.description" class="input" placeholder="Short description" />
        </div>

        <!-- Machine + Field row -->
        <div class="grid grid-cols-2 gap-3">
          <div>
            <label class="form-label">Machine</label>
            <select v-model="form.machineId" class="input" @change="form.field = ''">
              <option value="" disabled>Select machine…</option>
              <option v-for="m in machineStore.machines" :key="m.id" :value="m.id">{{ m.name }}</option>
            </select>
          </div>
          <div>
            <label class="form-label">Field</label>
            <select v-if="availableFields.length" v-model="form.field" class="input">
              <option value="" disabled>Select field…</option>
              <option v-for="f in availableFields" :key="f.key" :value="f.key">{{ f.label || f.key }}</option>
            </select>
            <input v-else v-model="form.field" class="input" placeholder="field key" />
          </div>
        </div>

        <!-- Condition + Threshold row -->
        <div class="grid grid-cols-2 gap-3">
          <div>
            <label class="form-label">Condition</label>
            <select v-model="form.condition" class="input">
              <option v-for="c in conditions" :key="c.value" :value="c.value">{{ c.label }}</option>
            </select>
          </div>
          <div>
            <label class="form-label">{{ needsHi ? 'Lower threshold' : 'Threshold' }}</label>
            <input v-model="form.threshold" type="number" step="any" class="input" placeholder="0" />
          </div>
        </div>

        <!-- Upper threshold (between / outside) -->
        <div v-if="needsHi">
          <label class="form-label">Upper threshold</label>
          <input v-model="form.thresholdHi" type="number" step="any" class="input" placeholder="0" />
        </div>

        <!-- Severity + Cooldown row -->
        <div class="grid grid-cols-2 gap-3">
          <div>
            <label class="form-label">Severity</label>
            <select v-model="form.severity" class="input">
              <option value="info">Info</option>
              <option value="warning">Warning</option>
              <option value="critical">Critical</option>
            </select>
          </div>
          <div>
            <label class="form-label">Cooldown (seconds)</label>
            <input v-model="form.cooldownSec" type="number" min="0" class="input" placeholder="300" />
          </div>
        </div>

        <!-- Active toggle -->
        <div class="flex items-center gap-3">
          <button
            type="button"
            class="relative inline-flex h-5 w-9 items-center rounded-full transition-colors"
            :class="form.isActive ? 'bg-primary-500' : 'bg-surface-300'"
            @click="form.isActive = !form.isActive"
          >
            <span
              class="inline-block h-3.5 w-3.5 transform rounded-full bg-white shadow transition-transform"
              :class="form.isActive ? 'translate-x-4' : 'translate-x-0.5'"
            />
          </button>
          <span class="text-sm text-gray-400">{{ form.isActive ? 'Active' : 'Disabled' }}</span>
        </div>

        <!-- Error -->
        <p v-if="error" class="text-xs text-red-400">{{ error }}</p>

        <!-- Actions -->
        <div class="flex justify-end gap-2 pt-2">
          <button type="button" class="btn-secondary" @click="emit('close')">Cancel</button>
          <button type="submit" class="btn-primary" :disabled="saving">
            {{ saving ? 'Saving…' : isEdit ? 'Save changes' : 'Create rule' }}
          </button>
        </div>
      </form>
    </div>
  </div>
</template>
