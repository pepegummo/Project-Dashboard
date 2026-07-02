<script setup lang="ts">
import { ref, computed, nextTick, onMounted, watch } from 'vue';
import { useRouter } from 'vue-router';
import { Sparkles, Send, Loader2, Bot, Save, Plus, Trash2, LayoutGrid } from 'lucide-vue-next';
import { api } from '@/services/api.service';
import { useDashboardStore } from '@/stores/dashboard.store';
import { useMachineStore } from '@/stores/machine.store';
import { useTelemetryStore } from '@/stores/telemetry.store';
import { useWidgetViewStateStore } from '@/stores/widget-view-state.store';
import GridStackCanvas from '@/components/dashboard/GridStackCanvas.vue';
import WidgetToolbox from '@/components/dashboard/WidgetToolbox.vue';
import WidgetConfigModal from '@/components/dashboard/WidgetConfigModal.vue';
import PreviewCanvasCard from '@/components/ai/PreviewCanvasCard.vue';
import CreatedCanvasCard from '@/components/ai/CreatedCanvasCard.vue';
import type { DashboardWidget, WidgetType, WidgetLayout, WidgetConfig } from '@/types';

type CanvasCard =
  | { kind: 'preview'; id: string; result: any; args: Record<string, unknown> }
  | { kind: 'created'; id: string; summary: string; dashboardId: string }
  | { kind: 'focus'; id: string; result: any }
  | { kind: 'dashboard'; id: string; dashboardId: string; result: any }

const router = useRouter();
const dashboardStore = useDashboardStore();
const machineStore = useMachineStore();
const telemetryStore = useTelemetryStore();
const widgetViewStateStore = useWidgetViewStateStore();
const canvasCards = ref<CanvasCard[]>([]);
const previewHighlightId = ref<string | undefined>(undefined);
const selectionResetToken = ref(0);
const mentionedWidgets = ref<Array<{ id: string; title: string }>>([]);
const previewCardRef = ref<InstanceType<typeof PreviewCanvasCard> | null>(null);
const aiSelectedIds = ref<string[]>([]);
const aiSelectedPreviewIds = ref<string[]>([]);
// While an AI preview card is on screen, it's the latest dashboard — hide the live grid.
const hasPreview = computed(() => canvasCards.value.some(c => c.kind === 'preview' || c.kind === 'dashboard'));
const processing = ref(false);
const input = ref('');
const canvasRef = ref<HTMLElement | null>(null);

function onMentionWidget({ widget, selected }: { widget: { id: string; title: string }; selected: boolean }) {
  mentionedWidgets.value = selected
    ? [...mentionedWidgets.value.filter(w => w.id !== widget.id), widget]
    : mentionedWidgets.value.filter(w => w.id !== widget.id);
  aiSelectedPreviewIds.value = aiSelectedPreviewIds.value.filter(x => x !== widget.id);
}

function onSelectLiveWidget(widget: DashboardWidget) {
  const inMentioned = mentionedWidgets.value.some(w => w.id === widget.id);
  const inAi = aiSelectedIds.value.includes(widget.id);
  if (inMentioned || inAi) {
    mentionedWidgets.value = mentionedWidgets.value.filter(w => w.id !== widget.id);
    aiSelectedIds.value = aiSelectedIds.value.filter(x => x !== widget.id);
  } else {
    mentionedWidgets.value = [...mentionedWidgets.value, { id: widget.id, title: widget.title ?? widget.widgetType }];
  }
}

function highlightWidgetById(id: string) {
  previewHighlightId.value = undefined;
  nextTick(() => {
    previewHighlightId.value = id;
  });
}

function setAiHighlight(id: string, title: string, isPreview: boolean) {
  if (isPreview) {
    if (!aiSelectedPreviewIds.value.includes(id))
      aiSelectedPreviewIds.value = [...aiSelectedPreviewIds.value, id];
    // Sync PreviewCanvasCard.selected so the first click deselects instead of re-adding
    nextTick(() => {
      const card = Array.isArray(previewCardRef.value) ? previewCardRef.value[0] : previewCardRef.value;
      card?.setSelection(id);
    });
  } else {
    if (!aiSelectedIds.value.includes(id))
      aiSelectedIds.value = [...aiSelectedIds.value, id];
  }
  mentionedWidgets.value = [...mentionedWidgets.value.filter(w => w.id !== id), { id, title }];
  highlightWidgetById(id);
}

function removeMention(id: string) {
  mentionedWidgets.value = mentionedWidgets.value.filter(w => w.id !== id);
  aiSelectedIds.value = aiSelectedIds.value.filter(x => x !== id);
  if (id.startsWith('preview-')) {
    aiSelectedPreviewIds.value = aiSelectedPreviewIds.value.filter(x => x !== id);
    // ref inside v-for is an array in Vue 3
    const card = Array.isArray(previewCardRef.value) ? previewCardRef.value[0] : previewCardRef.value;
    card?.clearSelection(id);
  }
}

const conversationId = ref<string | null>(null);

// Editor state for live canvas
const showToolbox = ref(false);
const showConfigModal = ref(false);
const editingWidget = ref<DashboardWidget | null>(null);
const gridCanvasRef = ref<InstanceType<typeof GridStackCanvas> | null>(null);
const saving = ref(false);
const dashboardSaving = ref(false);

// Toast state
const toastMsg = ref('');
const toastVisible = ref(false);
let toastTimer: ReturnType<typeof setTimeout> | null = null;

// True while restoring the saved draft on mount, so the save-watcher doesn't echo it back.
const restoring = ref(false);

function toPreviewWidgets(dbWidgets: DashboardWidget[]) {
  return dbWidgets.map(w => ({
    widgetId:    w.id,
    type:        w.widgetType,
    layout:      w.layout,
    title:       w.title ?? '',
    machine:     w.machine?.name ?? '',
    machineUuid: w.machineId,
    metric:      (w.config.field as string) ?? '',
    unit:        (w.config.unit as string) ?? '',
    ...(w.config.min    !== undefined ? { min:    w.config.min    as number } : {}),
    ...(w.config.max    !== undefined ? { max:    w.config.max    as number } : {}),
    ...(w.config.bucket ? { bucket: w.config.bucket as string } : {}),
    ...(w.config.sku    ? { sku:    w.config.sku    as string } : {}),
    ...(w.config.status ? { status: w.config.status as 'all' | 'good' | 'reject' } : {}),
  }));
}

