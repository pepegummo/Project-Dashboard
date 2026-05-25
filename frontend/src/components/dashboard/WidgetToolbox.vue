<script setup lang="ts">
import { X, TrendingUp, Gauge, CreditCard, Activity, Table2, Bell } from 'lucide-vue-next';
import type { WidgetType } from '@/types';

defineEmits<{ select: [type: WidgetType]; close: [] }>();

const widgetTypes: Array<{ type: WidgetType; label: string; description: string; icon: any; color: string }> = [
  {
    type: 'line-chart',
    label: 'Line Chart',
    description: 'Time-series trend visualization',
    icon: TrendingUp,
    color: 'from-blue-500/20 to-blue-600/10 border-blue-500/30',
  },
  {
    type: 'gauge',
    label: 'Gauge',
    description: 'Circular metric with threshold bands',
    icon: Gauge,
    color: 'from-violet-500/20 to-violet-600/10 border-violet-500/30',
  },
  {
    type: 'kpi-card',
    label: 'KPI Card',
    description: 'Key metric with current value',
    icon: CreditCard,
    color: 'from-emerald-500/20 to-emerald-600/10 border-emerald-500/30',
  },
  {
    type: 'status-card',
    label: 'Status Card',
    description: 'Machine status overview',
    icon: Activity,
    color: 'from-cyan-500/20 to-cyan-600/10 border-cyan-500/30',
  },
  {
    type: 'table',
    label: 'Data Table',
    description: 'Tabular telemetry data',
    icon: Table2,
    color: 'from-amber-500/20 to-amber-600/10 border-amber-500/30',
  },
  {
    type: 'alarm-panel',
    label: 'Alarm Panel',
    description: 'Active alert events feed',
    icon: Bell,
    color: 'from-red-500/20 to-red-600/10 border-red-500/30',
  },
];
</script>

<template>
  <div class="w-56 flex-shrink-0">
    <div class="card sticky top-0">
      <div class="flex items-center justify-between mb-4">
        <h3 class="text-sm font-semibold text-white">Widget Toolbox</h3>
        <button class="btn-ghost btn-icon btn-sm" @click="$emit('close')">
          <X class="w-3.5 h-3.5" />
        </button>
      </div>

      <div class="space-y-2">
        <button
          v-for="w in widgetTypes"
          :key="w.type"
          class="w-full text-left p-3 rounded-xl bg-gradient-to-br border hover:scale-[1.02] transition-transform"
          :class="w.color"
          @click="$emit('select', w.type)"
        >
          <div class="flex items-center gap-2 mb-1">
            <component :is="w.icon" class="w-4 h-4 text-white/70" />
            <span class="text-sm font-medium text-white">{{ w.label }}</span>
          </div>
          <p class="text-[11px] text-gray-400 leading-snug">{{ w.description }}</p>
        </button>
      </div>
    </div>
  </div>
</template>
