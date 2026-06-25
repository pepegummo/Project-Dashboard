<script setup lang="ts">
import { ref, onMounted } from 'vue';
import type { Component } from 'vue';
import { useRouter } from 'vue-router';
import { useDashboardStore } from '@/stores/dashboard.store';
import { useAuthStore } from '@/stores/auth.store';
import { useToast } from '@/composables/useToast';
import { Plus, Download, Star, Globe, Trash2, Edit3, Loader2, TrendingUp, Gauge, Target, Activity, Table, Bell, CalendarDays, LayoutGrid } from 'lucide-vue-next';
import type { Dashboard } from '@/types';

const router = useRouter();
const dashboardStore = useDashboardStore();
const authStore = useAuthStore();
const toast = useToast();

const showCreate = ref(false);
const creating = ref(false);
const importing = ref(false);
const importFileRef = ref<HTMLInputElement | null>(null);
const newDashboard = ref({ name: '', description: '' });

onMounted(() => dashboardStore.fetchDashboards());

async function openDashboard(d: Dashboard) {
  router.push(`/dashboards/${d.id}`);
}

async function createDashboard() {
  if (!newDashboard.value.name.trim()) return;
  creating.value = true;
  try {
    const d = await dashboardStore.createDashboard(newDashboard.value);
    showCreate.value = false;
    newDashboard.value = { name: '', description: '' };
    router.push(`/dashboards/${d.id}`);
  } finally {
    creating.value = false;
  }
}

async function deleteDashboard(id: string, e: Event) {
  e.stopPropagation();
  if (!confirm('Delete this dashboard?')) return;
  await dashboardStore.deleteDashboard(id);
}

async function importDashboard(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0];
  if (!file) return;

  importing.value = true;
  try {
    const text = await file.text();
    const data = JSON.parse(text);

    if (!data.name || !Array.isArray(data.widgets)) {
      toast.show('Invalid dashboard config file', 'error');
      return;
    }

    if (data.orgId && data.orgId !== authStore.activeOrgId) {
      toast.show('Cannot import: this dashboard was exported from a different organization', 'error');
      return;
    }

    const d = await dashboardStore.createDashboard({
      name: data.name,
      description: data.description,
      tags: data.tags,
    });

    // Set currentDashboard so addWidget knows which dashboard to use
    await dashboardStore.fetchDashboard(d.id);

    for (const widget of data.widgets) {
      await dashboardStore.addWidget(widget);
    }

    toast.show(`Dashboard imported — ${data.widgets.length} widget${data.widgets.length !== 1 ? 's' : ''} loaded`);
    router.push(`/dashboards/${d.id}`);
  } catch {
    toast.show('Failed to import dashboard', 'error');
  } finally {
    importing.value = false;
    if (importFileRef.value) importFileRef.value.value = '';
  }
}

const timeAgo = (date: string) => {
  const diff = Date.now() - new Date(date).getTime();
  const mins = Math.floor(diff / 60000);
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 24) return `${hrs}h ago`;
  return `${Math.floor(hrs / 24)}d ago`;
};

const WIDGET_ICONS: Record<string, Component> = {
  'line-chart':  TrendingUp,
  'gauge':       Gauge,
  'kpi-card':    Target,
  'status-card': Activity,
  'table':       Table,
  'alarm-panel': Bell,
  'daily-count': CalendarDays,
};

type WL = { type: string; x: number; y: number; w: number; h: number }

function maxRows(layouts: WL[]): number {
  return Math.max(...layouts.map(l => l.y + l.h))
}

function cellStyle(wl: WL, rows: number) {
  const COLS = 12
  return {
    left:   `${(wl.x / COLS) * 100}%`,
    top:    `${(wl.y / rows) * 100}%`,
    width:  `${(wl.w / COLS) * 100}%`,
    height: `${(wl.h / rows) * 100}%`,
  }
}
</script>

