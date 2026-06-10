<script setup lang="ts">
import { ref, onMounted, nextTick, computed } from 'vue';
import { api } from '@/services/api.service';
import { Bot, Send, Plus, Loader2, Wrench, ChevronDown, ChevronRight } from 'lucide-vue-next';
import type { AiConversation, AiMessage, AiTool } from '@/types';
import ChatBox from '@/components/ai/ChatBox.vue';

const conversations = ref<AiConversation[]>([]);
const activeConversation = ref<AiConversation | null>(null);
const messages = ref<AiMessage[]>([]);
const tools = ref<AiTool[]>([]);
const input = ref('');
const loading = ref(false);
const loadingConvs = ref(false);
const messagesRef = ref<HTMLElement | null>(null);
const inputRef = ref<HTMLTextAreaElement | null>(null);
const showTools = ref(false);
const confirming = ref<string[]>([]); // message ids currently being confirmed/cancelled

// Which messages should render: all non-tool messages, plus tool messages that
// carry a renderable card (pending confirmation, cancelled, or an executed write).
function isRenderable(m: AiMessage): boolean {
  if (m.role !== 'tool') return true;
  if (m.toolName === 'create_custom_dashboard') return true;
  const r = m.toolResult as Record<string, any> | undefined;
  if (!r) return false;
  return r.status === 'pending_confirmation' || r.status === 'cancelled' || (r.success && !!r.summary);
}

async function confirmAction(message: AiMessage, confirm: boolean) {
  if (confirming.value.includes(message.id)) return;
  confirming.value.push(message.id);
  try {
    const updated = await api.confirmAiAction(message.id, confirm);
    const idx = messages.value.findIndex(m => m.id === message.id);
    if (idx !== -1 && updated.length) {
      messages.value.splice(idx, 1, ...updated);
    }
  } catch (e: any) {
    messages.value.push({
      id: Date.now().toString(),
      conversationId: activeConversation.value!.id,
      role: 'assistant',
      content: `Could not complete the action: ${e?.message ?? 'Unknown error'}`,
      createdAt: new Date().toISOString(),
    });
  } finally {
    confirming.value = confirming.value.filter(id => id !== message.id);
    scrollToBottom();
  }
}

onMounted(async () => {
  loadingConvs.value = true;
  const [convs, toolList] = await Promise.all([
    api.getConversations(),
    api.getAiTools(),
  ]);
  conversations.value = convs;
  tools.value = toolList;
  loadingConvs.value = false;
});

async function selectConversation(conv: AiConversation) {
  activeConversation.value = conv;
  messages.value = await api.getMessages(conv.id);
  scrollToBottom();
}

async function newConversation() {
  const conv = await api.createConversation();
  conversations.value.unshift(conv);
  await selectConversation(conv);
}

async function sendMessage() {
  if (!input.value.trim() || loading.value) return;
  if (!activeConversation.value) await newConversation();

  const userContent = input.value.trim();
  input.value = '';

  // Optimistic UI
  messages.value.push({
    id: Date.now().toString(),
    conversationId: activeConversation.value!.id,
    role: 'user',
    content: userContent,
    createdAt: new Date().toISOString(),
  });
  scrollToBottom();

  loading.value = true;

  try {
    const newMsgs = await api.chat(activeConversation.value!.id, userContent);
    // Replace the optimistic user message with the persisted ones from the server
    messages.value.pop();
    messages.value.push(...newMsgs);
  } catch (e: any) {
    messages.value.push({
      id: Date.now().toString(),
      conversationId: activeConversation.value!.id,
      role: 'assistant',
      content: `Sorry, I encountered an error: ${e?.message ?? 'Unknown error'}`,
      createdAt: new Date().toISOString(),
    });
  } finally {
    loading.value = false;
    scrollToBottom();
  }
}


// Example prompts shown when a tool chip is clicked. Clicking prefills the input
// so the user can edit and send — routing through the normal agentic chat (which
// supplies parameters and renders the result) instead of a raw, paramless call.
const examplePrompts: Record<string, string> = {
  get_machines: 'List all machines and their current status.',
  get_latest_telemetry: 'What is the current weight on Checkweigher CW-01?',
  get_telemetry_trend: 'Show the temperature trend for Temp Sensor TS-01 over the last 24h.',
  get_active_alerts: 'Show all active alerts right now.',
  get_daily_count: 'How many items did Checkweigher CW-01 produce in the last 7 days?',
  get_factory_overview: 'Give me an overview of the whole factory — which machines need attention?',
  list_dashboards: 'List my dashboards.',
  create_custom_dashboard: 'Create a dashboard to monitor Temp Sensor TS-01.',
  add_widget_to_dashboard: 'Add a temperature gauge for Temp Sensor TS-01 to the Production Overview dashboard.',
  remove_widget: "Remove the 'Weight' widget from the Production Overview dashboard.",
  create_alert: 'Alert me if the temperature on Temp Sensor TS-01 goes above 30.',
  acknowledge_alert: 'Acknowledge the latest active alert.',
  resolve_alert: 'Resolve the latest active alert.',
};

function useToolExample(tool: AiTool) {
  input.value = examplePrompts[tool.name] ?? tool.description ?? '';
  showTools.value = false;
  nextTick(() => inputRef.value?.focus());
}

function scrollToBottom() {
  nextTick(() => {
    if (messagesRef.value) {
      messagesRef.value.scrollTop = messagesRef.value.scrollHeight;
    }
  });
}

function handleKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault();
    sendMessage();
  }
}
</script>