async function saveDashboardCard(card: any, layouts: Record<string, WidgetLayout> = {}) {
  if (dashboardSaving.value) return;
  dashboardSaving.value = true;
  try {
    const { dashboardId } = card;
    const previewWidgets: any[] = card.result.widgets;
    const originalIds = new Set(dashboardStore.widgets.map(w => w.id));
    const currentIds  = new Set(previewWidgets.filter(w => w.widgetId).map((w: any) => w.widgetId as string));

    // Remove deleted widgets
    for (const id of originalIds) {
      if (!currentIds.has(id)) await api.deleteWidget(dashboardId, id);
    }

    // Update existing / add new
    for (let i = 0; i < previewWidgets.length; i++) {
      const pw = previewWidgets[i];
      const config: Record<string, unknown> = {
        field: pw.metric ?? '',
        unit:  pw.unit  ?? '',
        ...(pw.min  !== undefined ? { min:  pw.min  } : {}),
        ...(pw.max  !== undefined ? { max:  pw.max  } : {}),
        ...(pw.bucket        ? { bucket:       pw.bucket        } : {}),
        ...(pw.sku           ? { sku:          pw.sku           } : {}),
        ...(pw.status        ? { status:       pw.status        } : {}),
        ...(pw.startDateTime ? { startDateTime:pw.startDateTime } : {}),
        ...(pw.endDateTime   ? { endDateTime:  pw.endDateTime   } : {}),
      };
      if (pw.widgetId) {
        await api.updateWidget(dashboardId, pw.widgetId, { widgetType: pw.type, machineId: pw.machineUuid, title: pw.title, config } as any);
      } else {
        const perRow = 2; const cellW = 6, cellH = 4;
        const layout = { x: (i % perRow) * cellW, y: Math.floor(i / perRow) * cellH, w: cellW, h: cellH };
        await api.addWidget(dashboardId, { widgetType: pw.type, machineId: pw.machineUuid, title: pw.title, layout, config });
      }
    }

    // Save layouts for widgets that were explicitly dragged
    if (Object.keys(layouts).length > 0) {
      const dbLayouts = Object.entries(layouts)
        .map(([previewId, layout]) => {
          const i = parseInt(previewId.replace('preview-', ''), 10);
          const pw = previewWidgets[i];
          return pw?.widgetId ? { id: pw.widgetId as string, layout } : null;
        })
        .filter((x): x is { id: string; layout: WidgetLayout } => x !== null);
      if (dbLayouts.length) await api.bulkUpdateLayout(dashboardId, dbLayouts);
    }

    // Re-fetch and rebuild card with fresh widget IDs
    const freshDash = await dashboardStore.fetchDashboard(dashboardId);
    const idx = canvasCards.value.findIndex(c => c === card);
    if (idx >= 0) {
      canvasCards.value[idx] = {
        kind: 'dashboard', id: uid(), dashboardId,
        result: { dashboardName: freshDash?.name ?? '', widgets: toPreviewWidgets(freshDash?.widgets ?? []), summary: '' },
      };
    }
    showToast('Dashboard saved.');
  } catch (e: any) {
    showToast(`Error: ${e?.message ?? 'Save failed'}`);
  } finally {
    dashboardSaving.value = false;
  }
}

onMounted(async () => {
  machineStore.fetchMachines();
  try {
    const draft = await api.getPreviewDraft();
    if (draft?.data?.result) {
      // A pending preview/focus card wins over auto-loading a dashboard.
      restoring.value = true;
      conversationId.value = draft.conversationId || null;
      const kind = draft.data.kind ?? 'preview';
      canvasCards.value = [{ kind, id: uid(), result: draft.data.result, args: draft.data.args ?? {} }];
      nextTick(() => { restoring.value = false; });
    } else if (draft?.dashboardId) {
      restoring.value = true;
      const dash = await dashboardStore.fetchDashboard(draft.dashboardId);
      canvasCards.value = [{
        kind: 'dashboard', id: uid(), dashboardId: draft.dashboardId,
        result: { dashboardName: dash?.name ?? '', widgets: toPreviewWidgets(dash?.widgets ?? []), summary: '' }
      }];
      nextTick(() => { restoring.value = false; });
    } else {
      dashboardStore.currentDashboard = null; // clear navigation leftovers so context is empty on first message
    }
  } catch { dashboardStore.currentDashboard = null; /* no draft / offline — start empty */ }
});

// Persist the in-progress preview per user so it survives a refresh. Preview mutations
// are discrete user/AI actions, so we save on each change.
// ponytail: no debounce — add one if widget edits ever fire this in a tight stream.
watch(canvasCards, () => {
  if (restoring.value) return;
  const card = canvasCards.value.find(c => c.kind === 'preview' || c.kind === 'focus') as any;
  if (card) {
    api.putPreviewDraft({ conversationId: conversationId.value, data: { kind: card.kind, result: card.result, args: card.args } }).catch(() => {});
  }
}, { deep: true });

function showToast(msg: string) {
  toastMsg.value = msg;
  toastVisible.value = true;
  if (toastTimer) clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { toastVisible.value = false; }, 15000);
}

function uid() { return Date.now().toString(36) + Math.random().toString(36).slice(2); }

// ── Live canvas editor handlers ─────────────────────────────────────────────

function onAddWidget(type: WidgetType) {
  showToolbox.value = false;
  editingWidget.value = {
    id: '',
    dashboardId: dashboardStore.currentDashboard?.id ?? '',
    widgetType: type,
    layout: { x: 0, y: 9999, w: 6, h: 4 },
    config: {},
    order: 0,
  };
  showConfigModal.value = true;
}

async function onSaveWidget(data: { machineId?: string; widgetType: WidgetType; title?: string; config: WidgetConfig; layout: WidgetLayout }) {
  if (!editingWidget.value) return;
  try {
    if (editingWidget.value.id) {
      await dashboardStore.updateWidget(editingWidget.value.id, {
        widgetType: data.widgetType,
        machineId: data.machineId,
        title: data.title,
        config: data.config,
      });
    } else {
      await dashboardStore.addWidget(data);
    }
  } catch (e: any) {
    showToast(`Error: ${e?.message ?? 'Could not save widget.'}`);
    return;
  } finally {
    showConfigModal.value = false;
    editingWidget.value = null;
  }
}

function onEditWidget(widget: DashboardWidget) {
  editingWidget.value = widget;
  showConfigModal.value = true;
}

async function onRemoveWidget(widgetId: string) {
  if (!confirm('Remove this widget?')) return;
  await dashboardStore.removeWidget(widgetId);
}

async function onSaveLayout() {
  saving.value = true;
  try {
    const layouts = gridCanvasRef.value?.getCurrentLayouts() ?? [];
    if (layouts.length) await dashboardStore.saveLayout(layouts);
  } finally {
    saving.value = false;
  }
}

// ── Preview card widget handlers ────────────────────────────────────────────

function addPreviewWidget(widget: any) {
  const card = canvasCards.value.find(c => c.kind === 'preview' || c.kind === 'focus' || c.kind === 'dashboard') as any;
  if (card) card.result.widgets.push(widget);
}

