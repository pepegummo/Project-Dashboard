<script setup lang="ts">
import { ref, nextTick, onMounted } from 'vue';
import { Sparkles, Send, Loader2, Bot, Save, Plus } from 'lucide-vue-next';
import { api } from '@/services/api.service';
import { useDashboardStore } from '@/stores/dashboard.store';
import { useMachineStore } from '@/stores/machine.store';
import GridStackCanvas from '@/components/dashboard/GridStackCanvas.vue';
import WidgetToolbox from '@/components/dashboard/WidgetToolbox.vue';
import WidgetConfigModal from '@/components/dashboard/WidgetConfigModal.vue';
import PreviewCanvasCard from '@/components/ai/PreviewCanvasCard.vue';
import CreatedCanvasCard from '@/components/ai/CreatedCanvasCard.vue';
import type { DashboardWidget, WidgetType, WidgetLayout, WidgetConfig } from '@/types';

type CanvasCard =
  | { kind: 'preview'; id: string; result: any; args: Record<string, unknown> }
  | { kind: 'created'; id: string; summary: string; dashboardId: string }

const dashboardStore = useDashboardStore();
const machineStore = useMachineStore();
const canvasCards = ref<CanvasCard[]>([]);
const highlightId = ref<string | undefined>(undefined);
const processing = ref(false);
const input = ref('');
const canvasRef = ref<HTMLElement | null>(null);
const conversationId = ref<string | null>(null);

// Editor state for live canvas
const showToolbox = ref(false);
const showConfigModal = ref(false);
const editingWidget = ref<DashboardWidget | null>(null);
const gridCanvasRef = ref<InstanceType<typeof GridStackCanvas> | null>(null);
const saving = ref(false);

// Toast state
const toastMsg = ref('');
const toastVisible = ref(false);
let toastTimer: ReturnType<typeof setTimeout> | null = null;

onMounted(() => machineStore.fetchMachines());

function showToast(msg: string) {
  toastMsg.value = msg;
  toastVisible.value = true;
  if (toastTimer) clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { toastVisible.value = false; }, 15000);
}

function uid() { return Date.now().toString(36) + Math.random().toString(36).slice(2); }

// ── Live canvas editor handlers ─────────────────────────────────────────────

function onAddWidget(type: WidgetType) {
  showToolbox.value = false;
  editingWidget.value = {
    id: '',
    dashboardId: dashboardStore.currentDashboard?.id ?? '',
    widgetType: type,
    layout: { x: 0, y: 9999, w: 6, h: 4 },
    config: {},
    order: 0,
  };
  showConfigModal.value = true;
}

