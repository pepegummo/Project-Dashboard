import { ref, computed, onMounted, onUnmounted } from 'vue'

export type ScreenMode = 'led' | 'mobile' | 'desktop'

function detect(): ScreenMode {
  if (typeof window === 'undefined') return 'desktop'
  const w = window.innerWidth
  const h = window.innerHeight
  // LED screen: narrow AND short (e.g. 640×320 industrial display)
  if (w <= 650 && h <= 350) return 'led'
  if (w < 768) return 'mobile'
  return 'desktop'
}

export function useScreenMode() {
  const mode = ref<ScreenMode>(detect())

  function onResize() {
    mode.value = detect()
  }

  onMounted(() => window.addEventListener('resize', onResize))
  onUnmounted(() => window.removeEventListener('resize', onResize))

  return {
    mode,
    isLED: computed(() => mode.value === 'led'),
    isMobile: computed(() => mode.value === 'mobile'),
    isDesktop: computed(() => mode.value === 'desktop'),
  }
}
