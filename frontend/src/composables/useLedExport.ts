/**
 * useLedExport — Export dashboard widget config as a shareable LED kiosk URL
 * ─────────────────────────────────────────────────────────────────────────────
 * Flow:
 *   DashboardWidget[]  →  mapToLedWidgets  →  LedWidget[]
 *   LedWidget[]        →  encode           →  base64 payload
 *   base64 payload     →  buildLedUrl      →  full URL  (/led?w=<payload>)
 *   URL                →  clipboard        →  button state flips to "Copied!"
 *
 * Encoding: JSON → encodeURIComponent → btoa
 *   • encodeURIComponent converts to ASCII-safe percent-encoding
 *     so btoa never hits a Unicode code-point > 0xFF.
 *   • Result is URL-safe base64 (no padding issues in query strings).
 *
 * The inverse (used by LedViewPage) is exported as `decodeLedPayload`.
 */

import { ref, computed } from 'vue'
import type { DashboardWidget } from '@/types'
import type { LedWidget } from '@/components/led/LedView.vue'

// ─── Widget type mapping ───────────────────────────────────────────────────────
//
// DashboardWidget.widgetType  →  LedWidget.type
// ─────────────────────────────────────────────
// kpi-card                   →  'metric'
// gauge                      →  'gauge'    (semicircle arc with min/max)
// daily-count                →  'daily-count' (bar chart + today count; data fetched by LedView)
// line-chart                 →  'sparkline'
// status-card (no field)     →  'status'   (shows machine RUNNING/OFFLINE badge)
// status-card (with field)   →  'metric'   (shows a specific sensor value)
// alarm-panel                →  'alarm'    (shows critical / warning counts)
// table                      →  'metric'   (best-effort: show first field value)
//
function mapToLedWidgets(widgets: DashboardWidget[]): LedWidget[] {
  return widgets.map((w): LedWidget | null => {
    const field        = w.config?.field as string | undefined
    const machineField = w.machine?.fields?.find(f => f.key === field)

    // Base properties shared by metric / sparkline / status widgets
    const base = {
      id:        w.id,
      title:     w.title ?? machineField?.label ?? (w.widgetType as string),
      machineId: w.machineId,
      field,
      unit:      (w.config?.unit as string | undefined) ?? machineField?.unit,
      precision: (w.config?.precision as number | undefined) ?? machineField?.precision ?? 2,
    } satisfies Partial<LedWidget>

    switch (w.widgetType) {
      case 'line-chart':
        return { ...base, type: 'sparkline' }

      case 'status-card':
        // If a specific field is configured → show it as a live metric;
        // otherwise → show the machine's operational state badge.
        return field
          ? { ...base, type: 'metric' }
          : { ...base, type: 'status' }

      case 'alarm-panel':
        // Alarm panel has no machine-specific metric — just counts.
        return {
          id:    w.id,
          type:  'alarm',
          title: w.title ?? 'System Alerts',
        }

      case 'gauge':
        // Semicircle arc gauge with min/max range from config or machine field limits
        return {
          ...base,
          type:     'gauge',
          gaugeMin: (w.config?.min as number | undefined) ?? machineField?.lowerLimit ?? 0,
          gaugeMax: (w.config?.max as number | undefined) ?? machineField?.upperLimit ?? 100,
        }

      case 'daily-count':
        return {
          id:        w.id,
          type:      'daily-count',
          title:     w.title ?? 'Daily Output',
          machineId: w.machineId,
          days:      (w.config?.days as number | undefined) ?? 7,
          colSpan:   2,
        }

      // kpi-card, table → rendered as a metric readout
      default:
        return { ...base, type: 'metric' }
    }
  }).filter((w): w is LedWidget => w !== null)
}

// ─── Encode / decode ───────────────────────────────────────────────────────────

/**
 * Encode a LedWidget array into a URL-safe base64 string.
 * Safe for any Unicode content (machine names, Thai characters, etc.)
 */
function encodeLedPayload(widgets: LedWidget[]): string {
  return btoa(encodeURIComponent(JSON.stringify(widgets)))
}

/**
 * Decode a base64 string back into a LedWidget array.
 * Used by LedViewPage.vue to reconstruct the widget configuration.
 * Returns an empty array on any parse / decode error (safe fallback).
 */
export function decodeLedPayload(raw: string): LedWidget[] {
  try {
    return JSON.parse(decodeURIComponent(atob(raw))) as LedWidget[]
  } catch {
    return []
  }
}

// ─── Composable ────────────────────────────────────────────────────────────────

export function useLedExport() {
  const copied  = ref(false)
  const loading = ref(false) // reserved for future async encoding
  let   resetTimer: ReturnType<typeof setTimeout> | null = null

  // Button label reacts to copy state
  const exportLabel = computed(() => (copied.value ? '✓ Copied!' : 'Export LED Link'))

  /**
   * Build the shareable LED URL from the current dashboard widget array.
   * The URL points to /led?w=<base64-payload>.
   *
   * @example
   * const url = buildLedUrl(dashboardStore.widgets)
   * // → https://yourapp.com/led?w=W3siaWQiOiIxIiwidHlwZ...
   */
  function buildLedUrl(widgets: DashboardWidget[]): string {
    const led     = mapToLedWidgets(widgets)
    const payload = encodeLedPayload(led)
    return `${window.location.origin}/led?w=${encodeURIComponent(payload)}`
  }

  /**
   * Map widgets → encode URL → copy to clipboard → flip button state for 3 s.
   * Returns the generated URL so callers can log or open it in a new tab.
   *
   * @example
   * const url = await exportLedLink(dashboardStore.widgets)
   */
  async function exportLedLink(widgets: DashboardWidget[]): Promise<string> {
    const url = buildLedUrl(widgets)

    try {
      // Modern Clipboard API (HTTPS or localhost)
      await navigator.clipboard.writeText(url)
    } catch {
      // Fallback: create a hidden textarea and use the legacy execCommand
      const ta = document.createElement('textarea')
      ta.value = url
      Object.assign(ta.style, {
        position: 'fixed',
        top:      '-9999px',
        left:     '-9999px',
        opacity:  '0',
      })
      document.body.appendChild(ta)
      ta.focus()
      ta.select()
      try { document.execCommand('copy') } catch { /* last resort */ }
      document.body.removeChild(ta)
    }

    // Flip button state
    copied.value = true
    if (resetTimer) clearTimeout(resetTimer)
    resetTimer = setTimeout(() => {
      copied.value = false
      resetTimer   = null
    }, 3000)

    return url
  }

  /**
   * Open the LED view in a new browser tab (useful for live preview).
   */
  function openLedPreview(widgets: DashboardWidget[]) {
    window.open(buildLedUrl(widgets), '_blank', 'noopener,noreferrer')
  }

  return {
    /** Whether the URL was just copied (resets after 3 s) */
    copied,
    /** Reactive button label: "Export LED Link" → "✓ Copied!" */
    exportLabel,
    /** Build the /led?w=... URL without copying */
    buildLedUrl,
    /** Copy the LED URL to clipboard and flip the button state */
    exportLedLink,
    /** Open the LED view in a new tab for quick preview */
    openLedPreview,
  }
}
