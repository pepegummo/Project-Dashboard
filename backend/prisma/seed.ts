import { PrismaClient } from '@prisma/client';
import bcrypt from 'bcryptjs';

const prisma = new PrismaClient();

// ─── Stable UUID-format seed IDs (valid UUIDs, all zeros + sequential suffix) ─
const ID = {
  org:          '00000000-0000-0000-0000-000000000001',
  factory:      '00000000-0000-0000-0000-000000000002',
  lineA:        '00000000-0000-0000-0000-000000000003',
  lineB:        '00000000-0000-0000-0000-000000000004',
  checkweigher: '00000000-0000-0000-0000-000000000005',
  tempSensor:   '00000000-0000-0000-0000-000000000006',
  conveyor:     '00000000-0000-0000-0000-000000000007',
  visionCam:    '00000000-0000-0000-0000-000000000008',
  adminUser:    '00000000-0000-0000-0000-000000000009',
  dashboard:    '00000000-0000-0000-0000-000000000010',
  alert1:       '00000000-0000-0000-0000-000000000011',
  alert2:       '00000000-0000-0000-0000-000000000012',
  alert3:       '00000000-0000-0000-0000-000000000013',
  widget1:      '00000000-0000-0000-0000-000000000014',
  widget2:      '00000000-0000-0000-0000-000000000015',
  widget3:      '00000000-0000-0000-0000-000000000016',
  widget4:      '00000000-0000-0000-0000-000000000017',
  widget5:      '00000000-0000-0000-0000-000000000018',
  widget6:      '00000000-0000-0000-0000-000000000019',
};

