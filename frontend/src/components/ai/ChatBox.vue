<script setup lang="ts">
import { computed } from 'vue';
import { Bot, LayoutDashboard, CheckCircle2, ClipboardList, Gauge, TrendingUp, CreditCard, Activity, Table2, Bell, BarChart2 } from 'lucide-vue-next';
import type { AiMessage } from '@/types';

const props = defineProps<{ message: AiMessage }>();
const emit = defineEmits<{ 'confirm-create': [dashboardName: string] }>();

const dashboardResult = computed(() => {
  if (props.message.toolName !== 'create_custom_dashboard') return null;
  const r = props.message.toolResult as
    | { success?: boolean; dashboardId?: string; url?: string; summary?: string }
    | undefined;
  return r?.success && r.dashboardId ? r : null;
});

// Did this turn return a dashboard preview (no DB write yet)?
const previewResult = computed(() => {
  if (props.message.toolName !== 'preview_dashboard') return null;
  const r = props.message.toolResult as
    | { preview?: boolean; dashboardName?: string; widgets?: Array<{ type: string; title: string; machine: string; metric: string; unit: string }>; summary?: string }
    | undefined;
  return r?.preview && r.dashboardName ? r : null;
});

const widgetIcon = (type: string) => ({
  'line-chart': TrendingUp, gauge: Gauge, 'kpi-card': CreditCard,
  'status-card': Activity, table: Table2, 'alarm-panel': Bell, 'daily-count': BarChart2,
}[type] ?? CreditCard);

const isUser = computed(() => props.message.role === 'user');
</script>

<template>
  <div class="flex gap-3" :class="isUser ? 'flex-row-reverse' : ''">
    <div
      class="w-7 h-7 rounded-full flex-shrink-0 flex items-center justify-center text-xs font-bold"
      :class="isUser ? 'bg-primary-500/30 text-primary-400' : 'bg-accent-violet/20 text-accent-violet'"
    >
      <Bot v-if="!isUser" class="w-3.5 h-3.5" />
      <span v-else>You</span>
    </div>

    <div
      class="max-w-[75%] rounded-xl px-4 py-2.5 text-sm"
      :class="isUser
        ? 'bg-primary-500/20 text-gray-100 border border-primary-500/20'
        : 'bg-surface-200 text-gray-300 border border-white/5'"
    >
      <!-- Normal text content -->
      <p v-if="message.content" class="whitespace-pre-wrap leading-relaxed">
        {{ message.content }}
      </p>

      <!-- Dashboard preview plan card (no DB write yet) -->
      <div
        v-if="previewResult"
        class="mt-3 rounded-lg border border-violet-500/25 bg-violet-500/10 p-3"
      >
        <div class="flex items-center gap-2 text-violet-400 font-medium mb-1">
          <ClipboardList class="w-4 h-4" />
          Dashboard Plan
        </div>
        <p class="text-xs font-semibold text-gray-200 mb-2">{{ previewResult.dashboardName }}</p>
        <ul class="space-y-1 mb-3">
          <li
            v-for="(w, i) in previewResult.widgets"
            :key="i"
            class="flex items-center gap-2 text-xs text-gray-400"
          >
            <component :is="widgetIcon(w.type)" class="w-3 h-3 text-violet-400 flex-shrink-0" />
            <span class="capitalize">{{ w.type.replace('-', ' ') }}</span>
            <span v-if="w.machine" class="text-gray-500">·</span>
            <span v-if="w.machine" class="truncate">{{ w.machine }}</span>
            <span v-if="w.metric" class="text-gray-600 text-[10px]">({{ w.metric }})</span>
          </li>
        </ul>
        <div class="flex gap-2">
          <button
            class="btn-sm bg-violet-600 hover:bg-violet-500 text-white border-0 gap-1"
            @click="emit('confirm-create', previewResult!.dashboardName!)"
          >
            <CheckCircle2 class="w-3.5 h-3.5" />
            Create Dashboard
          </button>
        </div>
      </div>

      <!-- Tool success: friendly card + navigation, NO raw JSON -->
      <div
        v-if="dashboardResult"
        class="mt-3 rounded-lg border border-emerald-500/25 bg-emerald-500/10 p-3"
      >
        <div class="flex items-center gap-2 text-emerald-400 font-medium">
          <CheckCircle2 class="w-4 h-4" />
          Dashboard ready
        </div>
        <p class="text-xs text-gray-400 mt-1">{{ dashboardResult.summary }}</p>

        <RouterLink
          :to="dashboardResult.url ?? `/dashboards/${dashboardResult.dashboardId}`"
          class="btn-primary btn-sm mt-2 inline-flex items-center gap-1.5"
        >
          <LayoutDashboard class="w-3.5 h-3.5" />
          Open dashboard
        </RouterLink>
      </div>
    </div>
  </div>
</template>
