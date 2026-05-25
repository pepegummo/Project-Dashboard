import { Request, Response, NextFunction } from 'express';
import { z } from 'zod';
import { TelemetryService } from './telemetry.service';
import { AuthenticatedRequest } from '../../types';

const ingestSchema = z.object({
  data: z.record(z.union([z.number(), z.boolean(), z.string()])),
  timestamp: z.string().datetime().optional(),
});

const seriesQuerySchema = z.object({
  field: z.string(),
  timeRange: z.enum(['5m', '15m', '30m', '1h', '6h', '24h', '7d']).default('1h'),
});

const aggregateQuerySchema = z.object({
  field:  z.string(),
  period: z.enum(['5m', '15m', '30m', '1h', '6h', '24h', '7d']).default('1h'),
});

export class TelemetryController {
  private svc = new TelemetryService();

  ingest = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const { orgId } = (req as AuthenticatedRequest).user;
      const body = ingestSchema.parse(req.body);
      const result = await this.svc.ingest(req.params.machineId, body.data, orgId);
      res.status(201).json({ success: true, data: result });
    } catch (err) { next(err); }
  };

  getLatest = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const { orgId } = (req as AuthenticatedRequest).user;
      const result = await this.svc.getLatest(req.params.machineId, orgId);
      res.json({ success: true, data: result });
    } catch (err) { next(err); }
  };

  getSeries = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const { orgId } = (req as AuthenticatedRequest).user;
      const { field, timeRange } = seriesQuerySchema.parse(req.query);
      const result = await this.svc.getSeries(req.params.machineId, field, timeRange, orgId);
      res.json({ success: true, data: result });
    } catch (err) { next(err); }
  };

  getAggregate = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const { orgId } = (req as AuthenticatedRequest).user;
      const { field, period } = aggregateQuerySchema.parse(req.query);
      const result = await this.svc.getAggregate(req.params.machineId, field, period, orgId);
      res.json({ success: true, data: result });
    } catch (err) { next(err); }
  };

  getMultiLatest = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const { orgId } = (req as AuthenticatedRequest).user;
      const machineIds = (req.query.ids as string ?? '').split(',').filter(Boolean);
      const result = await this.svc.getMultiMachineLatest(machineIds, orgId);
      res.json({ success: true, data: result });
    } catch (err) { next(err); }
  };
}
