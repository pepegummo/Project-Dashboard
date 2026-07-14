<script setup lang="ts">
import { ref, computed, onMounted, reactive } from 'vue';
import { api } from '@/services/api.service';
import type { AskDataResult, AskBoardSummary, AskBoard, AskBoardChart } from '@/types';
import { Sparkles, Loader2, Save, Trash2, RefreshCw } from 'lucide-vue-next';

// ── Ask state ────────────────────────────────────────────────────────────────
const question = ref('');
const asking = ref(false);
const askError = ref('');
const result = ref<AskDataResult | null>(null);
// Prose/clarification follow-ups about the current chart — shown as a Q&A thread
// inside the result card instead of clearing the chart. Reset on a new data turn.
const notes = ref<{ q: string; text: string; kind: 'answer' | 'clarification' }[]>([]);
// Snapshot of the question the current result answered (textarea keeps changing).
const askedQuestion = ref('');
// Previous turn, so a follow-up ("make it a bar chart") refines it instead of being rejected.
// clarification set instead of sql when the previous turn asked back (B3) — the next
// message is the user's reply to that question.
const prev = ref<{ question: string; sql: string; clarification?: string } | null>(null);

// Merge the LLM's ECharts option (no data) with the result rows as an ECharts
// dataset. Re-running the SQL just swaps the source — the encoding stays put.
// Long-format results (a text category column beside the encoded x/y, e.g.
// bucket/machine_name/avg_speed) would zigzag as the LLM's single series — split
// into one filter-transform series per category value so 1 line = 1 machine.
function withDataset(option: Record<string, unknown>, columns: string[], rows: unknown[][]) {
  // rows can be null (backend serializes an empty result set as null) — coerce so the
  // array spread never throws "not iterable".
  const safeRows = Array.isArray(rows) ? rows : [];
  const safeCols = Array.isArray(columns) ? columns : [];
  // grid.top clears the chart title; the LLM's own grid (if any) wins on the rest.
  const merged = {
    ...(option ?? {}),
    grid: { containLabel: true, ...(option?.grid as object), top: 56 },
    dataset: { source: [safeCols, ...safeRows] },
  };

  const seriesRaw = option?.series;
  const seriesArr = Array.isArray(seriesRaw) ? seriesRaw : seriesRaw ? [seriesRaw] : [];
  if (seriesArr.length !== 1) return merged;
  const s = seriesArr[0] as Record<string, unknown>;
  if (!['line', 'bar', 'scatter'].includes(s.type as string)) return merged;
  const enc = (s.encode ?? {}) as Record<string, unknown>;
  if (typeof enc.x !== 'string' || typeof enc.y !== 'string') return merged;

  // Category column = first column outside encode.x/y whose values are strings.
  // (encode.seriesName may also point at it — that names one series, it doesn't split.)
  const catIdx = safeCols.findIndex(
    (c, i) => c !== enc.x && c !== enc.y && typeof safeRows.find((r) => r[i] != null)?.[i] === 'string',
  );
  if (catIdx < 0) return merged;
  const vals = [...new Set(safeRows.map((r) => r[catIdx]).filter((v): v is string => typeof v === 'string'))];
  // ponytail: 20-category ceiling — beyond that the single-series fallback stands.
  if (vals.length < 2 || vals.length > 20) return merged;

  // Per-machine legend is a vertical list at the top right; grid reserves room for it.
  return {
    ...merged,
    legend: { ...(option.legend as object), left: undefined, top: 8, right: 8, orient: 'vertical' },
    grid: { ...merged.grid, right: 220 },
    dataset: [
      { source: [safeCols, ...safeRows] },
      ...vals.map((v) => ({ transform: { type: 'filter', config: { dimension: safeCols[catIdx], value: v } } })),
    ],
    series: vals.map((v, i) => ({ ...s, encode: { ...enc, seriesName: undefined }, name: v, datasetIndex: i + 1 })),
  };
}

// An empty option ({}) is the backend's "render as a table" signal (text-only result).
function isTabular(option: Record<string, unknown> | null | undefined) {
  return !option || Object.keys(option).length === 0;
}

