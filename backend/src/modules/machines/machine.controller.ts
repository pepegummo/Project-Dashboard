import { Request, Response, NextFunction } from 'express';
import { z } from 'zod';
import { MachineService } from './machine.service';
import { AuthenticatedRequest } from '../../types';

const createMachineSchema = z.object({
  productionLineId: z.string().uuid(),
  name: z.string().min(1).max(100),
  type: z.enum(['checkweigher', 'temperature_sensor', 'conveyor', 'vision_camera']),
  serialNumber: z.string().optional(),
  model: z.string().optional(),
  manufacturer: z.string().optional(),
  metadata: z.record(z.unknown()).optional(),
  fields: z.array(z.object({
    key: z.string(),
    label: z.string(),
    unit: z.string().optional(),
    dataType: z.enum(['number', 'boolean', 'string', 'enum']).optional(),
    min: z.number().optional(),
    max: z.number().optional(),
    isKey: z.boolean().optional(),
  })).optional(),
});

const fieldSchema = z.object({
  key: z.string().regex(/^[a-z][a-z0-9_]*$/),
  label: z.string().min(1),
  unit: z.string().optional(),
  dataType: z.enum(['number', 'boolean', 'string', 'enum']).optional(),
  min: z.number().optional(),
  max: z.number().optional(),
  isKey: z.boolean().optional(),
});

export class MachineController {
  private svc = new MachineService();

  list = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const { orgId } = (req as AuthenticatedRequest).user;
      const machines = await this.svc.getMachines(orgId, req.query as Record<string, string>);
      res.json({ success: true, data: machines });
    } catch (err) { next(err); }
  };

  getById = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const { orgId } = (req as AuthenticatedRequest).user;
      const machine = await this.svc.getMachineById(req.params.id, orgId);
      res.json({ success: true, data: machine });
    } catch (err) { next(err); }
  };

  create = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const { orgId } = (req as AuthenticatedRequest).user;
      const body = createMachineSchema.parse(req.body);
      const machine = await this.svc.createMachine(orgId, body);
      res.status(201).json({ success: true, data: machine });
    } catch (err) { next(err); }
  };

  update = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const { orgId } = (req as AuthenticatedRequest).user;
      const machine = await this.svc.updateMachine(req.params.id, orgId, req.body);
      res.json({ success: true, data: machine });
    } catch (err) { next(err); }
  };

  delete = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const { orgId } = (req as AuthenticatedRequest).user;
      await this.svc.deleteMachine(req.params.id, orgId);
      res.json({ success: true, data: { deleted: true } });
    } catch (err) { next(err); }
  };

  getFields = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const { orgId } = (req as AuthenticatedRequest).user;
      const fields = await this.svc.getMachineFields(req.params.id, orgId);
      res.json({ success: true, data: fields });
    } catch (err) { next(err); }
  };

  upsertField = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const { orgId } = (req as AuthenticatedRequest).user;
      const body = fieldSchema.parse(req.body);
      const field = await this.svc.upsertMachineField(req.params.id, orgId, body);
      res.json({ success: true, data: field });
    } catch (err) { next(err); }
  };

  getProductionLines = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const { orgId } = (req as AuthenticatedRequest).user;
      const lines = await this.svc.getProductionLines(orgId);
      res.json({ success: true, data: lines });
    } catch (err) { next(err); }
  };

  getFactories = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const { orgId } = (req as AuthenticatedRequest).user;
      const factories = await this.svc.getFactories(orgId);
      res.json({ success: true, data: factories });
    } catch (err) { next(err); }
  };
}
