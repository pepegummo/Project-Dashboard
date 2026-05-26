<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'

export interface LEDItem {
  label: string
  value: string | number
  unit?: string
  /** Tailwind text-color class, e.g. "text-green-400" */
  color?: string
}

const props = withDefaults(defineProps<{
  items: LEDItem[]
  /** Milliseconds between auto-advances */
  interval?: number
}>(), {
  interval: 5000,
})

const currentIndex = ref(0)
const current = computed(() => props.items[currentIndex.value] ?? null)

let timer: ReturnType<typeof setInterval> | null = null

function advance() {
  currentIndex.value = (currentIndex.value + 1) % props.items.length
}

function goTo(i: number) {
  currentIndex.value = i
  // Reset timer so the new item gets a full interval
  if (timer !== null) {
    clearInterval(timer)
    timer = setInterval(advance, props.interval)
  }
}

onMounted(() => {
  if (props.items.length > 1) {
    timer = setInterval(advance, props.interval)
  }
})

onUnmounted(() => {
  if (timer !== null) {
    clearInterval(timer)
    timer = null
  }
})
</script>

<template>
  <div
    class="fixed inset-0 bg-black flex flex-col items-center justify-center overflow-hidden select-none"
    style="width: 100vw; height: 100vh;"
  >
    <!-- Progress bar: restarts via :key on every index change -->
    <div class="absolute top-0 inset-x-0 h-0.5 bg-white/5 overflow-hidden">
      <div
        :key="currentIndex"
        class="h-full bg-current led-progress"
        :class="current?.color ?? 'text-green-400'"
        :style="`animation-duration: ${interval}ms`"
      />
    </div>

    <!-- KPI display -->
    <Transition name="led-slide" mode="out-in">
      <div
        v-if="current"
        :key="currentIndex"
        class="flex flex-col items-center justify-center gap-0.5 px-4 text-center"
      >
        <!-- Metric label -->
        <p class="font-mono text-[9px] uppercase tracking-[0.45em] text-gray-600 mb-1">
          {{ current.label }}
        </p>

        <!-- Big number — clamp keeps it readable on both 320w and 640w screens -->
        <p
          class="font-mono font-black leading-none tabular-nums"
          :class="current.color ?? 'text-green-400'"
          style="
            font-size: clamp(3rem, 17vw, 5.5rem);
            text-shadow: 0 0 20px currentColor, 0 0 55px currentColor, 0 0 100px currentColor;
          "
        >
          {{ current.value }}
        </p>

        <!-- Unit -->
        <p
          v-if="current.unit"
          class="font-mono text-[9px] uppercase tracking-[0.35em] mt-1"
          :class="current.color ?? 'text-green-400'"
          style="opacity: 0.45;"
        >
          {{ current.unit }}
        </p>
      </div>
    </Transition>

    <!-- Dot / pill indicators -->
    <div class="absolute bottom-2.5 flex items-center gap-1.5">
      <button
        v-for="(_, i) in items"
        :key="i"
        class="h-1 rounded-full transition-all duration-300 focus:outline-none"
        :class="[
          i === currentIndex
            ? ['w-4', current?.color ?? 'text-green-400', 'bg-current']
            : 'w-1 bg-white/20',
        ]"
        @click="goTo(i)"
      />
    </div>
  </div>
</template>

<style scoped>
/* Progress bar grows left→right over `animation-duration` ms */
.led-progress {
  width: 100%;
  transform-origin: left center;
  animation: led-fill linear forwards;
}

@keyframes led-fill {
  from { transform: scaleX(0); }
  to   { transform: scaleX(1); }
}

/* Slide-up / slide-down between KPIs */
.led-slide-enter-active,
.led-slide-leave-active {
  transition: opacity 0.35s ease, transform 0.35s ease;
}
.led-slide-enter-from {
  opacity: 0;
  transform: translateY(10px);
}
.led-slide-leave-to {
  opacity: 0;
  transform: translateY(-10px);
}
</style>
