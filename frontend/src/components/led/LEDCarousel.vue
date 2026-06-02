<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useDashboardStore } from '@/stores/dashboard.store'
import { useMachineStore } from '@/stores/machine.store'
import { useTelemetryStore } from '@/stores/telemetry.store'
import { useAlertStore } from '@/stores/alert.store'
import { useWebSocket } from '@/composables/useWebSocket'
import { wsService } from '@/services/ws.service'
import { api } from '@/services/api.service'

const props = withDefaults(defineProps<{
  interval?: number
}>(), { interval: 5000 })

const dashboardStore = useDashboardStore()
const machineStore   = useMachineStore()
const telemetryStore = useTelemetryStore()
const alertStore     = useAlertStore()

// Register WS handlers (telemetry → store, alerts → store)
// Required because TopBar (the normal host of useWebSocket) is not rendered in LED mode.
useWebSocket()

// ── Slides: one per dashboard widget, sorted by position order ─────────────
// Keep alarm-panel (no machine) and any widget that has a machineId.
const slides = computed(() =>
  dashboardStore.widgets
    .slice()
    .sort((a, b) => a.order - b.order)
    .filter(w => w.widgetType === 'alarm-panel' || w.machineId != null)
)

const currentIndex = ref(0)
const current      = computed(() => slides.value[currentIndex.value] ?? null)

// ── Machine & field resolved from the active slide ──────────────────────────
const currentMachine = computed(() => {
  const id = current.value?.machineId
  return id ? (machineStore.machineById(id) ?? null) : null
})

const currentField = computed(() => {
  const key = current.value?.config?.field
  if (!key || !currentMachine.value) return null
  return currentMachine.value.fields.find(f => f.key === key) ?? null
})

// ── Display helpers ─────────────────────────────────────────────────────────
const slideTitle = computed(() =>
  current.value?.title ?? currentField.value?.label ?? currentMachine.value?.name ?? 'Metric'
)

const precision = computed(() =>
  current.value?.config?.precision ?? currentField.value?.precision ?? 1
)

const liveValue = computed<string>(() => {
  const w = current.value
  if (!w?.machineId || !w.config?.field) return '—'
  const val = telemetryStore.getFieldValue(w.machineId, w.config.field)
  return val !== undefined ? val.toFixed(precision.value) : '—'
})

const displayUnit = computed(() =>
  current.value?.config?.unit ?? currentField.value?.unit ?? ''
)

const threshold  = computed(() => currentField.value?.threshold)

const achievementPct = computed<string | null>(() => {
  if (!threshold.value || liveValue.value === '—') return null
  const ratio = parseFloat(liveValue.value) / threshold.value
  return (ratio * 100).toFixed(1) + '%'
})

// ── Status config ───────────────────────────────────────────────────────────
const STATUS = {
  online:      { label: 'RUNNING', textClass: 'text-emerald-400', dotClass: 'bg-emerald-400' },
  offline:     { label: 'OFFLINE', textClass: 'text-gray-500',    dotClass: 'bg-gray-600'    },
  maintenance: { label: 'MAINT.',  textClass: 'text-amber-400',   dotClass: 'bg-amber-400'   },
  error:       { label: 'ALARM',   textClass: 'text-red-400',     dotClass: 'bg-red-500'     },
} as const

const statusCfg = computed(() => STATUS[currentMachine.value?.status ?? 'offline'])

// ── Value color: reflects out-of-limit or machine fault ─────────────────────
const valueColorClass = computed(() => {
  const m = currentMachine.value
  if (m?.status === 'error')       return 'text-red-400'
  if (m?.status === 'maintenance') return 'text-amber-400'
  if (m?.status === 'offline')     return 'text-gray-600'
  if (liveValue.value === '—')     return 'text-gray-700'

  const f   = currentField.value
  const val = parseFloat(liveValue.value)
  if (f?.upperLimit !== undefined && val > f.upperLimit) return 'text-red-400'
  if (f?.lowerLimit !== undefined && val < f.lowerLimit) return 'text-amber-400'
  return 'text-emerald-400'
})

const achievementColorClass = computed(() => {
  if (!threshold.value || liveValue.value === '—') return 'text-gray-600'
  const ratio = parseFloat(liveValue.value) / threshold.value
  if (ratio >= 0.95 && ratio <= 1.05) return 'text-emerald-400'
  if (ratio < 0.85  || ratio > 1.15)  return 'text-red-400'
  return 'text-amber-400'
})

