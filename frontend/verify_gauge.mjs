import { chromium } from '@playwright/test';

const BASE    = 'http://localhost:5174';
const EMAIL   = 'admin@acme-foods.com';
const PASS    = 'Admin@1234';
const DASH_ID = '00000000-0000-0000-0000-000000000010';

const browser = await chromium.launch({ headless: true });
const ctx     = await browser.newContext({ viewport: { width: 1400, height: 900 } });
const page    = await ctx.newPage();

// ── 1. Login ──────────────────────────────────────────────────────────────────
await page.goto(`${BASE}/login`);
await page.waitForLoadState('networkidle');

// Log the login page HTML to debug selectors
const formHtml = await page.locator('form').first().innerHTML().catch(() => 'no form');
console.log('Login form HTML (first 500):', formHtml.slice(0, 500));

// Try multiple selector strategies
const emailInput = page.locator('input').filter({ hasAttribute: 'type' }).first();
const inputs = await page.locator('input').all();
console.log('Input count:', inputs.length);
for (const inp of inputs) {
  const t = await inp.getAttribute('type');
  const p = await inp.getAttribute('placeholder');
  const n = await inp.getAttribute('name');
  console.log('  input type=%s placeholder=%s name=%s', t, p, n);
}

await page.locator('input[type="email"], input[name="email"], input[placeholder*="email" i]').first().fill(EMAIL);
await page.locator('input[type="password"]').first().fill(PASS);
await page.screenshot({ path: 'verify_login_filled.png' });
console.log('📸 Login form filled → verify_login_filled.png');

await page.locator('button[type="submit"], button:has-text("Login"), button:has-text("Sign in")').first().click();

// Wait for navigation with a longer timeout
await page.waitForURL(url => !url.includes('/login'), { timeout: 15_000 }).catch(async () => {
  await page.screenshot({ path: 'verify_login_failed.png' });
  console.log('⚠️  waitForURL timed out → verify_login_failed.png');
});
console.log('After login URL:', page.url());
await page.screenshot({ path: 'verify_after_login.png' });
console.log('📸 After login → verify_after_login.png');

// ── 2. Dashboard view with the gauge widget ───────────────────────────────────
await page.goto(`${BASE}/dashboard/${DASH_ID}`);
const canvasFound = await page.waitForSelector('canvas', { timeout: 15_000 }).then(() => true).catch(() => false);
console.log('Canvas found on dashboard:', canvasFound);
await page.waitForTimeout(3000);
await page.screenshot({ path: 'verify_gauge_dashboard.png' });
console.log('📸 Dashboard → verify_gauge_dashboard.png');

// ── 3. Close-up: try to find the gauge widget cell ────────────────────────────
// Look for a div containing "Current Weight" text AND a canvas
const allItems = await page.locator('[class*="grid-stack"]').all();
console.log('Grid-stack items found:', allItems.length);

// Screenshot each canvas individually
const canvases = await page.locator('canvas').all();
console.log('Total canvases:', canvases.length);
for (let i = 0; i < canvases.length; i++) {
  await canvases[i].screenshot({ path: `verify_canvas_${i}.png` });
  const box = await canvases[i].boundingBox();
  console.log(`  canvas[${i}] size: ${box?.width}x${box?.height}`);
}

// ── 4. LED kiosk (/led) ───────────────────────────────────────────────────────
await page.goto(`${BASE}/led`);
await page.waitForTimeout(2500);
await page.screenshot({ path: 'verify_gauge_led.png' });
console.log('📸 LED view → verify_gauge_led.png');

await browser.close();
console.log('Done.');
