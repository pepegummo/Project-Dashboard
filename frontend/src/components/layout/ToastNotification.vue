<script setup lang="ts">
import { useToast } from '@/composables/useToast';
import { CheckCircle, XCircle, X } from 'lucide-vue-next';

const { visible, message, type, hide } = useToast();
</script>

<template>
  <Transition
    enter-active-class="transition duration-300 ease-out"
    enter-from-class="opacity-0 translate-y-[-12px]"
    enter-to-class="opacity-100 translate-y-0"
    leave-active-class="transition duration-200 ease-in"
    leave-from-class="opacity-100 translate-y-0"
    leave-to-class="opacity-0 translate-y-[-12px]"
  >
    <div
      v-if="visible"
      class="fixed top-4 right-4 z-[9999] flex items-center gap-2.5 px-4 py-3 rounded-xl shadow-lg text-sm font-medium border"
      :class="type === 'success'
        ? 'bg-emerald-500/15 border-emerald-500/30 text-emerald-300'
        : 'bg-red-500/15 border-red-500/30 text-red-300'"
    >
      <CheckCircle v-if="type === 'success'" class="w-4 h-4 flex-shrink-0" />
      <XCircle     v-else                    class="w-4 h-4 flex-shrink-0" />

      <span>{{ message }}</span>

      <button class="ml-1 opacity-60 hover:opacity-100 transition-opacity" @click="hide">
        <X class="w-3.5 h-3.5" />
      </button>
    </div>
  </Transition>
</template>