const resultOption = computed(() =>
  result.value ? withDataset(result.value.echartOption, result.value.columns, result.value.rows) : null,
);
const resultIsTable = computed(() => !!result.value && isTabular(result.value.echartOption));
const resultIsEmpty = computed(() => !!result.value && (result.value.rows?.length ?? 0) === 0);

async function ask() {
  const q = question.value.trim();
  if (!q || asking.value) return;
  asking.value = true;
  askError.value = '';
  try {
    const res = await api.askData(q, prev.value ?? undefined);
    // A data turn replaces the chart and resets the thread; prose/clarification
    // turns annotate the current chart instead of clearing it. Only a data turn
    // advances the SQL context.
    if (res.sql) {
      result.value = res;
      askedQuestion.value = q;
      notes.value = [];
      prev.value = { question: q, sql: res.sql };
    } else if (res.clarification) {
      notes.value.push({ q, text: res.clarification, kind: 'clarification' });
      prev.value = { question: q, sql: '', clarification: res.clarification };
    } else {
      notes.value.push({ q, text: res.answer ?? '', kind: 'answer' });
    }
    question.value = '';
  } catch (e) {
    askError.value = (e as Error).message;
  } finally {
    asking.value = false;
  }
}

// ── Boards ───────────────────────────────────────────────────────────────────
const boards = ref<AskBoardSummary[]>([]);
const activeBoard = ref<AskBoard | null>(null);
// Per-chart live data fetched by re-running its stored SQL.
const chartData = reactive<Record<string, { columns: string[]; rows: unknown[][] } | 'loading' | 'error'>>({});

const saveTarget = ref<string>(''); // board id, or '__new__'
const newBoardName = ref('');
const saving = ref(false);

async function loadBoards() {
  boards.value = await api.listBoards();
}

async function openBoard(id: string) {
  activeBoard.value = await api.getBoard(id);
  for (const ch of activeBoard.value.charts) void runChart(ch);
}

async function runChart(ch: AskBoardChart) {
  chartData[ch.id] = 'loading';
  try {
    chartData[ch.id] = await api.runSql(ch.sql);
  } catch {
    chartData[ch.id] = 'error';
  }
}

// Loaded {columns, rows} for a board chart, or null while loading/errored.
function loadedData(ch: AskBoardChart) {
  const d = chartData[ch.id];
  return !d || d === 'loading' || d === 'error' ? null : d;
}

function boardChartOption(ch: AskBoardChart) {
  const d = loadedData(ch);
  return d ? withDataset(ch.echartOption, d.columns, d.rows) : null;
}

async function saveToBoard() {
  if (!result.value || saving.value) return;
  saving.value = true;
  try {
    let boardId = saveTarget.value;
    if (boardId === '__new__' || !boardId) {
      const name = newBoardName.value.trim() || 'My Board';
      const created = await api.createBoard(name);
      boardId = created.id;
      await loadBoards();
    }
    await api.addBoardChart(boardId, {
      question: askedQuestion.value,
      sql: result.value.sql,
      echartOption: result.value.echartOption,
    });
    saveTarget.value = boardId;
    newBoardName.value = '';
    await openBoard(boardId);
    // Clear the compose result now that it lives on a board.
    result.value = null;
    notes.value = [];
    question.value = '';
    prev.value = null;
  } catch (e) {
    askError.value = (e as Error).message;
  } finally {
    saving.value = false;
  }
}

async function deleteChart(ch: AskBoardChart) {
  if (!activeBoard.value) return;
  await api.deleteBoardChart(activeBoard.value.id, ch.id);
  await openBoard(activeBoard.value.id);
}

async function removeBoard(id: string) {
  await api.deleteBoard(id);
  if (activeBoard.value?.id === id) activeBoard.value = null;
  await loadBoards();
}

onMounted(loadBoards);
</script>

