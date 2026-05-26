<script setup lang="ts">
import { defineAsyncComponent, computed } from 'vue';
import { GripVertical, Settings, X, TrendingUp, Gauge, CreditCard, Activity, Table2, Bell, BarChart2 } from 'lucide-vue-next';
import type { DashboardWidget } from '@/types';

const props = defineProps<{
  widget: DashboardWidget;
  onEdit?: () => void;
  onRemove?: () => void;
}>();

// Dynamic component loading by widget type
const widgetComponents = {
  'line-chart':   defineAsyncComponent(() => import('./LineChartWidget.vue')),
  'gauge':        defineAsyncComponent(() => import('./GaugeWidget.vue')),
  'kpi-card':     defineAsyncComponent(() => import('./KpiCardWidget.vue')),
  'status-card':  defineAsyncComponent(() => import('./StatusCardWidget.vue')),
  'table':        defineAsyncComponent(() => import('./TableWidget.vue')),
  'alarm-panel':  defineAsyncComponent(() => import('./AlarmPanelWidget.vue')),
  'daily-count':  defineAsyncComponent(() => import('./MachineDailyCountWidget.vue')),
};

const WidgetComponent = computed(() => widgetComponents[props.widget.widgetType] ?? null);

const widgetTitle = computed(() => {
  if (props.widget.title) return props.widget.title;
  const field = props.widget.config?.field;
  const machine = props.widget.machine?.name;
  if (machine && field) return `${machine} — ${field}`;
  if (machine) return machine;
  return props.widget.widgetType.replace('-', ' ').replace(/\b\w/g, l => l.toUpperCase());
});

const typeIcon = computed(() => {
  const map: Record<string, any> = {
    'line-chart': TrendingUp, 'gauge': Gauge, 'kpi-card': CreditCard,
    'status-card': Activity, 'table': Table2, 'alarm-panel': Bell,
    'daily-count': BarChart2,
  };
  return map[props.widget.widgetType] ?? CreditCard;
});
</script>

<template>
  <!-- `group` on the outer div so header buttons appear on ANY hover -->
  <div class="flex flex-col h-full bg-surface-100 group">
    <!-- Header -->
    <div class="flex items-center gap-2 px-3 py-2 border-b border-white/5 flex-shrink-0 min-h-[36px]">
      <div class="gs-drag-handle cursor-grab active:cursor-grabbing p-0.5 -ml-1 text-gray-600 hover:text-gray-400 transition-colors">
        <GripVertical class="w-3.5 h-3.5" />
      </div>
      <component :is="typeIcon" class="w-3 h-3 text-gray-600 flex-shrink-0" />
      <span class="text-xs font-medium text-gray-300 truncate flex-1">{{ widgetTitle }}</span>
      <div class="flex items-center gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity duration-150">
        <button
          v-if="onEdit"
          title="Edit widget"
          class="p-1 rounded hover:bg-surface-300 text-gray-500 hover:text-white transition-colors"
          @click.stop="onEdit?.()"
        >
          <Settings class="w-3 h-3" />
        </button>
        <button
          v-if="onRemove"
          title="Remove widget"
          class="p-1 rounded hover:bg-red-500/20 text-gray-500 hover:text-red-400 transition-colors"
          @click.stop="onRemove?.()"
        >
          <X class="w-3 h-3" />
        </button>
      </div>
    </div>

    <!-- Widget body -->
    <div class="flex-1 overflow-hidden p-2">
      <Suspense>
        <component
          :is="WidgetComponent"
          :widget="widget"
          class="w-full h-full"
        />
        <template #fallback>
          <div class="flex items-center justify-center h-full">
            <div class="spinner" />
          </div>
        </template>
      </Suspense>
    </div>
  </div>
</template>

<style>
/* จัดการให้ตัว Container หลักของ GridStack ไม่ล้น */
.grid-stack-item-content {
  overflow: hidden !important; 
}

/* บังคับให้พวกกราฟ (SVG/Canvas) หรือ Widget ด้านในหดตัวตามกรอบ */
.grid-stack-item-content :deep(svg),
.grid-stack-item-content :deep(canvas) {
  max-width: 100% !important;
  max-height: 100% !important;
}
</style>