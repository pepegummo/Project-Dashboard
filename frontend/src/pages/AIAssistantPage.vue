<script setup lang="ts">
import { ref, onMounted, nextTick, computed } from 'vue';
import { api } from '@/services/api.service';
import { useAuthStore } from '@/stores/auth.store';
import { Bot, Send, Plus, Loader2, Wrench, ChevronDown, ChevronRight } from 'lucide-vue-next';
import type { AiConversation, AiMessage, AiTool } from '@/types';

const auth = useAuthStore();

const conversations = ref<AiConversation[]>([]);
const activeConversation = ref<AiConversation | null>(null);
const messages = ref<AiMessage[]>([]);
const tools = ref<AiTool[]>([]);
const input = ref('');
const loading = ref(false);
const loadingConvs = ref(false);
const messagesRef = ref<HTMLElement | null>(null);
const showTools = ref(false);

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

  // Persist user message
  await api.addMessage(activeConversation.value!.id, { role: 'user', content: userContent });

  // Simulate AI response (in production, this calls your LLM with the tool layer)
  await simulateAiResponse(userContent);

  loading.value = false;
  scrollToBottom();
}

async function simulateAiResponse(userMsg: string) {
  // This is a placeholder. In production you'd call Claude/OpenAI API
  // with the tool definitions from api.getAiTools() enabling tool-use.
  const lowerMsg = userMsg.toLowerCase();
  let responseContent = '';
  let toolUsed: { name: string; result: unknown } | null = null;

  if (lowerMsg.includes('machine') || lowerMsg.includes('status')) {
    toolUsed = { name: 'getMachines', result: await api.executeAiTool('getMachines', {}) };
    const result = toolUsed.result as any[];
    const online = result?.filter((m: any) => m.status === 'online').length ?? 0;
    responseContent = `I checked the machine status using \`getMachines\`. You have **${result?.length ?? 0} machines total**, with **${online} online**. Would you like details on a specific machine or type?`;
  } else if (lowerMsg.includes('alert')) {
    toolUsed = { name: 'getActiveAlerts', result: await api.executeAiTool('getActiveAlerts', {}) };
    const result = toolUsed.result as any[];
    responseContent = `I retrieved the active alerts using \`getActiveAlerts\`. There are currently **${result?.length ?? 0} open alert events**. ${result?.length ? 'Would you like me to acknowledge or resolve any of them?' : 'Great news — all systems are operating within normal parameters!'}`;
  } else if (lowerMsg.includes('dashboard') || lowerMsg.includes('widget')) {
    toolUsed = { name: 'getDashboards', result: await api.executeAiTool('getDashboards', {}) };
    const result = toolUsed.result as any[];
    responseContent = `I found **${result?.length ?? 0} dashboards** in your workspace. I can help you create widgets, move layouts, or configure data sources. What would you like to do?`;
  } else {
    responseContent = `I'm the IotVision AI assistant. I can help you:
- **Monitor machines**: Check status, telemetry, and key metrics
- **Manage alerts**: View, acknowledge, and resolve alert events
- **Configure dashboards**: Add widgets, adjust layouts
- **Analyze telemetry**: Query historical data and trends

What would you like to do today?`;
  }

  // Save assistant response
  const assistantMsg = await api.addMessage(activeConversation.value!.id, {
    role: 'assistant',
    content: responseContent,
    ...(toolUsed && { toolName: toolUsed.name, toolResult: toolUsed.result as any }),
  });

  messages.value.push(assistantMsg);
}

async function executeTool(tool: AiTool) {
  if (!activeConversation.value) await newConversation();
  loading.value = true;
  const result = await api.executeAiTool(tool.name, {});
  const msg = await api.addMessage(activeConversation.value!.id, {
    role: 'tool',
    content: `Tool executed: ${tool.name}`,
    toolName: tool.name,
    toolResult: result as any,
  });
  messages.value.push(msg);
  loading.value = false;
  scrollToBottom();
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
        <p class="text-xs text-gray-500 mb-2 uppercase tracking-wide font-medium">Available AI Tools</p>
        <div class="flex flex-wrap gap-2">
          <button
            v-for="tool in tools"
            :key="tool.name"
            class="px-2.5 py-1 rounded-full text-xs bg-surface-300 text-gray-300 hover:bg-primary-500/20 hover:text-primary-400 border border-white/5 transition-colors"
            @click="executeTool(tool)"
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
          <div
            v-for="msg in messages"
            :key="msg.id"
            class="flex gap-3"
            :class="msg.role === 'user' ? 'flex-row-reverse' : ''"
          >
            <!-- Avatar -->
            <div
              class="w-7 h-7 rounded-full flex-shrink-0 flex items-center justify-center text-xs font-bold"
              :class="msg.role === 'user' ? 'bg-primary-500/30 text-primary-400' : msg.role === 'tool' ? 'bg-accent-teal/20 text-accent-teal' : 'bg-accent-violet/20 text-accent-violet'"
            >
              {{ msg.role === 'user' ? auth.user?.name?.[0] : msg.role === 'tool' ? '⚡' : 'AI' }}
            </div>

            <!-- Content -->
            <div
              class="max-w-[75%] rounded-xl px-4 py-2.5 text-sm"
              :class="msg.role === 'user' ? 'bg-primary-500/20 text-gray-100 border border-primary-500/20' : 'bg-surface-200 text-gray-300 border border-white/5'"
            >
              <p class="whitespace-pre-wrap leading-relaxed">{{ msg.content }}</p>
              <div v-if="msg.toolName" class="mt-2 pt-2 border-t border-white/10">
                <p class="text-[10px] text-gray-600 flex items-center gap-1">
                  <Wrench class="w-3 h-3" />
                  Tool: <code class="text-cyan-500">{{ msg.toolName }}</code>
                </p>
              </div>
            </div>
          </div>

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
