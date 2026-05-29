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
    // Phase 1: Smoke — 2 VUs, verify no errors
    smoke: {
      executor:  'constant-vus',
      vus:       2,
      duration:  '30s',
      startTime: '0s',
    },
    // Phase 2: Ramp — find max subscribers before broadcast degrades
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
    // Phase 3: Sustained — SLA baseline for 100 concurrent subscribers
    load: {
      executor:  'constant-vus',
      vus:       100,
      duration:  '3m',
      startTime: '2m10s',
    },
    // Phase 4: Spike — burst to 300 VUs, test ws_gateway fan-out under pressure
    spike: {
      executor:  'constant-vus',
      vus:       300,
      duration:  '30s',
      startTime: '5m30s',
    },
    // Phase 5: Cool down — confirm goroutine cleanup, no leak
    cool: {
      executor: 'ramping-vus',
      startVUs: 100,
      stages: [{ target: 0, duration: '30s' }],
      startTime: '6m5s',
    },
  },

  thresholds: {
    ws_connecting:        ['p(95)<500'],    // connect + JWT verify < 500ms
    telemetry_msgs:       ['count>0'],      // must receive at least one broadcast
    broadcast_latency_ms: ['p(95)<3000'],  // server → client under 3s
  },
};

// ── Setup: login once, share token across all VUs ─────────────────────────────
export function setup() {
  const res = http.post(
    `${BASE}/api/auth/login`,
    JSON.stringify({ email: 'admin@acme-foods.com', password: 'Admin@1234' }),
    { headers: { 'Content-Type': 'application/json' } },
  );
  check(res, { 'login 200': (r) => r.status === 200 });

  const token = res.json('data.token');
  if (!token) throw new Error('login failed — no token in response');
  console.log(`Token acquired: ${token.slice(0, 20)}…`);
  return { token };
}

// ── VU lifecycle ──────────────────────────────────────────────────────────────
export default function ({ token }) {
  const url = `${WS_URL}?token=${encodeURIComponent(token)}`;

  const res = ws.connect(url, {}, (socket) => {

    // 1. Subscribe to all 4 machines on connect
    socket.on('open', () => {
      socket.send(JSON.stringify({
        type:      'subscribe',
        payload:   { machineIds: MACHINE_IDS },
        timestamp: Date.now(),
      }));
    });

    // 2. Handle incoming messages
    socket.on('message', (raw) => {
      let msg;
      try { msg = JSON.parse(raw); } catch { return; }

      if (msg.type === 'telemetry') {
        telemetryMsgs.add(1);

        // measure broadcast latency using server-side timestamp in payload
        if (msg.payload?.timestamp) {
          const serverTs = new Date(msg.payload.timestamp).getTime();
          broadcastLatency.add(Date.now() - serverTs);
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

    // 3. Keep-alive ping every 10s
    socket.setInterval(() => {
      socket.send(JSON.stringify({
        type:      'ping',
        payload:   {},
        timestamp: Date.now(),
      }));
    }, 10000);

    // 4. Partial unsubscribe at 15s — simulate user removing 2 widgets
    socket.setTimeout(() => {
      socket.send(JSON.stringify({
        type:      'unsubscribe',
        payload:   { machineIds: [MACHINE_IDS[2], MACHINE_IDS[3]] }, // CB-01 + VC-01
        timestamp: Date.now(),
      }));
    }, 15000);

    // 5. Close connection after 30s
    socket.setTimeout(() => socket.close(), 30000);
  });

  check(res, { 'WS handshake 101': (r) => r && r.status === 101 });
}
