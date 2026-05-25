/**
 * Realtime telemetry simulator.
 * Generates realistic sensor data for all seeded machines and
 * publishes it through the WebSocket gateway + persists to DB.
 */

import { WsGateway } from '../websocket/ws.gateway';
import { TelemetryRepository } from '../modules/telemetry/telemetry.repository';
import { AlertService } from '../modules/alerts/alert.service';
import { TelemetryData } from '../types';

interface SimulatedMachine {
  id: string;
  name: string;
  type: 'checkweigher' | 'temperature_sensor' | 'conveyor' | 'vision_camera';
  generate: () => TelemetryData;
}

function clamp(val: number, min: number, max: number) {
  return Math.max(min, Math.min(max, val));
}

function randomWalk(current: number, step: number, min: number, max: number): number {
  return clamp(current + (Math.random() - 0.5) * 2 * step, min, max);
}

function jitter(base: number, pct = 0.02): number {
  return base * (1 + (Math.random() - 0.5) * 2 * pct);
}

// Internal state for random walks
const state = {
  weight: 501.5,
  cwSpeed: 60.0,
  rejects: 0,
  throughput: 60.0,

  temp: 22.0,
  humidity: 55.0,

  convSpeed: 1000.0,
  motorLoad: 45.0,
  rpm: 750.0,
  vibration: 5.0,

  defectRate: 0.8,
  inspected: 0,
  passed: 0,
  failed: 0,
  confidence: 97.5,
};

function generateCheckweigher(): TelemetryData {
  state.weight = randomWalk(state.weight, 3, 470, 540);
  state.cwSpeed = randomWalk(state.cwSpeed, 2, 40, 80);
  state.throughput = jitter(state.cwSpeed, 0.05);

  // Occasional reject spike
  if (Math.random() < 0.05) {
    state.rejects = Math.floor(Math.random() * 3) + 1;
  } else {
    state.rejects = 0;
  }

  return {
    weight: parseFloat(state.weight.toFixed(2)),
    speed: parseFloat(state.cwSpeed.toFixed(1)),
    rejects: state.rejects,
    throughput: parseFloat(state.throughput.toFixed(1)),
    status_code: state.rejects > 0 ? 1 : 0,
  };
}

function generateTemperatureSensor(): TelemetryData {
  state.temp = randomWalk(state.temp, 0.3, 18, 40);
  state.humidity = randomWalk(state.humidity, 1, 40, 80);
  const dewPoint = state.temp - ((100 - state.humidity) / 5);
  return {
    temp: parseFloat(state.temp.toFixed(2)),
    humidity: parseFloat(state.humidity.toFixed(1)),
    dew_point: parseFloat(dewPoint.toFixed(2)),
  };
}

function generateConveyor(): TelemetryData {
  state.convSpeed = randomWalk(state.convSpeed, 30, 600, 1800);
  state.motorLoad = randomWalk(state.motorLoad, 2, 20, 90);
  state.rpm = state.convSpeed * 0.7 + (Math.random() - 0.5) * 20;
  state.vibration = randomWalk(state.vibration, 0.5, 1, 25);
  return {
    speed: parseFloat(state.convSpeed.toFixed(1)),
    load: parseFloat(state.motorLoad.toFixed(1)),
    rpm: parseFloat(state.rpm.toFixed(0)),
    vibration: parseFloat(state.vibration.toFixed(2)),
  };
}

function generateVisionCamera(): TelemetryData {
  state.defectRate = randomWalk(state.defectRate, 0.2, 0, 5);
  state.confidence = randomWalk(state.confidence, 0.5, 85, 99.9);
  const newInspected = Math.floor(Math.random() * 3) + 1;
  state.inspected += newInspected;
  const newFailed = Math.random() < state.defectRate / 100 ? 1 : 0;
  state.failed += newFailed;
  state.passed += newInspected - newFailed;
  return {
    defect_rate: parseFloat(state.defectRate.toFixed(3)),
    inspected: state.inspected,
    passed: state.passed,
    failed: state.failed,
    confidence: parseFloat(state.confidence.toFixed(1)),
  };
}

export class TelemetrySimulator {
  private gateway: WsGateway;
  private telemetryRepo: TelemetryRepository;
  private alertService: AlertService;
  private machines: SimulatedMachine[] = [];
  private timer: NodeJS.Timeout | null = null;
  private intervalMs: number;
  private persistEvery: number; // only write to DB every N ticks to reduce load
  private tickCount = 0;

  constructor(gateway: WsGateway, intervalMs = 1000, persistEvery = 1) {
    this.gateway = gateway;
    this.telemetryRepo = new TelemetryRepository();
    this.alertService = new AlertService();
    this.intervalMs = intervalMs;
    this.persistEvery = persistEvery;
  }

  configureMachines(machines: Array<{ id: string; name: string; type: string }>) {
    this.machines = machines.map(m => {
      let generate: () => TelemetryData;
      switch (m.type) {
        case 'checkweigher':       generate = generateCheckweigher; break;
        case 'temperature_sensor': generate = generateTemperatureSensor; break;
        case 'conveyor':           generate = generateConveyor; break;
        case 'vision_camera':      generate = generateVisionCamera; break;
        default:                   generate = () => ({ value: Math.random() * 100 });
      }
      return { id: m.id, name: m.name, type: m.type as SimulatedMachine['type'], generate };
    });
    console.log(`🤖 Simulator configured for ${this.machines.length} machines`);
  }

  start() {
    if (this.timer) return;
    this.timer = setInterval(async () => {
      this.tickCount++;
      const shouldPersist = this.tickCount % this.persistEvery === 0;
      await this.tick(shouldPersist);
    }, this.intervalMs);
    console.log(`🚀 Telemetry simulator started (${this.intervalMs}ms interval)`);
  }

  stop() {
    if (this.timer) {
      clearInterval(this.timer);
      this.timer = null;
    }
    console.log('⏹  Telemetry simulator stopped');
  }

  private async tick(persist: boolean): Promise<void> {
    const timestamp = new Date();
    const promises: Promise<void>[] = [];

    for (const machine of this.machines) {
      promises.push(this.processMachine(machine, timestamp, persist));
    }

    await Promise.allSettled(promises);
  }

  private async processMachine(machine: SimulatedMachine, timestamp: Date, persist: boolean): Promise<void> {
    try {
      const data = machine.generate();

      // Broadcast via WebSocket
      this.gateway.broadcastTelemetry({
        machineId: machine.id,
        machineName: machine.name,
        timestamp: timestamp.toISOString(),
        data,
      });

      // Persist to database
      if (persist) {
        await this.telemetryRepo.ingest(machine.id, data, timestamp);
      }

      // Evaluate alerts
      const triggered = await this.alertService.evaluateTelemetry(machine.id, data);
      for (const alert of triggered) {
        this.gateway.broadcastAlert({
          alertId: alert.alertId,
          alertName: alert.alertName,
          machineId: machine.id,
          machineName: machine.name,
          field: alert.field,
          value: alert.value,
          threshold: alert.threshold,
          condition: alert.condition,
          severity: alert.severity as 'info' | 'warning' | 'critical',
          message: alert.message,
          timestamp: timestamp.toISOString(),
        });
      }
    } catch (err) {
      console.error(`[Simulator] Error processing ${machine.name}:`, (err as Error).message);
    }
  }
}
