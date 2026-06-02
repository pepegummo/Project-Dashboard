<script setup lang="ts">
/**
 * LedView.vue — 640 × 320 px Kiosk LED Screen
 * ─────────────────────────────────────────────
 * • Strict 640 × 320 container, overflow: hidden, no scrollbars
 * • Dark / high-contrast theme with LED phosphor glow
 * • Accepts `activeWidgets` prop; falls back to built-in mock data
 * • Smart CSS-Grid layout that scales 1 → 6+ widgets automatically
 * • Live data via Telemetry store + WebSocket polling (optional per widget)
 * • Widget types: 'metric' | 'sparkline' | 'status' | 'alarm' | 'daily-count'
 */

import { computed, ref, onMounted, onUnmounted } from 'vue'
import MachineDailyCountWidget from '@/components/widgets/MachineDailyCountWidget.vue'
import type { LedDailyData }   from '@/components/widgets/MachineDailyCountWidget.vue'
import { useTelemetryStore } from '@/stores/telemetry.store'
import { useMachineStore }   from '@/stores/machine.store'
import { useAlertStore }     from '@/stores/alert.store'
import { wsService }         from '@/services/ws.service'
import { api }               from '@/services/api.service'

// ─── Public Types ─────────────────────────────────────────────────────────────
export type LedWidgetType = 'metric' | 'sparkline' | 'status' | 'alarm' | 'gauge' | 'daily-count'

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

  // ── Gauge widget specific ─────────────────────────────────────────────────
  /** Minimum value for the gauge arc (default 0) */
  gaugeMin?: number
  /** Maximum value for the gauge arc (default 100) */
  gaugeMax?: number

  // ── Daily-count widget specific ───────────────────────────────────────────
  /** Number of days to fetch for daily-count bar chart (default 7) */
  days?: number

  // ── Grid layout ───────────────────────────────────────────────────────────
  /** Number of grid columns this cell spans (default 1). Use 2 for wide widgets. */
  colSpan?: number
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

// ─── Initial REST seed — called once on mount; WS handler keeps data live ────
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

  if (!wsService.isConnected) {
    wsService.connect(null)  // public kiosk — no token needed
    ownedWsConnection = true
  }

  if (liveMachineIds.value.length > 0) {
    wsService.subscribe(liveMachineIds.value)
    await fetchAllLatest() // seed store once; WS takes over from here
  }

  // Seed sparkline widgets with 30-min REST history (1-min buckets = 30 points, same as dashboard)
  const sparkWidgets = displayWidgets.value.filter(w => w.type === 'sparkline' && w.machineId && w.field)
  async function refreshSparklines() {
    for (const w of sparkWidgets) {
      try {
        const series = await api.getTelemetrySeries(w.machineId!, w.field!, { timeRange: '30m' })
        liveSparkData.value[w.id] = (series?.data ?? []).map((p: any) => ({
          ts:    p.bucket ?? p.ts,
          value: Number(p.avg ?? p.value),
        }))
      } catch { /* silently ignored */ }
    }
  }
  await refreshSparklines()
  // Re-fetch every 60s so the sparkline stays aligned with the dashboard chart
  // (replaces WS-accumulated raw points with proper 1-min bucket averages)
  if (sparkWidgets.length > 0) {
    sparkRefreshTimer = setInterval(refreshSparklines, 60_000)
  }

  // WS handler: drives metric / gauge / status cells only — sparklines are REST-only
  offSparkTelemetry = wsService.onTelemetry('*', (payload) => {
    telemetryStore.updateSnapshot(payload.machineId, payload.timestamp, payload.data as any)
  })

  // Preload alert event counts (non-blocking)
  if (alertStore.activeEvents.length === 0) {
    alertStore.fetchActiveEvents().catch(() => {})
  }

  // Fetch daily-count widget data; refresh every 60 s (daily data changes slowly)
  if (displayWidgets.value.some(w => w.type === 'daily-count')) {
    await fetchDailyCountWidgets()
    dailyCountTimer = setInterval(fetchDailyCountWidgets, 60_000)
  }
})

