/**
 * Realtime telemetry simulator
 * Shape : pulse wave — 1 cycle = CYCLE_TICKS ticks = 2 hours (at 1-min interval)
 * Noise : Gaussian σ=5% + random spikes P=3% + slow sinusoidal drift ±30%
 */

import { WsGateway } from '../websocket/ws.gateway';
import { TelemetryRepository } from '../modules/telemetry/telemetry.repository';
import { AlertService } from '../modules/alerts/alert.service';
import { TelemetryData } from '../types';

const CYCLE_TICKS  = 120; // 1 full pulse = 120 ticks = 2 hours at 1-min/tick
const TRANS_TICKS  = 5;   // smooth edge over ±5 ticks

// ─── Pulse wave + layered noise ───────────────────────────────────────────────
function pulse(
  threshold:   number,
  tick:        number,
  phaseOffset: number,
  precision:   number,
  dutyCycle  = 0.45,
): number {
  const amplitude = threshold * 0.1;
  const period    = CYCLE_TICKS;
  const phase     = ((tick + phaseOffset) % period + period) % period;
  const highEnd   = dutyCycle * period;

  // ── Pulse shape ──────────────────────────────────────────────────────────
  let base: number;
  if (phase < highEnd - TRANS_TICKS) {
    base = amplitude;
  } else if (phase < highEnd + TRANS_TICKS) {
    const t = (phase - (highEnd - TRANS_TICKS)) / (2 * TRANS_TICKS);
    base = amplitude * (1 - t) + (-amplitude) * t;                       // falling edge
  } else if (phase < period - TRANS_TICKS) {
    base = -amplitude;
  } else {
    const t = (phase - (period - TRANS_TICKS)) / TRANS_TICKS;
    base = -amplitude * (1 - t) + amplitude * t;                         // rising edge
  }

  // ── Noise layer 1: Gaussian (σ = 5% of amplitude) ────────────────────────
  const u1    = Math.random() + 1e-10;
  const u2    = Math.random();
  const gauss = Math.sqrt(-2 * Math.log(u1)) * Math.cos(2 * Math.PI * u2);
  const gaussNoise = gauss * 0.05 * amplitude;

  // ── Noise layer 2: Random spike (P=3%, magnitude ±35% of amplitude) ──────
  const spikeNoise = Math.random() < 0.03
    ? (Math.random() - 0.5) * 0.70 * amplitude
    : 0;

  // ── Noise layer 3: Slow sinusoidal drift (period = 8 cycles = 16 hours) ──
  const drift = (amplitude * 0.30) * Math.sin((2 * Math.PI * tick) / (8 * CYCLE_TICKS));

  const value = threshold + base + gaussNoise + spikeNoise + drift;
  return +Math.max(threshold * 0.82, Math.min(threshold * 1.18, value)).toFixed(precision);
}

// ─── Cumulative counters for vision camera ────────────────────────────────────
const vcState = { inspected: 0, passed: 0, failed: 0 };

// ─── Generators ──────────────────────────────────────────────────────────────
function generateCheckweigher(tick: number): TelemetryData {
  const rejects = Math.max(0, Math.round(pulse(1.5, tick, 10, 0, 0.30)));
  return {
    weight:      pulse(500,  tick,  0, 2, 0.45),
    speed:       pulse(60,   tick,  5, 1, 0.50),
    throughput:  pulse(60,   tick,  5, 1, 0.50),
    rejects,
    status_code: rejects > 0 ? 1 : 0,
  };
}

function generateTemperatureSensor(tick: number): TelemetryData {
  return {
    temp:      pulse(22,  tick,  0, 2, 0.55),
    humidity:  pulse(55,  tick, 20, 1, 0.48),
    dew_point: pulse(11,  tick, 10, 2, 0.52),
  };
}

function generateConveyor(tick: number): TelemetryData {
  return {
    speed:     pulse(1000, tick,  0, 1, 0.45),
    load:      pulse(45,   tick, 30, 1, 0.50),
    rpm:       pulse(750,  tick, 15, 0, 0.45),
    vibration: pulse(5,    tick,  8, 2, 0.40),
  };
}

