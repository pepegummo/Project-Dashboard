import { Request, Response, NextFunction } from 'express';
import { z } from 'zod';
import { AiToolsService } from './ai-tools.service';
import { AuthenticatedRequest } from '../types';

export class AiToolsController {
  private svc = new AiToolsService();

  getTools = async (_req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      res.json({ success: true, data: this.svc.getToolDefinitions() });
    } catch (err) { next(err); }
  };

  executeTool = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const { sub, orgId } = (req as AuthenticatedRequest).user;
      const { toolName, params } = z.object({
        toolName: z.string(),
        params: z.record(z.unknown()).default({}),
      }).parse(req.body);

      const result = await this.svc.executeTool(toolName, params, {
        userId: sub,
        organizationId: orgId,
      });
      res.json({ success: true, data: result });
    } catch (err) { next(err); }
  };

  getConversations = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const { sub } = (req as AuthenticatedRequest).user;
      const conversations = await this.svc.getConversations(sub);
      res.json({ success: true, data: conversations });
    } catch (err) { next(err); }
  };

  createConversation = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const { sub } = (req as AuthenticatedRequest).user;
      const { title } = z.object({ title: z.string().optional() }).parse(req.body);
      const conversation = await this.svc.createConversation(sub, title);
      res.status(201).json({ success: true, data: conversation });
    } catch (err) { next(err); }
  };

  getMessages = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const messages = await this.svc.getConversationMessages(req.params.conversationId);
      res.json({ success: true, data: messages });
    } catch (err) { next(err); }
  };

  addMessage = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const { role, content, toolName, toolInput, toolResult } = z.object({
        role: z.enum(['user', 'assistant', 'tool']),
        content: z.string(),
        toolName: z.string().optional(),
        toolInput: z.record(z.unknown()).optional(),
        toolResult: z.record(z.unknown()).optional(),
      }).parse(req.body);

      const message = await this.svc.saveMessage(req.params.conversationId, role, content, {
        toolName,
        toolInput,
        toolResult,
      });
      res.status(201).json({ success: true, data: message });
    } catch (err) { next(err); }
  };
}