onUnmounted(() => {
  if (clockTimer)        { clearInterval(clockTimer);        clockTimer        = null }
  if (dailyCountTimer)   { clearInterval(dailyCountTimer);   dailyCountTimer   = null }
  if (sparkRefreshTimer) { clearInterval(sparkRefreshTimer); sparkRefreshTimer = null }
  offSparkTelemetry?.()
  if (liveMachineIds.value.length > 0) {
    wsService.unsubscribe(liveMachineIds.value)
  }
  if (ownedWsConnection) {
    wsService.disconnect()
    ownedWsConnection = false
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

// ─── Sparkline: REST-seeded 30-min rolling data per widget ───────────────────
const liveSparkData = ref<Record<string | number, Array<{ ts: string; value: number }>>>({})
let offSparkTelemetry: (() => void) | null = null
let ownedWsConnection = false
let sparkRefreshTimer: ReturnType<typeof setInterval> | null = null

function resolveSparkHistory(widget: LedWidget): { values: number[]; timestamps: string[] } {
  const live = liveSparkData.value[widget.id]
  if (!live?.length) return { values: [], timestamps: [] }
  return { values: live.map(p => p.value), timestamps: live.map(p => p.ts) }
}

// ─── Daily count data ─────────────────────────────────────────────────────────
interface DayPoint { date: string; count: number }
const dailyCountData = ref<Record<string | number, DayPoint[]>>({})
let dailyCountTimer: ReturnType<typeof setInterval> | null = null

async function fetchDailyCountWidgets() {
  const dcWidgets = displayWidgets.value.filter(w => w.type === 'daily-count' && w.machineId)
  for (const w of dcWidgets) {
    try {
      const result = await api.getTelemetryDailyCount(w.machineId!, w.days ?? 7)
      dailyCountData.value = {
        ...dailyCountData.value,
        [w.id]: (result?.data ?? []).map((r: any) => ({ date: r.date, count: r.count })),
      }
    } catch { /* silently ignored — non-critical display data */ }
  }
}

// Keep legacy alias (used only for v-if check in sparkline template)
function resolveSparkData(widget: LedWidget): number[] {
  return resolveSparkHistory(widget).values
}

// ─── Sparkline SVG helpers ─────────────────────────────────────────────────────
// Chart area inside the axes: x 40–198, y 2–40  (W=158, H=38)
// xOff=40 gives a 38-unit Y-label zone — fits up to 7 monospace chars at font-size=9
const SPARK = { xOff: 40, yOff: 2, W: 158, H: 38, xEnd: 198, yEnd: 40 } as const

function buildSparkPoints(values: number[]): string {
  if (values.length < 2) return ''
  const min   = Math.min(...values)
  const max   = Math.max(...values)
  const range = max - min || 1
  const { xOff, yOff, W, H } = SPARK
  return values
    .map((v, i) => {
      const x = xOff + (i / (values.length - 1)) * W
      const y = yOff + H - ((v - min) / range) * (H - 4) - 2
      return `${x.toFixed(1)},${y.toFixed(1)}`
    })
    .join(' ')
}

function fmtTime(iso: string): string {
  try {
    return new Date(iso).toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit' })
  } catch { return '' }
}

function sparkYLabel(widget: LedWidget, pos: 'max' | 'mid' | 'min'): string {
  const { values } = resolveSparkHistory(widget)
  if (!values.length) return ''
  const lo  = Math.min(...values)
  const hi  = Math.max(...values)
  const val = pos === 'max' ? hi : pos === 'min' ? lo : (hi + lo) / 2
  // Smart precision: fewer decimals for large numbers so label stays short
  const dec = val >= 100 ? 0 : val >= 10 ? 1 : (widget.precision ?? 1)
  return val.toFixed(dec)
}

// X axis always shows a fixed 30-min window anchored to the live clock,
// independent of how many history points have accumulated.
function sparkXLabel(_widget: LedWidget, idx: 0 | 1 | 2 | 3): string {
  const minutesAgo = [30, 20, 10, 0][idx]
  const t = new Date(now.value.getTime() - minutesAgo * 60_000)
  return fmtTime(t.toISOString())
}

// ─── Gauge helpers ─────────────────────────────────────────────────────────────

/** Resolve 0–1 fill ratio for the gauge arc */
function resolveGaugePercent(widget: LedWidget): number {
  const raw = (widget.machineId && widget.field)
    ? telemetryStore.getFieldValue(widget.machineId, widget.field)
    : (widget.value !== undefined ? parseFloat(String(widget.value)) : NaN)
  if (raw === undefined || raw === null || isNaN(Number(raw))) return 0
  const val = Number(raw)
  const min = widget.gaugeMin ?? 0
  const max = widget.gaugeMax ?? 100
  return Math.max(0, Math.min(1, (val - min) / (max - min)))
}

/**
 * SVG arc path for a semicircle gauge.
 * Center=(100,105), radius=80, sweeps from left (−180°) to right (0°).
 */
function buildGaugePath(pct: number): string {
  if (pct <= 0) return 'M 20,105 A 80,80 0 0,1 20.01,105'
  if (pct >= 1) return 'M 20,105 A 80,80 0 0,1 180,105'
  const angle = -Math.PI + pct * Math.PI
  const x = (100 + 80 * Math.cos(angle)).toFixed(2)
  const y = (105 + 80 * Math.sin(angle)).toFixed(2)
  return `M 20,105 A 80,80 0 0,1 ${x},${y}`
}

/** Emerald → amber → red based on how full the gauge is */
function gaugeColor(pct: number): string {
  if (pct >= 0.9)  return '#f87171'   // red-400   danger
  if (pct >= 0.75) return '#f59e0b'   // amber-400 warning
  return '#10b981'                     // emerald-400 normal
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

// ─── Daily count → LedDailyData transformer ───────────────────────────────────
/**
 * Converts raw DayPoint[] (from getTelemetryDailyCount) into the LedDailyData
 * shape expected by MachineDailyCountWidget's LED mode.
 * Returns undefined while data is still loading.
 */
function buildLedDailyData(widget: LedWidget): LedDailyData | undefined {
  const raw = dailyCountData.value[widget.id]
  if (!raw) return undefined

  const machine     = widget.machineId ? machineStore.machineById(widget.machineId) : undefined
  const machineName = machine?.name ?? widget.title

  // Midnight of today in local time
  const todayMidnight = new Date()
  todayMidnight.setHours(0, 0, 0, 0)
  const todayIso = todayMidnight.toISOString().slice(0, 10)

  // Index raw data by YYYY-MM-DD key
  const countByDate = new Map(raw.map(r => [r.date.slice(0, 10), r.count]))

  // Build the last 7 days (oldest → newest), ending on today
  const DAY_LABELS = ['SUN', 'MON', 'TUE', 'WED', 'THU', 'FRI', 'SAT'] as const
  const weeklyData = Array.from({ length: 7 }, (_, i) => {
    const d = new Date(todayMidnight)
    d.setDate(d.getDate() - (6 - i))          // i=0 → 6 days ago … i=6 → today
    const isoDate = d.toISOString().slice(0, 10)
    return {
      day:      DAY_LABELS[d.getDay()],
      count:    countByDate.get(isoDate) ?? 0,
      isToday:  isoDate === todayIso,
      isFuture: d > todayMidnight,
    }
  })

  const todayCount = weeklyData.find(d => d.isToday)?.count ?? 0
  const pastDays   = weeklyData.filter(d => !d.isFuture)
  const avgPerDay  = pastDays.length
    ? Math.round(pastDays.reduce((s, d) => s + d.count, 0) / pastDays.length)
    : 0

  return { machineName, todayCount, avgPerDay, weeklyData }
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

// Effective column span for a widget. `daily-count` is wide by design — its
// 8-hour bar chart needs two cells to breathe — so it defaults to span 2 even
// when an older exported URL omits colSpan. Spans are clamped to the column
// count so a wide widget never overflows the grid.
function effectiveSpan(w: LedWidget): number {
  const span = w.type === 'daily-count' ? (w.colSpan ?? 2) : (w.colSpan ?? 1)
  return Math.min(Math.max(span, 1), columns.value)
}

// Total grid cells occupied once colSpans are taken into account.
const totalUnits = computed(() =>
  displayWidgets.value.reduce((sum, w) => sum + effectiveSpan(w), 0),
)

const rows = computed(() => (count.value === 0 ? 1 : Math.ceil(totalUnits.value / columns.value)))

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
  return Math.max(0, total - totalUnits.value)
})

// ─── Per-cell typography — shrinks as more widgets appear ─────────────────────
const valueSize = computed(() => {
  const n = count.value
  if (n === 1) return 'clamp(4.5rem, 14vw, 6.25rem)'  // ~72 – 100 px  (full-width hero)
  if (n === 2) return 'clamp(3rem,   9vw, 4.5rem)'     // ~48 – 72 px   (half-width)
  if (n === 3) return 'clamp(2.25rem, 6vw, 3.25rem)'   // ~36 – 52 px   (third-width)
  if (n === 4) return 'clamp(2rem,   5vw, 3rem)'       // ~32 – 48 px   (quarter)
  if (n <= 6)  return 'clamp(1.375rem, 4vw, 2.25rem)'  // ~22 – 36 px   (3-col × 2-row)
  if (n <= 8)  return 'clamp(1.75rem,  5.5vw, 3rem)'    // ~28 – 48 px   (4-col × 2-row)
  return            'clamp(0.9rem,   2.5vw, 1.4rem)'   // ~14 – 22 px   (9+)
})

// Smaller value size for sparkline cells — chart is the focus, number can be compact
const sparkValueSize = computed(() => {
  const n = count.value
  if (n === 1) return 'clamp(2.75rem, 8vw,  3.75rem)'  // ~44 – 60 px
  if (n === 2) return 'clamp(2rem,    5vw,  2.75rem)'   // ~32 – 44 px
  if (n === 3) return 'clamp(1.5rem,  4vw,  2.25rem)'   // ~24 – 36 px
  if (n === 4) return 'clamp(1.375rem,3.5vw,2rem)'      // ~22 – 32 px
  if (n <= 6)  return 'clamp(1rem,    2.5vw, 1.5rem)'   // ~16 – 24 px
  if (n <= 8)  return 'clamp(1.25rem,  3.5vw,  2rem)'    // ~20 – 32 px
  return            'clamp(0.75rem,  1.5vw,  1rem)'     // ~12 – 16 px   (9+)
})

const titleSize = computed(() => {
  const n = count.value
  if (n <= 2) return '0.75rem'   // 12 px
  if (n <= 4) return '0.68rem'   // 10.9 px
  if (n <= 6) return '0.6rem'    // 9.6 px
  if (n <= 8) return '0.65rem'   // 10.4 px
  return           '0.5rem'     // 8 px   (9+)
})

const unitSize = computed(() => {
  const n = count.value
  if (n <= 2) return '0.85rem'   // 13.6 px
  if (n <= 4) return '0.75rem'   // 12 px
  if (n <= 6) return '0.68rem'   // 10.9 px
  if (n <= 8) return '0.72rem'   // 11.5 px
  return           '0.55rem'    // 8.8 px  (9+)
})

const cellPad = computed(() => {
  const n = count.value
  if (n === 1) return '1.25rem 1.75rem'
  if (n <= 3)  return '0.75rem 1rem'
  if (n <= 6)  return '0.5rem 0.75rem'
  if (n <= 8)  return '0.2rem 0.25rem'
  return            '0.25rem 0.4rem'
})

// Status dot size scales with available space
const dotSize = computed(() => {
  const n = count.value
  if (n <= 2) return '11px'
  if (n <= 4) return '8px'
  if (n <= 6) return '6px'
  if (n <= 8) return '5px'
  return '4px'
})

// SVG axis font-size (viewBox units) — grows for smaller cells to stay legible
// At n=7-8 (4-col), X scale ≈ 0.715× so font-size=11 → ~7.9 px screen width per char
const axisFont = computed(() => {
  const n = count.value
  if (n <= 4) return 9
  if (n <= 6) return 10
  return 11   // 7-8 widgets
})

// Gap between unit label and sparkline SVG — reclaim vertical space at high counts
const svgMarginTop = computed(() => {
  const n = count.value
  if (n <= 4) return '0.45em'
  if (n <= 6) return '0.35em'
  return '0.2em'   // 7-8 widgets
})

// SVG display height — grows for large widget counts to fill freed vertical space
const svgHeight = computed(() => {
  const n = count.value
  if (n <= 4) return '50px'
  if (n <= 6) return '55px'
  return '68px'   // 7-8: fills ~43px of previously empty top/bottom space
})

// Gauge SVG height — taller than sparkline since the arc IS the main content
const gaugeHeight = computed(() => {
  const n = count.value
  if (n <= 2) return '120px'
  if (n <= 4) return '100px'
  if (n <= 6) return '90px'
  return '85px'   // 7-8: fills most of the 140px available cell height
})

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
        <div class="w-1.5 h-1.5 rounded-full bg-emerald-400 animate-pulse" style="transform: translateY(-0.05em);"></div>

        <span
          class="text-gray-400 uppercase font-bold leading-none"
          style="font-size: 0.65rem; letter-spacing: 0.18em;"
        >
          CPF · LED View
        </span>
      </div>

      <!-- Right: widget count pill + date + clock -->
      <div class="flex items-center gap-3">
        <span
          v-if="count > 0"
          class="text-gray-500 font-bold tabular-nums"
          style="font-size: 0.62rem; letter-spacing: 0.12em;"
        >
          {{ count }}&thinsp;WIDGET{{ count !== 1 ? 'S' : '' }}
        </span>
        <span class="text-gray-200 tabular-nums font-semibold" style="font-size: 0.7rem; margin-left: 0.5rem; margin-right: 0.75rem;">{{ dateStr }}</span>
        <span
          class="text-gray-200 font-bold tabular-nums"
          style="font-size: 0.75rem; letter-spacing: 0.1em;"
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
        :style="{
          padding:    widget.type === 'daily-count' ? '0' : cellPad,
          gridColumn: effectiveSpan(widget) > 1 ? `span ${effectiveSpan(widget)}` : undefined,
        }"
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
            class="text-gray-300 uppercase font-bold truncate max-w-full text-center leading-none"
            :style="{ fontSize: titleSize, letterSpacing: '0.16em', marginBottom: '0.35em' }"
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
            class="text-gray-300 uppercase font-semibold text-center leading-none"
            :style="{ fontSize: unitSize, letterSpacing: '0.14em', marginTop: '0.4em' }"
          >
            {{ widget.unit }}
          </p>

          <!-- Trend -->
          <p
            v-if="widget.trend && trendSymbol(widget.trend)"
            class="font-bold text-center leading-none"
            :class="trendClass(widget.trend)"
            style="font-size: 0.65rem; letter-spacing: 0.14em; margin-top: 0.45em;"
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
            class="text-gray-300 uppercase font-bold truncate max-w-full text-center leading-none"
            :style="{ fontSize: titleSize, letterSpacing: '0.16em', marginBottom: '0.35em' }"
          >
            {{ widget.title }}
          </p>

          <!-- Main value (compact — chart is the focus) -->
          <p
            class="tabular-nums leading-none text-center font-black transition-colors duration-500"
            :class="widget.colorClass ?? 'text-emerald-400'"
            :style="{
              fontSize: sparkValueSize,
              textShadow: metricGlow(widget.colorClass ?? 'text-emerald-400'),
            }"
          >
            {{ resolveValue(widget) }}
          </p>

          <!-- Unit -->
          <p
            v-if="widget.unit"
            class="text-gray-300 uppercase font-semibold text-center leading-none"
            :style="{ fontSize: unitSize, letterSpacing: '0.14em', marginTop: '0.35em' }"
          >
            {{ widget.unit }}
          </p>

          <!-- Sparkline SVG with X/Y axes — last 30 min, 30 data points -->
          <svg
            v-if="resolveSparkData(widget).length >= 2"
            class="w-full flex-shrink-0"
            :style="{ height: svgHeight, marginTop: svgMarginTop }"
            viewBox="0 0 200 56"
            preserveAspectRatio="none"
          >
            <defs>
              <linearGradient :id="`sg-${widget.id}`" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%"   stop-color="#10b981" stop-opacity="0.35" />
                <stop offset="100%" stop-color="#10b981" stop-opacity="0"   />
              </linearGradient>
            </defs>

            <!-- ── Y axis grid lines (max / mid / min) ──────────────────── -->
            <line x1="40" :y1="SPARK.yOff"  x2="198" :y2="SPARK.yOff"  stroke="rgba(255,255,255,0.07)" stroke-width="0.5" />
            <line x1="40" y1="21"            x2="198" y2="21"            stroke="rgba(255,255,255,0.07)" stroke-width="0.5" />
            <line x1="40" :y1="SPARK.yEnd"  x2="198" :y2="SPARK.yEnd"  stroke="rgba(255,255,255,0.07)" stroke-width="0.5" />

            <!-- ── Y axis labels ─────────────────────────────────────────── -->
            <text x="38" :y="SPARK.yOff + 7" text-anchor="end" fill="rgba(255,255,255,0.92)" :font-size="axisFont" font-weight="bold" font-family="monospace">{{ sparkYLabel(widget, 'max') }}</text>
            <text x="38" y="25"              text-anchor="end" fill="rgba(255,255,255,0.92)" :font-size="axisFont" font-weight="bold" font-family="monospace">{{ sparkYLabel(widget, 'mid') }}</text>
            <text x="38" :y="SPARK.yEnd"     text-anchor="end" fill="rgba(255,255,255,0.92)" :font-size="axisFont" font-weight="bold" font-family="monospace">{{ sparkYLabel(widget, 'min') }}</text>

            <!-- ── Axes ──────────────────────────────────────────────────── -->
            <!-- Y axis rule -->
            <line x1="40" :y1="SPARK.yOff" x2="40" :y2="SPARK.yEnd + 1" stroke="rgba(255,255,255,0.18)" stroke-width="0.6" />
            <!-- X axis rule -->
            <line x1="39" :y1="SPARK.yEnd + 1" x2="199" :y2="SPARK.yEnd + 1" stroke="rgba(255,255,255,0.18)" stroke-width="0.6" />

            <!-- ── Area fill ──────────────────────────────────────────────── -->
            <polygon
              :points="`${SPARK.xOff},${SPARK.yEnd} ${buildSparkPoints(resolveSparkData(widget))} ${SPARK.xEnd},${SPARK.yEnd}`"
              :fill="`url(#sg-${widget.id})`"
            />

            <!-- ── Line ──────────────────────────────────────────────────── -->
            <polyline
              :points="buildSparkPoints(resolveSparkData(widget))"
              fill="none"
              stroke="#10b981"
              stroke-width="1.5"
              stroke-linecap="round"
              stroke-linejoin="round"
              opacity="0.9"
            />

            <!-- ── X axis labels (4 evenly spaced time marks) ────────────── -->
            <!-- chart spans 40–198 (W=158); centres at W/8 intervals = 60,99,139,178 -->
            <!-- all text-anchor="middle" → visual centres are perfectly equidistant  -->
            <text x="60"  y="54" text-anchor="middle" fill="rgba(255,255,255,0.92)" :font-size="axisFont" font-weight="bold" font-family="monospace">{{ sparkXLabel(widget, 0) }}</text>
            <text x="99"  y="54" text-anchor="middle" fill="rgba(255,255,255,0.92)" :font-size="axisFont" font-weight="bold" font-family="monospace">{{ sparkXLabel(widget, 1) }}</text>
            <text x="139" y="54" text-anchor="middle" fill="rgba(255,255,255,0.92)" :font-size="axisFont" font-weight="bold" font-family="monospace">{{ sparkXLabel(widget, 2) }}</text>
            <text x="178" y="54" text-anchor="middle" fill="rgba(255,255,255,0.92)" :font-size="axisFont" font-weight="bold" font-family="monospace">{{ sparkXLabel(widget, 3) }}</text>
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
            class="text-gray-300 uppercase font-bold truncate max-w-full text-center leading-none"
            :style="{ fontSize: titleSize, letterSpacing: '0.16em', marginBottom: '0.55em' }"
          >
            {{ widget.title }}
          </p>

          <!-- Status row: dot + label -->
          <div class="flex gap-2.5" style="align-items: center;">
            <span
              class="rounded-full flex-shrink-0"
              :class="[
                STATUS_CFG[resolveStatus(widget)].dotClass,
                resolveStatus(widget) === 'error' ? 'led-blink' : 'animate-pulse',
              ]"
              :style="{ width: dotSize, height: dotSize, display: 'block', lineHeight: '1', transform: 'translateY(-0.05em)' }"
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
                lineHeight: '1',
                margin: '0',
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
            class="text-gray-300 uppercase font-bold truncate max-w-full text-center leading-none"
            :style="{ fontSize: titleSize, letterSpacing: '0.16em', marginBottom: '0.6em' }"
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
                  style="transform: translateY(-0.05em);"
                />
                <span
                  class="font-bold uppercase leading-none"
                  :class="resolveCritical(widget) > 0 ? 'text-red-500' : 'text-gray-700'"
                  style="font-size: 0.65rem; letter-spacing: 0.14em;"
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
                  style="transform: translateY(-0.05em);"
                />
                <span
                  class="font-bold uppercase leading-none"
                  :class="resolveWarning(widget) > 0 ? 'text-amber-500' : 'text-gray-700'"
                  style="font-size: 0.65rem; letter-spacing: 0.14em;"
                >
                  WARN
                </span>
              </div>
            </div>

          </div>
        </template>

        <!-- ════════════════════════════════════════════════════════════════ -->
        <!--  GAUGE  ─  Semicircle arc with live value and range              -->
        <!-- ════════════════════════════════════════════════════════════════ -->
        <template v-else-if="widget.type === 'gauge'">
          <!-- Title label -->
          <p
            class="text-gray-300 uppercase font-bold truncate max-w-full text-center leading-none"
            :style="{ fontSize: titleSize, letterSpacing: '0.16em', marginBottom: '0.25em' }"
          >
            {{ widget.title }}
          </p>

          <!-- Semicircle arc gauge — viewBox 200×115 -->
          <svg
            class="w-full flex-shrink-0"
            :style="{ height: gaugeHeight }"
            viewBox="0 0 200 115"
            preserveAspectRatio="xMidYMid meet"
          >
            <!-- Background track -->
            <path
              d="M 20,105 A 80,80 0 0,1 180,105"
              fill="none"
              stroke="rgba(255,255,255,0.08)"
              stroke-width="14"
              stroke-linecap="round"
            />

            <!-- Glow layer (wide, faint) -->
            <path
              :d="buildGaugePath(resolveGaugePercent(widget))"
              fill="none"
              :stroke="gaugeColor(resolveGaugePercent(widget))"
              stroke-width="22"
              stroke-linecap="round"
              opacity="0.12"
            />

            <!-- Filled arc -->
            <path
              :d="buildGaugePath(resolveGaugePercent(widget))"
              fill="none"
              :stroke="gaugeColor(resolveGaugePercent(widget))"
              stroke-width="14"
              stroke-linecap="round"
            />

            <!-- Current value — large, centred in the arc hollow -->
            <text
              x="100" y="86"
              text-anchor="middle"
              font-family="monospace"
              font-weight="900"
              :font-size="axisFont + 8"
              :fill="gaugeColor(resolveGaugePercent(widget))"
            >{{ resolveValue(widget) }}</text>

            <!-- Unit -->
            <text
              x="100" y="99"
              text-anchor="middle"
              font-family="monospace"
              font-weight="bold"
              font-size="8"
              fill="rgba(255,255,255,0.55)"
            >{{ widget.unit ?? '' }}</text>

            <!-- Min label -->
            <text
              x="16" y="115"
              text-anchor="start"
              font-family="monospace"
              font-size="7"
              fill="rgba(255,255,255,0.4)"
            >{{ widget.gaugeMin ?? 0 }}</text>

            <!-- Max label -->
            <text
              x="184" y="115"
              text-anchor="end"
              font-family="monospace"
              font-size="7"
              fill="rgba(255,255,255,0.4)"
            >{{ widget.gaugeMax ?? 100 }}</text>
          </svg>
        </template>

        <!-- ════════════════════════════════════════════════════════════════ -->
        <!--  DAILY-COUNT  ─  MachineDailyCountWidget in LED mode            -->
        <!-- ════════════════════════════════════════════════════════════════ -->
        <template v-else-if="widget.type === 'daily-count'">
          <MachineDailyCountWidget
            v-if="buildLedDailyData(widget)"
            :widget="{
              id: String(widget.id),
              dashboardId: '',
              widgetType: 'daily-count',
              layout: { x: 0, y: 0, w: 1, h: 1 },
              config: { days: widget.days ?? 7 },
              machineId: widget.machineId,
              order: 0,
            }"
            :led-mode="true"
            :data="buildLedDailyData(widget)"
            class="w-full h-full"
          />
          <!-- Shown while daily data is still loading -->
          <div
            v-else
            class="flex flex-col items-center justify-center w-full h-full gap-1"
          >
            <span class="w-1 h-1 rounded-full bg-emerald-800 animate-pulse" />
            <p
              class="font-mono uppercase font-bold text-emerald-900"
              style="font-size: 0.55rem; letter-spacing: 0.3em;"
            >
              {{ widget.title }}
            </p>
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
