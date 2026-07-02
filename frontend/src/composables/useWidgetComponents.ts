/**
 * useWidgetComponents — single source of truth for the widgetType → component map.
 * ─────────────────────────────────────────────────────────────────────────────
 * Both WidgetWrapper.vue (editor chrome) and DashboardRenderer.vue (read-only)
 * resolve widgets through here, so the mapping lives in exactly one place and
 * stays aligned with the backend's allowedWidgetTypes (ai/schema.go).
 */
import { defineAsyncComponent, type Component } from 'vue';
import type { WidgetType } from '@/types';

export const widgetComponents: Record<WidgetType, Component> = {
  'line-chart':  defineAsyncComponent(() => import('@/components/widgets/LineChartWidget.vue')),
  'gauge':       defineAsyncComponent(() => import('@/components/widgets/GaugeWidget.vue')),
  'kpi-card':    defineAsyncComponent(() => import('@/components/widgets/KpiCardWidget.vue')),
  'status-card': defineAsyncComponent(() => import('@/components/widgets/StatusCardWidget.vue')),
  'table':       defineAsyncComponent(() => import('@/components/widgets/TableWidget.vue')),
  'alarm-panel': defineAsyncComponent(() => import('@/components/widgets/AlarmPanelWidget.vue')),
  'daily-count': defineAsyncComponent(() => import('@/components/widgets/MachineDailyCountWidget.vue')),
  'chart':       defineAsyncComponent(() => import('@/components/widgets/CustomChartWidget.vue')),
};

export function useWidgetComponents() {
  /** Resolve the component for a widget type, or null if unknown. */
  function resolveWidget(type: WidgetType): Component | null {
    return widgetComponents[type] ?? null;
  }
  return { widgetComponents, resolveWidget };
}
