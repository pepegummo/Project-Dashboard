<script setup lang="ts">
/**
 * LedViewPage.vue — Full-screen kiosk wrapper for the LED display
 * ───────────────────────────────────────────────────────────────
 * Route:  /led?w=<base64-payload>
 * Layout: blank (no sidebar, no topbar, no auth guard)
 * Usage:  Paste the URL into Viplex Express / kiosk browser set to 640×320
 *
 * The ?w= parameter is a base64-encoded JSON array of LedWidget objects
 * produced by useLedExport → encodeLedPayload.
 * If the parameter is absent or malformed, LedView falls back to its own
 * built-in mock data so the page is always renderable.
 */

import { computed } from 'vue'
import { useRoute } from 'vue-router'
import LedView          from '@/components/led/LedView.vue'
import { decodeLedPayload } from '@/composables/useLedExport'

const route = useRoute()

// ── Decode the widget config from the query string ──────────────────────────
const activeWidgets = computed(() => {
  const raw = route.query.w
  if (!raw || typeof raw !== 'string') return undefined // → LedView uses its mock data
  const decoded = decodeLedPayload(raw)
  return decoded.length > 0 ? decoded : undefined
})

// ── Detect if we're running in a real 640×320 kiosk ────────────────────────
// When true, we skip the centering wrapper and let the LED fill the viewport.
const isKioskViewport = computed(() =>
  typeof window !== 'undefined' &&
  window.innerWidth  <= 650 &&
  window.innerHeight <= 350
)
</script>

<template>
  <!--
    The outer div is pure black, full-viewport.
    In a real 640×320 kiosk the LED fills it exactly.
    On a larger dev screen it centers the panel and shows a subtle frame.
  -->
  <div
    class="min-h-screen w-full bg-black flex items-center justify-center"
    :class="isKioskViewport ? 'overflow-hidden' : 'py-8'"
  >

    <!-- Kiosk panel wrapper (adds subtle glow + outer border in dev mode) -->
    <div
      class="relative flex-shrink-0"
      :class="isKioskViewport ? '' : 'led-panel-frame'"
    >
      <!-- The actual 640 × 320 LED display -->
      <LedView :active-widgets="activeWidgets" />

      <!-- Dev-mode hint bar — hidden in real kiosk (viewport ≤ 650px wide) -->
      <div
        v-if="!isKioskViewport"
        class="absolute -bottom-7 left-0 right-0 flex items-center justify-between"
      >
        <p class="font-mono text-[10px] text-gray-700 tracking-widest uppercase">
          640 × 320 px &middot; LED Kiosk Preview
        </p>
        <p
          v-if="activeWidgets"
          class="font-mono text-[10px] text-gray-700 tracking-widest"
        >
          {{ activeWidgets.length }} widget{{ activeWidgets.length !== 1 ? 's' : '' }} loaded from URL
        </p>
        <p v-else class="font-mono text-[10px] text-gray-700 tracking-widest">
          No ?w= payload — showing mock data
        </p>
      </div>
    </div>

  </div>
</template>

<style scoped>
/* Subtle outer glow + border to demarcate the 640×320 panel in dev mode */
.led-panel-frame {
  box-shadow:
    0 0 0 1px rgba(255, 255, 255, 0.07),
    0 0 40px rgba(16, 185, 129, 0.08),
    0 0 80px rgba(16, 185, 129, 0.04);
  border-radius: 2px;
}
</style>
