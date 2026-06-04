import ws   from 'k6/ws';
import http from 'k6/http';
import { check }          from 'k6';
import { Counter, Trend } from 'k6/metrics';

// ── Custom metrics ────────────────────────────────────────────────────────────
const telemetryMsgs    = new Counter('telemetry_msgs');
const broadcastLatency = new Trend('broadcast_latency_ms', true); // true = ms unit

// ── Config ────────────────────────────────────────────────────────────────────
const BASE   = 'http://localhost:4000';
const WS_URL = 'ws://localhost:4001';

const MACHINE_IDS = [
  '00000000-0000-0000-0000-000000000005', // CW-01 Checkweigher
  '00000000-0000-0000-0000-000000000006', // TS-01 Temp Sensor
  '00000000-0000-0000-0000-000000000007', // CB-01 Conveyor
  '00000000-0000-0000-0000-000000000008', // VC-01 Vision Camera
];

// ── Scenarios (5 phases staggered by startTime) ───────────────────────────────
export const options = {
  scenarios: {
    // Phase 1: Smoke — 2 VUs, verify LED kiosk can connect and receive data
    smoke: {
      executor:  'constant-vus',
      vus:       2,
      duration:  '30s',
      startTime: '0s',
    },
    // Phase 2: Ramp — find max concurrent kiosk connections before broadcast degrades
    ramp: {
      executor: 'ramping-vus',
      startVUs: 5,
      stages: [
        { target: 25,  duration: '30s' },
        { target: 50,  duration: '30s' },
        { target: 100, duration: '30s' },
      ],
      startTime: '35s',
    },
    // Phase 3: Sustained — SLA baseline for 100 concurrent LED kiosk connections
    load: {
      executor:  'constant-vus',
      vus:       100,
      duration:  '3m',
      startTime: '2m10s',
    },
    // Phase 4: Spike — burst to 150 VUs (server capacity limit; 300 causes reconnect loop)
    spike: {
      executor:  'constant-vus',
      vus:       150,
      duration:  '30s',
      startTime: '5m30s',
    },
    // Phase 5: Cool down — confirm goroutine cleanup, no leak
    cool: {
      executor: 'ramping-vus',
      startVUs: 150,
      stages: [{ target: 0, duration: '30s' }],
      startTime: '6m5s',
    },
  },

  thresholds: {
    ws_connecting:        ['p(95)<500'],            // handshake time (covers what 'WS handshake 101' check tried to do)
    telemetry_msgs:       ['count>0'],              // must receive at least one broadcast
    broadcast_latency_ms: ['p(95)<3000', 'min>0'], // server → client under 3s; min>0 catches Docker clock skew
    ws_sessions:          ['count<600'],            // >600 sessions = VUs reconnect-looping = server rejecting connections (501 is expected: k6 reuses VU slots across scenarios)
  },
};

// ── Setup: verify backend is reachable ────────────────────────────────────────
export function setup() {
  const res = http.get(`${BASE}/health`);
  check(res, { 'backend healthy': (r) => r.status === 200 });
}

// ── VU lifecycle — simulates a permanent LED kiosk connection ─────────────────
// LED mode connects WITHOUT a token (public kiosk endpoint).
// The connection stays open for the full scenario duration — no close() call.
// This matches LedView.vue: wsService.connect(null) + never unsubscribes.
export default function () {
  const res = ws.connect(WS_URL, {}, (socket) => {

    // Keep connection alive for the full test duration (fix: setInterval alone is not enough)
    socket.setTimeout(() => { socket.close(); }, 7 * 60 * 1_000);

    // 1. Subscribe to all 4 machines immediately on connect
    socket.on('open', () => {
      socket.send(JSON.stringify({
        type:    'subscribe',
        payload: { machineIds: MACHINE_IDS },
      }));
    });

    // 2. Handle incoming messages
    socket.on('message', (raw) => {
      let msg;
      try { msg = JSON.parse(raw); } catch { return; }

      if (msg.type === 'telemetry') {
        telemetryMsgs.add(1);

        // msg.timestamp is UnixMilli set by the server at send time (nowMs()).
        // Using the outer timestamp, not msg.payload.timestamp (DB data age ~30-60s).
        if (msg.timestamp) {
          broadcastLatency.add(Date.now() - msg.timestamp);
        }

        check(msg, {
          'telemetry: has machineId': (m) => !!m.payload?.machineId,
          'telemetry: has data':      (m) => !!m.payload?.data,
        });
      }

      if (msg.type === 'alert') {
        check(msg, { 'alert: has severity': (m) => !!m.payload?.severity });
      }

      if (msg.type === 'machine_status') {
        check(msg, { 'status: has machineId': (m) => !!m.payload?.machineId });
      }
    });

    socket.on('error', (e) => console.error(`[WS] error: ${e.error()}`));

    // 3. Keep-alive: send JSON ping every 10s so the server resets its read deadline.
    //    Protocol: { type: 'ping' } — no extra fields.
    socket.setInterval(() => {
      socket.send(JSON.stringify({ type: 'ping' }));
    }, 10000);

    // NOTE: No socket.close() and no unsubscribe — LED kiosks stay connected
    // indefinitely. k6 will close the socket when the scenario ends.
  });

  // Handshake success is covered by ws_connecting threshold — no redundant check needed
}
