import { Router } from 'express';
import { MachineController } from './machine.controller';
import { authenticate, requireRole } from '../../middleware/auth';

const router = Router();
const ctrl = new MachineController();

router.use(authenticate);

router.get('/factories', ctrl.getFactories);
router.get('/production-lines', ctrl.getProductionLines);

router.get('/', ctrl.list);
router.get('/:id', ctrl.getById);
router.post('/', requireRole('admin', 'editor'), ctrl.create);
router.patch('/:id', requireRole('admin', 'editor'), ctrl.update);
router.delete('/:id', requireRole('admin'), ctrl.delete);

router.get('/:id/fields', ctrl.getFields);
router.put('/:id/fields', requireRole('admin', 'editor'), ctrl.upsertField);

export { router as machineRoutes };
