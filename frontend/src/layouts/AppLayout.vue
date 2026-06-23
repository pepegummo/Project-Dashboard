<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import Sidebar from '@/components/layout/Sidebar.vue'
import TopBar from '@/components/layout/TopBar.vue'
import LEDCarousel from '@/components/led/LEDCarousel.vue'
import ToastNotification from '@/components/layout/ToastNotification.vue'
import { useScreenMode } from '@/composables/useScreenMode'
import { useDashboardStore } from '@/stores/dashboard.store'
import { api } from '@/services/api.service'

const { isLED, isMobile } = useScreenMode()
const route = useRoute()
const dashboardStore = useDashboardStore()

// Remember the dashboard the user opens in the main app as the AI page's view state,
// so it survives refresh (and replaces any AI preview). Skip LED kiosk mode — its
// carousel rotates dashboards and would clobber the user's view.
watch(() => dashboardStore.currentDashboard?.id, (id) => {
  if (id && !isLED.value) api.putSelectedDashboard(id).catch(() => {})
})

// ── Sidebar state ───────────────────────────────────────────────────────────
const sidebarOpen = ref(false)       // mobile overlay
const desktopSidebarOpen = ref(true) // desktop collapsible

const toggleSidebar = () => {
  if (isMobile.value) sidebarOpen.value = !sidebarOpen.value
  else desktopSidebarOpen.value = !desktopSidebarOpen.value
}
const closeSidebar = () => { sidebarOpen.value = false }

// Close mobile overlay on navigation
watch(() => route.path, () => { if (isMobile.value) closeSidebar() })
</script>

<template>
  <!-- ── LED Screen mode: full-viewport carousel, nothing else ── -->
  <LEDCarousel v-if="isLED" :interval="5000" />

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

  <ToastNotification />
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
