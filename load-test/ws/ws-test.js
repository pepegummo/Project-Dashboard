import ws   from 'k6/ws';
import http from 'k6/http';
import { check }                    from 'k6';
import { Counter, Trend }           from 'k6/metrics';

// ── Custom metrics ─────────────────────────────────────────────────────────────
const pingRtt         = new Trend('ping_rtt_ms',          true); // ping → pong RTT
const timeToFirstMsg  = new Trend('time_to_first_msg_ms', true); // subscribe → first telemetry
const broadcastLatency = new Trend('broadcast_latency_ms', true); // server send → client recv
const telemetryMsgs   = new Counter('telemetry_msgs');
const integrityFails  = new Counter('integrity_failures'); // bad payloads

// ── Config ─────────────────────────────────────────────────────────────────────
const BASE   = 'http://localhost:4000';
const WS_URL = 'ws://localhost:4001';

const MACHINE_IDS = [
  '00000000-0000-0000-0000-000000000005', // CW-01 Checkweigher
  '00000000-0000-0000-0000-000000000006', // TS-01 Temp Sensor
  '00000000-0000-0000-0000-000000000007', // CB-01 Conveyor
  '00000000-0000-0000-0000-000000000008', // VC-01 Vision Camera
];

// ── Scenarios ──────────────────────────────────────────────────────────────────
export const options = {
  scenarios: {
    // Phase 1 — Smoke: verify connect + subscribe + receive at least 1 tick (60s cycle)
    smoke: {
      executor:  'constant-vus',
      vus:       2,
      duration:  '70s',
      startTime: '0s',
    },
    // Phase 2 — Ping RTT baseline: measure round-trip under light load
    ping_baseline: {
      executor:  'constant-vus',
      vus:       10,
      duration:  '60s',
      startTime: '75s',
    },
    // Phase 3 — Load: RTT + integrity under 50 sustained connections
    load: {
      executor:  'constant-vus',
      vus:       50,
      duration:  '3m',
      startTime: '2m20s',
    },
    // Phase 4 — Spike: RTT degradation under burst of 150 connections
    spike: {
      executor:  'constant-vus',
      vus:       150,
      duration:  '30s',
      startTime: '5m30s',
    },
  },

  thresholds: {
    ws_connecting:         ['p(95)<500'],    // handshake time
    ping_rtt_ms:           ['p(95)<200'],    // true request-response RTT
    time_to_first_msg_ms:  ['p(95)<70000'],  // 70s: worst case = connect just after 60s tick
    broadcast_latency_ms:  ['p(95)<3000', 'min>0'], // server push → client; min>0 catches Docker clock skew
    integrity_failures:    ['count==0'],     // zero tolerance for bad payloads
  },
};

// ── Setup: verify backend is up ────────────────────────────────────────────────
export function setup() {
  const res = http.get(`${BASE}/health`);
  check(res, { 'backend healthy': (r) => r.status === 200 });
}

// ── VU lifecycle ───────────────────────────────────────────────────────────────
export default function () {
  // Per-VU mutable state (closure — safe, each VU is a separate JS context)
  let connected        = false; // fix: ws.connect() returns null in k6 v2, track via open event
  let pingSentAt       = 0;
  let subscribedAt     = 0;
  let firstMsgReceived = false;

  const res = ws.connect(WS_URL, {}, (socket) => {

    // Keep connection alive for the full test duration (fix: setInterval alone is not enough)
    socket.setTimeout(() => { socket.close(); }, 7 * 60 * 1_000);

    // ── 1. On connect: subscribe to all 4 machines ─────────────────────────────
    socket.on('open', () => {
      connected    = true;
      subscribedAt = Date.now();
      socket.send(JSON.stringify({
        type:    'subscribe',
        payload: { machineIds: MACHINE_IDS },
      }));
    });

    // ── 2. Handle incoming messages ────────────────────────────────────────────
    socket.on('message', (raw) => {
      let msg;
      try { msg = JSON.parse(raw); } catch { return; }

      // ── Pong: complete the ping-pong RTT measurement
      if (msg.type === 'pong') {
        if (pingSentAt > 0) {
          pingRtt.add(Date.now() - pingSentAt);
          pingSentAt = 0;
        }
        return;
      }

      // ── Telemetry: timing + real-time data integrity
      if (msg.type === 'telemetry') {
        telemetryMsgs.add(1);

        // Time from subscribe → first message received (measures feed latency)
        if (!firstMsgReceived) {
          firstMsgReceived = true;
          timeToFirstMsg.add(Date.now() - subscribedAt);
        }

        // Broadcast latency: msg.timestamp is set by server at send time (nowMs())
        if (msg.timestamp) {
          broadcastLatency.add(Date.now() - msg.timestamp);
        }

        // ── Real-time data integrity checks ─────────────────────────────────
        const p = msg.payload;

        const hasMachineId       = typeof p?.machineId === 'string' && p.machineId.length > 0;
        const hasData            = p?.data !== null && typeof p?.data === 'object';
        const isSubscribedMachine = MACHINE_IDS.includes(p?.machineId);
        const dataValuesNumeric  = hasData && Object.values(p.data).every(v => typeof v === 'number');

        // Freshness: payload.timestamp is DB insert time from simulator.
        // Simulator ticks every 60s → expect < 2 min old.
        const payloadTs = p?.timestamp ? new Date(p.timestamp).getTime() : 0;
        const isFresh   = payloadTs > 0 && (Date.now() - payloadTs) < 120_000;

        const allPassed = check(msg, {
          'telemetry: has machineId':           () => hasMachineId,
          'telemetry: has data object':          () => hasData,
          'telemetry: machineId is subscribed':  () => isSubscribedMachine,
          'telemetry: data values are numbers':  () => dataValuesNumeric,
          'telemetry: data is fresh (<2 min)':   () => isFresh,
        });

        if (!allPassed) integrityFails.add(1);
      }

      // ── Alert: basic shape check
      if (msg.type === 'alert') {
        check(msg, {
          'alert: has severity':  (m) => !!m.payload?.severity,
          'alert: has machineId': (m) => !!m.payload?.machineId,
        });
      }

      // ── Machine status: basic shape check
      if (msg.type === 'machine_status') {
        check(msg, {
          'status: has machineId': (m) => !!m.payload?.machineId,
          'status: valid value':   (m) => ['online', 'offline', 'maintenance']
                                            .includes(m.payload?.status),
        });
      }
    });

    socket.on('error', (e) => console.error(`[WS] error: ${e.error()}`));

    // ── 3. Ping every 10s — resets server read deadline + measures RTT ─────────
    socket.setInterval(() => {
      pingSentAt = Date.now();
      socket.send(JSON.stringify({ type: 'ping' }));
    }, 10_000);
  });

  check(res, { 'WS handshake 101': () => connected }); // fix: use open-event flag, not res.status
}
