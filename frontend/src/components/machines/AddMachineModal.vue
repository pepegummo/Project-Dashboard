<script setup lang="ts">
import { ref, reactive } from 'vue';
import { X, Plus, Trash2, ChevronRight, ChevronLeft } from 'lucide-vue-next';
import { useMachineStore } from '@/stores/machine.store';
import { api } from '@/services/api.service';
import type { MachineType, ProductionLine } from '@/types';

defineProps<{ productionLines: ProductionLine[] }>();
const emit = defineEmits<{ close: []; created: [] }>();

const machineStore = useMachineStore();

// ── Step ──────────────────────────────────────────────────────────────────────
const step = ref<1 | 2>(1);

// ── Step 1 ────────────────────────────────────────────────────────────────────
const info = reactive({
  name: '',
  type: '',
  productionLineId: '',
  serialNumber: '',
  model: '',
  manufacturer: '',
});

// ── Step 2 ────────────────────────────────────────────────────────────────────
interface FieldRow {
  key: string;
  label: string;
  unit: string;
  dataType: 'number' | 'boolean' | 'string';
  min: string;
  max: string;
  threshold: string;
  precision: string;
  isKey: boolean;
}

const fields = ref<FieldRow[]>([]);

function addField() {
  fields.value.push({
    key: '', label: '', unit: '',
    dataType: 'number',
    min: '', max: '', threshold: '',
    precision: '2',
    isKey: false,
  });
}

function removeField(i: number) {
  fields.value.splice(i, 1);
}

// Auto-fill label from key when label is still empty
function onKeyBlur(f: FieldRow) {
  if (!f.label && f.key) {
    f.label = f.key.replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase());
  }
}

// ── Validation & navigation ───────────────────────────────────────────────────
const error = ref<string | null>(null);

function goToStep2() {
  error.value = null;
  if (!info.name.trim())        { error.value = 'Machine name is required.'; return; }
  if (!info.type.trim())        { error.value = 'Machine type is required.'; return; }
  if (!info.productionLineId)   { error.value = 'Production line is required.'; return; }
  step.value = 2;
}

function goBack() {
  error.value = null;
  step.value = 1;
}

// ── Submit ────────────────────────────────────────────────────────────────────
const submitting = ref(false);

async function submit() {
  error.value = null;

  for (const [i, f] of fields.value.entries()) {
    if (!f.key.trim())   { error.value = `Field #${i + 1}: key is required.`;   return; }
    if (!f.label.trim()) { error.value = `Field #${i + 1}: label is required.`; return; }
  }

  const keys = fields.value.map(f => f.key.trim());
  if (new Set(keys).size !== keys.length) {
    error.value = 'Field keys must be unique.';
    return;
  }

  submitting.value = true;
  try {
    const machine = await machineStore.createMachine({
      name: info.name.trim(),
      type: info.type.trim() as MachineType,
      productionLineId: info.productionLineId,
      serialNumber: info.serialNumber.trim() || undefined,
      model:        info.model.trim()        || undefined,
      manufacturer: info.manufacturer.trim() || undefined,
    });

    if (fields.value.length) {
      await Promise.all(fields.value.map(f =>
        api.upsertMachineField(machine.id, {
          key:       f.key.trim(),
          label:     f.label.trim(),
          unit:      f.unit.trim() || undefined,
          dataType:  f.dataType,
          min:       f.min       !== '' ? Number(f.min)       : undefined,
          max:       f.max       !== '' ? Number(f.max)       : undefined,
          threshold: f.threshold !== '' ? Number(f.threshold) : undefined,
          precision: f.precision !== '' ? Number(f.precision) : 2,
          isKey:     f.isKey,
        } as any),
      ));
      await machineStore.fetchMachine(machine.id);
    }

    emit('created');
    emit('close');
  } catch (err) {
    error.value = (err as Error).message;
  } finally {
    submitting.value = false;
  }
}
</script>

