<script setup lang="ts">
/**
 * LedView.vue — 640 × 320 px Kiosk LED Screen
 * ─────────────────────────────────────────────
 * • Strict 640 × 320 container, overflow: hidden, no scrollbars
 * • Dark / high-contrast theme with LED phosphor glow
 * • Accepts `activeWidgets` prop; falls back to built-in mock data
 * • Smart CSS-Grid layout that scales 1 → 6+ widgets automatically
 * • Live data via Telemetry store + WebSocket polling (optional per widget)
 * • Widget types: 'metric' | 'sparkline' | 'status' | 'alarm'
 */

import { computed, ref, onMounted, onUnmounted } from 'vue'
import { useTelemetryStore } from '@/stores/telemetry.store'
import { useMachineStore }   from '@/stores/machine.store'
import { useAlertStore }     from '@/stores/alert.store'
import { wsService }         from '@/services/ws.service'
import { api }               from '@/services/api.service'

// ─── Public Types ─────────────────────────────────────────────────────────────
export type LedWidgetType = 'metric' | 'sparkline' | 'status' | 'alarm'

export interface LedWidget {
  /** Unique identifier for v-for key */
  id: string | number
  /** Widget variant */
  type: LedWidgetType
  /** Label shown above the main value */
  title: string

  // ── Numeric display (metric / sparkline) ──────────────────────────────────
  /** Static value string/number. Ignored when machineId + field are set. */
  value?: string | number
  /** Physical unit displayed below the value */
  unit?: string
  /** Decimal places when displaying a live number (default 2) */
  precision?: number
  /** Optional trend arrow: 'up' | 'down' | 'stable' */
  trend?: 'up' | 'down' | 'stable'
  /**
   * Tailwind text-color class for value color override.
   * e.g. 'text-red-400' | 'text-amber-400' | 'text-emerald-400'
   * Defaults to 'text-emerald-400'.
   */
  colorClass?: string

  // ── Live data binding (optional) ──────────────────────────────────────────
  /** Machine ID to subscribe for live WebSocket telemetry */
  machineId?: string
  /** Field key inside the machine telemetry snapshot */
  field?: string

  // ── Status widget specific ────────────────────────────────────────────────
  /** Fallback status when no machineId is supplied */
  status?: 'online' | 'offline' | 'maintenance' | 'error'

  // ── Alarm widget specific ─────────────────────────────────────────────────
  /** Override the alert-store critical count */
  criticalCount?: number
  /** Override the alert-store warning count */
  warningCount?: number
}

// ─── Default mock widgets (used when no prop is passed) ────────────────────────
const MOCK_WIDGETS: LedWidget[] = [
  {
    id: 1,
    type: 'metric',
    title: 'Current Weight',
    value: '482.79',
    unit: 'g',
    trend: 'up',
  },
  {
    id: 2,
    type: 'sparkline',
    title: 'Temperature',
    value: '72.3',
    unit: '°C',
  },
  {
    id: 3,
    type: 'status',
    title: 'Line Status',
    status: 'online',
  },
  {
    id: 4,
    type: 'alarm',
    title: 'System Alerts',
  },
]

// ─── Props ─────────────────────────────────────────────────────────────────────
// NOTE: withDefaults() cannot reference MOCK_WIDGETS here because defineProps()
// is hoisted to module scope by the Vue compiler. Use a computed fallback instead.
const props = defineProps<{
  /** Array of widget configurations to render */
  activeWidgets?: LedWidget[]
}>()

// Resolved widget list — falls back to built-in mock data when no prop is passed
const displayWidgets = computed(() => props.activeWidgets ?? MOCK_WIDGETS)

// ─── Stores ────────────────────────────────────────────────────────────────────
const telemetryStore = useTelemetryStore()
const machineStore   = useMachineStore()
const alertStore     = useAlertStore()

// ─── Live clock ────────────────────────────────────────────────────────────────
const now = ref(new Date())
let clockTimer: ReturnType<typeof setInterval> | null = null

const timeStr = computed(() =>
  now.value.toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit', second: '2-digit' })
)
const dateStr = computed(() =>
  now.value.toLocaleDateString('en-GB', { day: '2-digit', month: 'short', year: '2-digit' })
)

// ─── Unique machine IDs that need live subscriptions ──────────────────────────
const liveMachineIds = computed(() =>
  [...new Set(displayWidgets.value.filter(w => w.machineId).map(w => w.machineId!))]
)