function updatePreviewWidget(index: number, data: any) {
  const card = canvasCards.value.find(c => c.kind === 'preview' || c.kind === 'focus' || c.kind === 'dashboard') as any;
  if (card) Object.assign(card.result.widgets[index], data);
}

function confirmDashboard(dashboardId: string) {
  router.push(`/dashboards/${dashboardId}`);
}

// ── Focus preview (ephemeral single-metric answer card) ─────────────────────
const readToolNames = new Set(['get_latest_telemetry', 'get_telemetry_trend', 'get_daily_count']);

// Match a machine against a tool's machine_id token the same way the backend does:
// id equals, or the machine NAME contains the token (e.g. "CW-01" ⊂ "Checkweigher CW-01").
// Mirrors resolveMachineID in backend dashboard_action.go (LOWER(name) LIKE %token%).
function machineMatches(m: { id: string; name: string }, token: string): boolean {
  const t = token.toLowerCase();
  return !!t && (m.id.toLowerCase() === t || m.name.toLowerCase().includes(t));
}

// Build a live PreviewWidget for the metric the user asked about, or null.
// Text-driven: resolves machine + field from the question and the AI reply, with a
// read tool's input only as a first preference (the model frequently answers a data
// question without calling a tool, so we can't depend on tool metadata being present).
function deriveFocusWidget(messages: any[], rawText: string): any | null {
  const assistantText = (messages.find((m: any) => m.role === 'assistant')?.content ?? '').toLowerCase();
  const hay = `${rawText} ${assistantText}`.toLowerCase();

  // Machine: prefer a read tool's machine_id (substring match like the backend), else
  // a machine whose code-like name token (e.g. "CW-01") appears in the text.
  let machine: typeof machineStore.machines[number] | undefined;
  for (const msg of messages) {
    if (!readToolNames.has(msg.toolName ?? '')) continue;
    const key = (msg.toolInput?.machine_id as string) ?? '';
    const m = machineStore.machines.find(mm => machineMatches(mm, key));
    if (m) { machine = m; break; }
  }
  if (!machine) {
    machine = machineStore.machines.find(m =>
      m.name && m.name.toLowerCase().split(/\s+/).some(t => /\d/.test(t) && hay.includes(t))
    );
  }
  if (!machine) return null;

  // Metric: prefer an explicit tool metric, else a field whose key/label appears in the text.
  const explicit = messages
    .find((m: any) => readToolNames.has(m.toolName ?? '') && m.toolInput?.metric)?.toolInput?.metric as string | undefined;
  const field = explicit
    ? machine.fields.find(f => f.key === explicit)
    : machine.fields.find(f => hay.includes(f.key.toLowerCase()) || hay.includes(f.label.toLowerCase()));
  if (!field) return null;

  const trend = messages.some((m: any) => m.toolName === 'get_telemetry_trend') || /trend|history|กราฟ|ย้อนหลัง|เทรนด์/.test(hay);
  const daily = messages.some((m: any) => m.toolName === 'get_daily_count') || /daily|count|รายวัน|ต่อวัน|จำนวน/.test(hay);
  const type = trend ? 'line-chart'
    : daily ? 'daily-count'
    : (field.min !== undefined && field.max !== undefined) ? 'gauge' : 'kpi-card';

  // Extract bucket size for daily-count (default 1h)
  let bucket = '1h';
  const bucketMatch = /(\d+)\s*(minute|min|hour|hr|day|วัน|นาที|ชั่วโมง)s?/i.exec(hay);
  if (bucketMatch) {
    const val = bucketMatch[1];
    const unit = bucketMatch[2].toLowerCase();
    const unitChar = /minute|min|นาที/.test(unit) ? 'm' : /hour|hr|ชั่วโมง/.test(unit) ? 'h' : 'd';
    bucket = `${val}${unitChar}`;
  }

  // Extract piece status filter (default all)
  let status = 'all';
  if (/good|ดี|ปกติ/.test(hay)) status = 'good';
  else if (/reject|เสีย|ไม่ผ่าน/.test(hay)) status = 'reject';

  // Extract SKU filter
  let sku = '';
  const skuMatch = /sku\s*([\w-]+)/i.exec(hay);
  if (skuMatch && !/all|every|ทุก/.test(skuMatch[1])) {
    sku = skuMatch[1];
  }

  return {
    type,
    title: type === 'daily-count' ? `${machine.name} — Count` : `${machine.name} — ${field.label}`,
    machine: machine.name,
    machineUuid: machine.id,
    metric: field.key,
    unit: field.unit ?? '',
    ...(field.min !== undefined ? { min: field.min } : {}),
    ...(field.max !== undefined ? { max: field.max } : {}),
    ...(type === 'daily-count' ? { bucket, sku, status } : {}),
  };
}

async function confirmFocusCreate(card: any, name: string, layouts: Record<string, WidgetLayout> = {}) {
  const widgets = card.result?.widgets ?? [];
  if (!widgets.length) return;
  const dashName = name || (widgets[0]?.machine ?? 'New') + ' Dashboard';
  try {
    const dash = await dashboardStore.createDashboard({ name: dashName });
    // Load the new dashboard so addWidget can target it via currentDashboard
    await dashboardStore.fetchDashboard(dash.id);
    for (let i = 0; i < widgets.length; i++) {
      const pw = widgets[i];
      await dashboardStore.addWidget({
        machineId: pw.machineUuid,
        widgetType: pw.type,
        title: pw.title,
        config: {
          field: pw.metric, unit: pw.unit,
          ...(pw.min !== undefined ? { min: pw.min } : {}),
          ...(pw.max !== undefined ? { max: pw.max } : {}),
          ...(pw.bucket !== undefined ? { bucket: pw.bucket } : {}),
          ...(pw.sku !== undefined ? { sku: pw.sku } : {}),
          ...(pw.status !== undefined ? { status: pw.status } : {}),
        },
        layout: layouts[`preview-${i}`] ?? { x: 0, y: 9999, w: 6, h: 4 },
      });
    }
    canvasCards.value = canvasCards.value.filter(c => c !== card);
    router.push(`/dashboards/${dash.id}`);
  } catch (e) {
    console.error('Failed to create dashboard from focus card', e);
  }
}

function removeFocusCard(id: string) {
  canvasCards.value = canvasCards.value.filter(c => c.id !== id);
}

// Analytical intent → the user wants a trend/prediction, so the focused widget's
// full on-screen series is worth its tokens. Simple/edit messages skip it.
const ANALYTICAL_RE = /trend|analy|predict|forecast|how'?s it doing|over time|history|แนวโน้ม|วิเคราะห์|ทำนาย/i;

