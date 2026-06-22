<script setup lang="ts">
import { ref, nextTick } from 'vue';
import { Sparkles, Send, Loader2, Bot } from 'lucide-vue-next';
import { api } from '@/services/api.service';
import { useDashboardStore } from '@/stores/dashboard.store';
import GridStackCanvas from '@/components/dashboard/GridStackCanvas.vue';
import PreviewCanvasCard from '@/components/ai/PreviewCanvasCard.vue';
import CreatedCanvasCard from '@/components/ai/CreatedCanvasCard.vue';

type CanvasCard =
  | { kind: 'preview'; id: string; result: any }
  | { kind: 'created'; id: string; summary: string; dashboardId: string }

const dashboardStore = useDashboardStore();
const canvasCards = ref<CanvasCard[]>([]);
const highlightId = ref<string | undefined>(undefined);
const processing = ref(false);
const input = ref('');
const canvasRef = ref<HTMLElement | null>(null);
const conversationId = ref<string | null>(null);

// Toast state
const toastMsg = ref('');
const toastVisible = ref(false);
let toastTimer: ReturnType<typeof setTimeout> | null = null;

function showToast(msg: string) {
  toastMsg.value = msg;
  toastVisible.value = true;
  if (toastTimer) clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { toastVisible.value = false; }, 5000);
}

function uid() { return Date.now().toString(36) + Math.random().toString(36).slice(2); }

async function sendMessage() {
  const text = input.value.trim();
  if (!text || processing.value) return;
  input.value = '';
  processing.value = true;

  try {
    if (!conversationId.value) {
      const conv = await api.createConversation();
      conversationId.value = conv.id;
    }

    const messages = await api.chat(conversationId.value!, text);

    for (const msg of messages) {
      // AI text replies → toast
      if (msg.role === 'assistant' && msg.content?.trim()) {
        showToast(msg.content);
      }
      // Dashboard preview (confirm-before-create)
      if (msg.toolName === 'preview_dashboard') {
        const r = msg.toolResult as any;
        if (r?.preview && r.dashboardName) {
          canvasCards.value.push({ kind: 'preview', id: uid(), result: r });
        }
      }
      // Dashboard created → load widgets onto canvas
      if (msg.toolName === 'create_custom_dashboard') {
        const r = msg.toolResult as any;
        if (r?.success && r.dashboardId) {
          canvasCards.value.push({ kind: 'created', id: uid(), summary: r.summary ?? '', dashboardId: r.dashboardId });
          await dashboardStore.fetchDashboard(r.dashboardId);
        }
      }
      // Locate widget → spotlight it on the canvas
      if (msg.toolName === 'locate_widget') {
        const r = msg.toolResult as any;
        if (r?.found && r.widgetId) {
          highlightId.value = r.widgetId;
          setTimeout(() => { highlightId.value = undefined; }, 3500);
        }
      }
    }
  } catch (e: any) {
    showToast(`Error: ${e?.message ?? 'Something went wrong. Please try again.'}`);
  } finally {
    processing.value = false;
    scrollToBottom();
  }
}

async function confirmCreate(dashboardName: string) {
  input.value = `Confirmed. Please create the dashboard "${dashboardName}".`;
  await sendMessage();
}

function scrollToBottom() {
  nextTick(() => {
    if (canvasRef.value) canvasRef.value.scrollTop = canvasRef.value.scrollHeight;
  });
}

function handleKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); sendMessage(); }
}
</script>

