<script setup lang="ts">
import { ref, onMounted, computed } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useDashboardStore } from '@/stores/dashboard.store';
import { useMachineStore } from '@/stores/machine.store';
import { Save, Plus, ArrowLeft, Loader2, LayoutGrid } from 'lucide-vue-next';
import GridStackCanvas from '@/components/dashboard/GridStackCanvas.vue';
import WidgetToolbox from '@/components/dashboard/WidgetToolbox.vue';
import WidgetConfigModal from '@/components/dashboard/WidgetConfigModal.vue';
import type { DashboardWidget, WidgetType, WidgetLayout, WidgetConfig } from '@/types';

const route = useRoute();
const router = useRouter();
const dashboardStore = useDashboardStore();
const machineStore = useMachineStore();

const showToolbox = ref(false);
const showConfigModal = ref(false);
const editingWidget = ref<DashboardWidget | null>(null);
const saving = ref(false);

const dashboardId = computed(() => route.params.id as string);

onMounted(async () => {
  await Promise.all([
    dashboardStore.fetchDashboard(dashboardId.value),
    machineStore.fetchMachines(),
  ]);
});

async function saveLayout(layouts: Array<{ id: string; layout: WidgetLayout }>) {
  saving.value = true;
  try {
    await dashboardStore.saveLayout(layouts);
  } finally {
    saving.value = false;
  }
}

async function onAddWidget(type: WidgetType) {
  showToolbox.value = false;
  // Open config modal for the chosen widget type
  editingWidget.value = {
    id: '',
    dashboardId: dashboardId.value,
    widgetType: type,
    layout: { x: 0, y: 9999, w: 6, h: 4 },
    config: {},
    order: 0,
  };
  showConfigModal.value = true;
}

async function onSaveWidget(widget: { machineId?: string; widgetType: WidgetType; title?: string; config: WidgetConfig; layout: WidgetLayout }) {
  if (!editingWidget.value) return;
  if (editingWidget.value.id) {
    // Update existing — include machineId so machine changes take effect
    await dashboardStore.updateWidget(editingWidget.value.id, {
      machineId: widget.machineId,
      title: widget.title,
      config: widget.config,
    });
  } else {
    // Add new
    await dashboardStore.addWidget(widget);
  }
  showConfigModal.value = false;
  editingWidget.value = null;
}

function onEditWidget(widget: DashboardWidget) {
  editingWidget.value = widget;
  showConfigModal.value = true;
}

async function onRemoveWidget(widgetId: string) {
  if (!confirm('Remove this widget?')) return;
  await dashboardStore.removeWidget(widgetId);
}
</script>

<template>
  <div class="flex flex-col h-full">
    <!-- Toolbar -->
    <div class="flex items-center justify-between mb-4 gap-3 flex-shrink-0">
      <div class="flex items-center gap-3">
        <button class="btn-ghost btn-icon" @click="router.back()">
          <ArrowLeft class="w-4 h-4" />
        </button>
        <div>
          <h1 class="text-lg font-bold text-white">
            {{ dashboardStore.currentDashboard?.name ?? 'Dashboard' }}
          </h1>
          <p class="text-xs text-gray-500">
            {{ dashboardStore.widgets.length }} widgets
          </p>
        </div>
      </div>

      <div class="flex items-center gap-2">
        <button class="btn-secondary" @click="showToolbox = !showToolbox">
          <Plus class="w-4 h-4" />
          Add Widget
        </button>
        <!-- Layout auto-saves on drag; this button just shows status -->
        <button
          class="btn-primary"
          :disabled="saving"
        >
          <Loader2 v-if="saving" class="w-4 h-4 animate-spin" />
          <Save v-else class="w-4 h-4" />
          {{ saving ? 'Saving…' : 'Saved' }}
        </button>
      </div>
    </div>

    <!-- Main area -->
    <div class="flex gap-4 flex-1 min-h-0">
      <!-- Toolbox -->
      <WidgetToolbox
        v-if="showToolbox"
        @select="onAddWidget"
        @close="showToolbox = false"
      />

      <!-- Grid canvas -->
      <div class="flex-1 overflow-auto">
        <div v-if="dashboardStore.loading" class="flex items-center justify-center h-64">
          <div class="spinner" />
        </div>
        <div v-else-if="!dashboardStore.widgets.length" class="flex flex-col items-center justify-center h-64 text-center border-2 border-dashed border-white/10 rounded-xl">
          <LayoutGrid class="w-10 h-10 text-gray-600 mb-3" />
          <p class="text-gray-400 font-medium">Empty dashboard</p>
          <p class="text-gray-600 text-sm mt-1">Click "Add Widget" to get started</p>
        </div>
        <GridStackCanvas
          v-else
          :widgets="dashboardStore.widgets"
          @layout-change="saveLayout"
          @edit-widget="onEditWidget"
          @remove-widget="onRemoveWidget"
        />
      </div>
    </div>

    <!-- Widget Config Modal -->
    <WidgetConfigModal
      v-if="showConfigModal && editingWidget"
      :widget="editingWidget"
      :machines="machineStore.machines"
      @save="onSaveWidget"
      @close="showConfigModal = false; editingWidget = null"
    />
  </div>
</template>
