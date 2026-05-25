<script setup lang="ts">
import { computed } from 'vue';
import { useRoute } from 'vue-router';
import { useAuthStore } from '@/stores/auth.store';
import { useAlertStore } from '@/stores/alert.store';
import {
  LayoutDashboard, MonitorSpeaker, Bell, Bot,
  Factory, ChevronRight, LogOut, Settings,
} from 'lucide-vue-next';

const route = useRoute();
const auth = useAuthStore();
const alertStore = useAlertStore();

const navItems = [
  { to: '/dashboards',  icon: LayoutDashboard,  label: 'Dashboards' },
  { to: '/machines',    icon: MonitorSpeaker,    label: 'Machines' },
  { to: '/alerts',      icon: Bell,              label: 'Alerts', badge: true },
  { to: '/ai',          icon: Bot,               label: 'AI Assistant' },
];

const isActive = (path: string) => route.path.startsWith(path);
const alertCount = computed(() => alertStore.openCount);
</script>

<template>
  <aside class="flex flex-col w-64 min-h-screen bg-surface-100 border-r border-white/5 flex-shrink-0">
    <!-- Logo -->
    <div class="flex items-center gap-3 px-5 py-5 border-b border-white/5">
      <div class="w-8 h-8 rounded-lg bg-gradient-to-br from-primary-500 to-accent-cyan flex items-center justify-center">
        <Factory class="w-4 h-4 text-white" />
      </div>
      <div>
        <div class="text-sm font-bold text-white tracking-tight">IotVision</div>
        <div class="text-[10px] text-gray-500 uppercase tracking-widest">Industrial AI</div>
      </div>
    </div>

    <!-- Navigation -->
    <nav class="flex-1 px-3 py-4 space-y-1">
      <RouterLink
        v-for="item in navItems"
        :key="item.to"
        :to="item.to"
        class="nav-link"
        :class="{ active: isActive(item.to) }"
      >
        <component :is="item.icon" class="w-4 h-4 flex-shrink-0" />
        <span class="flex-1">{{ item.label }}</span>
        <span
          v-if="item.badge && alertCount > 0"
          class="flex items-center justify-center w-5 h-5 rounded-full bg-red-500 text-white text-[10px] font-bold"
        >
          {{ alertCount > 9 ? '9+' : alertCount }}
        </span>
        <ChevronRight
          v-if="isActive(item.to)"
          class="w-3.5 h-3.5 text-primary-400"
        />
      </RouterLink>
    </nav>

    <!-- Divider -->
    <div class="border-t border-white/5 mx-3" />

    <!-- User area -->
    <div class="px-3 py-4 space-y-1">
      <button class="nav-link w-full">
        <Settings class="w-4 h-4" />
        <span>Settings</span>
      </button>
      <button class="nav-link w-full text-red-400 hover:text-red-300 hover:bg-red-500/10" @click="auth.logout()">
        <LogOut class="w-4 h-4" />
        <span>Sign out</span>
      </button>
    </div>

    <!-- User info -->
    <div class="px-4 py-4 border-t border-white/5">
      <div class="flex items-center gap-3">
        <div class="w-8 h-8 rounded-full bg-gradient-to-br from-primary-500 to-accent-violet flex items-center justify-center text-xs font-bold text-white">
          {{ auth.user?.name?.[0]?.toUpperCase() ?? '?' }}
        </div>
        <div class="flex-1 min-w-0">
          <div class="text-xs font-medium text-white truncate">{{ auth.user?.name }}</div>
          <div class="text-[10px] text-gray-500 uppercase truncate">{{ auth.user?.role }}</div>
        </div>
      </div>
    </div>
  </aside>
</template>