async function onSaveWidget(data: { machineId?: string; widgetType: WidgetType; title?: string; config: WidgetConfig; layout: WidgetLayout }) {
  if (!editingWidget.value) return;
  if (editingWidget.value.id) {
    await dashboardStore.updateWidget(editingWidget.value.id, {
      widgetType: data.widgetType,
      machineId: data.machineId,
      title: data.title,
      config: data.config,
    });
  } else {
    await dashboardStore.addWidget(data);
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

async function onSaveLayout() {
  saving.value = true;
  try {
    const layouts = gridCanvasRef.value?.getCurrentLayouts() ?? [];
    if (layouts.length) await dashboardStore.saveLayout(layouts);
  } finally {
    saving.value = false;
  }
}

// ── Preview card widget handlers ────────────────────────────────────────────

function addPreviewWidget(widget: any) {
  const card = canvasCards.value.find(c => c.kind === 'preview') as any;
  if (card) card.result.widgets.push(widget);
}

function updatePreviewWidget(index: number, data: any) {
  const card = canvasCards.value.find(c => c.kind === 'preview') as any;
  if (card) Object.assign(card.result.widgets[index], data);
}

// ── Chat ────────────────────────────────────────────────────────────────────

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
      if (msg.role === 'assistant' && msg.content?.trim()) {
        showToast(msg.content);
      }
      if (msg.toolName === 'preview_dashboard') {
        const r = msg.toolResult as any;
        if (r?.preview && r.dashboardName) {
          const args = (msg.toolInput ?? {}) as Record<string, unknown>;
          const existing = canvasCards.value.findIndex(c => c.kind === 'preview');
          if (existing >= 0) {
            canvasCards.value[existing] = { kind: 'preview', id: uid(), result: r, args };
          } else {
            canvasCards.value.push({ kind: 'preview', id: uid(), result: r, args });
          }
        }
      }
      if (msg.toolName === 'preview_add_widget') {
        const pw = msg.toolResult as any;
        const previewCard = canvasCards.value.find(c => c.kind === 'preview') as any;
        if (pw && previewCard) {
          previewCard.result.widgets.push(pw);
        }
      }
      if (msg.toolName === 'preview_remove_widget') {
        const r = msg.toolResult as any;
        const previewCard = canvasCards.value.find(c => c.kind === 'preview') as any;
        if (r?.widgetTitle && previewCard) {
          const rt = r.widgetTitle.toLowerCase();
          const idx = previewCard.result.widgets.findIndex(
            (w: any) => { const wt = (w.title ?? '').toLowerCase(); return wt === rt || wt.includes(rt) || rt.includes(wt); }
          );
          if (idx >= 0) previewCard.result.widgets.splice(idx, 1);
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
  const card = canvasCards.value.find(c => c.kind === 'preview') as any;
  try {
    processing.value = true;
    const r = await api.executeAiTool('create_custom_dashboard', {
      ...(card?.args ?? {}),
      name: dashboardName,
      widgets: card?.result?.widgets ?? [],
    }) as any;
    if (r?.success && r.dashboardId) {
      canvasCards.value = [];
      conversationId.value = null;
      dashboardStore.currentDashboard = null;
      showToast(`Dashboard "${dashboardName}" created! Find it in the Dashboards list.`);
    }
  } catch (e: any) {
    showToast(`Error: ${e?.message ?? 'Failed to create dashboard'}`);
  } finally {
    processing.value = false;
  }
}

function removePreviewWidget(index: number) {
  const card = canvasCards.value.find(c => c.kind === 'preview') as any;
  if (card) card.result.widgets.splice(index, 1);
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
          @remove-widget="removePreviewWidget"
          @add-widget="addPreviewWidget"
          @update-widget="updatePreviewWidget"
        />
        <CreatedCanvasCard
          v-else-if="card.kind === 'created'"
          :summary="(card as any).summary"
          :dashboard-id="(card as any).dashboardId"
        />
      </template>

      <!-- Live widget grid -->
      <div v-if="dashboardStore.widgets.length" class="mt-2">
        <!-- Mini toolbar -->
        <div class="flex items-center justify-between mb-2">
          <p class="text-xs text-gray-500">{{ dashboardStore.widgets.length }} widgets · drag to move, gear to configure</p>
          <div class="flex items-center gap-2">
            <button class="btn-secondary text-xs py-1 px-2.5" @click="showToolbox = !showToolbox">
              <Plus class="w-3.5 h-3.5" />
              Add Widget
            </button>
            <button class="btn-primary text-xs py-1 px-2.5" :disabled="saving" @click="onSaveLayout">
              <Loader2 v-if="saving" class="w-3.5 h-3.5 animate-spin" />
              <Save v-else class="w-3.5 h-3.5" />
              {{ saving ? 'Saving…' : 'Save' }}
            </button>
          </div>
        </div>

        <!-- Toolbox + canvas in flex row (same pattern as DashboardEditorPage) -->
        <div class="flex gap-4">
          <WidgetToolbox
            v-if="showToolbox"
            @select="onAddWidget"
            @close="showToolbox = false"
          />
          <div class="flex-1">
            <GridStackCanvas
              ref="gridCanvasRef"
              :widgets="dashboardStore.widgets"
              :highlighted-id="highlightId"
              @edit-widget="onEditWidget"
              @remove-widget="onRemoveWidget"
            />
          </div>
        </div>
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

    <!-- Widget Config Modal (live canvas) -->
    <WidgetConfigModal
      v-if="showConfigModal && editingWidget"
      :widget="editingWidget"
      :machines="machineStore.machines"
      @save="onSaveWidget"
      @close="showConfigModal = false; editingWidget = null"
    />
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