<template>
  <div class="flex h-full gap-4">
    <!-- Sidebar — conversations -->
    <div class="w-60 flex flex-col gap-2 flex-shrink-0">
      <button class="btn-primary w-full justify-center" @click="newConversation">
        <Plus class="w-4 h-4" />
        New Chat
      </button>

      <div class="flex-1 overflow-y-auto space-y-1">
        <div v-if="loadingConvs" class="flex justify-center py-4"><div class="spinner" /></div>
        <button
          v-for="conv in conversations"
          :key="conv.id"
          class="w-full text-left px-3 py-2 rounded-lg text-sm transition-colors"
          :class="activeConversation?.id === conv.id ? 'bg-primary-500/20 text-white border border-primary-500/30' : 'text-gray-400 hover:bg-surface-200 hover:text-white'"
          @click="selectConversation(conv)"
        >
          <div class="flex items-center gap-2">
            <Bot class="w-3.5 h-3.5 flex-shrink-0" />
            <span class="truncate">{{ conv.title }}</span>
          </div>
          <div class="text-[10px] text-gray-600 ml-5 mt-0.5">{{ conv._count?.messages ?? 0 }} messages</div>
        </button>

        <div v-if="!loadingConvs && !conversations.length" class="text-center py-8 text-gray-600 text-xs">
          No conversations yet
        </div>
      </div>
    </div>

    <!-- Main chat area -->
    <div class="flex-1 flex flex-col card overflow-hidden">
      <!-- Chat header -->
      <div class="flex items-center justify-between px-4 py-3 border-b border-white/5 flex-shrink-0">
        <div class="flex items-center gap-2">
          <div class="w-7 h-7 rounded-full bg-gradient-to-br from-primary-500 to-accent-violet flex items-center justify-center">
            <Bot class="w-3.5 h-3.5 text-white" />
          </div>
          <div>
            <div class="text-sm font-medium text-white">IotVision AI</div>
            <div class="text-[10px] text-emerald-400">Powered by the AI Tool Layer</div>
          </div>
        </div>
        <button
          class="btn-sm btn-ghost"
          @click="showTools = !showTools"
        >
          <Wrench class="w-3.5 h-3.5" />
          Tools ({{ tools.length }})
          <component :is="showTools ? ChevronDown : ChevronRight" class="w-3 h-3" />
        </button>
      </div>

      <!-- Tools panel -->
      <div v-if="showTools" class="px-4 py-3 border-b border-white/5 bg-surface-200/50">
        <p class="text-xs text-gray-500 mb-2 uppercase tracking-wide font-medium">Available AI Tools <span class="normal-case tracking-normal text-gray-600">· click to insert an example prompt</span></p>
        <div class="flex flex-wrap gap-2">
          <button
            v-for="tool in tools"
            :key="tool.name"
            class="px-2.5 py-1 rounded-full text-xs bg-surface-300 text-gray-300 hover:bg-primary-500/20 hover:text-primary-400 border border-white/5 transition-colors"
            :title="`Insert an example prompt for ${tool.name}`"
            @click="useToolExample(tool)"
          >
            {{ tool.name }}
          </button>
        </div>
      </div>

      <!-- Messages -->
      <div ref="messagesRef" class="flex-1 overflow-y-auto px-4 py-4 space-y-4">
        <!-- Empty state -->
        <div v-if="!activeConversation" class="flex flex-col items-center justify-center h-full text-center py-12">
          <div class="w-14 h-14 rounded-2xl bg-gradient-to-br from-primary-500/20 to-accent-violet/20 border border-primary-500/20 flex items-center justify-center mb-4">
            <Bot class="w-7 h-7 text-primary-400" />
          </div>
          <h3 class="text-white font-semibold">IotVision AI Assistant</h3>
          <p class="text-gray-500 text-sm mt-2 max-w-xs">
            Ask me about your machines, telemetry, alerts, or have me configure your dashboards.
          </p>
        </div>

        <!-- Message list -->
        <template v-else>
          <ChatBox
            v-for="msg in messages.filter(isRenderable)"
            :key="msg.id"
            :message="msg"
            :busy="confirming.includes(msg.id)"
            @confirm="confirmAction($event, true)"
            @cancel="confirmAction($event, false)"
          />

          <!-- Typing indicator -->
          <div v-if="loading" class="flex gap-3">
            <div class="w-7 h-7 rounded-full bg-accent-violet/20 flex items-center justify-center text-xs text-accent-violet">AI</div>
            <div class="bg-surface-200 border border-white/5 rounded-xl px-4 py-3">
              <div class="flex gap-1">
                <div v-for="i in 3" :key="i" class="w-1.5 h-1.5 rounded-full bg-gray-500 animate-bounce" :style="{ animationDelay: `${i * 0.15}s` }" />
              </div>
            </div>
          </div>
        </template>
      </div>

      <!-- Input area -->
      <div class="px-4 py-3 border-t border-white/5 flex-shrink-0">
        <div class="flex gap-2">
          <textarea
            ref="inputRef"
            v-model="input"
            rows="1"
            class="input flex-1 resize-none py-2.5"
            placeholder="Ask about machines, alerts, dashboards…"
            @keydown="handleKeydown"
          />
          <button
            class="btn-primary px-3 self-end"
            :disabled="!input.trim() || loading"
            @click="sendMessage"
          >
            <Loader2 v-if="loading" class="w-4 h-4 animate-spin" />
            <Send v-else class="w-4 h-4" />
          </button>
        </div>
        <p class="text-[10px] text-gray-600 mt-1.5 text-center">Press Enter to send · Shift+Enter for newline</p>
      </div>
    </div>
  </div>
</template>