// Summarize the on-screen preview (widgets + live values) so the AI can answer
// questions about what the user is looking at. For a focused line-chart/daily-count
// widget on an analytical question, also inject the exact series it's rendering.
function buildDashboardContext(text = '', focusedIds: string[] = []): string {
  const analytical = ANALYTICAL_RE.test(text);
  // When the user has clicked/mentioned specific widgets, scope the context to just
  // those. Otherwise a salient sibling (e.g. a "Trend" line-chart's current value)
  // pulls the model off the widget the user actually focused. No focus → send all.
  const inScope = (id: string) => !focusedIds.length || focusedIds.includes(id);
  // For a focused chart/count widget on an analytical question, append the exact
  // series the widget already rendered (from widget-view-state) so the AI reads
  // on-screen data with no tool call. Empty for anything else.
  const seriesLine = (id: string, type: string): string => {
    if (!analytical || !focusedIds.includes(id)) return '';
    if (type !== 'line-chart' && type !== 'daily-count') return '';
    const s = widgetViewStateStore.seriesStates[id];
    if (!s?.data?.length) return '';
    // Last ~24 points is enough to read a trend — half the tokens of the full 48.
    return `\n  on-screen data — columns ${JSON.stringify(s.columns)}, data ${JSON.stringify(s.data.slice(-24))}`;
  };
  // Shown time window of a focused chart, so "what range is shown?" answers from
  // context with no tool. Cheap (two timestamps); focused widgets only.
  const windowLine = (id: string): string => {
    if (!focusedIds.includes(id)) return '';
    const d = widgetViewStateStore.datetimeStates[id];
    if (!d?.startDateTime || !d?.endDateTime) return '';
    return `, window ${d.startDateTime} → ${d.endDateTime}`;
  };

  const card = canvasCards.value.find(c => c.kind === 'preview' || c.kind === 'dashboard') as any;
  const widgets = card?.result?.widgets ?? [];
  if (widgets.length) {
    const lines = widgets.map((w: any, i: number) => {
      if (!inScope(`preview-${i}`)) return null;
      const focus = focusedIds.includes(`preview-${i}`) ? '[FOCUSED] ' : '';
      const parts = [`- ${focus}${w.type} "${w.title || ''}" — machine ${w.machine}`];
      if (w.machineUuid) {

          const val = telemetryStore.getFieldValue(w.machineUuid, w.metric);

          if (val !== undefined && val !== null) {

            parts.push(`current value: ${val}${w.unit || ''}`);

          }

        }
      // Bucket chip is local widget state, not persisted config — read the live selection.
      const bucket = widgetViewStateStore.bucketStates[`preview-${i}`] ?? w.bucket;
      if (bucket) parts.push(`bucket ${bucket}`);
      if (w.sku) parts.push(`sku ${w.sku}`);
      if (w.status) parts.push(`status ${w.status}`);
      return parts.join(', ') + windowLine(`preview-${i}`) + seriesLine(`preview-${i}`, w.type);
    }).filter(Boolean);
    const label = card.kind === 'dashboard' ? 'Active dashboard' : 'Current dashboard preview';
    return `${label} "${card.result.dashboardName}" on screen:\n${lines.join('\n')}`;
  }

  // Focus card — ephemeral metric widgets shown when show_metric ran
  const focusCard = canvasCards.value.find(c => c.kind === 'focus') as any;
  const focusWidgets = focusCard?.result?.widgets ?? [];
  if (focusWidgets.length) {
    const lines = focusWidgets.map((w: any, i: number) => {
      if (!inScope(`preview-${i}`)) return null;
      const focus = focusedIds.includes(`preview-${i}`) ? '[FOCUSED] ' : '';
      const parts = [`- ${focus}${w.type} "${w.title || ''}" — machine ${w.machine}`];
      if (w.metric) {
        parts.push(`metric ${w.metric}`);
        if (w.machineUuid) {
          const val = telemetryStore.getFieldValue(w.machineUuid, w.metric);
          if (val !== undefined && val !== null) {
            parts.push(`current value: ${val}${w.unit || ''}`);
          }
        }
      }
      const bucket = widgetViewStateStore.bucketStates[`preview-${i}`] ?? w.bucket;
      if (bucket) parts.push(`bucket ${bucket}`);
      if (w.sku) parts.push(`sku ${w.sku}`);
      if (w.status) parts.push(`status ${w.status}`);
      return parts.join(', ') + windowLine(`preview-${i}`) + seriesLine(`preview-${i}`, w.type);
    }).filter(Boolean);
    return `Metric focus card on screen (call show_metric to highlight/refresh):\n${lines.join('\n')}`;
  }

  // Active dashboard widgets — send full configurations and current values to the AI
  const live = dashboardStore.widgets;
  if (live.length) {
    const lines = live.map((w: any) => {
      if (!inScope(w.id)) return null;
      const focus = focusedIds.includes(w.id) ? '[FOCUSED] ' : '';
      const parts = [`- ${focus}${w.widgetType} "${w.title || ''}"`];
      const uuid = w.machineId || w.machine?.id;
      if (w.machine?.name) parts.push(`machine ${w.machine.name}`);
      if (w.config?.field) {
        parts.push(`metric ${w.config.field}`);
        if (uuid) {
          const val = telemetryStore.getFieldValue(uuid, w.config.field);
          if (val !== undefined && val !== null) {
            parts.push(`current value: ${val}${w.config.unit || ''}`);
          }
        }
      }
      const bucket = widgetViewStateStore.bucketStates[w.id] ?? w.config?.bucket;
      if (bucket) parts.push(`bucket ${bucket}`);
      if (w.config?.sku) parts.push(`sku ${w.config.sku}`);
      if (w.config?.status) parts.push(`status ${w.config.status}`);
      return parts.join(', ') + windowLine(w.id) + seriesLine(w.id, w.widgetType);
    }).filter(Boolean);
    return `Dashboard "${dashboardStore.currentDashboard?.name ?? ''}" is active on screen with widgets:\n${lines.join('\n')}`;
  }

  return '';
}

// ── Chat ────────────────────────────────────────────────────────────────────

