import { Router } from 'express';
import { TelemetryController } from './telemetry.controller';
import { authenticate } from '../../middleware/auth';

const router = Router();
const ctrl = new TelemetryController();

router.use(authenticate);

router.get('/latest', ctrl.getMultiLatest);                         // GET /telemetry/latest?ids=id1,id2
router.get('/:machineId/latest', ctrl.getLatest);                   // GET /telemetry/:machineId/latest
router.get('/:machineId/series', ctrl.getSeries);                   // GET /telemetry/:machineId/series?field=weight&timeRange=1h
router.get('/:machineId/aggregate', ctrl.getAggregate);             // GET /telemetry/:machineId/aggregate?field=throughput&period=1h
router.get('/:machineId/daily-count', ctrl.getDailyCount);          // GET /telemetry/:machineId/daily-count?days=7
router.post('/:machineId/ingest', ctrl.ingest);                     // POST /telemetry/:machineId/ingest

export { router as telemetryRoutes };