// Progress bar color: red for alarm panel, machine-status-color otherwise
const progressColorClass = computed(() =>
  current.value?.widgetType === 'alarm-panel' ? 'bg-red-500' : statusCfg.value.dotClass
)

// ── Alarm panel counts ──────────────────────────────────────────────────────
const criticalCount = computed(() => alertStore.criticalCount)
const warningCount  = computed(() => alertStore.warningCount)

// ── Carousel timer ──────────────────────────────────────────────────────────
let timer: ReturnType<typeof setInterval> | null = null
const isPaused = ref(false)

function advance() {
  if (isPaused.value) return
  if (slides.value.length < 2) return
  currentIndex.value = (currentIndex.value + 1) % slides.value.length
}

function resetTimer() {
  if (isPaused.value) return
  if (timer !== null) clearInterval(timer)
  timer = setInterval(advance, props.interval)
}

function nextSlide() {
  if (slides.value.length < 2) return
  currentIndex.value = (currentIndex.value + 1) % slides.value.length
  resetTimer()
}

function prevSlide() {
  if (slides.value.length < 2) return
  currentIndex.value = (currentIndex.value - 1 + slides.value.length) % slides.value.length
  resetTimer()
}

function toggleLock() {
  isPaused.value = !isPaused.value
  if (isPaused.value) {
    if (timer !== null) { clearInterval(timer); timer = null }
  } else {
    resetTimer()
  }
}

function goTo(i: number) {
  currentIndex.value = i
  resetTimer()
}

function handleKeydown(e: KeyboardEvent) {
  if (e.key === 'ArrowRight' || e.key === 'PageDown') { e.preventDefault(); nextSlide() }
  if (e.key === 'ArrowLeft'  || e.key === 'PageUp')   { e.preventDefault(); prevSlide() }
  if (e.key === ' '          || e.key === 'Enter')     { e.preventDefault(); toggleLock() }
}

watch(slides, (list) => {
  if (currentIndex.value >= list.length && list.length > 0) currentIndex.value = 0
  if (timer === null && list.length > 1) timer = setInterval(advance, props.interval)
})

onMounted(async () => {
  // Ensure dashboard widgets are loaded
  if (dashboardStore.widgets.length === 0) {
    if (dashboardStore.dashboards.length === 0) await dashboardStore.fetchDashboards()
    const target = dashboardStore.defaultDashboard ?? dashboardStore.dashboards[0]
    if (target) await dashboardStore.fetchDashboard(target.id)
  }
  if (machineStore.machines.length === 0) await machineStore.fetchMachines()
  if (alertStore.activeEvents.length === 0) await alertStore.fetchActiveEvents()

  // Subscribe to live WS data for all machines and seed initial values from DB.
  // Without this, LED mode would show '—' until the next simulator tick (up to 60s).
  const machineIds = machineStore.machines.map(m => m.id)
  if (machineIds.length > 0) {
    wsService.subscribe(machineIds)
    try {
      const snapshots = await api.getMultiLatestTelemetry(machineIds)
      for (const [machineId, snap] of Object.entries(snapshots)) {
        telemetryStore.updateSnapshot(machineId, snap.timestamp, snap.data as any)
      }
    } catch { /* non-fatal — WS will populate on next tick */ }
  }

  if (slides.value.length > 1 && timer === null) timer = setInterval(advance, props.interval)
  window.addEventListener('keydown', handleKeydown)
})

onUnmounted(() => {
  if (timer !== null) { clearInterval(timer); timer = null }
  window.removeEventListener('keydown', handleKeydown)
  const machineIds = machineStore.machines.map(m => m.id)
  if (machineIds.length > 0) wsService.unsubscribe(machineIds)
})
</script>