async function sendMessage() {
  const rawText = input.value.trim();
  if ((!rawText && !mentionedWidgets.value.length) || processing.value) return;
  const mentionSnapshot = [...mentionedWidgets.value];
  const mentionSuffix = mentionSnapshot.map(w => `@${w.title}`).join(' ');
  const text = mentionSuffix ? `${rawText} ${mentionSuffix}`.trim() : rawText;
  if (!text) return;
  input.value = '';
  mentionedWidgets.value = [];
  aiSelectedIds.value = [];
  aiSelectedPreviewIds.value = [];
  selectionResetToken.value++;   // clear the widget mention rings once the message is sent
  processing.value = true;

  try {
    if (!conversationId.value) {
      const conv = await api.createConversation();
      conversationId.value = conv.id;
    }

    const messages = await api.chat(conversationId.value!, text, buildDashboardContext(text, mentionSnapshot.map(w => w.id)));

    for (const msg of messages) {
      if (msg.role === 'assistant' && msg.content?.trim()) {
        showToast(msg.content);
      }
      if (msg.toolName === 'show_metric') {
        const result = msg.toolResult as any;
        // Handles both single widget (happy path) and widgets array (fallback when metric not found).
        const widgetsToShow: any[] = result?.widgets ?? (result?.widget ? [result.widget] : []);
        const isFallback = !!result?.fallback;
        let liveHighlightDone = false;
        for (const w of widgetsToShow) {
          if (!w?.machineUuid || (!w.metric && w.type !== 'daily-count')) continue;
          const sameMachineWidgets = dashboardStore.widgets.filter(dw =>
            dw.machineId === w.machineUuid || dw.machine?.id === w.machineUuid
          );
          const existing = w.type === 'daily-count'
            ? sameMachineWidgets.find(dw => dw.widgetType === 'daily-count')
            : (sameMachineWidgets.find(dw => dw.config.field === w.metric && dw.widgetType === w.type)
               ?? sameMachineWidgets.find(dw => dw.config.field === w.metric));
          const focusCard = canvasCards.value.find(c => c.kind === 'focus') as any;
          const focusIdx = focusCard?.result.widgets.findIndex(
            (fw: any) => fw.machineUuid === w.machineUuid && fw.metric === w.metric && fw.type === w.type
          ) ?? -1;
          if (existing) {
            if (isFallback || !liveHighlightDone) {
              setAiHighlight(existing.id, existing.title ?? existing.widgetType, false);
              liveHighlightDone = true;
            }
          } else if (focusIdx >= 0) {
            aiSelectedPreviewIds.value = [...new Set([...aiSelectedPreviewIds.value, `preview-${focusIdx}`])];
            previewHighlightId.value = undefined;
            nextTick(() => { previewHighlightId.value = `preview-${focusIdx}`; });
          } else if (hasPreview.value) {
            // A preview dashboard is on screen — highlight its widget(s) for this metric.
            // Preview stores machine as the short name (e.g. "CW-01"); show_metric returns
            // the full name ("Checkweigher CW-01") + machineUuid, so match by substring/uuid.
            const previewCard = canvasCards.value.find(c => c.kind === 'preview' || c.kind === 'dashboard') as any;
            const pws: any[] = previewCard?.result?.widgets ?? [];
            pws.forEach((pw: any, i: number) => {
              const machineOk = (pw.machineUuid && pw.machineUuid === w.machineUuid)
                || (!!pw.machine && !!w.machine && w.machine.toLowerCase().includes((pw.machine as string).toLowerCase()));
              if (pw.metric === w.metric && machineOk) {
                setAiHighlight(`preview-${i}`, pw.title ?? pw.metric ?? `preview-${i}`, true);
                previewHighlightId.value = undefined;
                nextTick(() => { previewHighlightId.value = `preview-${i}`; });
              }
            });
          } else {
            if (focusCard) {
              focusCard.result.widgets.push(w);
              const idx = focusCard.result.widgets.length - 1;
              previewHighlightId.value = undefined;
              nextTick(() => { previewHighlightId.value = `preview-${idx}`; });
            } else {
              canvasCards.value = [{ kind: 'focus', id: uid(), result: { dashboardName: '', widgets: [w], summary: '' } }];
              previewHighlightId.value = undefined;
              nextTick(() => { previewHighlightId.value = 'preview-0'; });
            }
          }
        }
      }
      if (msg.toolName === 'preview_dashboard') {
        const r = msg.toolResult as any;
        if (r?.preview && r.dashboardName) {
          const args = (msg.toolInput ?? {}) as Record<string, unknown>;
          const existing = canvasCards.value.findIndex(c => c.kind === 'preview');
          if (existing >= 0) {
            canvasCards.value[existing] = { kind: 'preview', id: uid(), result: r, args };
          } else {
            canvasCards.value = [{ kind: 'preview', id: uid(), result: r, args }];
          }
        }
      }
      if (msg.toolName === 'preview_add_widget') {
        const pw = msg.toolResult as any;
        const previewCard = canvasCards.value.find(c => c.kind === 'preview' || c.kind === 'dashboard') as any;
        if (pw && previewCard) {
          previewCard.result.widgets.push(pw);
        }
      }
      if (msg.toolName === 'preview_remove_widget') {
        const r = msg.toolResult as any;
        const previewCard = canvasCards.value.find(c => c.kind === 'preview' || c.kind === 'dashboard') as any;
        if (r?.widgetTitle && previewCard) {
          const idx = findPreviewWidgetIdx(previewCard, r.widgetTitle);
          if (idx >= 0) {
            previewCard.result.widgets.splice(idx, 1);
          } else {
            showToast(`Widget "${r.widgetTitle}" not found — nothing removed`);
          }
        }
      }
      if (msg.toolName === 'preview_update_widget') {
        const r = msg.toolResult as any;
        const previewCard = canvasCards.value.find(c => c.kind === 'preview' || c.kind === 'focus' || c.kind === 'dashboard') as any;
        if (r?.widgetTitle && r.changes && previewCard) {
          const idx = findPreviewWidgetIdx(previewCard, r.widgetTitle);
          if (idx >= 0) {
            const changes = { ...r.changes };
            // Canonicalize SKU casing against the machine's real SKUs so the chip shows the
            // stored value (e.g. "CW-1001") regardless of how the user typed it to the AI.
            const machineUuid = previewCard.result.widgets[idx]?.machineUuid;
            if (changes.sku && machineUuid) {
              const skus = await api.getMachineSkus(machineUuid).catch(() => [] as string[]);
              const canon = skus.find(s => s.toLowerCase() === String(changes.sku).toLowerCase());
              if (canon) changes.sku = canon;
            }
            Object.assign(previewCard.result.widgets[idx], changes);
            // Flash the edited widget so the change is visible. Clear first so re-editing
            // the same widget still triggers the highlight watch; set after the remount settles.
            previewHighlightId.value = undefined;
            nextTick(() => { previewHighlightId.value = `preview-${idx}`; });
          }
        }
      }
    }

    // Focus cards are ephemeral: keep them if this turn used show_metric, clear otherwise.
    if (!messages.some(m => m.toolName === 'show_metric')) {
      canvasCards.value = canvasCards.value.filter(c => c.kind !== 'focus');
    }

    // Skip highlight when the AI performed a builder action — those turns are not data queries
    const builderToolNames = new Set([
      'preview_dashboard', 'create_custom_dashboard',
      'preview_add_widget', 'preview_remove_widget', 'preview_update_widget',
      'add_widget_to_dashboard', 'remove_widget',
      'create_alert', 'manage_alert_event',
      'show_metric', // its own loop handler owns the highlight/focus decision
    ]);
    const usedBuilderTool = messages.some(m => m.toolName && builderToolNames.has(m.toolName));

    if (!usedBuilderTool) {
    const liveMentioned = mentionSnapshot.filter(w => !w.id.startsWith('preview-'));
    const previewMentioned = mentionSnapshot.filter(w => w.id.startsWith('preview-'));

    // Highlight follows what the AI actually answered about, not just the focused widget:
    // derive from the read tools first so "tell me about reject" while Weight is focused
    // moves the ring to reject. The focused widget is only a fallback (end of block).
    {
      const assistantText = messages.find(m => m.role === 'assistant')?.content?.toLowerCase() ?? '';
      const previewCard = canvasCards.value.find(c => c.kind === 'preview' || c.kind === 'dashboard') as any;
      for (const msg of messages) {
        if (!readToolNames.has(msg.toolName ?? '')) continue;
        const machineId = ((msg.toolInput as any)?.machine_id as string ?? '').toLowerCase();
        const metric = (msg.toolInput as any)?.metric as string | undefined;

        // Live canvas — match the machine the same substring way the backend resolves it
        // (tool machine_id "CW-01" ⊂ name "Checkweigher CW-01"), not exact equality.
        const liveCandidates = dashboardStore.widgets.filter(dw =>
          dw.machineId === machineId || (!!dw.machine?.name && dw.machine.name.toLowerCase().includes(machineId))
        );
        if (liveCandidates.length) {
          // No arbitrary fallback: only highlight a widget whose field the AI actually
          // read/answered about. Otherwise leave it to the value-based text-scan below —
          // highlighting the machine's first widget (e.g. Speed) for a status query is wrong.
          const w = metric
            ? liveCandidates.find(dw => dw.config.field === metric)
            : msg.toolName === 'get_daily_count'
              ? liveCandidates.find(dw => dw.widgetType === 'daily-count')
              : liveCandidates.find(dw => dw.config.field && assistantText.includes(dw.config.field.toLowerCase()));
          if (w) {
            const sameField = w.config.field
              ? liveCandidates.filter(dw => dw.config.field === w.config.field)
              : [w];
            for (const sw of sameField) setAiHighlight(sw.id, sw.title ?? sw.widgetType, false);
            break;
          }
        }

        // Preview canvas (machine stored as name string)
        if (previewCard?.result?.widgets) {
          const pws: any[] = previewCard.result.widgets;
          const previewCandidates = pws
            .map((w, i) => ({ w, id: `preview-${i}` }))
            .filter(({ w }) => (w.machine as string)?.toLowerCase() === machineId);
          if (previewCandidates.length) {
            const match = metric
              ? previewCandidates.find(({ w }) => w.metric === metric)
              : previewCandidates.find(({ w }) => w.metric && assistantText.includes((w.metric as string).toLowerCase()));
            if (match) {
              const sameMetric = match.w.metric
                ? previewCandidates.filter(({ w }) => w.metric === match.w.metric)
                : [match];
              for (const m of sameMetric) setAiHighlight(m.id, m.w.title ?? m.w.metric ?? m.id, true);
              break;
            }
          }
        }
      }
    }

    // Text-scan fallback: AI answered from context (no tool call fired) → match by reply text
    if (!aiSelectedIds.value.length && !aiSelectedPreviewIds.value.length) {
      const assistantText = messages.find(m => m.role === 'assistant')?.content?.toLowerCase() ?? '';
      if (assistantText) {
        // Extract standalone numbers from the AI reply — works regardless of language.
        // Boundary guards ignore digits glued to letters/hyphens so SKU/ID codes like
        // "cw-1001" don't create a false value match against some widget's reading.
        const nums = (assistantText.match(/(?<![\w-])\d[\d,]*(?:\.\d+)?(?![\w-])/g) ?? [])
          .map(s => parseFloat(s.replace(/,/g, ''))).filter(n => !isNaN(n));

        // Resolve a preview widget's UUID: prefer machineUuid on the widget,
        // fall back to looking up the machine name in the machine store.
        const resolveUuid = (w: any): string | undefined =>
          (w.machineUuid as string | undefined) ||
          machineStore.machines.find(m => m.name.toLowerCase() === (w.machine as string ?? '').toLowerCase())?.id;

        const previewCard = canvasCards.value.find(c => c.kind === 'preview' || c.kind === 'dashboard') as any;
        if (previewCard?.result?.widgets) {
          const candidates = (previewCard.result.widgets as any[]).map((w, i) => ({ w, id: `preview-${i}` }));

          // 1. Metric/title text match (works when AI responds in same language as field names)
          let match = candidates.find(({ w }) =>
            (w.metric && assistantText.includes((w.metric as string).toLowerCase())) ||
            (w.title && assistantText.includes((w.title as string).toLowerCase()))
          );

          // 2. Value match: AI always states the current reading — language-agnostic
          if (!match && nums.length) {
            match = candidates.find(({ w }) => {
              const uuid = resolveUuid(w);
              if (!uuid || !w.metric) return false;
              const val = Number(telemetryStore.getFieldValue(uuid, w.metric));
              return !isNaN(val) && nums.some(n => Math.abs(n - val) < 1);
            });
          }

          // 3. Machine name fallback (least specific — highlights first widget for that machine).
          // Skipped when a widget is focused: the focused-widget fallback below is a better
          // signal than "first widget for the machine" (e.g. a SKU answer that only names the
          // machine shouldn't grab the reject KPI over the clicked count widget).
          if (!match && !mentionSnapshot.length) {
            match = candidates.find(({ w }) =>
              w.machine && assistantText.includes((w.machine as string).toLowerCase())
            );
          }

          if (match) {
            const sameMetric = match.w.metric
              ? candidates.filter(({ w }) => w.metric === match.w.metric)
              : [match];
            for (const m of sameMetric) setAiHighlight(m.id, m.w.title ?? m.w.metric ?? m.id, true);
          }
        }

        if (!aiSelectedPreviewIds.value.length) {
          const liveCandidates = dashboardStore.widgets;
          let liveMatch = liveCandidates.find(dw =>
            (dw.config.field && assistantText.includes(dw.config.field.toLowerCase())) ||
            (dw.title && assistantText.includes(dw.title.toLowerCase()))
          );
          if (!liveMatch && nums.length) {
            liveMatch = liveCandidates.find(dw => {
              const uuid = dw.machineId ?? dw.machine?.id;
              const field = dw.config.field;
              if (!uuid || !field) return false;
              const val = Number(telemetryStore.getFieldValue(uuid, field));
              return !isNaN(val) && nums.some(n => Math.abs(n - val) < 1);
            });
          }
          if (liveMatch) {
            const sameField = liveMatch.config.field
              ? liveCandidates.filter(dw => dw.config.field === liveMatch.config.field)
              : [liveMatch];
            for (const sw of sameField) setAiHighlight(sw.id, sw.title ?? sw.widgetType, false);
          }
        }
      }
    }

    // Focused-widget fallback: user mentioned a widget but the answer gave no subject
    // to derive (e.g. "what is this?") → honor the focused widget.
    if (!aiSelectedIds.value.length && !aiSelectedPreviewIds.value.length) {
      if (liveMentioned.length)    setAiHighlight(liveMentioned[0].id, liveMentioned[0].title, false);
      if (previewMentioned.length) setAiHighlight(previewMentioned[0].id, previewMentioned[0].title, true);
    }

    } // end if (!usedBuilderTool)

    // Focus preview: a data question whose metric has no live widget to highlight →
    // show an ephemeral, live single-metric card (not persisted, user adds it manually).
    // Runs even when show_metric (a builder tool) fired but produced no highlight — e.g.
    // it errored, matched nothing, or was refused because a preview was open. Text-driven,
    // so it needs no tool to have fired. Guarded against duplicating a card show_metric
    // already created (no existing focus card, nothing highlighted, no preview open).
    if (!aiSelectedIds.value.length && !aiSelectedPreviewIds.value.length && !hasPreview.value
        && !canvasCards.value.some(c => c.kind === 'focus')) {
      if (!machineStore.machines.length) await machineStore.fetchMachines();
      const pw = deriveFocusWidget(messages, rawText);
      if (pw) canvasCards.value = [{ kind: 'focus', id: uid(), result: { dashboardName: '', widgets: [pw], summary: '' } }];
    }
  } catch (e: any) {
    showToast(`Error: ${e?.message ?? 'Something went wrong. Please try again.'}`);
  } finally {
    processing.value = false;
    scrollToBottom();
  }
}

