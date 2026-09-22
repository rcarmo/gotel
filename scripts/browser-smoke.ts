/**
 * @description Browser integration checks, called by smoke.ts when GOTEL_BROWSER_SMOKE=1.
 * @usage GOTEL_BROWSER_SMOKE=1 GOTEL_PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs make smoke
 */
export async function browserSmoke(web: string, traceId: string, bundlePath: string) {
  const { chromium } = await import(process.env.GOTEL_PLAYWRIGHT_MODULE || 'playwright');
  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1100 } });
  const errors: string[] = [];
  page.on('pageerror', (error: Error) => errors.push(error.message));
  const assert = (condition: unknown, message: string) => { if (!condition) throw new Error(message); };
  async function navigate(name: string) { await page.getByRole('navigation').getByRole('button', { name, exact: true }).click(); }
  try {
    await page.goto(web);
    await page.getByRole('heading', { name: 'Slow operations', exact: true }).waitFor();
    assert((await page.locator('main').innerText()).includes('gotel-smoke'), 'Landing page missed service');
    // The main pane owns scrolling; the navigation must fill the viewport even on long pages.
    for (const width of [1440, 1024, 768, 390, 320]) {
      await page.setViewportSize({ width, height: 900 });
      const layout = await page.evaluate(() => {
        const main = document.querySelector<HTMLElement>('.portal-main')!;
        const nav = document.querySelector<HTMLElement>('.portal-nav')!;
        return {
          documentWidth: document.documentElement.scrollWidth,
          documentHeight: document.documentElement.scrollHeight,
          navBottom: nav.getBoundingClientRect().bottom,
          mainBottom: main.getBoundingClientRect().bottom,
          mainScrollable: main.scrollHeight > main.clientHeight,
        };
      });
      assert(layout.documentWidth === width, `Page overflows horizontally at ${width}px`);
      assert(layout.documentHeight === 900, `Document scrolls instead of main pane at ${width}px`);
      assert(layout.navBottom === 900 && layout.mainBottom === 900, `Sidebar/main height mismatch at ${width}px`);
      assert(layout.mainScrollable, `Long overview cannot scroll at ${width}px`);
      await page.locator('main').evaluate((main: HTMLElement) => { main.scrollTop = main.scrollHeight; });
      assert(await page.locator('main').evaluate((main: HTMLElement) => main.scrollTop > 0), 'Main pane did not scroll');
      if (width > 768) {
        assert(await page.getByRole('navigation').isVisible(), 'Desktop navigation hidden');
        assert(!(await page.getByRole('button', { name: 'Open navigation', exact: true }).isVisible()), 'Desktop menu toggle visible');
        const navBox = await page.getByRole('navigation').boundingBox();
        assert(navBox?.y === 48 && navBox.height === 852, 'Desktop sidebar moved while scrolling');
      } else {
        assert(!(await page.getByRole('navigation').isVisible()), 'Closed mobile drawer is visible');
        const toggle = page.locator('.portal-menu-toggle');
        await toggle.click();
        await page.getByRole('navigation').waitFor({ state: 'visible' });
        assert(await toggle.getAttribute('aria-expanded') === 'true', 'Drawer state not announced');
        assert(await page.getByRole('button', { name: 'Overview', exact: true }).evaluate((el: HTMLElement) => el === document.activeElement), 'Drawer did not receive keyboard focus');
        await page.keyboard.press('Shift+Tab');
        assert(await toggle.evaluate((el: HTMLElement) => el === document.activeElement), 'Drawer toggle missing from keyboard loop');
        await page.keyboard.press('Shift+Tab');
        assert(await page.getByRole('link', { name: 'Documentation' }).evaluate((el: HTMLElement) => el === document.activeElement), 'Keyboard focus escaped drawer');
        if (width === 390 && process.env.GOTEL_SCREENSHOT) {
          await page.screenshot({ path: process.env.GOTEL_SCREENSHOT.replace(/\.png$/, '-mobile.png') });
        }
        await page.keyboard.press('Escape');
        await page.getByRole('navigation').waitFor({ state: 'hidden' });
        assert(await toggle.evaluate((el: HTMLElement) => el === document.activeElement), 'Escape did not restore toggle focus');
        await toggle.click();
        await navigate('Agents');
        await page.getByText('smoke-model', { exact: true }).waitFor();
        assert(await toggle.getAttribute('aria-expanded') === 'false', 'Selecting a view left the drawer open');
        await toggle.click();
        await page.getByRole('button', { name: 'Close navigation overlay', exact: true }).click({ position: { x: width - 10, y: 100 } });
        await page.getByRole('navigation').waitFor({ state: 'hidden' });
        await toggle.click();
        await toggle.click();
        await page.getByRole('navigation').waitFor({ state: 'hidden' });
        await toggle.click();
        await navigate('Overview');
        await page.getByRole('heading', { name: 'Slow operations', exact: true }).waitFor();
      }
      await page.locator('main').evaluate((main: HTMLElement) => { main.scrollTop = 0; });
    }
    // Crossing the breakpoint must not resurrect an old open drawer.
    await page.locator('.portal-menu-toggle').click();
    await page.setViewportSize({ width: 1024, height: 900 });
    await page.waitForFunction(() => document.querySelector('.portal-menu-toggle')?.getAttribute('aria-expanded') === 'false');
    await page.setViewportSize({ width: 390, height: 900 });
    await page.getByRole('navigation').waitFor({ state: 'hidden' });
    await page.setViewportSize({ width: 1440, height: 1100 });
    await page.emulateMedia({ media: 'print' });
    assert(await page.locator('main').evaluate((main: HTMLElement) => getComputedStyle(main).overflow === 'visible'), 'Print clips the scrollable main pane');
    await page.emulateMedia({ media: 'screen' });
    if (process.env.GOTEL_SCREENSHOT) await page.screenshot({path: process.env.GOTEL_SCREENSHOT, fullPage:true});
    await navigate('Agents');
    await page.getByText('smoke-model', { exact:true }).waitFor();
    assert((await page.locator('main').innerText()).includes('123 in / unknown out'), 'Agent token count missing');
    assert(!(await page.locator('main').innerText()).includes('SECRET-SMOKE-PROMPT'), 'Prompt visible in agent view');
    await navigate('Trace Search');
    const checkbox = page.getByRole('checkbox', {name:`Select trace ${traceId}`});
    await checkbox.check();
    assert(!(await page.locator('main').innerText()).includes('Invalid Date'), 'Nanoseconds rendered as seconds');
    const downloadEvent = page.waitForEvent('download');
    await page.getByRole('button', {name:'Export selected traces', exact:true}).click();
    const download = await downloadEvent;
    const exported = JSON.parse(await Bun.file(await download.path()).text());
    assert(exported.traces[0].spans.length === 125, 'Browser export not complete');
    await page.getByRole('button', {name:'Open timeline', exact:true}).click();
    await page.getByText('Total Spans', {exact:true}).waitFor();
    assert((await page.locator('main').innerText()).includes('125'), 'Timeline not complete');
    await page.locator('main').evaluate((main: HTMLElement) => { main.scrollTop = 0; });
    const waterfallTop = await page.locator('.perfcascade-container').evaluate((el: HTMLElement) => el.getBoundingClientRect().top);
    assert(waterfallTop <= 510, `Headers push the desktop waterfall too far down: ${waterfallTop}px`);
    await page.getByRole('button', {name:'Export this trace',exact:true}).waitFor();
    await navigate('Metrics');
    await page.locator('button.gotel-chart-row').filter({hasText:/[1-9]\d* ·/}).first().click();
    await page.getByRole('button', {name:'Open timeline',exact:true}).waitFor();

    // A live response already in flight must not overwrite imported evidence.
    let release: () => void = () => {};
    const gate = new Promise<void>(resolve => { release = resolve; });
    let requestStarted: () => void = () => {};
    const started = new Promise<void>(resolve => { requestStarted = resolve; });
    await page.route('**/api/insights?*', async (route: any) => {
      const response = await route.fetch(); requestStarted(); await gate; await route.fulfill({ response });
    });
    await page.getByRole('button', {name:'Refresh data',exact:true}).click();
    await started;
    const captured = JSON.parse(await Bun.file(bundlePath).text());
    captured.insights.summary.p95_ms = 98765;
    let offlineRequests = 0;
    page.on('request', (r: any) => { if (r.url().includes('/api/')) offlineRequests++; });
    await page.locator('input[type=file]').setInputFiles({name:'incident.json',mimeType:'application/json',buffer:Buffer.from(JSON.stringify(captured))});
    await page.getByRole('button', {name:'Exit import',exact:true}).waitFor();
    const baseline = offlineRequests;
    release(); await page.waitForTimeout(300);
    assert((await page.locator('main').innerText()).includes('1.6m'), 'Late live response replaced imported insights');
    await navigate('Agents'); await page.getByText('smoke-model',{exact:true}).waitFor();
    await navigate('Trace Search');
    await page.getByRole('button', {name:'Open timeline',exact:true}).click();
    await page.getByText('Total Spans',{exact:true}).waitFor();
    assert(offlineRequests === baseline, 'Imported bundle made API requests');
    await navigate('Metrics');
    await page.locator('button.gotel-chart-row').filter({hasText:/[1-9]\d* ·/}).first().click();
    await page.getByRole('button', {name:'Open timeline',exact:true}).waitFor();
    assert(offlineRequests === baseline, 'Imported bucket drilldown fetched live data');
    assert(errors.length === 0, `Browser errors: ${errors.join('; ')}`);
    console.log('Browser passed: desktop sidebar, mobile drawer/keyboard/resize, print layout, landing, agents, real export, complete timeline, metric drilldown, local import and stale-response isolation.');
  } catch (error) {
    console.error('Browser DOM at failure:', (await page.locator('main').innerText()).slice(-16000));
    throw error;
  } finally { await browser.close(); }
}
