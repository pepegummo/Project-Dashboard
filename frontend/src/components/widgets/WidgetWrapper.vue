<script setup lang="ts">
import { computed } from 'vue';
import { GripVertical, Settings, X, TrendingUp, Gauge, CreditCard, Activity, Table2, Bell, BarChart2, LineChart } from 'lucide-vue-next';
import type { DashboardWidget } from '@/types';
import { useWidgetComponents } from '@/composables/useWidgetComponents';
import { useWidgetViewStateStore } from '@/stores/widget-view-state.store';

const props = defineProps<{
  widget: DashboardWidget;
  onEdit?: () => void;
  onRemove?: () => void;
  onSelect?: () => void;
}>();

// Dynamic component loading by widget type (shared map — see useWidgetComponents)
const { resolveWidget } = useWidgetComponents();
const WidgetComponent = computed(() => resolveWidget(props.widget.widgetType));

const widgetViewStateStore = useWidgetViewStateStore();

// Element-pick mode delegation: HTML elements are tagged with `data-ai-el` by the widget
// components themselves (no per-widget click handler needed) — a single outer click here
// catches them all. Off (the default outside the AI page) → pristine `onSelect` behavior.
function onRootClick(e: MouseEvent) {
  if (widgetViewStateStore.elementPickMode) {
    const el = (e.target as HTMLElement).closest('[data-ai-el]') as HTMLElement | null;
    if (el) {
      widgetViewStateStore.setElementClick({
        widgetId: props.widget.id,
        title: widgetTitle.value,
        element: el.dataset.aiEl as any,
        detail: el.dataset.aiDetail ?? el.textContent?.trim(),
      });
      return;
    }
  }
  props.onSelect?.();
}

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
    'daily-count': BarChart2, 'chart': LineChart,
  };
  return map[props.widget.widgetType] ?? CreditCard;
});
</script>

<template>
  <!-- `group` on the outer div so header buttons appear on ANY hover -->
  <div
    class="flex flex-col h-full bg-surface-100 group"
    :class="{ 'ai-pick': widgetViewStateStore.elementPickMode }"
    @click="onRootClick"
  >
    <!-- Header -->
    <div class="flex items-center gap-2 px-3 py-2 border-b border-white/5 flex-shrink-0 min-h-[36px]">
      <div class="gs-drag-handle cursor-grab active:cursor-grabbing p-0.5 -ml-1 text-gray-600 hover:text-gray-400 transition-colors">
        <GripVertical class="w-3.5 h-3.5" />
      </div>
      <component :is="typeIcon" class="w-3 h-3 text-gray-600 flex-shrink-0" />
      <span class="text-xs font-medium text-gray-300 truncate flex-1" data-ai-el="title">{{ widgetTitle }}</span>
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

<style scoped>
/* Element-pick mode (AI page only): hover cue on any HTML element tagged data-ai-el,
   even though those elements live inside a child widget component. */
.ai-pick :deep([data-ai-el]:hover) {
  outline: 1px dashed rgba(139, 92, 246, 0.8);
  background: rgba(139, 92, 246, 0.1);
  border-radius: 4px;
  cursor: pointer;
}

/* Element-pick mode: cue that the grid area (point-snap, not a tagged overlay) is
   clickable too — !important wins over zrender's inline cursor style on the canvas. */
.ai-pick :deep(canvas) {
  cursor: crosshair !important;
}
</style>