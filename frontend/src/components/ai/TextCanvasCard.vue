<script setup lang="ts">
import { Bot } from 'lucide-vue-next';
defineProps<{ content: string }>();
</script>

<template>
  <div class="flex gap-3 animate-slide-in">
    <div class="w-7 h-7 rounded-full bg-accent-violet/20 flex items-center justify-center flex-shrink-0 mt-0.5">
      <Bot class="w-3.5 h-3.5 text-accent-violet" />
    </div>
    <div class="flex-1 bg-surface-100 border border-white/5 rounded-xl px-4 py-3 text-sm text-gray-300 whitespace-pre-wrap leading-relaxed max-w-2xl">
      {{ content }}
    </div>
  </div>
</template>
