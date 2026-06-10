<script setup lang="ts">
/**
 * ChatBox — renders a single AI chat message.
 * ───────────────────────────────────────────
 * Write actions the AI proposes are NOT executed automatically. They arrive as a
 * "pending_confirmation" tool result and render a Confirm/Cancel card. Once the
 * user confirms, the result becomes a success card (a dashboard link, or a short
 * "done" summary); cancelling shows a muted note. Reads never reach this card.
 */
import { computed } from 'vue';
import { Bot, LayoutDashboard, CheckCircle2, ShieldQuestion, XCircle, Loader2 } from 'lucide-vue-next';
import type { AiMessage } from '@/types';

const props = defineProps<{ message: AiMessage; busy?: boolean }>();
const emit = defineEmits<{ (e: 'confirm', m: AiMessage): void; (e: 'cancel', m: AiMessage): void }>();

const isUser = computed(() => props.message.role === 'user');
const result = computed(() => props.message.toolResult as Record<string, any> | undefined);

// A write action awaiting the user's decision.
const pending = computed(() => (result.value?.status === 'pending_confirmation' ? result.value : null));
// The user declined the action.
const cancelled = computed(() => (result.value?.status === 'cancelled' ? result.value : null));

// Did this turn create a dashboard? (executed create_custom_dashboard)
const dashboardResult = computed(() => {
  if (props.message.toolName !== 'create_custom_dashboard') return null;
  const r = result.value;
  return r?.success && r.dashboardId ? r : null;
});

// Any other executed write tool that returned a summary (alert, widget, …).
const doneResult = computed(() => {
  if (dashboardResult.value) return null;
  const r = result.value;
  return r?.success && r.summary ? r : null;
});
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
      <p v-if="message.content && !pending && !cancelled" class="whitespace-pre-wrap leading-relaxed">
        {{ message.content }}
      </p>

      <!-- Pending write action: ask the user to confirm before anything happens -->
      <div
        v-if="pending"
        class="rounded-lg border border-amber-500/30 bg-amber-500/10 p-3"
      >
        <div class="flex items-center gap-2 text-amber-400 font-medium">
          <ShieldQuestion class="w-4 h-4" />
          Confirm this action?
        </div>
        <p class="text-xs text-gray-300 mt-1">{{ pending.summary }}</p>
        <div class="flex gap-2 mt-2.5">
          <button
            class="btn-primary btn-sm inline-flex items-center gap-1.5"
            :disabled="busy"
            @click="emit('confirm', message)"
          >
            <Loader2 v-if="busy" class="w-3.5 h-3.5 animate-spin" />
            <CheckCircle2 v-else class="w-3.5 h-3.5" />
            Confirm
          </button>
          <button
            class="btn-sm btn-ghost inline-flex items-center gap-1.5"
            :disabled="busy"
            @click="emit('cancel', message)"
          >
            <XCircle class="w-3.5 h-3.5" />
            Cancel
          </button>
        </div>
      </div>

      <!-- Cancelled -->
      <div v-else-if="cancelled" class="flex items-center gap-2 text-gray-500 text-xs">
        <XCircle class="w-3.5 h-3.5" />
        Cancelled — {{ cancelled.summary }}
      </div>

      <!-- Executed: created a dashboard -->
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

      <!-- Executed: any other write action -->
      <div
        v-else-if="doneResult"
        class="mt-3 rounded-lg border border-emerald-500/25 bg-emerald-500/10 p-3"
      >
        <div class="flex items-center gap-2 text-emerald-400 font-medium">
          <CheckCircle2 class="w-4 h-4" />
          Done
        </div>
        <p class="text-xs text-gray-400 mt-1">{{ doneResult.summary }}</p>
      </div>
    </div>
  </div>
</template>
