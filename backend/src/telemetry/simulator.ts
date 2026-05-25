/**
 * Realtime telemetry simulator
 * Shape: sine wave — 1 complete cycle = 10 ticks, amplitude = threshold ± 10%
 */

import { WsGateway } from '../websocket/ws.gateway';
import { TelemetryRepository } from '../modules/telemetry/telemetry.repository';
import { AlertService } from '../modules/alerts/alert.service';
import { TelemetryData } from '../types';

const POINTS_PER_CYCLE = 10; // 1 full sine wave = 10 data points

// ─── Sine helper ──────────────────────────────────────────────────────────────
function sine(threshold: number, tick: number, phaseOffset: number, precision: number): number {
  const amplitude = threshold * 0.1;
  const noise     = (Math.random() - 0.5) * 0.01 * amplitude;
  const value     = threshold
    + amplitude * Math.sin((2 * Math.PI * (tick + phaseOffset)) / POINTS_PER_CYCLE)
    + noise;
  return +Math.max(threshold * 0.9, Math.min(threshold * 1.1, value)).toFixed(precision);
}

// ─── Cumulative counters for vision camera ────────────────────────────────────
const vcState = { inspected: 0, passed: 0, failed: 0 };

// ─── Generators ──────────────────────────────────────────────────────────────
function generateCheckweigher(tick: number): TelemetryData {
  const rejects = Math.max(0, Math.round(sine(1.5, tick, 5, 0)));
  return {
    weight:      sine(500,  tick, 0, 2),
    speed:       sine(60,   tick, 2, 1),
    throughput:  sine(60,   tick, 2, 1),
    rejects,
    status_code: rejects > 0 ? 1 : 0,
  };
}

function generateTemperatureSensor(tick: number): TelemetryData {
  return {
    temp:      sine(22, tick, 0, 2),
    humidity:  sine(55, tick, 3, 1),
    dew_point: sine(11, tick, 1, 2),
  };
}

function generateConveyor(tick: number): TelemetryData {
  return {
    speed:     sine(1000, tick, 0, 1),
    load:      sine(45,   tick, 3, 1),
    rpm:       sine(750,  tick, 3, 0),
    vibration: sine(5,    tick, 5, 2),
  };
}

function generateVisionCamera(tick: number): TelemetryData {
  const defect_rate = sine(1,  tick, 0, 3);
  const confidence  = sine(97, tick, 4, 1);
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

  constructor(gateway: WsGateway, intervalMs = 1000, persistEvery = 1) {
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
    console.log(`🤖 Simulator configured for ${this.machines.length} machines (sine wave, ${POINTS_PER_CYCLE} pts/cycle)`);
  }

  start() {
    if (this.timer) return;
    this.timer = setInterval(async () => {
      this.tickCount++;
      await this.tick(this.tickCount % this.persistEvery === 0);
    }, this.intervalMs);
    console.log(`🌊 Simulator started — ${this.intervalMs}ms/tick, 1 cycle = ${POINTS_PER_CYCLE} ticks`);
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

      this.gateway.broadcastTelemetry({ machineId: machine.id, machineName: machine.name, timestamp: timestamp.toISOString(), data });

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