// ─── Polling: fetch latest snapshot every 2 s as WebSocket fallback ───────────
let pollTimer: ReturnType<typeof setInterval> | null = null

async function fetchAllLatest() {
  for (const mid of liveMachineIds.value) {
    try {
      const snap = await api.getLatestTelemetry(mid)
      if (snap) {
        telemetryStore.updateSnapshot(
          mid,
          (snap as any).timestamp ?? new Date().toISOString(),
          (snap as any).data ?? {},
        )
      }
    } catch { /* silently ignored — WS covers real-time gaps */ }
  }
}

onMounted(async () => {
  clockTimer = setInterval(() => { now.value = new Date() }, 1000)

  if (liveMachineIds.value.length > 0) {
    wsService.subscribe(liveMachineIds.value)
    await fetchAllLatest()
    pollTimer = setInterval(fetchAllLatest, 2000)
  }

  // Preload alert event counts (non-blocking)
  if (alertStore.activeEvents.length === 0) {
    alertStore.fetchActiveEvents().catch(() => {})
  }
})

onUnmounted(() => {
  if (clockTimer) { clearInterval(clockTimer); clockTimer = null }
  if (pollTimer)  { clearInterval(pollTimer);  pollTimer  = null }
  if (liveMachineIds.value.length > 0) {
    wsService.unsubscribe(liveMachineIds.value)
  }
})

// ─── Value resolvers ───────────────────────────────────────────────────────────
function resolveValue(widget: LedWidget): string {
  if (widget.machineId && widget.field) {
    const val = telemetryStore.getFieldValue(widget.machineId, widget.field)
    if (val !== undefined) return val.toFixed(widget.precision ?? 2)
  }
  return widget.value !== undefined ? String(widget.value) : '—'
}

function resolveStatus(widget: LedWidget): 'online' | 'offline' | 'maintenance' | 'error' {
  if (widget.machineId) {
    return machineStore.machineById(widget.machineId)?.status ?? widget.status ?? 'offline'
  }
  return widget.status ?? 'offline'
}

function resolveCritical(widget: LedWidget): number {
  return widget.criticalCount ?? alertStore.criticalCount
}

function resolveWarning(widget: LedWidget): number {
  return widget.warningCount ?? alertStore.warningCount
}

function resolveSparkData(widget: LedWidget): number[] {
  if (widget.machineId && widget.field) {
    const hist = telemetryStore.getHistory(widget.machineId, widget.field)
    if (hist?.values.length) return hist.values.slice(-50)
  }
  return []
}

// ─── Sparkline SVG point string ────────────────────────────────────────────────
function buildSparkPoints(values: number[], W = 180, H = 22): string {
  if (values.length < 2) return ''
  const min   = Math.min(...values)
  const max   = Math.max(...values)
  const range = max - min || 1
  return values
    .map((v, i) => {
      const x = (i / (values.length - 1)) * W
      const y = H - ((v - min) / range) * (H - 4) - 2
      return `${x.toFixed(1)},${y.toFixed(1)}`
    })
    .join(' ')
}

// ─── Status config map ─────────────────────────────────────────────────────────
type MachineStatus = 'online' | 'offline' | 'maintenance' | 'error'

const STATUS_CFG: Record<MachineStatus, {
  label: string
  textClass: string
  dotClass: string
  glow: string
}> = {
  online:      { label: 'RUNNING', textClass: 'text-emerald-400', dotClass: 'bg-emerald-400', glow: '#10b981' },
  offline:     { label: 'OFFLINE', textClass: 'text-gray-500',    dotClass: 'bg-gray-600',    glow: '#6b7280' },
  maintenance: { label: 'MAINT.',  textClass: 'text-amber-400',   dotClass: 'bg-amber-400',   glow: '#f59e0b' },
  error:       { label: 'ALARM',   textClass: 'text-red-400',     dotClass: 'bg-red-500',     glow: '#f87171' },
}

// ─── Glow color lookup ─────────────────────────────────────────────────────────
const GLOW_MAP: Record<string, string> = {
  'text-emerald-400': '#10b981',
  'text-green-400':   '#4ade80',
  'text-red-400':     '#f87171',
  'text-amber-400':   '#f59e0b',
  'text-yellow-400':  '#facc15',
  'text-blue-400':    '#60a5fa',
  'text-cyan-400':    '#22d3ee',
  'text-violet-400':  '#a78bfa',
  'text-white':       '#ffffff',
}

