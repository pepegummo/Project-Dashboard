<script setup lang="ts">
import { computed } from 'vue';
import { useRoute } from 'vue-router';
import AppLayout from '@/layouts/AppLayout.vue';

const route = useRoute();
const isBlankLayout = computed(() => route.meta.layout === 'blank');
</script>

<template>
  <div class="min-h-screen bg-surface bg-grid-pattern">
    <template v-if="isBlankLayout">
      <RouterView />
    </template>
    <AppLayout v-else>
      <RouterView />
    </AppLayout>
  </div>
</template>
