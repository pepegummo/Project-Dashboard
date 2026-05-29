<script setup lang="ts">
/**
 * ChatBox — renders a single AI chat message.
 * ───────────────────────────────────────────
 * When the assistant turn carries a create_custom_dashboard tool result, it shows
 * a friendly confirmation card + a link to the new dashboard — never raw JSON.
 */
import { computed } from 'vue';
import { Bot, LayoutDashboard, CheckCircle2 } from 'lucide-vue-next';
import type { AiMessage } from '@/types';

const props = defineProps<{ message: AiMessage }>();

// Did this assistant turn create a dashboard?
const dashboardResult = computed(() => {
  if (props.message.toolName !== 'create_custom_dashboard') return null;
  const r = props.message.toolResult as
    | { success?: boolean; dashboardId?: string; url?: string; summary?: string }
    | undefined;
  return r?.success && r.dashboardId ? r : null;
});

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
