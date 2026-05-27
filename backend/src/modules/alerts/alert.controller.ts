import { Request, Response, NextFunction } from 'express';
import { z } from 'zod';
import { AlertService } from './alert.service';
import { AuthenticatedRequest } from '../../types';

const createAlertSchema = z.object({
  machineId: z.string().uuid(),
  name: z.string().min(1).max(100),
  description: z.string().optional(),
  field: z.string().min(1),
  condition: z.enum(['gt', 'lt', 'eq', 'gte', 'lte', 'neq', 'between', 'outside']),
  threshold: z.number(),
  thresholdHi: z.number().optional(),
  severity: z.enum(['info', 'warning', 'critical']).default('warning'),
  cooldownSec: z.number().int().min(0).default(300),
  notifyEmail: z.array(z.string().email()).optional(),
});

export class AlertController {
  private svc = new AlertService();

  list = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const { orgId } = (req as AuthenticatedRequest).user;
      const machineId = req.query.machineId as string | undefined;
      const alerts = await this.svc.getAlerts(orgId, machineId);
      res.json({ success: true, data: alerts });
    } catch (err) { next(err); }
  };

  getById = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const { orgId } = (req as AuthenticatedRequest).user;
      const alert = await this.svc.getAlertById(req.params.id, orgId);
      res.json({ success: true, data: alert });
    } catch (err) { next(err); }
  };

  create = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const { orgId } = (req as AuthenticatedRequest).user;
      const body = createAlertSchema.parse(req.body);
      const alert = await this.svc.createAlert(orgId, body);
      res.status(201).json({ success: true, data: alert });
    } catch (err) { next(err); }
  };

  update = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const { orgId } = (req as AuthenticatedRequest).user;
      const alert = await this.svc.updateAlert(req.params.id, orgId, req.body);
      res.json({ success: true, data: alert });
    } catch (err) { next(err); }
  };

  delete = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const { orgId } = (req as AuthenticatedRequest).user;
      await this.svc.deleteAlert(req.params.id, orgId);
      res.json({ success: true, data: { deleted: true } });
    } catch (err) { next(err); }
  };

  getActiveEvents = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const orgId = (req as AuthenticatedRequest).user?.orgId ?? null;
      const events = await this.svc.getActiveEvents(orgId);
      res.json({ success: true, data: events });
    } catch (err) { next(err); }
  };

  acknowledgeEvent = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const { sub } = (req as AuthenticatedRequest).user;
      const event = await this.svc.acknowledgeEvent(req.params.eventId, sub);
      res.json({ success: true, data: event });
    } catch (err) { next(err); }
  };

  resolveEvent = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const { sub } = (req as AuthenticatedRequest).user;
      const event = await this.svc.resolveEvent(req.params.eventId, sub);
      res.json({ success: true, data: event });
    } catch (err) { next(err); }
  };
}