function clearChat() {
  canvasCards.value = [];
  conversationId.value = null;
  dashboardStore.currentDashboard = null;   // also drop the dashboard selected from main
  input.value = '';
  toastVisible.value = false;
  mentionedWidgets.value = [];
  aiSelectedIds.value = [];
  aiSelectedPreviewIds.value = [];
  api.deletePreviewDraft().catch(() => {});
}

async function confirmCreate(dashboardName: string, layouts: Record<string, WidgetLayout> = {}) {
  const card = canvasCards.value.find(c => c.kind === 'preview') as any;
  try {
    processing.value = true;
    const widgetsWithLayouts = (card?.result?.widgets ?? []).map((w: any, i: number) => ({
      ...w,
      layout: layouts[`preview-${i}`] ?? undefined,
    }));
    const r = await api.executeAiTool('create_custom_dashboard', {
      ...(card?.args ?? {}),
      name: dashboardName,
      widgets: widgetsWithLayouts,
    }) as any;
    if (r?.success && r.dashboardId) {
      canvasCards.value = [];
      conversationId.value = null;
      dashboardStore.currentDashboard = null;
      mentionedWidgets.value = [];
      aiSelectedIds.value = [];
      aiSelectedPreviewIds.value = [];
      api.deletePreviewDraft().catch(() => {});
      showToast(`Dashboard "${dashboardName}" created! Find it in the Dashboards list.`);
      router.push(`/dashboards/${r.dashboardId}`);
    }
  } catch (e: any) {
    showToast(`Error: ${e?.message ?? 'Failed to create dashboard'}`);
  } finally {
    processing.value = false;
  }
}