function generateVisionCamera(tick: number): TelemetryData {
  const defect_rate = pulse(1,  tick,  0, 3, 0.35);
  const confidence  = pulse(97, tick, 25, 1, 0.60);
  const newInspected = Math.floor(Math.random() * 3) + 1;
  vcState.inspected += newInspected;
  const newFailed    = Math.random() < defect_rate / 100 ? 1 : 0;
  vcState.failed    += newFailed;
  vcState.passed    += newInspected - newFailed;
  return {
    defect_rate,
    confidence,
    inspected: vcState.inspected,
    passed:    vcState.passed,
    failed:    vcState.failed,
  };
}

// ─── Simulator ────────────────────────────────────────────────────────────────
interface SimulatedMachine {
  id: string;
  name: string;
  type: 'checkweigher' | 'temperature_sensor' | 'conveyor' | 'vision_camera';
  generate: (tick: number) => TelemetryData;
}

export class TelemetrySimulator {
  private gateway:       WsGateway;
  private telemetryRepo: TelemetryRepository;
  private alertService:  AlertService;
  private machines:      SimulatedMachine[] = [];
  private timer:         NodeJS.Timeout | null = null;
  private intervalMs:    number;
  private persistEvery:  number;
  private tickCount = 0;

  constructor(gateway: WsGateway, intervalMs = 60_000, persistEvery = 1) {
    this.gateway       = gateway;
    this.telemetryRepo = new TelemetryRepository();
    this.alertService  = new AlertService();
    this.intervalMs    = intervalMs;
    this.persistEvery  = persistEvery;
  }

  configureMachines(machines: Array<{ id: string; name: string; type: string }>) {
    this.machines = machines.map(m => {
      let generate: (tick: number) => TelemetryData;
      switch (m.type) {
        case 'checkweigher':       generate = generateCheckweigher;      break;
        case 'temperature_sensor': generate = generateTemperatureSensor; break;
        case 'conveyor':           generate = generateConveyor;          break;
        case 'vision_camera':      generate = generateVisionCamera;      break;
        default:                   generate = () => ({ value: 0 });
      }
      return { id: m.id, name: m.name, type: m.type as SimulatedMachine['type'], generate };
    });
    console.log(`🤖 Simulator configured for ${this.machines.length} machines (pulse wave, 2-hr cycle, layered noise)`);
  }

  start() {
    if (this.timer) return;
    this.timer = setInterval(async () => {
      this.tickCount++;
      await this.tick(this.tickCount % this.persistEvery === 0);
    }, this.intervalMs);
    console.log(`⚡ Simulator started — ${this.intervalMs / 1000}s/tick, 1 cycle = ${CYCLE_TICKS} ticks (2 hrs)`);
  }

  stop() {
    if (this.timer) { clearInterval(this.timer); this.timer = null; }
    console.log('⏹  Simulator stopped');
  }

  private async tick(persist: boolean): Promise<void> {
    const timestamp = new Date();
    await Promise.allSettled(this.machines.map(m => this.processMachine(m, timestamp, persist)));
  }

  private async processMachine(machine: SimulatedMachine, timestamp: Date, persist: boolean): Promise<void> {
    try {
      const data = machine.generate(this.tickCount);
      this.gateway.broadcastTelemetry({
        machineId: machine.id, machineName: machine.name,
        timestamp: timestamp.toISOString(), data,
      });
      if (persist) await this.telemetryRepo.ingest(machine.id, data, timestamp);
      const triggered = await this.alertService.evaluateTelemetry(machine.id, data);
      for (const alert of triggered) {
        this.gateway.broadcastAlert({
          alertId: alert.alertId, alertName: alert.alertName,
          machineId: machine.id, machineName: machine.name,
          field: alert.field, value: alert.value, threshold: alert.threshold,
          condition: alert.condition, severity: alert.severity as 'info' | 'warning' | 'critical',
          message: alert.message, timestamp: timestamp.toISOString(),
        });
      }
    } catch (err) {
      console.error(`[Simulator] ${machine.name}:`, (err as Error).message);
    }
  }
}