async function main() {
  console.log('🌱 Seeding database...');

  // ─── Organization ────────────────────────────────────────────────────────────
  const org = await prisma.organization.upsert({
    where: { id: ID.org },
    update: {},
    create: { id: ID.org, name: 'ACME Foods Co.', slug: 'acme-foods', plan: 'pro' },
  });
  console.log(`  ✅ Organization: ${org.name}`);

  // ─── Admin User ──────────────────────────────────────────────────────────────
  const passwordHash = await bcrypt.hash('Admin@1234', 12);
  const adminUser = await prisma.user.upsert({
    where: { id: ID.adminUser },
    update: {},
    create: {
      id: ID.adminUser,
      organizationId: ID.org,
      email: 'admin@acme-foods.com',
      name: 'Admin User',
      passwordHash,
      role: 'admin',
    },
  });
  console.log(`  ✅ Admin user: ${adminUser.email}`);

  // ─── Factory ─────────────────────────────────────────────────────────────────
  const factory = await prisma.factory.upsert({
    where: { id: ID.factory },
    update: {},
    create: {
      id: ID.factory,
      organizationId: ID.org,
      name: 'Bangkok Plant 1',
      location: 'Bangkok, Thailand',
      timezone: 'Asia/Bangkok',
    },
  });
  console.log(`  ✅ Factory: ${factory.name}`);

  // ─── Production Lines ────────────────────────────────────────────────────────
  await prisma.productionLine.upsert({
    where: { id: ID.lineA },
    update: {},
    create: { id: ID.lineA, factoryId: ID.factory, name: 'Line A — Packaging', code: 'LINE-A', status: 'active' },
  });
  await prisma.productionLine.upsert({
    where: { id: ID.lineB },
    update: {},
    create: { id: ID.lineB, factoryId: ID.factory, name: 'Line B — Filling', code: 'LINE-B', status: 'active' },
  });
  console.log('  ✅ Production lines: Line A, Line B');

  // ─── Machines ────────────────────────────────────────────────────────────────
  await prisma.machine.upsert({
    where: { id: ID.checkweigher },
    update: {},
    create: {
      id: ID.checkweigher,
      productionLineId: ID.lineA,
      name: 'Checkweigher CW-01',
      type: 'checkweigher',
      serialNumber: 'CW-2024-001',
      model: 'ProCheck X200',
      manufacturer: 'MettlerToledo',
      status: 'online',
      metadata: { targetWeight: 500, tolerance: 5 },
    },
  });

  await prisma.machine.upsert({
    where: { id: ID.tempSensor },
    update: {},
    create: {
      id: ID.tempSensor,
      productionLineId: ID.lineA,
      name: 'Temp Sensor TS-01',
      type: 'temperature_sensor',
      serialNumber: 'TS-2024-001',
      model: 'ThermoGuard Pro',
      manufacturer: 'Omega',
      status: 'online',
      metadata: { location: 'cold_storage_a' },
    },
  });

  await prisma.machine.upsert({
    where: { id: ID.conveyor },
    update: {},
    create: {
      id: ID.conveyor,
      productionLineId: ID.lineA,
      name: 'Conveyor Belt CB-01',
      type: 'conveyor',
      serialNumber: 'CB-2024-001',
      model: 'FlexLine 500',
      manufacturer: 'Interroll',
      status: 'online',
      metadata: { maxSpeed: 2000 },
    },
  });

  await prisma.machine.upsert({
    where: { id: ID.visionCam },
    update: {},
    create: {
      id: ID.visionCam,
      productionLineId: ID.lineB,
      name: 'Vision AI Camera VC-01',
      type: 'vision_camera',
      serialNumber: 'VC-2024-001',
      model: 'SmartEye AI-Pro',
      manufacturer: 'Cognex',
      status: 'online',
      metadata: { resolution: '4K', model: 'defect-detection-v2' },
    },
  });
  console.log('  ✅ Machines: Checkweigher, Temp Sensor, Conveyor, Vision Camera');

  // ─── Machine Fields ──────────────────────────────────────────────────────────
  // threshold = nominal value, upper/lower = threshold ±10%
  const cwFields = [
    { key: 'weight',      label: 'Weight',       unit: 'g',       min: 0, max: 2000, isKey: true, threshold: 500,  upperLimit: 550,  lowerLimit: 450  },
    { key: 'speed',       label: 'Belt Speed',   unit: 'ppm',     min: 0, max: 120,              threshold: 60,   upperLimit: 66,   lowerLimit: 54   },
    { key: 'rejects',     label: 'Reject Count', unit: 'pcs',     min: 0, max: 9999,             threshold: 0,    upperLimit: 3,    lowerLimit: 0    },
    { key: 'throughput',  label: 'Throughput',   unit: 'pcs/min', min: 0, max: 120,              threshold: 60,   upperLimit: 66,   lowerLimit: 54   },
    { key: 'status_code', label: 'Status Code',  unit: '' },
  ];
  const tsFields = [
    { key: 'temp',      label: 'Temperature', unit: '°C',  min: -20, max: 80,  isKey: true, threshold: 22,  upperLimit: 24.2, lowerLimit: 19.8 },
    { key: 'humidity',  label: 'Humidity',    unit: '%RH', min: 0,   max: 100,              threshold: 55,  upperLimit: 60.5, lowerLimit: 49.5 },
    { key: 'dew_point', label: 'Dew Point',   unit: '°C',  min: -30, max: 60,               threshold: 11,  upperLimit: 12.1, lowerLimit: 9.9  },
  ];
  const convFields = [
    { key: 'speed',     label: 'Belt Speed', unit: 'mm/s',  min: 0, max: 2000, isKey: true, threshold: 1000, upperLimit: 1100, lowerLimit: 900  },
    { key: 'load',      label: 'Motor Load', unit: '%',     min: 0, max: 100,               threshold: 45,   upperLimit: 49.5, lowerLimit: 40.5 },
    { key: 'rpm',       label: 'Motor RPM',  unit: 'rpm',   min: 0, max: 1500,              threshold: 750,  upperLimit: 825,  lowerLimit: 675  },
    { key: 'vibration', label: 'Vibration',  unit: 'mm/s²', min: 0, max: 50,               threshold: 5,    upperLimit: 5.5,  lowerLimit: 4.5  },
  ];
  const vcFields = [
    { key: 'defect_rate', label: 'Defect Rate',     unit: '%',  min: 0, max: 100,    isKey: true, threshold: 1,  upperLimit: 1.1, lowerLimit: 0.9  },
    { key: 'inspected',   label: 'Items Inspected', unit: 'pcs',min: 0, max: 999999 },
    { key: 'passed',      label: 'Items Passed',    unit: 'pcs',min: 0, max: 999999 },
    { key: 'failed',      label: 'Items Failed',    unit: 'pcs',min: 0, max: 999999 },
    { key: 'confidence',  label: 'AI Confidence',   unit: '%',  min: 0, max: 100,              threshold: 97, upperLimit: 99.9, lowerLimit: 87.3 },
  ];

  const fieldSets: Array<{ machineId: string; fields: typeof cwFields }> = [
    { machineId: ID.checkweigher, fields: cwFields },
    { machineId: ID.tempSensor,   fields: tsFields },
    { machineId: ID.conveyor,     fields: convFields },
    { machineId: ID.visionCam,    fields: vcFields },
  ];

  for (const { machineId, fields } of fieldSets) {
    for (const f of fields) {
      await prisma.machineField.upsert({
        where: { machineId_key: { machineId, key: f.key } },
        update: {
          label:      f.label,
          unit:       f.unit ?? null,
          threshold:  (f as any).threshold   ?? null,
          upperLimit: (f as any).upperLimit  ?? null,
          lowerLimit: (f as any).lowerLimit  ?? null,
        },
        create: { machineId, ...f },
      });
    }
  }
  console.log('  ✅ Machine fields created');

  // ─── Alert Rules ─────────────────────────────────────────────────────────────
  await prisma.alert.upsert({
    where: { id: ID.alert1 },
    update: {},
    create: { id: ID.alert1, machineId: ID.checkweigher, name: 'Weight Over Tolerance',  field: 'weight', condition: 'gt',  threshold: 510, severity: 'warning' },
  });
  await prisma.alert.upsert({
    where: { id: ID.alert2 },
    update: {},
    create: { id: ID.alert2, machineId: ID.checkweigher, name: 'Weight Under Tolerance', field: 'weight', condition: 'lt',  threshold: 490, severity: 'critical' },
  });
  await prisma.alert.upsert({
    where: { id: ID.alert3 },
    update: {},
    create: { id: ID.alert3, machineId: ID.tempSensor,   name: 'High Temperature',       field: 'temp',   condition: 'gt',  threshold: 35,  severity: 'critical' },
  });
  console.log('  ✅ Alert rules created');

  // ─── Dashboard + Widgets ─────────────────────────────────────────────────────
  await prisma.dashboard.upsert({
    where: { id: ID.dashboard },
    update: {},
    create: {
      id: ID.dashboard,
      organizationId: ID.org,
      userId: ID.adminUser,
      name: 'Production Overview',
      description: 'Main production monitoring dashboard',
      isDefault: true,
      tags: ['production', 'overview'],
    },
  });

  const widgets = [
    {
      id: ID.widget1,
      dashboardId: ID.dashboard,
      machineId: ID.checkweigher,
      widgetType: 'line-chart',
      title: 'Weight Over Time',
      layout: { x: 0, y: 0, w: 6, h: 4 },
      config: { field: 'weight', timeRange: '1h', color: '#3b82f6' },
    },
    {
      id: ID.widget2,
      dashboardId: ID.dashboard,
      machineId: ID.checkweigher,
      widgetType: 'gauge',
      title: 'Current Weight',
      layout: { x: 6, y: 0, w: 3, h: 4 },
      config: { field: 'weight', min: 400, max: 600, unit: 'g' },
    },
    {
      id: ID.widget3,
      dashboardId: ID.dashboard,
      machineId: ID.tempSensor,
      widgetType: 'kpi-card',
      title: 'Temperature',
      layout: { x: 9, y: 0, w: 3, h: 2 },
      config: { field: 'temp', unit: '°C', precision: 1 },
    },
    {
      id: ID.widget4,
      dashboardId: ID.dashboard,
      machineId: ID.checkweigher,
      widgetType: 'kpi-card',
      title: 'Throughput',
      layout: { x: 9, y: 2, w: 3, h: 2 },
      config: { field: 'throughput', unit: 'pcs/min', precision: 0 },
    },
    {
      id: ID.widget5,
      dashboardId: ID.dashboard,
      machineId: ID.conveyor,
      widgetType: 'line-chart',
      title: 'Conveyor Speed',
      layout: { x: 0, y: 4, w: 6, h: 4 },
      config: { field: 'speed', timeRange: '30m', color: '#10b981' },
    },
    {
      id: ID.widget6,
      dashboardId: ID.dashboard,
      machineId: null,
      widgetType: 'alarm-panel',
      title: 'Active Alerts',
      layout: { x: 6, y: 4, w: 6, h: 4 },
      config: { maxItems: 10, severities: ['warning', 'critical'] },
    },
  ];

  for (const w of widgets) {
    await prisma.dashboardWidget.upsert({
      where: { id: w.id },
      update: {},
      create: {
        id: w.id,
        dashboardId: w.dashboardId,
        ...(w.machineId ? { machineId: w.machineId } : {}),
        widgetType: w.widgetType,
        title: w.title,
        layout: w.layout,
        config: w.config,
      },
    });
  }
  console.log(`  ✅ Dashboard "Production Overview" with ${widgets.length} widgets`);

  console.log('\n🎉 Seed complete!');
  console.log('   Login: admin@acme-foods.com / Admin@1234');
}

main()
  .catch((e) => { console.error('❌ Seed failed:', e); process.exit(1); })
  .finally(() => prisma.$disconnect());