<template>
  <Teleport to="body">
    <div
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/70 backdrop-blur-sm p-4"
      @click.self="$emit('close')"
    >
      <div
        class="bg-surface-100 border border-white/10 rounded-xl w-full shadow-2xl animate-fade-in flex flex-col"
        :class="step === 2 ? 'max-w-2xl' : 'max-w-lg'"
        style="max-height: 90vh"
      >
        <!-- Header -->
        <div class="flex items-center justify-between px-6 py-4 border-b border-white/5 flex-shrink-0">
          <div>
            <h2 class="text-base font-semibold text-white">Add Machine</h2>
            <p class="text-xs text-gray-500 mt-0.5">
              {{ step === 1 ? 'Basic info' : 'Sensor fields (optional)' }}
            </p>
          </div>
          <div class="flex items-center gap-4">
            <!-- Step indicator -->
            <div class="flex items-center gap-2 text-xs">
              <div class="flex items-center gap-1.5">
                <span
                  class="w-5 h-5 rounded-full flex items-center justify-center text-[10px] font-bold transition-colors"
                  :class="step === 1 ? 'bg-primary-500 text-white' : 'bg-primary-500/30 text-primary-400'"
                >1</span>
                <span :class="step === 1 ? 'text-white' : 'text-gray-500'">Info</span>
              </div>
              <div class="w-6 h-px bg-white/10" />
              <div class="flex items-center gap-1.5">
                <span
                  class="w-5 h-5 rounded-full flex items-center justify-center text-[10px] font-bold transition-colors"
                  :class="step === 2 ? 'bg-primary-500 text-white' : 'bg-surface-400 text-gray-500'"
                >2</span>
                <span :class="step === 2 ? 'text-white' : 'text-gray-500'">Fields</span>
              </div>
            </div>
            <button class="btn-ghost btn-icon" @click="$emit('close')">
              <X class="w-4 h-4" />
            </button>
          </div>
        </div>

        <!-- ── Step 1: Basic info ─────────────────────────────────────────── -->
        <div v-if="step === 1" class="px-6 py-5 space-y-4 overflow-y-auto">
          <div v-if="error" class="rounded-lg bg-red-500/10 border border-red-500/20 px-4 py-2 text-sm text-red-400">
            {{ error }}
          </div>

          <div class="grid grid-cols-2 gap-3">
            <div class="col-span-2">
              <label class="label">Name <span class="text-red-400">*</span></label>
              <input v-model="info.name" class="input" placeholder="e.g. Checkweigher Line 1" />
            </div>
            <div>
              <label class="label">Type <span class="text-red-400">*</span></label>
              <input v-model="info.type" class="input" placeholder="e.g. checkweigher, conveyor…" />
            </div>
            <div>
              <label class="label">Production Line <span class="text-red-400">*</span></label>
              <select v-model="info.productionLineId" class="input">
                <option value="">— Select —</option>
                <option v-for="pl in productionLines" :key="pl.id" :value="pl.id">{{ pl.name }}</option>
              </select>
            </div>
            <div>
              <label class="label">Serial Number</label>
              <input v-model="info.serialNumber" class="input" placeholder="Optional" />
            </div>
            <div>
              <label class="label">Model</label>
              <input v-model="info.model" class="input" placeholder="Optional" />
            </div>
            <div class="col-span-2">
              <label class="label">Manufacturer</label>
              <input v-model="info.manufacturer" class="input" placeholder="Optional" />
            </div>
          </div>
        </div>

        <!-- ── Step 2: Fields ─────────────────────────────────────────────── -->
        <div v-else class="flex flex-col flex-1 min-h-0">

          <!-- Error -->
          <div v-if="error" class="mx-6 mt-4 rounded-lg bg-red-500/10 border border-red-500/20 px-4 py-2 text-sm text-red-400 flex-shrink-0">
            {{ error }}
          </div>

          <!-- Scrollable field list -->
          <div class="flex-1 overflow-y-auto px-6 py-4 space-y-3">

            <!-- Empty state -->
            <div v-if="!fields.length" class="py-10 flex flex-col items-center gap-3 text-gray-500 border border-dashed border-white/10 rounded-xl">
              <div class="w-10 h-10 rounded-full bg-surface-200 flex items-center justify-center">
                <Plus class="w-5 h-5 text-gray-500" />
              </div>
              <div class="text-center">
                <p class="text-sm font-medium text-gray-400">No fields added yet</p>
                <p class="text-xs mt-1 text-gray-600">Fields define what sensor data this machine reports.<br>You can also add them later.</p>
              </div>
            </div>

            <!-- Field cards -->
            <div
              v-for="(f, i) in fields"
              :key="i"
              class="rounded-xl border border-white/10 bg-surface-200 overflow-hidden"
            >
              <!-- Top row: Key / Label / Unit + delete button -->
              <div class="flex items-end gap-3 px-4 pt-4 pb-3">
                <!-- grid: 1fr 1fr 80px -->
                <div class="grid gap-3 flex-1" style="grid-template-columns: 1fr 1fr 80px">
                  <div>
                    <label class="label">Key <span class="text-red-400">*</span></label>
                    <input
                      v-model="f.key"
                      class="input font-mono"
                      placeholder="e.g. temperature"
                      @blur="onKeyBlur(f)"
                    />
                  </div>
                  <div>
                    <label class="label">Label <span class="text-red-400">*</span></label>
                    <input v-model="f.label" class="input" placeholder="e.g. Temperature" />
                  </div>
                  <div>
                    <label class="label">Unit</label>
                    <input v-model="f.unit" class="input" placeholder="e.g. °C" />
                  </div>
                </div>
                <!-- Delete -->
                <button
                  class="mb-0.5 p-1.5 rounded-lg text-gray-600 hover:text-red-400 hover:bg-red-500/10 transition-colors flex-shrink-0"
                  @click="removeField(i)"
                >
                  <Trash2 class="w-4 h-4" />
                </button>
              </div>

              <!-- Divider -->
              <div class="mx-4 h-px bg-white/5" />

              <!-- Bottom row: DataType / Threshold / Min / Max / Precision / Key Metric -->
              <div class="px-4 pt-3 pb-4">
                <!-- grid: 1fr 1fr 80px 80px 80px 36px -->
                <div class="grid gap-3 items-end" style="grid-template-columns: 1fr 1fr 80px 80px 80px 36px">
                  <div>
                    <label class="label">Data Type</label>
                    <select v-model="f.dataType" class="input">
                      <option value="number">number</option>
                      <option value="boolean">boolean</option>
                      <option value="string">string</option>
                    </select>
                  </div>
                  <div>
                    <label class="label">Threshold</label>
                    <input v-model="f.threshold" class="input" placeholder="e.g. 80" :disabled="f.dataType !== 'number'" />
                  </div>
                  <div>
                    <label class="label">Min</label>
                    <input v-model="f.min" class="input" placeholder="—" type="number" :disabled="f.dataType !== 'number'" />
                  </div>
                  <div>
                    <label class="label">Max</label>
                    <input v-model="f.max" class="input" placeholder="—" type="number" :disabled="f.dataType !== 'number'" />
                  </div>
                  <div>
                    <label class="label">Precision</label>
                    <input v-model="f.precision" class="input" placeholder="2" type="number" min="0" max="6" :disabled="f.dataType !== 'number'" />
                  </div>
                  <!-- Key Metric checkbox -->
                  <div class="flex flex-col items-center gap-1.5 pb-0.5">
                    <label class="label text-center leading-tight">Key</label>
                    <input
                      type="checkbox"
                      v-model="f.isKey"
                      class="w-4 h-4 rounded cursor-pointer accent-blue-500"
                      title="Mark as key metric shown in the machines table"
                      @change="f.isKey && fields.forEach((x, j) => { if (j !== i) x.isKey = false })"
                    />
                  </div>
                </div>
              </div>
            </div>

            <!-- + Add field (dashed) -->
            <button
              class="w-full py-3 rounded-xl border border-dashed border-white/20 text-sm text-gray-500 hover:text-gray-300 hover:border-white/40 hover:bg-white/[0.03] transition-colors flex items-center justify-center gap-2"
              @click="addField"
            >
              <Plus class="w-4 h-4" />
              Add field
            </button>
          </div>
        </div>

        <!-- Footer -->
        <div class="flex gap-3 px-6 py-4 border-t border-white/5 flex-shrink-0">
          <template v-if="step === 1">
            <button class="btn-secondary flex-1" @click="$emit('close')">Cancel</button>
            <button class="btn-primary flex-1 justify-center" @click="goToStep2">
              Next — Add Fields
              <ChevronRight class="w-4 h-4" />
            </button>
          </template>
          <template v-else>
            <button class="btn-secondary flex-1 gap-1.5" :disabled="submitting" @click="goBack">
              <ChevronLeft class="w-4 h-4" />
              Back
            </button>
            <button class="btn-primary flex-1 justify-center" :disabled="submitting" @click="submit">
              <Plus class="w-4 h-4" />
              {{ submitting ? 'Saving…' : fields.length ? `Save Machine + ${fields.length} field${fields.length > 1 ? 's' : ''}` : 'Save Machine' }}
            </button>
          </template>
        </div>
      </div>
    </div>
  </Teleport>
</template>