<template>
  <div class="flex h-full min-h-screen">
    <!-- Boards rail -->
    <aside class="w-72 flex-shrink-0 border-r border-white/5 bg-surface-100 p-4 space-y-1.5">
      <div class="px-3 py-2 text-[11px] font-bold uppercase tracking-widest text-gray-500">Boards</div>
      <button
        v-for="b in boards" :key="b.id"
        class="group flex w-full items-center gap-2 rounded-lg px-3 py-2.5 text-left text-[15px] transition-colors"
        :class="activeBoard?.id === b.id ? 'bg-surface-200 text-white' : 'text-gray-400 hover:bg-surface-200/50'"
        @click="openBoard(b.id)"
      >
        <span class="flex-1 truncate">{{ b.name }}</span>
        <span class="text-xs text-gray-500">{{ b.chartCount }}</span>
        <Trash2 class="h-4 w-4 opacity-0 group-hover:opacity-100 hover:text-red-400" @click.stop="removeBoard(b.id)" />
      </button>
      <p v-if="boards.length === 0" class="px-3 py-2 text-sm text-gray-600">No boards yet. Ask a question and save the chart.</p>
    </aside>

    <!-- Main -->
    <div class="flex-1 overflow-y-auto p-8 lg:p-10">
      <!-- Ask bar -->
      <div class="mx-auto max-w-7xl">
        <div class="flex items-center gap-3 text-white">
          <Sparkles class="h-7 w-7 text-primary-400" />
          <h1 class="text-2xl font-bold lg:text-3xl">Ask your data</h1>
        </div>
        <p class="mt-2 text-base text-gray-500">Ask in plain language — a chart is generated to answer you.</p>

        <div class="mt-6 flex gap-3">
          <textarea
            v-model="question"
            rows="3"
            placeholder="e.g. average speed per machine over the last 24 hours, hourly"
            class="flex-1 resize-none rounded-xl border border-white/10 bg-surface-100 px-5 py-4 text-base text-gray-200 outline-none focus:border-primary-500"
            @keydown.enter.exact.prevent="ask"
          />
          <button
            class="flex items-center gap-2 rounded-xl bg-primary-500 px-7 py-4 text-base font-semibold text-white transition-colors hover:bg-primary-600 disabled:opacity-50"
            :disabled="asking || !question.trim()"
            @click="ask"
          >
            <Loader2 v-if="asking" class="h-5 w-5 animate-spin" />
            <Sparkles v-else class="h-5 w-5" />
            Ask
          </button>
        </div>
        <p class="mt-2 text-sm text-gray-600">Press Enter to ask · Shift+Enter for a new line</p>

        <p v-if="askError" class="mt-4 rounded-lg bg-red-500/10 px-4 py-3 text-base text-red-300">{{ askError }}</p>

        <!-- Follow-up answers — own card, above the chart so fresh answers stay visible -->
        <div v-if="notes.length" class="mt-10 rounded-2xl border border-white/10 bg-surface-100 p-6 lg:p-8">
          <div class="mb-4 flex items-center gap-2 text-[11px] font-bold uppercase tracking-widest text-gray-500">
            <Sparkles class="h-3.5 w-3.5 text-primary-400" /> Answers
          </div>
          <div class="space-y-5">
            <div v-for="(n, i) in notes" :key="i">
              <p class="text-sm font-semibold text-primary-300">{{ n.q }}</p>
              <p
                class="mt-1.5 whitespace-pre-wrap text-base leading-relaxed"
                :class="n.kind === 'clarification' ? 'italic text-amber-300' : 'text-gray-200'"
              >{{ n.text }}</p>
            </div>
          </div>
        </div>

        <!-- Chart / table result — its own card -->
        <div v-if="result" class="mt-10 rounded-2xl border border-white/10 bg-surface-100 p-6 lg:p-8">
          <h2 class="mb-5 text-lg font-semibold text-white">{{ askedQuestion }}</h2>

          <template v-if="result">
          <div v-if="resultIsEmpty" class="rounded-lg border border-white/5 bg-surface-200/50 px-5 py-8 text-center text-base text-gray-500">No data matched — try a wider time range or check the machine name.</div>
          <div v-else-if="resultIsTable" class="max-h-[40rem] overflow-auto rounded-lg border border-white/5">
            <table class="w-full text-left text-sm text-gray-300">
              <thead class="sticky top-0 bg-surface-200 text-gray-400">
                <tr><th v-for="col in result!.columns" :key="col" class="px-4 py-2 font-semibold">{{ col }}</th></tr>
              </thead>
              <tbody>
                <tr v-for="(row, i) in result!.rows" :key="i" class="border-t border-white/5">
                  <td v-for="(cell, j) in row" :key="j" class="px-4 py-2">{{ cell }}</td>
                </tr>
              </tbody>
            </table>
          </div>
          <div v-else-if="resultOption" class="h-[40rem] w-full">
            <v-chart :option="resultOption" theme="cpf-dark" autoresize />
          </div>

          <!-- Save to board -->
          <div v-if="!resultIsEmpty" class="mt-6 flex flex-wrap items-center gap-3 border-t border-white/5 pt-6">
            <select v-model="saveTarget" class="rounded-lg border border-white/10 bg-surface-200 px-4 py-2.5 text-base text-gray-300 outline-none">
              <option value="__new__">＋ New board…</option>
              <option v-for="b in boards" :key="b.id" :value="b.id">{{ b.name }}</option>
            </select>
            <input
              v-if="saveTarget === '__new__' || !saveTarget"
              v-model="newBoardName"
              placeholder="Board name"
              class="rounded-lg border border-white/10 bg-surface-200 px-4 py-2.5 text-base text-gray-200 outline-none focus:border-primary-500"
            />
            <button
              class="flex items-center gap-2 rounded-lg bg-surface-200 px-5 py-2.5 text-base font-medium text-white hover:bg-surface-300 disabled:opacity-50"
              :disabled="saving"
              @click="saveToBoard"
            >
              <Save class="h-5 w-5" /> Save to board
            </button>
          </div>
          </template>
        </div>
      </div>

      <!-- Active board -->
      <div v-if="activeBoard" class="mx-auto mt-14 max-w-7xl">
        <h2 class="mb-6 text-xl font-bold text-white">{{ activeBoard.name }}</h2>
        <div v-if="activeBoard.charts.length === 0" class="text-base text-gray-500">This board is empty.</div>
        <div class="grid grid-cols-1 gap-8">
          <div v-for="ch in activeBoard.charts" :key="ch.id" class="rounded-2xl border border-white/10 bg-surface-100 p-6">
            <div class="mb-4 flex items-start gap-3">
              <p class="flex-1 text-base font-medium text-gray-200">{{ ch.question }}</p>
              <button class="text-gray-500 hover:text-gray-200" title="Re-run" @click="runChart(ch)"><RefreshCw class="h-5 w-5" /></button>
              <button class="text-gray-500 hover:text-red-400" title="Delete" @click="deleteChart(ch)"><Trash2 class="h-5 w-5" /></button>
            </div>
            <div v-if="loadedData(ch) && isTabular(ch.echartOption)" class="max-h-[34rem] overflow-auto rounded-lg border border-white/5">
              <table class="w-full text-left text-sm text-gray-300">
                <thead class="sticky top-0 bg-surface-200 text-gray-400">
                  <tr><th v-for="col in loadedData(ch)!.columns" :key="col" class="px-4 py-2 font-semibold">{{ col }}</th></tr>
                </thead>
                <tbody>
                  <tr v-for="(row, i) in loadedData(ch)!.rows" :key="i" class="border-t border-white/5">
                    <td v-for="(cell, j) in row" :key="j" class="px-4 py-2">{{ cell }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
            <div v-else-if="boardChartOption(ch)" class="h-[34rem] w-full">
              <v-chart :option="boardChartOption(ch)" theme="cpf-dark" autoresize />
            </div>
            <div v-else-if="chartData[ch.id] === 'loading'" class="flex h-[34rem] items-center justify-center text-gray-600">
              <Loader2 class="h-6 w-6 animate-spin" />
            </div>
            <div v-else class="flex h-[34rem] items-center justify-center text-base text-red-400">Failed to load data.</div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
