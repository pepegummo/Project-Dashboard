import { Router } from 'express';
import { AiToolsController } from './ai-tools.controller';
import { authenticate } from '../middleware/auth';

const router = Router();
const ctrl = new AiToolsController();

router.use(authenticate);

router.get('/tools', ctrl.getTools);
router.post('/tools/execute', ctrl.executeTool);

router.get('/conversations', ctrl.getConversations);
router.post('/conversations', ctrl.createConversation);
router.get('/conversations/:conversationId/messages', ctrl.getMessages);
router.post('/conversations/:conversationId/messages', ctrl.addMessage);

export { router as aiRoutes };
