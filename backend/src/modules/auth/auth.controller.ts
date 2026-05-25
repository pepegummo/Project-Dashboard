import { Request, Response, NextFunction } from 'express';
import { z } from 'zod';
import { AuthService } from './auth.service';
import { AuthenticatedRequest } from '../../types';

const loginSchema = z.object({
  email: z.string().email(),
  password: z.string().min(1),
});

export class AuthController {
  private svc = new AuthService();

  login = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const body = loginSchema.parse(req.body);
      const result = await this.svc.login(
        body.email,
        body.password,
        req.ip,
        req.headers['user-agent'],
      );
      res.json({ success: true, data: result });
    } catch (err) {
      next(err);
    }
  };

  getProfile = async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      const userId = (req as AuthenticatedRequest).user.sub;
      const profile = await this.svc.getProfile(userId);
      res.json({ success: true, data: profile });
    } catch (err) {
      next(err);
    }
  };
}