<template>
  <div class="fixed inset-0 bg-black overflow-hidden select-none flex flex-col">

    <!-- ── Progress bar ─────────────────────────────────────────────────── -->
    <div class="h-0.5 flex-shrink-0 bg-white/5 overflow-hidden">
      <div
        :key="currentIndex"
        class="h-full led-progress"
        :class="progressColorClass"
        :style="`animation-duration: ${interval}ms; animation-play-state: ${isPaused ? 'paused' : 'running'}`"
      />
    </div>

    <!-- ── Header: machine name + status ────────────────────────────────── -->
    <div class="flex items-center justify-between px-4 py-2.5 border-b border-white/15 flex-shrink-0 gap-3 min-h-[42px]">

      <!-- Machine name (left) -->
      <Transition name="led-slide" mode="out-in">
        <p
          v-if="currentMachine"
          :key="currentMachine.id + '-name'"
          class="font-mono text-sm font-black text-white uppercase tracking-widest truncate min-w-0"
        >
          {{ currentMachine.name }}
        </p>
        <p v-else :key="'alarm-hdr'" class="font-mono text-sm font-black text-gray-400 uppercase tracking-widest">
          SYSTEM
        </p>
      </Transition>

      <!-- Status badge (right) -->
      <div class="flex items-center gap-3 flex-shrink-0">
        <div v-if="currentMachine" class="flex items-center gap-2">
          <span
            class="w-2.5 h-2.5 rounded-full"
            :class="[statusCfg.dotClass, currentMachine.status === 'error' ? 'led-blink-fast' : 'animate-pulse']"
          />
          <span class="font-mono text-xs font-bold tracking-[0.25em]" :class="statusCfg.textClass">
            {{ statusCfg.label }}
          </span>
        </div>

        <!-- Active alert count badge -->
        <div
          v-if="criticalCount > 0 || warningCount > 0"
          class="flex items-center gap-1.5 px-2.5 py-1 rounded border border-red-500/40 bg-red-500/15"
        >
          <span class="font-mono text-xs font-black text-red-400 tracking-wide leading-none">
            ⚡ {{ criticalCount + warningCount }}
          </span>
        </div>
      </div>
    </div>

    <!-- ── Main content area: transitions per slide ──────────────────────── -->
    <Transition name="led-slide" mode="out-in">

      <!-- ── ALARM PANEL slide ─────────────────────────────────────────── -->
      <div
        v-if="current?.widgetType === 'alarm-panel'"
        :key="current.id"
        class="flex-1 flex flex-col items-center justify-center gap-4 px-6"
      >
        <p class="font-mono text-xs font-bold uppercase tracking-[0.4em] text-gray-400 mb-1">
          {{ current.title ?? 'System Alerts' }}
        </p>

        <!-- Critical -->
        <div class="flex items-center gap-4">
          <span class="w-2.5 h-2.5 rounded-full bg-red-500 led-blink-fast" />
          <p
            class="font-mono font-black tabular-nums leading-none"
            :class="criticalCount > 0 ? 'text-red-400' : 'text-gray-700'"
            style="font-size: clamp(2.25rem, 9vw, 4rem); text-shadow: 0 0 2px rgba(255,255,255,0.6), 0 0 18px currentColor, 0 0 55px currentColor;"
          >
            {{ criticalCount }}
          </p>
          <p class="font-mono text-sm font-bold uppercase tracking-[0.3em]" :class="criticalCount > 0 ? 'text-red-400' : 'text-gray-700'">
            CRITICAL
          </p>
        </div>

        <!-- Warning -->
        <div class="flex items-center gap-4">
          <span class="w-2.5 h-2.5 rounded-full bg-amber-400 animate-pulse" />
          <p
            class="font-mono font-black tabular-nums leading-none"
            :class="warningCount > 0 ? 'text-amber-400' : 'text-gray-700'"
            style="font-size: clamp(2.25rem, 9vw, 4rem); text-shadow: 0 0 2px rgba(255,255,255,0.6), 0 0 18px currentColor, 0 0 55px currentColor;"
          >
            {{ warningCount }}
          </p>
          <p class="font-mono text-sm font-bold uppercase tracking-[0.3em]" :class="warningCount > 0 ? 'text-amber-400' : 'text-gray-700'">
            WARNING
          </p>
        </div>
      </div>

      <!-- ── STATUS CARD slide (machine, no specific field) ────────────── -->
      <div
        v-else-if="current?.widgetType === 'status-card' && !current.config?.field"
        :key="current.id"
        class="flex-1 flex flex-col items-center justify-center gap-3 px-6"
      >
        <p class="font-mono text-xs font-bold uppercase tracking-[0.4em] text-gray-400 mb-2">
          {{ slideTitle }}
        </p>
        <div class="flex items-center gap-4">
          <span
            class="w-3 h-3 rounded-full"
            :class="[statusCfg.dotClass, currentMachine?.status === 'error' ? 'led-blink-fast' : 'animate-pulse']"
          />
          <p
            class="font-mono font-black leading-none tracking-widest"
            :class="statusCfg.textClass"
            style="font-size: clamp(2.5rem, 10vw, 4.5rem); text-shadow: 0 0 2px rgba(255,255,255,0.6), 0 0 20px currentColor, 0 0 60px currentColor;"
          >
            {{ statusCfg.label }}
          </p>
        </div>
      </div>

      <!-- ── METRIC slide (kpi-card, gauge, line-chart, daily-count …) ─── -->
      <div
        v-else-if="current"
        :key="current.id"
        class="flex-1 flex flex-col items-center justify-center gap-0 px-6"
      >
        <!-- Widget / field label -->
        <p class="font-mono text-xs font-bold uppercase tracking-[0.4em] text-gray-400 mb-3 truncate max-w-full">
          {{ slideTitle }}
        </p>

        <!-- Live value — large, glowing, crisp white halo for sharpness -->
        <p
          class="font-mono font-black leading-none tabular-nums transition-colors duration-500"
          :class="valueColorClass"
          style="
            font-size: clamp(3rem, 12vw, 5.5rem);
            text-shadow: 0 0 2px rgba(255,255,255,0.7), 0 0 20px currentColor, 0 0 60px currentColor, 0 0 120px currentColor;
          "
        >
          {{ liveValue }}
        </p>

        <!-- Unit -->
        <p
          v-if="displayUnit"
          class="font-mono text-sm font-semibold uppercase tracking-[0.3em] text-gray-400 mt-2.5"
        >
          {{ displayUnit }}
        </p>

        <!-- Target + achievement % -->
        <div v-if="threshold" class="flex items-center gap-3 mt-3">
          <span class="font-mono text-[11px] font-medium text-gray-500 tracking-wide">
            TGT&nbsp;{{ threshold.toFixed(precision) }}
          </span>
          <span
            v-if="achievementPct"
            class="font-mono text-xs font-bold tracking-wide"
            :class="achievementColorClass"
          >
            {{ achievementPct }}
          </span>
        </div>
      </div>

      <!-- ── Empty / loading state ─────────────────────────────────────── -->
      <div v-else :key="'empty'" class="flex-1 flex flex-col items-center justify-center gap-2">
        <div class="w-1.5 h-1.5 rounded-full bg-gray-700 animate-pulse" />
        <p class="font-mono text-[10px] text-gray-800 tracking-[0.45em] uppercase">Loading</p>
      </div>

    </Transition>

    <!-- ── Footer: dot indicators + controls ────────────────────────────── -->
    <div
      v-if="slides.length > 1"
      class="flex-shrink-0 flex items-center justify-between px-4 py-2 border-t border-white/5 gap-4"
    >
      <!-- Prev -->
      <button
        class="font-mono text-xs font-bold tracking-widest text-gray-500 hover:text-white transition-colors focus:outline-none cursor-pointer"
        @click="prevSlide"
      >
        ◀ PREV
      </button>

      <!-- Dot indicators (centre) -->
      <div class="flex items-center gap-1.5">
        <button
          v-for="(_, i) in slides"
          :key="i"
          class="h-1 rounded-full transition-all duration-300 focus:outline-none"
          :class="i === currentIndex ? ['w-4', progressColorClass] : 'w-1 bg-white/20'"
          @click="goTo(i)"
        />
      </div>

      <!-- Lock / Auto toggle -->
      <button
        class="font-mono text-xs font-bold tracking-widest transition-colors focus:outline-none cursor-pointer"
        :class="isPaused ? 'text-amber-400 hover:text-amber-300' : 'text-gray-500 hover:text-white'"
        @click="toggleLock"
      >
        {{ isPaused ? '🔒 LOCKED' : '🔄 AUTO' }}
      </button>

      <!-- Next -->
      <button
        class="font-mono text-xs font-bold tracking-widest text-gray-500 hover:text-white transition-colors focus:outline-none cursor-pointer"
        @click="nextSlide"
      >
        NEXT ▶
      </button>
    </div>

  </div>
</template>

<style scoped>
.led-progress {
  width: 100%;
  transform-origin: left center;
  animation: led-fill linear forwards;
}
@keyframes led-fill {
  from { transform: scaleX(0); }
  to   { transform: scaleX(1); }
}

.led-blink-fast {
  animation: led-blink 0.7s ease-in-out infinite;
}
@keyframes led-blink {
  0%, 100% { opacity: 1;   }
  50%       { opacity: 0.1; }
}

.led-slide-enter-active,
.led-slide-leave-active {
  transition: opacity 0.3s ease, transform 0.3s ease;
}
.led-slide-enter-from { opacity: 0; transform: translateY(8px);  }
.led-slide-leave-to   { opacity: 0; transform: translateY(-8px); }
</style>