function metricGlow(colorClass?: string): string {
  const hex = (colorClass && GLOW_MAP[colorClass]) ?? '#10b981'
  return `0 0 2px rgba(255,255,255,0.55), 0 0 14px ${hex}, 0 0 45px ${hex}55`
}

// ─── Layout engine ─────────────────────────────────────────────────────────────
/**
 * Column count by widget count:
 *   1 → 1 col (full-width)
 *   2 → 2 cols (50/50)
 *   3 → 3 cols (33/33/33)
 *   4 → 2×2 grid
 *   5-6 → 3-col × 2-row
 *   7-8 → 4-col × 2-row
 *   9+  → 4-col × wrapping rows (very tight — best effort)
 */
const count = computed(() => displayWidgets.value.length)

function colsForCount(n: number): number {
  if (n <= 1) return 1
  if (n <= 2) return 2
  if (n <= 3) return 3
  if (n <= 4) return 2
  if (n <= 6) return 3
  return 4
}

const columns = computed(() => colsForCount(count.value))
const rows    = computed(() => (count.value === 0 ? 1 : Math.ceil(count.value / columns.value)))

/** CSS Grid container style — gap creates the 1-px divider via wrapper bg */
const gridStyle = computed(() => ({
  display: 'grid',
  gridTemplateColumns: `repeat(${columns.value}, 1fr)`,
  gridTemplateRows: `repeat(${rows.value}, 1fr)`,
  gap: '1px',
  height: '292px',        // 320px total − 28px header
  background: 'rgba(255,255,255,0.07)', // divider colour bleeding through gap
}))

/** Empty cells needed to fill the last row of the grid */
const emptySlotCount = computed(() => {
  if (count.value === 0) return 0
  const total = columns.value * rows.value
  return Math.max(0, total - count.value)
})

// ─── Per-cell typography — shrinks as more widgets appear ─────────────────────
const valueSize = computed(() => {
  const n = count.value
  if (n === 1) return 'clamp(4.5rem, 14vw, 6.25rem)'  // ~72 – 100 px  (full-width hero)
  if (n === 2) return 'clamp(3rem,   9vw, 4.5rem)'     // ~48 – 72 px   (half-width)
  if (n === 3) return 'clamp(2.25rem, 6vw, 3.25rem)'   // ~36 – 52 px   (third-width)
  if (n === 4) return 'clamp(2rem,   5vw, 3rem)'       // ~32 – 48 px   (quarter)
  return           'clamp(1.375rem, 4vw, 2.25rem)'     // ~22 – 36 px   (small cells)
})

const titleSize = computed(() => {
  const n = count.value
  if (n <= 2) return '0.6rem'   // 9.6 px
  if (n <= 4) return '0.55rem'  // 8.8 px
  return           '0.5rem'    // 8 px
})

const unitSize = computed(() => {
  const n = count.value
  if (n <= 2) return '0.7rem'   // 11.2 px
  if (n <= 4) return '0.625rem' // 10 px
  return           '0.565rem'  // ~9 px
})

const cellPad = computed(() => {
  const n = count.value
  if (n === 1) return '1.25rem 1.75rem'
  if (n <= 3) return '0.75rem 1rem'
  return '0.5rem 0.75rem'
})

// Status dot size scales with available space
const dotSize = computed(() => count.value <= 2 ? '11px' : count.value <= 4 ? '8px' : '6px')

// ─── Trend helpers ─────────────────────────────────────────────────────────────
function trendSymbol(t?: string): string {
  if (t === 'up')     return '▲'
  if (t === 'down')   return '▼'
  if (t === 'stable') return '─'
  return ''
}
function trendClass(t?: string): string {
  if (t === 'up')   return 'text-emerald-400'
  if (t === 'down') return 'text-red-400'
  return 'text-gray-600'
}
</script>

<!---------------------------------------------------------------------------->

