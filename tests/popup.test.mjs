import assert from 'node:assert/strict';
import { chromium } from '../ui/node_modules/@playwright/test/index.mjs';
const browser = await chromium.launch({executablePath: process.env.BROWSER_EXECUTABLE || '/usr/bin/chromium', headless: true});
try {
  const page = await browser.newPage({viewport: {width: 340, height: 200}});
  await page.addInitScript(() => {
    window.chrome = {
      tabs: {query: () => new Promise(resolve => window.loadTab = resolve)},
      runtime: {sendMessage: () => new Promise(resolve => window.finishSave = resolve)},
    };
  });
  await page.goto(new URL('../internal/httpapi/extension/popup.html', import.meta.url).href);
  const height = await page.evaluate(() => document.documentElement.scrollHeight);
  for (let i = 0; i < 5; i++) {
    await page.setViewportSize({width: 340, height});
    assert.equal(await page.evaluate(() => document.documentElement.scrollHeight), height, 'popup height must not depend on its viewport');
  }
  const positions = () => page.locator('#bookmark, #kindle, #settings').evaluateAll(elements => elements.map(el => el.getBoundingClientRect().top));
  const initial = await positions();
  await page.evaluate(() => window.loadTab([{id: 1, title: 'A long article title '.repeat(30), url: 'https://example.com'}]));
  await page.getByRole('button', {name: 'Send to Kindle'}).click();
  assert.deepEqual(await positions(), initial, 'loading a title and saving must not shift actions');
  await page.evaluate(() => window.finishSave({ok: false, error: 'A long failure message '.repeat(30)}));
  await page.waitForFunction(() => !document.getElementById('kindle').disabled);
  assert.deepEqual(await positions(), initial, 'status messages must not shift actions');
  assert.equal(await page.evaluate(() => document.documentElement.scrollHeight), height);
  console.log('Popup stays stable during resizing, title loading, saving, and long errors');
} finally { await browser.close(); }
