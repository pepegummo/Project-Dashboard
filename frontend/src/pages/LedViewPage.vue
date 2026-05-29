<script setup lang="ts">
/**
 * LedViewPage.vue — Full-screen kiosk wrapper for the LED display
 * ---------------------------------------------------------------
 * Route:  /led?w=<base64-payload>
 * Layout: blank (no sidebar, no topbar, no auth guard)
 * Usage:  Paste the URL into Viplex Express / kiosk browser set to 640x320
 *
 * Scaling strategy:
 *   LedView is always rendered at exactly 640x320 px internally.
 *   This page wraps it in a transform: scale() layer so the design fills
 *   whatever viewport it is shown in (dev monitor, kiosk, projector) without
 *   ever reflowing the inner layout. The holder div is sized to the visual
 *   (post-scale) dimensions so the flexbox centering stays correct.
 */

import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRoute } from 'vue-router'
import LedView           from '@/components/led/LedView.vue'
import { decodeLedPayload } from '@/composables/useLedExport'

const DESIGN_W = 640
const DESIGN_H = 320

const route = useRoute()

// -- Decode the widget config from the query string -------------------------
const activeWidgets = computed(() => {
  const raw = route.query.w
  if (!raw || typeof raw !== 'string') return undefined
  const decoded = decodeLedPayload(raw)
  return decoded.length > 0 ? decoded : undefined
})

// -- Reactive viewport size ------------------------------------------------
const vw = ref(typeof window !== 'undefined' ? window.innerWidth  : DESIGN_W)
const vh = ref(typeof window !== 'undefined' ? window.innerHeight : DESIGN_H)

function onResize() {
  vw.value = window.innerWidth
  vh.value = window.innerHeight
}
onMounted(()   => window.addEventListener('resize', onResize))
onUnmounted(() => window.removeEventListener('resize', onResize))

// Largest uniform scale that fits the design canvas inside the viewport
const scale = computed(() => Math.min(vw.value / DESIGN_W, vh.value / DESIGN_H))

// True when scale ~ 1 (running in the actual 640x320 kiosk browser)
const isKiosk = computed(() => scale.value >= 0.98)

// -- Style objects ----------------------------------------------------------
// The holder div takes up the visual (post-scale) dimensions so the outer
// flexbox can centre it without knowing about transform.
const holderStyle = computed(() => ({
  position:   'relative' as const,
  flexShrink: 0,
  width:      `${DESIGN_W * scale.value}px`,
  height:     `${DESIGN_H * scale.value}px`,
}))

// The canvas is always the raw design size; CSS transform scales it visually.
const canvasStyle = computed(() => ({
  position:        'absolute' as const,
  top:             '0',
  left:            '0',
  width:           `${DESIGN_W}px`,
  height:          `${DESIGN_H}px`,
  transformOrigin: 'top left',
  transform:       `scale(${scale.value})`,
}))
</script>

<template>
  <div class="led-page-root">

    <!-- Holder: visual size = scale x design size -> flexbox centres it cleanly -->
    <div
      :style="holderStyle"
      :class="!isKiosk ? 'led-panel-frame' : ''"
    >
      <!-- Inner canvas: always 640x320, scaled outward from top-left origin -->
      <div :style="canvasStyle">
        <LedView :active-widgets="activeWidgets" />
      </div>
    </div>

    <!-- Dev-mode caption: scale %, widget count, payload status -->
    <div
      v-if="!isKiosk"
      class="led-dev-bar"
    >
      <p class="font-mono text-[10px] text-gray-700 tracking-widest uppercase">
        640 x 320 px &middot; LED Kiosk Preview &middot; {{ Math.round(scale * 100) }}%
      </p>
      <p
        v-if="activeWidgets"
        class="font-mono text-[10px] text-gray-700 tracking-widest"
      >
        {{ activeWidgets.length }} widget{{ activeWidgets.length !== 1 ? 's' : '' }} loaded from URL
      </p>
      <p v-else class="font-mono text-[10px] text-gray-700 tracking-widest">
        No ?w= payload - showing mock data
      </p>
    </div>

  </div>
</template>

<style scoped>
.led-page-root {
  width:           100vw;
  height:          100vh;
  overflow:        hidden;
  background:      #000;
  display:         flex;
  flex-direction:  column;
  align-items:     center;
  justify-content: center;
  gap:             10px;
}

.led-panel-frame {
  box-shadow:
    0 0 0 1px rgba(255, 255, 255, 0.07),
    0 0 40px rgba(16, 185, 129, 0.08),
    0 0 80px rgba(16, 185, 129, 0.04);
  border-radius: 2px;
}

.led-dev-bar {
  display:         flex;
  align-items:     center;
  justify-content: space-between;
  width:           100%;
  max-width:       640px;
  padding:         0 2px;
}
</style>
