import { Router } from 'express';
import { AlertController } from './alert.controller';
import { authenticate, requireRole } from '../../middleware/auth';

const router = Router();
const ctrl = new AlertController();

router.use(authenticate);

router.get('/', ctrl.list);
router.get('/events/active', ctrl.getActiveEvents);
router.get('/:id', ctrl.getById);
router.post('/', requireRole('admin', 'editor'), ctrl.create);
router.patch('/:id', requireRole('admin', 'editor'), ctrl.update);
router.delete('/:id', requireRole('admin'), ctrl.delete);

router.patch('/events/:eventId/acknowledge', ctrl.acknowledgeEvent);
router.patch('/events/:eventId/resolve', ctrl.resolveEvent);

export { router as alertRoutes };