<template>
  <div>
    <!-- Header -->
    <div class="page-header">
      <div>
        <h1 class="page-title">Dashboards</h1>
        <p class="page-subtitle">Monitor your production in real-time</p>
      </div>
      <div class="flex items-center gap-2">
        <input
          ref="importFileRef"
          type="file"
          accept=".json"
          class="hidden"
          @change="importDashboard"
        />
        <button
          class="btn-secondary"
          :disabled="importing"
          title="Import dashboard from JSON file"
          @click="importFileRef?.click()"
        >
          <Loader2 v-if="importing" class="w-4 h-4 animate-spin" />
          <Download v-else class="w-4 h-4" />
          {{ importing ? 'Importing…' : 'Import' }}
        </button>
        <button class="btn-primary" @click="showCreate = true">
          <Plus class="w-4 h-4" />
          New Dashboard
        </button>
      </div>
    </div>

    <!-- Loading -->
    <div v-if="dashboardStore.loading" class="flex items-center justify-center h-64">
      <div class="spinner" />
    </div>

    <!-- Grid -->
    <div v-else-if="dashboardStore.dashboards.length" class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
      <div
        v-for="d in dashboardStore.dashboards"
        :key="d.id"
        class="card-hover cursor-pointer group relative"
        @click="openDashboard(d)"
      >
        <!-- Preview area -->
        <div class="h-44 bg-gradient-to-br from-surface-200 to-surface-300 rounded-lg mb-4 overflow-hidden relative">
          <div class="absolute inset-0 p-1.5">
            <div class="relative w-full h-full">
              <template v-if="d.widgetLayouts?.length">
                <div
                  v-for="(wl, i) in d.widgetLayouts"
                  :key="i"
                  :style="cellStyle(wl, maxRows(d.widgetLayouts))"
                  class="absolute p-0.5"
                >
                  <div class="w-full h-full flex flex-col items-center justify-center gap-0.5 bg-white/10 rounded">
                    <component :is="WIDGET_ICONS[wl.type] ?? LayoutGrid" class="w-3.5 h-3.5 text-white/60" />
                    <span class="text-[8px] text-white/40 capitalize leading-none">{{ wl.type.replace(/-/g, ' ') }}</span>
                  </div>
                </div>
              </template>
              <div v-else class="w-full h-full flex items-center justify-center">
                <LayoutGrid class="w-6 h-6 text-white/20" />
              </div>
            </div>
          </div>
          <div
            v-if="d.isDefault"
            class="absolute top-2 left-2 badge badge-blue"
          >
            <Star class="w-3 h-3" />
            Default
          </div>
          <div
            v-if="d.isPublic"
            class="absolute top-2 right-2 badge badge-green"
          >
            <Globe class="w-3 h-3" />
            Public
          </div>
        </div>

        <div class="flex items-start justify-between">
          <div class="flex-1 min-w-0">
            <h3 class="text-sm font-semibold text-white truncate">{{ d.name }}</h3>
            <p v-if="d.description" class="text-xs text-gray-500 truncate mt-0.5">{{ d.description }}</p>
            <div class="flex items-center gap-3 mt-2 text-xs text-gray-500">
              <span>{{ d._count?.widgets ?? 0 }} widgets</span>
              <span>·</span>
              <span>{{ timeAgo(d.updatedAt) }}</span>
            </div>
          </div>

          <!-- Actions -->
          <div class="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity ml-2">
            <button
              class="p-1.5 rounded hover:bg-surface-300 text-gray-400 hover:text-white transition-colors"
              title="Edit"
              @click.stop="router.push(`/dashboards/${d.id}`)"
            >
              <Edit3 class="w-3.5 h-3.5" />
            </button>
            <button
              class="p-1.5 rounded hover:bg-red-500/20 text-gray-400 hover:text-red-400 transition-colors"
              title="Delete"
              @click="deleteDashboard(d.id, $event)"
            >
              <Trash2 class="w-3.5 h-3.5" />
            </button>
          </div>
        </div>

        <!-- Tags -->
        <div v-if="d.tags?.length" class="flex flex-wrap gap-1 mt-3">
          <span
            v-for="tag in d.tags"
            :key="tag"
            class="badge badge-gray text-[10px]"
          >
            {{ tag }}
          </span>
        </div>
      </div>
    </div>

    <!-- Empty state -->
    <div v-else class="flex flex-col items-center justify-center h-64 text-center">
      <LayoutDashboard class="w-12 h-12 text-gray-600 mb-4" />
      <p class="text-gray-400 font-medium">No dashboards yet</p>
      <p class="text-gray-600 text-sm mt-1">Create your first dashboard to start monitoring</p>
      <button class="btn-primary mt-4" @click="showCreate = true">
        <Plus class="w-4 h-4" />
        Create Dashboard
      </button>
    </div>

    <!-- Create Modal -->
    <Teleport to="body">
      <div
        v-if="showCreate"
        class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm"
        @click.self="showCreate = false"
      >
        <div class="bg-surface-100 border border-white/10 rounded-xl p-6 w-full max-w-md shadow-2xl animate-fade-in">
          <h2 class="text-lg font-semibold text-white mb-5">Create New Dashboard</h2>

          <div class="space-y-4">
            <div>
              <label class="label">Dashboard Name *</label>
              <input v-model="newDashboard.name" class="input" placeholder="Production Overview" autofocus />
            </div>
            <div>
              <label class="label">Description</label>
              <textarea v-model="newDashboard.description" class="input resize-none" rows="2" placeholder="Optional description" />
            </div>
          </div>

          <div class="flex gap-3 mt-6">
            <button class="btn-secondary flex-1" @click="showCreate = false">Cancel</button>
            <button
              class="btn-primary flex-1 justify-center"
              :disabled="!newDashboard.name.trim() || creating"
              @click="createDashboard"
            >
              <Loader2 v-if="creating" class="w-4 h-4 animate-spin" />
              {{ creating ? 'Creating…' : 'Create' }}
            </button>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>
