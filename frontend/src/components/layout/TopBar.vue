<script setup lang="ts">
import { ref } from 'vue';
import { useRoute } from 'vue-router';
import { Wifi, WifiOff, Bell, RefreshCw } from 'lucide-vue-next';
import { useAlertStore } from '@/stores/alert.store';
import { useWebSocket } from '@/composables/useWebSocket';

const route = useRoute();
const alertStore = useAlertStore();
const { isConnected } = useWebSocket();

const now = ref(new Date());
setInterval(() => { now.value = new Date(); }, 1000);

const timeStr = () => now.value.toLocaleTimeString('en-US', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' });
const dateStr = () => now.value.toLocaleDateString('en-US', { weekday: 'short', month: 'short', day: 'numeric' });
</script>

<template>
  <header class="flex items-center justify-between h-14 px-6 bg-surface-100/80 backdrop-blur border-b border-white/5 flex-shrink-0">
    <!-- Breadcrumb / title -->
    <div>
      <h1 class="text-sm font-semibold text-white">{{ route.meta.title ?? 'Dashboard' }}</h1>
      <p class="text-xs text-gray-500">{{ dateStr() }}</p>
    </div>

    <!-- Right actions -->
    <div class="flex items-center gap-3">
      <!-- Clock -->
      <div class="hidden md:block font-mono text-sm text-gray-400 tabular-nums">
        {{ timeStr() }}
      </div>

      <!-- WS Status -->
      <div
        class="flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg text-xs font-medium"
        :class="isConnected ? 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/20' : 'bg-red-500/10 text-red-400 border border-red-500/20'"
      >
        <Wifi v-if="isConnected" class="w-3.5 h-3.5" />
        <WifiOff v-else class="w-3.5 h-3.5" />
        <span>{{ isConnected ? 'Live' : 'Offline' }}</span>
      </div>

      <!-- Alerts indicator -->
      <RouterLink to="/alerts" class="relative p-2 rounded-lg hover:bg-surface-200 transition-colors">
        <Bell class="w-4 h-4 text-gray-400" />
        <span
          v-if="alertStore.openCount > 0"
          class="absolute -top-0.5 -right-0.5 flex items-center justify-center w-4 h-4 rounded-full bg-red-500 text-white text-[9px] font-bold"
        >
          {{ alertStore.openCount > 9 ? '9+' : alertStore.openCount }}
        </span>
      </RouterLink>
    </div>
  </header>
</template>
