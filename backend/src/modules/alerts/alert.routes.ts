import { Router } from 'express';
import { AlertController } from './alert.controller';
import { authenticate, requireRole } from '../../middleware/auth';

const router = Router();
const ctrl = new AlertController();

// ── Public (no auth) — alert counts shown on the LED kiosk ─────────────────
router.get('/events/active', ctrl.getActiveEvents);

// ── Protected ───────────────────────────────────────────────────────────────
router.get(   '/',    authenticate, ctrl.list);
router.get(   '/:id', authenticate, ctrl.getById);
router.post(  '/',    authenticate, requireRole('admin', 'editor'), ctrl.create);
router.patch( '/:id', authenticate, requireRole('admin', 'editor'), ctrl.update);
router.delete('/:id', authenticate, requireRole('admin'), ctrl.delete);

router.patch('/events/:eventId/acknowledge', authenticate, ctrl.acknowledgeEvent);
router.patch('/events/:eventId/resolve',     authenticate, ctrl.resolveEvent);

export { router as alertRoutes };
