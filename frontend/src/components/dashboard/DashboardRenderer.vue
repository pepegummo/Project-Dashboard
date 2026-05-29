<script setup lang="ts">
/**
 * DashboardRenderer — read-only render of a dashboard's widgets.
 * ─────────────────────────────────────────────────────────────
 * Standalone: resolves each widget through the shared useWidgetComponents map
 * (no editor chrome, no drag/drop). Used as the landing target when the AI
 * assistant creates a dashboard, and anywhere a non-editable view is wanted.
 *
 * Pass either `dashboardId` (fetches via the API) or a `config` array directly.
 */
import { ref, onMounted } from 'vue';
import { api } from '@/services/api.service';
import { useWidgetComponents } from '@/composables/useWidgetComponents';
import type { Dashboard, DashboardWidget } from '@/types';

const props = defineProps<{
  dashboardId?: string;        // fetch by id …
  config?: DashboardWidget[];  // … or render a passed-in config directly
}>();

const { resolveWidget } = useWidgetComponents();

const dashboard = ref<Dashboard | null>(null);
const widgets = ref<DashboardWidget[]>([]);
const loading = ref(false);
const error = ref<string | null>(null);

onMounted(async () => {
  if (props.config) {
    widgets.value = props.config;
    return;
  }
  if (!props.dashboardId) return;
  loading.value = true;
  try {
    dashboard.value = await api.getDashboard(props.dashboardId);
    widgets.value = dashboard.value.widgets ?? [];
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
});

function widgetTitle(w: DashboardWidget): string {
  if (w.title) return w.title;
  const field = w.config?.field;
  const machine = w.machine?.name;
  if (machine && field) return `${machine} — ${field}`;
  if (machine) return machine;
  return w.widgetType.replace('-', ' ').replace(/\b\w/g, l => l.toUpperCase());
}
</script>

<template>
  <div class="h-full">
    <div v-if="loading" class="flex items-center justify-center h-full"><div class="spinner" /></div>

    <div v-else-if="error" class="flex items-center justify-center h-full text-sm text-red-400 px-4 text-center">
      {{ error }}
    </div>

    <div v-else-if="!widgets.length" class="flex items-center justify-center h-full text-sm text-gray-600">
      This dashboard has no widgets yet.
    </div>

    <!-- Responsive grid; each cell delegates to the per-type widget component -->
    <div v-else class="grid gap-3 p-3 auto-rows-[220px] grid-cols-1 sm:grid-cols-2 xl:grid-cols-3">
      <div
        v-for="widget in widgets"
        :key="widget.id"
        class="flex flex-col rounded-xl border border-white/5 bg-surface-100 overflow-hidden"
      >
        <!-- Minimal read-only header (no edit/remove chrome) -->
        <div class="flex items-center px-3 py-2 border-b border-white/5 flex-shrink-0 min-h-[36px]">
          <span class="text-xs font-medium text-gray-300 truncate">{{ widgetTitle(widget) }}</span>
        </div>

        <div class="flex-1 overflow-hidden p-2">
          <Suspense>
            <component
              :is="resolveWidget(widget.widgetType)"
              :widget="widget"
              class="w-full h-full"
            />
            <template #fallback>
              <div class="flex items-center justify-center h-full"><div class="spinner" /></div>
            </template>
          </Suspense>
        </div>
      </div>
    </div>
  </div>
</template>
