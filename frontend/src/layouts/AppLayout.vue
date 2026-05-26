<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useRoute } from 'vue-router'
import Sidebar from '@/components/layout/Sidebar.vue'
import TopBar from '@/components/layout/TopBar.vue'
import LEDCarousel from '@/components/led/LEDCarousel.vue'
import type { LEDItem } from '@/components/led/LEDCarousel.vue'
import { useScreenMode } from '@/composables/useScreenMode'
import { useAlertStore } from '@/stores/alert.store'

const { isLED, isMobile } = useScreenMode()
const alertStore = useAlertStore()
const route = useRoute()

// ── Sidebar state ───────────────────────────────────────────────────────────
const sidebarOpen = ref(false)           // mobile overlay
const desktopSidebarOpen = ref(true)     // desktop collapsible

const toggleSidebar = () => {
  if (isMobile.value) sidebarOpen.value = !sidebarOpen.value
  else desktopSidebarOpen.value = !desktopSidebarOpen.value
}
const closeSidebar = () => { sidebarOpen.value = false }

// Close mobile overlay on navigation
watch(() => route.path, () => { if (isMobile.value) closeSidebar() })

// ── LED carousel items ──────────────────────────────────────────────────────
// Replace static values with live store/composable values as needed.
const ledItems = computed<LEDItem[]>(() => [
  {
    label: 'Weight',
    value: '483.88',
    unit: 'g',
    color: 'text-green-400',
  },
  {
    label: 'Throughput',
    value: '1,240',
    unit: 'u / h',
    color: 'text-cyan-400',
  },
  {
    label: 'Alerts',
    value: alertStore.openCount,
    unit: 'open',
    color: alertStore.openCount > 0 ? 'text-red-400' : 'text-gray-500',
  },
])
</script>

<template>
  <!-- ── LED Screen mode: full-viewport carousel, nothing else ── -->
  <LEDCarousel v-if="isLED" :items="ledItems" :interval="5000" />

  <!-- ── Desktop / Mobile layout ── -->
  <div v-else class="flex h-screen overflow-hidden">

    <!-- Mobile backdrop: tap outside to close sidebar -->
    <Transition name="backdrop">
      <div
        v-if="isMobile && sidebarOpen"
        class="fixed inset-0 z-30 bg-black/60 backdrop-blur-sm"
        @click="closeSidebar"
      />
    </Transition>

    <!-- Desktop sidebar: collapses by shrinking width -->
    <div
      v-if="!isMobile"
      class="transition-[width] duration-300 ease-in-out overflow-hidden flex-shrink-0"
      :style="{ width: desktopSidebarOpen ? '16rem' : '0px' }"
    >
      <Sidebar />
    </div>

    <!-- Mobile sidebar: fixed overlay, CSS-translated in/out -->
    <Sidebar
      v-if="isMobile"
      show-close
      class="fixed inset-y-0 left-0 z-40 transition-transform duration-300 ease-in-out will-change-transform"
      :class="sidebarOpen ? 'translate-x-0' : '-translate-x-full'"
      @close="closeSidebar"
    />

    <!-- Main column -->
    <div class="flex flex-col flex-1 overflow-hidden min-w-0">
      <TopBar @toggle-sidebar="toggleSidebar" />

      <main class="flex-1 overflow-y-auto p-4 md:p-6">
        <slot />
      </main>
    </div>
  </div>
</template>

<style scoped>
/* Mobile backdrop fade */
.backdrop-enter-active,
.backdrop-leave-active {
  transition: opacity 0.25s ease;
}
.backdrop-enter-from,
.backdrop-leave-to {
  opacity: 0;
}
</style>