// Locate a preview widget by (fuzzy) title — shared by the remove/update chat handlers.
function findPreviewWidgetIdx(previewCard: any, title: string): number {
  const rt = title.toLowerCase();
  return previewCard.result.widgets.findIndex((w: any) => {
    const wt = (w.title ?? '').toLowerCase();
    return wt === rt || wt.includes(rt) || rt.includes(wt);
  });
}

function removePreviewWidget(index: number) {
  const card = canvasCards.value.find(c => c.kind === 'preview' || c.kind === 'dashboard') as any;
  if (card) card.result.widgets.splice(index, 1);
}

function scrollToBottom() {
  nextTick(() => {
    if (canvasRef.value) canvasRef.value.scrollTop = canvasRef.value.scrollHeight;
  });
}

function handleKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); sendMessage(); }
}
</script>

<template>
  <div class="flex flex-col h-full">
    <!-- ── CANVAS AREA ── -->
    <div ref="canvasRef" class="flex-1 overflow-y-auto px-6 py-6 flex flex-col gap-4 relative min-h-0">

      <!-- Empty state -->
      <div
        v-if="!canvasCards.length && !dashboardStore.widgets.length"
        class="flex-1 flex flex-col items-center justify-center text-center py-24 select-none"
      >
        <div class="w-16 h-16 rounded-2xl bg-gradient-to-br from-primary-500/20 to-accent-violet/20 border border-primary-500/20 flex items-center justify-center mb-5">
          <Sparkles class="w-8 h-8 text-primary-400" />
        </div>
        <h2 class="text-xl font-semibold text-white mb-2">IotVision AI</h2>
        <p class="text-gray-500 text-sm max-w-sm leading-relaxed">
          Describe what you want to monitor and dashboards will appear here.<br>
          <span class="text-gray-600">Try "Create a temperature monitor for Machine A."</span>
        </p>
      </div>

      <!-- Card feed (preview + created only) -->
      <template v-for="card in canvasCards" :key="card.id">
        <PreviewCanvasCard
          v-if="card.kind === 'preview'"
          ref="previewCardRef"
          :result="(card as any).result"
          :highlight-id="previewHighlightId"
          :ai-selected-widget-ids="aiSelectedPreviewIds"
          :reset-token="selectionResetToken"
          @confirm="(name, layouts) => confirmCreate(name, layouts)"
          @remove-widget="removePreviewWidget"
          @add-widget="addPreviewWidget"
          @update-widget="updatePreviewWidget"
          @mention-widget="onMentionWidget"
        />
        <PreviewCanvasCard
          v-else-if="card.kind === 'focus'"
          variant="focus"
          ref="previewCardRef"
          :result="(card as any).result"
          :highlight-id="previewHighlightId"
          :ai-selected-widget-ids="aiSelectedPreviewIds"
          @confirm="(name, layouts) => confirmFocusCreate(card, name, layouts)"
          @remove-widget="() => removeFocusCard(card.id)"
          @add-widget="addPreviewWidget"
          @update-widget="updatePreviewWidget"
          @mention-widget="onMentionWidget"
        />
        <PreviewCanvasCard
          v-else-if="card.kind === 'dashboard'"
          variant="dashboard"
          ref="previewCardRef"
          :result="(card as any).result"
          :saving="dashboardSaving"
          :highlight-id="previewHighlightId"
          :ai-selected-widget-ids="aiSelectedPreviewIds"
          :reset-token="selectionResetToken"
          @confirm="() => confirmDashboard((card as any).dashboardId)"
          @save="(layouts) => saveDashboardCard(card, layouts)"
          @remove-widget="removePreviewWidget"
          @add-widget="addPreviewWidget"
          @update-widget="updatePreviewWidget"
          @mention-widget="onMentionWidget"
        />
        <CreatedCanvasCard
          v-else-if="card.kind === 'created'"
          :summary="(card as any).summary"
          :dashboard-id="(card as any).dashboardId"
        />
      </template>

      <!-- Live widget grid -->
      <div v-if="dashboardStore.widgets.length && !hasPreview" class="mt-2">
        <!-- Mini toolbar -->
        <div class="flex items-center justify-between mb-2">
          <p class="text-xs text-gray-500">{{ dashboardStore.widgets.length }} widgets · drag to move, gear to configure</p>
          <div class="flex items-center gap-2">
            <button class="btn-secondary text-xs py-1 px-2.5" @click="showToolbox = !showToolbox">
              <Plus class="w-3.5 h-3.5" />
              Add Widget
            </button>
            <button class="btn-primary text-xs py-1 px-2.5" :disabled="saving" @click="onSaveLayout">
              <Loader2 v-if="saving" class="w-3.5 h-3.5 animate-spin" />
              <Save v-else class="w-3.5 h-3.5" />
              {{ saving ? 'Saving…' : 'Save' }}
            </button>
          </div>
        </div>

        <!-- Toolbox + canvas in flex row (same pattern as DashboardEditorPage) -->
        <div class="flex gap-4">
          <WidgetToolbox
            v-if="showToolbox"
            @select="onAddWidget"
            @close="showToolbox = false"
          />
          <div class="flex-1">
            <GridStackCanvas
              ref="gridCanvasRef"
              :widgets="dashboardStore.widgets"
              :selected-ids="[...mentionedWidgets.filter(w => !w.id.startsWith('preview-')).map(w => w.id), ...aiSelectedIds]"
              :highlighted-id="previewHighlightId"
              @edit-widget="onEditWidget"
              @remove-widget="onRemoveWidget"
              @select-widget="onSelectLiveWidget"
            />
          </div>
        </div>
      </div>

    </div>

    <!-- ── COMMAND BAR ── -->
    <div class="flex-shrink-0 px-6 pb-6 pt-2">
      <!-- Toast -->
      <Transition name="toast">
        <div
          v-if="toastVisible"
          class="mb-3 flex items-start gap-2.5 bg-surface-200 border border-white/10 rounded-xl px-4 py-3 shadow-card"
        >
          <div class="w-5 h-5 rounded-full bg-accent-violet/20 flex items-center justify-center flex-shrink-0 mt-0.5">
            <Bot class="w-3 h-3 text-accent-violet" />
          </div>
          <p class="text-sm text-gray-300 leading-relaxed whitespace-pre-wrap flex-1">{{ toastMsg }}</p>
          <button class="text-gray-600 hover:text-gray-400 flex-shrink-0 mt-0.5" @click="toastVisible = false">✕</button>
        </div>
      </Transition>

      <!-- Processing indicator -->
      <Transition name="fade">
        <p v-if="processing" class="text-xs text-gray-600 text-center mb-2">Processing…</p>
      </Transition>

      <!-- Widget mention chips -->
      <div v-if="mentionedWidgets.length" class="flex flex-wrap gap-1 mb-2">
        <span
          v-for="w in mentionedWidgets"
          :key="w.id"
          class="inline-flex items-center gap-1.5 px-2 py-0.5 rounded-md bg-accent-violet/15 border border-accent-violet/30 text-xs text-accent-violet cursor-pointer hover:bg-accent-violet/25 select-none transition-colors"
          @click="highlightWidgetById(w.id)"
        >
          <LayoutGrid class="w-3 h-3 shrink-0" />
          <span class="max-w-[120px] truncate">{{ w.title }}</span>
          <button class="opacity-60 hover:opacity-100 ml-0.5 leading-none" @click.stop="removeMention(w.id)">✕</button>
        </span>
      </div>

      <div class="spotlight-bar">
        <Sparkles class="w-4 h-4 text-gray-500 flex-shrink-0" />
        <button
          v-if="canvasCards.length || conversationId || dashboardStore.widgets.length"
          :disabled="processing"
          title="Clear chat"
          class="w-8 h-8 rounded-xl hover:bg-white/5 disabled:opacity-30 flex items-center justify-center transition-colors flex-shrink-0 text-gray-500 hover:text-gray-300"
          @click="clearChat"
        >
          <Trash2 class="w-4 h-4" />
        </button>
        <textarea
          v-model="input"
          rows="1"
          class="flex-1 bg-transparent border-0 outline-none resize-none text-sm text-gray-100 placeholder-gray-600 leading-6"
          placeholder="Ask about your factory…"
          @keydown="handleKeydown"
        />
        <button
          :disabled="(!input.trim() && !mentionedWidgets.length) || processing"
          class="w-8 h-8 rounded-xl bg-primary-500 hover:bg-primary-600 disabled:opacity-30 disabled:cursor-not-allowed flex items-center justify-center transition-colors flex-shrink-0"
          @click="sendMessage"
        >
          <Loader2 v-if="processing" class="w-4 h-4 text-white animate-spin" />
          <Send v-else class="w-4 h-4 text-white" />
        </button>
      </div>
      <p class="text-[10px] text-gray-700 text-center mt-2">Enter to send · Shift+Enter for new line</p>
    </div>

    <!-- Widget Config Modal (live canvas) -->
    <WidgetConfigModal
      v-if="showConfigModal && editingWidget"
      :widget="editingWidget"
      :machines="machineStore.machines"
      @save="onSaveWidget"
      @close="showConfigModal = false; editingWidget = null"
    />
  </div>
</template>

<style scoped>
.fade-enter-active, .fade-leave-active { transition: opacity 0.2s; }
.fade-enter-from, .fade-leave-to { opacity: 0; }

.toast-enter-active { transition: all 0.25s ease-out; }
.toast-leave-active { transition: all 0.2s ease-in; }
.toast-enter-from { opacity: 0; transform: translateY(8px); }
.toast-leave-to   { opacity: 0; transform: translateY(4px); }
</style>
