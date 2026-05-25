import { AlertRepository } from './alert.repository';
import { AppError } from '../../middleware/error';
import { TelemetryData } from '../../types';

type AlertCondition = 'gt' | 'lt' | 'eq' | 'gte' | 'lte' | 'neq' | 'between' | 'outside';

function evaluateCondition(value: number, condition: AlertCondition, threshold: number, thresholdHi?: number | null): boolean {
  switch (condition) {
    case 'gt':  return value > threshold;
    case 'lt':  return value < threshold;
    case 'gte': return value >= threshold;
    case 'lte': return value <= threshold;
    case 'eq':  return value === threshold;
    case 'neq': return value !== threshold;
    case 'between': return thresholdHi != null && value >= threshold && value <= thresholdHi;
    case 'outside': return thresholdHi != null && (value < threshold || value > thresholdHi);
    default: return false;
  }
}

export class AlertService {
  private repo = new AlertRepository();
  private lastFiredAt = new Map<string, number>(); // alertId → timestamp

  async getAlerts(organizationId: string, machineId?: string) {
    return this.repo.findAll(organizationId, machineId);
  }

  async getAlertById(id: string, organizationId: string) {
    const alert = await this.repo.findById(id);
    if (!alert || alert.machine.productionLine.factory.organizationId !== organizationId) {
      throw new AppError(404, 'NOT_FOUND', 'Alert not found');
    }
    return alert;
  }

  async createAlert(organizationId: string, data: {
    machineId: string;
    name: string;
    description?: string;
    field: string;
    condition: string;
    threshold: number;
    thresholdHi?: number;
    severity: string;
    cooldownSec?: number;
    notifyEmail?: string[];
  }) {
    return this.repo.create({
      machine: { connect: { id: data.machineId } },
      name: data.name,
      description: data.description,
      field: data.field,
      condition: data.condition,
      threshold: data.threshold,
      thresholdHi: data.thresholdHi,
      severity: data.severity,
      cooldownSec: data.cooldownSec ?? 300,
      notifyEmail: data.notifyEmail ?? [],
    });
  }

  async updateAlert(id: string, organizationId: string, data: Record<string, unknown>) {
    await this.getAlertById(id, organizationId);
    return this.repo.update(id, data);
  }

  async deleteAlert(id: string, organizationId: string) {
    await this.getAlertById(id, organizationId);
    return this.repo.delete(id);
  }

  async getActiveEvents(organizationId: string) {
    return this.repo.getActiveAlerts(organizationId);
  }

  async acknowledgeEvent(eventId: string, userId: string) {
    return this.repo.acknowledgeEvent(eventId, userId);
  }

  async resolveEvent(eventId: string, userId: string) {
    return this.repo.resolveEvent(eventId, userId);
  }

  /** Called by simulator/ingestion to evaluate alert rules */
  async evaluateTelemetry(machineId: string, data: TelemetryData): Promise<Array<{
    alertId: string;
    alertName: string;
    field: string;
    value: number;
    threshold: number;
    condition: string;
    severity: string;
    message: string;
  }>> {
    const alerts = await this.repo.getAlertsForMachines([machineId]);
    const triggered = [];

    for (const alert of alerts) {
      const rawValue = data[alert.field];
      if (typeof rawValue !== 'number') continue;

      const conditionMet = evaluateCondition(
        rawValue,
        alert.condition as AlertCondition,
        alert.threshold,
        alert.thresholdHi,
      );

      if (!conditionMet) continue;

      // Cooldown check
      const lastFired = this.lastFiredAt.get(alert.id) ?? 0;
      if (Date.now() - lastFired < alert.cooldownSec * 1000) continue;

      this.lastFiredAt.set(alert.id, Date.now());

      const message = `${alert.name}: ${alert.field} = ${rawValue} (${alert.condition} ${alert.threshold})`;
      await this.repo.createEvent({ alertId: alert.id, value: rawValue, message });

      triggered.push({
        alertId: alert.id,
        alertName: alert.name,
        field: alert.field,
        value: rawValue,
        threshold: alert.threshold,
        condition: alert.condition,
        severity: alert.severity,
        message,
      });
    }

    return triggered;
  }
}
