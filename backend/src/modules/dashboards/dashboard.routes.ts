import { Router } from 'express';
import { DashboardController } from './dashboard.controller';
import { authenticate, requireRole } from '../../middleware/auth';

const router = Router();
const ctrl = new DashboardController();

router.use(authenticate);

router.get('/', ctrl.list);
router.get('/:id', ctrl.getById);
router.post('/', ctrl.create);
router.patch('/:id', ctrl.update);
router.delete('/:id', ctrl.delete);

// Widgets
router.post('/:id/widgets', ctrl.addWidget);
router.patch('/:id/layout', ctrl.bulkUpdateLayout);
router.patch('/:id/widgets/:widgetId', ctrl.updateWidget);
router.delete('/:id/widgets/:widgetId', ctrl.deleteWidget);

export { router as dashboardRoutes };
