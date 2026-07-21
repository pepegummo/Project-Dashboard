// ponytail: one-off capture for the IotVision report.
// Skips the UI login/org-picker by injecting a real JWT into localStorage (guard only needs auth_token).
// Run from frontend/: node capture-report-shots.mjs   (app up on :5173, backend :4000)
import { chromium } from 'playwright';

const BASE = process.env.BASE_URL || 'http://localhost:5173';
const API  = process.env.API_URL  || 'http://localhost:4000';
const OUT  = process.env.OUT_DIR  || '../docs/iotvision-report/images/ui';
const EMAIL = 'admin@acme-foods.com';
const PASS  = 'Admin@1234';
const DASH_ID = '00000000-0000-0000-0000-000000000010';

// 1. get a real token from the API
const res = await fetch(`${API}/api/auth/login`, {
  method: 'POST', headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ email: EMAIL, password: PASS }),
});
const token = (await res.json()).data.token;
if (!token) throw new Error('no token from login API');

const shots = [
  { name: '02-dashboards',       path: '/dashboards',            wait: 2500 },
  { name: '03-dashboard-editor', path: `/dashboards/${DASH_ID}`, wait: 4500, canvas: true },
  { name: '04-machines',         path: '/machines',             wait: 2500 },
  { name: '05-alerts',           path: '/alerts',               wait: 2500 },
  { name: '06-ai-assistant',     path: '/ai',                   wait: 2500 },
  { name: '07-ask-data',         path: '/ask',                  wait: 2500 },
];

const browser = await chromium.launch({ headless: true });
const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 2 });
// inject token before any app code runs
await ctx.addInitScript(t => localStorage.setItem('auth_token', t), token);
const page = await ctx.newPage();

// login page (clean, before auth) — separate context so it isn't authenticated
const anon = await browser.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 2 });
const anonPage = await anon.newPage();
await anonPage.goto(`${BASE}/login`);
await anonPage.waitForLoadState('networkidle');
await anonPage.screenshot({ path: `${OUT}/01-login.png` });
console.log('shot 01-login');
await anon.close();

for (const s of shots) {
  await page.goto(`${BASE}${s.path}`);
  await page.waitForLoadState('networkidle').catch(() => {});
  if (s.canvas) await page.waitForSelector('canvas', { timeout: 15000 }).catch(() => console.log('  no canvas'));
  await page.waitForTimeout(s.wait);
  await page.screenshot({ path: `${OUT}/${s.name}.png`, fullPage: true });
  console.log('shot', s.name, '(url', page.url() + ')');
}

await browser.close();
console.log('Done.');