<template>
  <!--
    ╔════════════════════════════════════════════════════╗
    ║   LED Screen View  ·  640 × 320 px  (kiosk mode) ║
    ║   No scrollbars · No sidebars · No interactivity  ║
    ╚════════════════════════════════════════════════════╝
  -->
  <div
    class="relative bg-black overflow-hidden select-none font-mono"
    style="width: 640px; height: 320px;"
    aria-hidden="true"
  >
    <!-- Full-screen CRT scanline texture (very subtle) -->
    <div
      class="absolute inset-0 pointer-events-none led-scanlines"
      style="opacity: 0.032; z-index: 20;"
    />

    <!-- ──────────────────────────────────────────────────────────────────── -->
    <!-- Header  28 px                                                        -->
    <!-- ──────────────────────────────────────────────────────────────────── -->
    <div
      class="flex items-center justify-between px-3 flex-shrink-0"
      style="height: 28px; border-bottom: 1px solid rgba(255,255,255,0.08);"
    >
      <!-- Live indicator + project label -->
      <div class="flex items-center gap-2">
        <span class="w-1.5 h-1.5 rounded-full bg-emerald-400 animate-pulse flex-shrink-0" />
        <span
          class="text-gray-500 uppercase font-bold"
          style="font-size: 0.52rem; letter-spacing: 0.32em;"
        >
          CPF &middot; LED View
        </span>
      </div>

      <!-- Right: widget count pill + date + clock -->
      <div class="flex items-center gap-3">
        <span
          v-if="count > 0"
          class="text-gray-700 font-bold tabular-nums"
          style="font-size: 0.52rem; letter-spacing: 0.18em;"
        >
          {{ count }}&thinsp;WIDGET{{ count !== 1 ? 'S' : '' }}
        </span>
        <span class="text-gray-700 tabular-nums" style="font-size: 0.58rem;">{{ dateStr }}</span>
        <span
          class="text-gray-300 font-bold tabular-nums"
          style="font-size: 0.62rem; letter-spacing: 0.14em;"
        >
          {{ timeStr }}
        </span>
      </div>
    </div>

    <!-- ──────────────────────────────────────────────────────────────────── -->
    <!-- Empty state  (no widgets configured)                                 -->
    <!-- ──────────────────────────────────────────────────────────────────── -->
    <div
      v-if="count === 0"
      class="bg-black w-full flex flex-col items-center justify-center gap-2"
      style="height: 292px;"
    >
      <!-- Dim pulse dot -->
      <span class="w-1 h-1 rounded-full bg-gray-800 animate-pulse" />
      <p
        class="text-gray-800 uppercase font-bold"
        style="font-size: 0.6rem; letter-spacing: 0.45em;"
      >
        No Widgets Configured
      </p>
    </div>

    <!-- ──────────────────────────────────────────────────────────────────── -->
    <!-- Widget Grid  (auto-layout by count)                                  -->
    <!-- ──────────────────────────────────────────────────────────────────── -->
    <div v-else :style="gridStyle" class="w-full">

      <!--
        ┌─────────────────────────────────────────────────┐
        │  v-for over activeWidgets — each is a grid cell  │
        └─────────────────────────────────────────────────┘
      -->
      <div
        v-for="widget in displayWidgets"
        :key="widget.id"
        class="bg-black relative flex flex-col items-center justify-center overflow-hidden"
        :style="{ padding: cellPad }"
      >
        <!-- Per-cell pixel-level scanlines (very faint) -->
        <div
          class="absolute inset-0 pointer-events-none led-scanlines"
          style="opacity: 0.022;"
        />

        <!-- ════════════════════════════════════════════════════════════════ -->
        <!--  METRIC  ─  Big number readout with optional trend indicator     -->
        <!-- ════════════════════════════════════════════════════════════════ -->
        <template v-if="widget.type === 'metric'">
          <!-- Title label -->
          <p
            class="text-gray-500 uppercase font-bold tracking-widest truncate max-w-full text-center leading-none"
            :style="{ fontSize: titleSize, letterSpacing: '0.28em', marginBottom: '0.35em' }"
          >
            {{ widget.title }}
          </p>

          <!-- Main value -->
          <p
            class="tabular-nums leading-none text-center font-black transition-colors duration-500"
            :class="widget.colorClass ?? 'text-emerald-400'"
            :style="{
              fontSize: valueSize,
              textShadow: metricGlow(widget.colorClass ?? 'text-emerald-400'),
            }"
          >
            {{ resolveValue(widget) }}
          </p>

          <!-- Unit -->
          <p
            v-if="widget.unit"
            class="text-gray-500 uppercase font-semibold text-center leading-none"
            :style="{ fontSize: unitSize, letterSpacing: '0.22em', marginTop: '0.4em' }"
          >
            {{ widget.unit }}
          </p>

          <!-- Trend -->
          <p
            v-if="widget.trend && trendSymbol(widget.trend)"
            class="font-bold text-center leading-none"
            :class="trendClass(widget.trend)"
            style="font-size: 0.52rem; letter-spacing: 0.2em; margin-top: 0.45em;"
          >
            {{ trendSymbol(widget.trend) }}&thinsp;{{ widget.trend.toUpperCase() }}
          </p>
        </template>

        <!-- ════════════════════════════════════════════════════════════════ -->
        <!--  SPARKLINE  ─  Value + live mini chart                           -->
        <!-- ════════════════════════════════════════════════════════════════ -->
        <template v-else-if="widget.type === 'sparkline'">
          <!-- Title label -->
          <p
            class="text-gray-500 uppercase font-bold tracking-widest truncate max-w-full text-center leading-none"
            :style="{ fontSize: titleSize, letterSpacing: '0.28em', marginBottom: '0.35em' }"
          >
            {{ widget.title }}
          </p>

          <!-- Main value -->
          <p
            class="tabular-nums leading-none text-center font-black transition-colors duration-500"
            :class="widget.colorClass ?? 'text-emerald-400'"
            :style="{
              fontSize: valueSize,
              textShadow: metricGlow(widget.colorClass ?? 'text-emerald-400'),
            }"
          >
            {{ resolveValue(widget) }}
          </p>

          <!-- Unit -->
          <p
            v-if="widget.unit"
            class="text-gray-500 uppercase font-semibold text-center leading-none"
            :style="{ fontSize: unitSize, letterSpacing: '0.22em', marginTop: '0.35em' }"
          >
            {{ widget.unit }}
          </p>

          <!-- Sparkline SVG — only rendered when live history exists -->
          <svg
            v-if="resolveSparkData(widget).length >= 2"
            class="w-full flex-shrink-0"
            style="height: 20px; margin-top: 0.45em;"
            viewBox="0 0 180 22"
            preserveAspectRatio="none"
          >
            <defs>
              <linearGradient :id="`sg-${widget.id}`" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%"   stop-color="#10b981" stop-opacity="0.45" />
                <stop offset="100%" stop-color="#10b981" stop-opacity="0"   />
              </linearGradient>
            </defs>
            <!-- Filled area under the curve -->
            <polygon
              :points="`0,22 ${buildSparkPoints(resolveSparkData(widget))} 180,22`"
              :fill="`url(#sg-${widget.id})`"
            />
            <!-- Line itself -->
            <polyline
              :points="buildSparkPoints(resolveSparkData(widget))"
              fill="none"
              stroke="#10b981"
              stroke-width="1.5"
              stroke-linecap="round"
              stroke-linejoin="round"
              opacity="0.9"
            />
          </svg>

          <!-- Placeholder bar when no history yet -->
          <div
            v-else
            class="w-full rounded-full bg-white/5 flex-shrink-0"
            style="height: 2px; margin-top: 0.65em;"
          />
        </template>

        <!-- ════════════════════════════════════════════════════════════════ -->
        <!--  STATUS  ─  Machine / line operational state                     -->
        <!-- ════════════════════════════════════════════════════════════════ -->
        <template v-else-if="widget.type === 'status'">
          <!-- Title label -->
          <p
            class="text-gray-500 uppercase font-bold tracking-widest truncate max-w-full text-center leading-none"
            :style="{ fontSize: titleSize, letterSpacing: '0.28em', marginBottom: '0.55em' }"
          >
            {{ widget.title }}
          </p>

          <!-- Status row: dot + label -->
          <div class="flex items-center gap-2.5">
            <span
              class="rounded-full flex-shrink-0"
              :class="[
                STATUS_CFG[resolveStatus(widget)].dotClass,
                resolveStatus(widget) === 'error' ? 'led-blink' : 'animate-pulse',
              ]"
              :style="{ width: dotSize, height: dotSize }"
            />
            <p
              class="leading-none font-black tracking-widest"
              :class="STATUS_CFG[resolveStatus(widget)].textClass"
              :style="{
                fontSize: count === 1 ? 'clamp(2.5rem, 9vw, 4rem)'
                        : count <= 2 ? 'clamp(2rem, 6vw, 3rem)'
                        : `calc(${valueSize} * 0.78)`,
                textShadow: [
                  '0 0 2px rgba(255,255,255,0.5)',
                  `0 0 18px ${STATUS_CFG[resolveStatus(widget)].glow}`,
                  `0 0 55px ${STATUS_CFG[resolveStatus(widget)].glow}55`,
                ].join(', '),
              }"
            >
              {{ STATUS_CFG[resolveStatus(widget)].label }}
            </p>
          </div>
        </template>

        <!-- ════════════════════════════════════════════════════════════════ -->
        <!--  ALARM  ─  Critical / warning event counts                       -->
        <!-- ════════════════════════════════════════════════════════════════ -->
        <template v-else-if="widget.type === 'alarm'">
          <!-- Title label -->
          <p
            class="text-gray-500 uppercase font-bold tracking-widest truncate max-w-full text-center leading-none"
            :style="{ fontSize: titleSize, letterSpacing: '0.28em', marginBottom: '0.6em' }"
          >
            {{ widget.title }}
          </p>

          <!-- Counts row -->
          <div class="flex items-center gap-5">

            <!-- Critical block -->
            <div class="flex flex-col items-center gap-0.5">
              <!-- Count value -->
              <p
                class="tabular-nums leading-none font-black"
                :class="resolveCritical(widget) > 0 ? 'text-red-400' : 'text-gray-800'"
                :style="{
                  fontSize: count <= 2
                    ? 'clamp(2.5rem, 8vw, 3.75rem)'
                    : valueSize,
                  textShadow: resolveCritical(widget) > 0
                    ? '0 0 2px rgba(255,255,255,0.5), 0 0 18px #f87171, 0 0 52px #f8717150'
                    : 'none',
                }"
              >
                {{ resolveCritical(widget) }}
              </p>
              <!-- Sub-label -->
              <div class="flex items-center gap-1">
                <span
                  class="w-1 h-1 rounded-full flex-shrink-0"
                  :class="resolveCritical(widget) > 0 ? 'bg-red-500 led-blink' : 'bg-gray-800'"
                />
                <span
                  class="font-bold uppercase leading-none"
                  :class="resolveCritical(widget) > 0 ? 'text-red-700' : 'text-gray-800'"
                  style="font-size: 0.48rem; letter-spacing: 0.28em;"
                >
                  CRIT
                </span>
              </div>
            </div>

            <!-- Divider glyph -->
            <span class="text-gray-800 font-bold" style="font-size: 0.9rem; line-height: 1;">╱</span>

            <!-- Warning block -->
            <div class="flex flex-col items-center gap-0.5">
              <!-- Count value -->
              <p
                class="tabular-nums leading-none font-black"
                :class="resolveWarning(widget) > 0 ? 'text-amber-400' : 'text-gray-800'"
                :style="{
                  fontSize: count <= 2
                    ? 'clamp(2.5rem, 8vw, 3.75rem)'
                    : valueSize,
                  textShadow: resolveWarning(widget) > 0
                    ? '0 0 2px rgba(255,255,255,0.5), 0 0 18px #f59e0b, 0 0 52px #f59e0b50'
                    : 'none',
                }"
              >
                {{ resolveWarning(widget) }}
              </p>
              <!-- Sub-label -->
              <div class="flex items-center gap-1">
                <span
                  class="w-1 h-1 rounded-full flex-shrink-0 animate-pulse"
                  :class="resolveWarning(widget) > 0 ? 'bg-amber-400' : 'bg-gray-800'"
                />
                <span
                  class="font-bold uppercase leading-none"
                  :class="resolveWarning(widget) > 0 ? 'text-amber-700' : 'text-gray-800'"
                  style="font-size: 0.48rem; letter-spacing: 0.28em;"
                >
                  WARN
                </span>
              </div>
            </div>

          </div>
        </template>

      </div><!-- /.widget cell -->

      <!--
        Fill trailing empty grid cells so the 1px gap background
        doesn't create visible grey strips in the last row.
      -->
      <div
        v-for="i in emptySlotCount"
        :key="`empty-${i}`"
        class="bg-black"
      />

    </div><!-- /.widget grid -->

  </div><!-- /.kiosk shell -->
</template>

<!---------------------------------------------------------------------------->

<style scoped>
/* ── CRT horizontal scanline texture ─────────────────────────────────────── */
.led-scanlines {
  background-image: repeating-linear-gradient(
    0deg,
    transparent,
    transparent 2px,
    rgba(255, 255, 255, 1) 2px,
    rgba(255, 255, 255, 1) 3px
  );
}

/* ── Error-state blink (fast) ─────────────────────────────────────────────── */
.led-blink {
  animation: led-blink 0.7s ease-in-out infinite;
}

@keyframes led-blink {
  0%, 100% { opacity: 1;   }
  50%       { opacity: 0.1; }
}
</style>
