<script setup lang="ts">
import { CheckCircle2, ExternalLink } from 'lucide-vue-next';
defineProps<{ summary: string; dashboardId: string }>();
</script>

<template>
  <div class="animate-slide-in rounded-xl border border-emerald-500/25 bg-emerald-500/10 p-4 max-w-lg">
    <div class="flex items-center gap-2 text-emerald-400 font-semibold text-sm mb-1">
      <CheckCircle2 class="w-4 h-4" />
      Dashboard created
    </div>
    <p class="text-xs text-gray-400 mb-3">{{ summary }}</p>
    <RouterLink
      :to="`/dashboards/${dashboardId}`"
      class="btn-primary btn-sm inline-flex items-center gap-1.5"
    >
      <ExternalLink class="w-3.5 h-3.5" />
      Open in Editor
    </RouterLink>
  </div>
</template>