<template>
  <div class="flex flex-col h-full">
    <!-- ── CANVAS AREA ── -->
    <div ref="canvasRef" class="flex-1 overflow-y-auto px-6 py-6 flex flex-col gap-4 relative min-h-0">

      <!-- Empty state -->
      <div
        v-if="!canvasCards.length && !dashboardStore.widgets.length"
        class="flex-1 flex flex-col items-center justify-center text-center py-24 select-none"
      >
        <div class="w-16 h-16 rounded-2xl bg-gradient-to-br from-primary-500/20 to-accent-violet/20 border border-primary-500/20 flex items-center justify-center mb-5">
          <Sparkles class="w-8 h-8 text-primary-400" />
        </div>
        <h2 class="text-xl font-semibold text-white mb-2">IotVision AI</h2>
        <p class="text-gray-500 text-sm max-w-sm leading-relaxed">
          Describe what you want to monitor and dashboards will appear here.<br>
          <span class="text-gray-600">Try "Create a temperature monitor for Machine A."</span>
        </p>
      </div>

      <!-- Card feed (preview + created only) -->
      <template v-for="card in canvasCards" :key="card.id">
        <PreviewCanvasCard
          v-if="card.kind === 'preview'"
          :result="(card as any).result"
          @confirm="confirmCreate"
        />
        <CreatedCanvasCard
          v-else-if="card.kind === 'created'"
          :summary="(card as any).summary"
          :dashboard-id="(card as any).dashboardId"
        />
      </template>

      <!-- Widget grid -->
      <div v-if="dashboardStore.widgets.length" class="mt-2">
        <GridStackCanvas
          :widgets="dashboardStore.widgets"
          :highlighted-id="highlightId"
        />
      </div>

      <!-- Spotlight overlay -->
      <div
        v-if="highlightId"
        class="canvas-spotlight-overlay"
        @click="highlightId = undefined"
      />
    </div>

    <!-- ── COMMAND BAR ── -->
    <div class="flex-shrink-0 px-6 pb-6 pt-2">
      <!-- Toast -->
      <Transition name="toast">
        <div
          v-if="toastVisible"
          class="mb-3 flex items-start gap-2.5 bg-surface-200 border border-white/10 rounded-xl px-4 py-3 shadow-card"
        >
          <div class="w-5 h-5 rounded-full bg-accent-violet/20 flex items-center justify-center flex-shrink-0 mt-0.5">
            <Bot class="w-3 h-3 text-accent-violet" />
          </div>
          <p class="text-sm text-gray-300 leading-relaxed whitespace-pre-wrap flex-1">{{ toastMsg }}</p>
          <button class="text-gray-600 hover:text-gray-400 flex-shrink-0 mt-0.5" @click="toastVisible = false">✕</button>
        </div>
      </Transition>

      <!-- Processing indicator -->
      <Transition name="fade">
        <p v-if="processing" class="text-xs text-gray-600 text-center mb-2">Processing…</p>
      </Transition>

      <div class="spotlight-bar">
        <Sparkles class="w-4 h-4 text-gray-500 flex-shrink-0" />
        <textarea
          v-model="input"
          rows="1"
          class="flex-1 bg-transparent border-0 outline-none resize-none text-sm text-gray-100 placeholder-gray-600 leading-6"
          placeholder="Ask about your factory…"
          @keydown="handleKeydown"
        />
        <button
          :disabled="!input.trim() || processing"
          class="w-8 h-8 rounded-xl bg-primary-500 hover:bg-primary-600 disabled:opacity-30 disabled:cursor-not-allowed flex items-center justify-center transition-colors flex-shrink-0"
          @click="sendMessage"
        >
          <Loader2 v-if="processing" class="w-4 h-4 text-white animate-spin" />
          <Send v-else class="w-4 h-4 text-white" />
        </button>
      </div>
      <p class="text-[10px] text-gray-700 text-center mt-2">Enter to send · Shift+Enter for new line</p>
    </div>
  </div>
</template>

<style scoped>
.fade-enter-active, .fade-leave-active { transition: opacity 0.2s; }
.fade-enter-from, .fade-leave-to { opacity: 0; }

.toast-enter-active { transition: all 0.25s ease-out; }
.toast-leave-active { transition: all 0.2s ease-in; }
.toast-enter-from { opacity: 0; transform: translateY(8px); }
.toast-leave-to   { opacity: 0; transform: translateY(4px); }
</style>
