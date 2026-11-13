// Browser-level tests of the admin UI: layout, theming and real input
// (pointer drags, keys, the mobile inspector) in headless Chrome against a
// running mock binary. Run with `make test-browser`. Tests are skipped when
// no Chrome or Chromium is found (set CHROME_BIN to point at one).
//
// MOCK_BIN names a prebuilt mock binary; otherwise one is built with `go
// build`. On failure a screenshot is saved to $BROWSER_ARTIFACTS (default:
// the system temp directory) and its path is added to the error.

import test, { after, before } from 'node:test';
import assert from 'node:assert/strict';
import { spawn, spawnSync } from 'node:child_process';
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { createServer } from 'node:net';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { Browser, findChrome } from './cdp.mjs';

const chrome = findChrome();
const skip = chrome ? false : 'Chrome or Chromium not found; set CHROME_BIN';
const repo = fileURLToPath(new URL('../../../', import.meta.url));

const routes = `### List users
GET /users
Content-Type: application/json

[{"id": 1, "name": "Ada"}, {"id": 2, "name": "Grace"}]

### Find user
GET /users/:id
Content-Type: application/json

{"id": "{{$id}}", "name": "User {{$id}}"}

### [201] Create user
@json.name = *
POST /users
Content-Type: application/json

{"created": "{{$json.name}}"}

### Team
@fixture = team.json
GET /team
Content-Type: application/json

{"lead": "{{$fixture.lead}}", "size": {{$fixture.size}}}
`;

let workdir, mock, browser, mockURL, adminURL, mockLog = '';

function freePort() {
    return new Promise((resolve, reject) => {
        const server = createServer().listen(0, '127.0.0.1', () => {
            const { port } = server.address();
            server.close(() => resolve(port));
        }).on('error', reject);
    });
}

async function waitForServer(url) {
    const deadline = Date.now() + 15000;
    while (Date.now() < deadline) {
        try { if ((await fetch(url)).ok) return; } catch { /* not yet */ }
        await new Promise(resolve => setTimeout(resolve, 100));
    }
    throw new Error(`mock did not start:\n${mockLog}`);
}

before(async () => {
    if (skip) return;
    workdir = mkdtempSync(join(tmpdir(), 'mock-browser-'));
    let binary = process.env.MOCK_BIN;
    if (!binary) {
        binary = join(workdir, process.platform === 'win32' ? 'mock.exe' : 'mock');
        const built = spawnSync('go', ['build', '-o', binary, '.'], { cwd: repo, encoding: 'utf8' });
        if (built.status !== 0) throw new Error(`go build failed:\n${built.stderr}`);
    }
    writeFileSync(join(workdir, 'api.http'), routes);
    writeFileSync(join(workdir, 'team.json'), '{"lead": "Ada", "size": 3}\n');
    const [port, adminPort] = [await freePort(), await freePort()];
    mockURL = `http://127.0.0.1:${port}`;
    adminURL = `http://127.0.0.1:${adminPort}/`;
    mock = spawn(binary, ['-p', String(port), '-a', String(adminPort), join(workdir, 'api.http')], { stdio: ['ignore', 'pipe', 'pipe'] });
    for (const stream of [mock.stdout, mock.stderr]) stream.on('data', chunk => { mockLog = (mockLog + chunk).slice(-8000); });
    await waitForServer(adminURL + 'state');
    browser = await Browser.launch(chrome);
});

after(async () => {
    await browser?.close();
    mock?.kill();
    if (workdir) rmSync(workdir, { recursive: true, force: true });
});

// traffic clears the request log and sends requests to the mock port.
async function traffic(...requests) {
    const cleared = await fetch(adminURL + 'clear', { method: 'POST', headers: { 'X-Requested-With': 'test' } });
    assert.equal(cleared.status, 204);
    for (const [method, path, body] of requests) {
        const response = await fetch(mockURL + path, body ? { method, body: JSON.stringify(body), headers: { 'Content-Type': 'application/json' } } : { method });
        await response.arrayBuffer();
    }
}

// uiTest opens a fresh page (its own storage) on the admin UI, runs fn, and
// fails if the page logged errors. A failing test saves a screenshot.
function uiTest(name, options, fn) {
    test(name, { skip, timeout: 30000 }, async () => {
        const page = await browser.newPage(options);
        try {
            await page.goto(adminURL);
            await page.waitFor(() => document.getElementById('streamStatus').textContent !== 'Connecting', { message: 'event stream connects' });
            await fn(page);
            assert.deepEqual(page.errors, [], 'the page logged errors');
        } catch (error) {
            const dir = process.env.BROWSER_ARTIFACTS || tmpdir();
            mkdirSync(dir, { recursive: true });
            const file = join(dir, `mock-ui-${name.replace(/[^a-z0-9]+/gi, '-').toLowerCase()}.png`);
            try { writeFileSync(file, await page.screenshot()); error.message += `\nscreenshot: ${file}`; } catch { /* page gone */ }
            throw error;
        } finally {
            await page.close();
        }
    });
}

const rowCount = () => document.querySelectorAll('#requestTableBody tr[data-id]').length;
const box = (selector) => {
    const rect = document.querySelector(selector).getBoundingClientRect();
    return { left: rect.left, right: rect.right, top: rect.top, bottom: rect.bottom, width: rect.width, height: rect.height };
};
const visible = (selector) => {
    const element = document.querySelector(selector);
    return !!element && getComputedStyle(element).display !== 'none' && element.getBoundingClientRect().width > 0;
};

uiTest('desktop layout shows traffic, routes and inspector side by side', { width: 1280, height: 800 }, async (page) => {
    await traffic(['GET', '/users'], ['POST', '/users', { name: 'Ada' }], ['GET', '/nope']);
    await page.waitFor(() => document.querySelectorAll('#requestTableBody tr[data-id]').length === 3, { message: 'three rows' });
    await page.waitFor(() => document.querySelectorAll('#routesList li').length === 4, { message: 'four routes' });
    const layout = await page.evaluate((boxSource) => {
        const box = eval(boxSource);
        return { traffic: box('#requestPanel'), routes: box('#routesPanel'), inspector: box('#inspectorPanel'), scroll: document.documentElement.scrollWidth, width: innerWidth };
    }, `(${box})`);
    assert.ok(layout.inspector.left >= layout.traffic.right, 'the inspector is right of the traffic list');
    assert.ok(layout.routes.top >= layout.traffic.top + 100, 'routes are below the traffic table');
    assert.ok(Math.abs(layout.traffic.width / layout.width - 0.45) < 0.08, `traffic takes the default 45% (${layout.traffic.width}px of ${layout.width})`);
    assert.ok(layout.scroll <= layout.width, 'no horizontal scrolling');

    // Newest first: the unmatched request is the top row.
    await page.click('#requestTableBody tr[data-id]:first-child');
    await page.waitFor(() => document.getElementById('matchSummary').textContent.length > 0);
    const unmatched = await page.evaluate(() => ({
        mismatch: !document.getElementById('mismatchPanel').hidden,
        request: document.getElementById('requestDetails').textContent,
    }));
    assert.match(unmatched.request, /GET \/nope/);
    assert.equal(unmatched.mismatch, true, 'an unmatched request explains near misses');
});

uiTest('keyboard moves the selection and / focuses the filter', {}, async (page) => {
    await traffic(['GET', '/users/1'], ['GET', '/users/2'], ['GET', '/users/3']);
    await page.waitFor(() => document.querySelectorAll('#requestTableBody tr[data-id]').length === 3);
    await page.click('#requestTableBody tr[data-id]:first-child');
    const selected = () => page.evaluate(() => ({
        row: document.querySelector('#requestTableBody tr[aria-selected="true"]')?.textContent || '',
        focused: document.activeElement?.getAttribute('aria-selected') === 'true',
        detail: document.getElementById('requestDetails').textContent,
    }));
    let state = await selected();
    assert.match(state.row, /\/users\/3/);
    assert.match(state.detail, /\/users\/3/);
    await page.press('ArrowDown');
    state = await selected();
    assert.match(state.row, /\/users\/2/);
    assert.equal(state.focused, true, 'focus follows the selection');
    await page.press('End');
    assert.match((await selected()).detail, /\/users\/1/);
    await page.press('Home');
    assert.match((await selected()).row, /\/users\/3/);
    await page.press('/');
    assert.equal(await page.evaluate(() => document.activeElement.id), 'filterInput');
    await page.type('users/2');
    await page.waitFor(() => document.querySelectorAll('#requestTableBody tr[data-id]').length === 1, { message: 'filter narrows to one row' });
});

uiTest('dividers resize by pointer and keyboard, persist, and reset on double-click', { width: 1280, height: 800 }, async (page) => {
    await traffic(['GET', '/users']);
    const width = () => page.evaluate(() => document.getElementById('requestPanel').getBoundingClientRect().width);
    const value = () => page.evaluate(() => Number(document.getElementById('workspaceSplitter').getAttribute('aria-valuenow')));
    const start = await width();
    assert.equal(await value(), 45);
    await page.drag('#workspaceSplitter', 128, 0);
    assert.ok(Math.abs(await value() - 55) <= 1, `dragging 10% of the width moves the divider to ~55 (got ${await value()})`);
    assert.ok(await width() > start + 100, 'the traffic panel grows');
    await page.reload();
    assert.ok(Math.abs(await value() - 55) <= 1, 'the size is remembered');
    await page.evaluate(() => document.getElementById('workspaceSplitter').focus());
    const before = await value();
    await page.press('ArrowLeft');
    assert.equal(await value(), before - 2);
    await page.click('#workspaceSplitter', 2);
    assert.equal(await value(), 45, 'double-click restores the default');

    const requestHeight = () => page.evaluate(() => Number(document.getElementById('exchangeSplitter').getAttribute('aria-valuenow')));
    await page.click('#requestTableBody tr[data-id]');
    await page.drag('#exchangeSplitter', 0, -60);
    assert.ok(await requestHeight() < 50, 'the request/response divider moves up');
});

uiTest('narrow screens swap between the list and the inspector', { width: 390, height: 844, mobile: true }, async (page) => {
    await traffic(['GET', '/users']);
    await page.waitFor(() => document.querySelectorAll('#requestTableBody tr[data-id]').length === 1);
    const view = () => page.evaluate((visibleSource) => {
        const visible = eval(visibleSource);
        return { list: visible('#requestPanel'), inspector: visible('#inspectorPanel'), back: visible('#backToList'), focus: document.activeElement?.id || document.activeElement?.tagName, scroll: document.documentElement.scrollWidth, width: innerWidth };
    }, `(${visible})`);
    let state = await view();
    assert.deepEqual([state.list, state.inspector], [true, false]);
    assert.ok(state.scroll <= state.width, `no horizontal scrolling (${state.scroll} > ${state.width})`);
    await page.click('#requestTableBody tr[data-id]');
    state = await view();
    assert.deepEqual([state.list, state.inspector, state.back], [false, true, true]);
    assert.equal(state.focus, 'requestTitle', 'focus moves to the inspector heading');
    assert.ok(state.scroll <= state.width, 'the inspector fits the screen');
    await page.click('#backToList');
    state = await view();
    assert.deepEqual([state.list, state.inspector], [true, false]);
    assert.equal(state.focus, 'TR', 'focus returns to the selected row');
});

// contrast returns the WCAG contrast ratio of the body text on its background.
function contrast() {
    const parse = (color) => color.match(/[\d.]+/g).slice(0, 3).map(Number);
    const luminance = (rgb) => {
        const [r, g, b] = rgb.map(v => { v /= 255; return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4; });
        return 0.2126 * r + 0.7152 * g + 0.0722 * b;
    };
    const style = getComputedStyle(document.body);
    const fg = luminance(parse(style.color)), bg = luminance(parse(style.backgroundColor));
    return { ratio: (Math.max(fg, bg) + 0.05) / (Math.min(fg, bg) + 0.05), background: bg, theme: document.documentElement.getAttribute('data-theme') || 'light', label: document.getElementById('themeToggleLabel').textContent };
}

uiTest('dark mode follows the system and the toggle, with readable text', { colorScheme: 'dark' }, async (page) => {
    let theme = await page.evaluate(contrast);
    assert.equal(theme.theme, 'dark', 'a dark system preference starts in dark mode');
    assert.ok(theme.background < 0.05, 'the dark background is dark');
    assert.ok(theme.ratio >= 7, `dark text contrast ${theme.ratio.toFixed(1)}`);
    await page.click('#themeToggle');
    theme = await page.evaluate(contrast);
    assert.equal(theme.theme, 'light');
    assert.equal(theme.label, 'Dark');
    assert.ok(theme.background > 0.6, 'the light background is light');
    assert.ok(theme.ratio >= 7, `light text contrast ${theme.ratio.toFixed(1)}`);
    await page.reload();
    assert.equal((await page.evaluate(contrast)).theme, 'light', 'the chosen theme overrides the system preference');
});

uiTest('help opens as a modal dialog and closes with Escape', {}, async (page) => {
    await page.click('#helpButton');
    assert.equal(await page.evaluate(() => document.getElementById('helpDialog').open), true);
    await page.press('Escape');
    assert.equal(await page.evaluate(() => document.getElementById('helpDialog').open), false);
    assert.equal(await page.evaluate(() => document.activeElement.id), 'helpButton', 'focus returns to the Help button');
});

uiTest('resend and compare round-trip through the running server', { width: 1280, height: 900 }, async (page) => {
    await traffic(['GET', '/users/1']);
    await page.waitFor(() => document.querySelectorAll('#requestTableBody tr[data-id]').length === 1);
    await page.click('#requestTableBody tr[data-id]');
    await page.click('#resendButton');
    await page.evaluate(() => {
        const url = document.getElementById('resendUrl');
        url.select();
    });
    await page.type('/users/42');
    await page.click('#resendSend');
    await page.waitFor(() => document.querySelectorAll('#requestTableBody tr[data-id]').length === 2, { message: 'the resent request is logged' });
    const resent = await page.waitFor(() => {
        const row = document.querySelector('#requestTableBody tr[aria-selected="true"]');
        return row && /users\/42/.test(row.textContent) && row.textContent;
    }, { message: 'the resent request is selected' });
    assert.match(resent, /resent/);
    assert.match(await page.evaluate(() => document.getElementById('responseDetails').textContent), /User 42/);

    await page.click('#compareButton');
    await page.click('#requestTableBody tr[data-id]:last-child');
    await page.click('#compareButton');
    await page.waitFor(() => !document.getElementById('compareView').hidden, { message: 'the comparison opens' });
    const comparison = await page.evaluate(() => ({
        title: document.getElementById('compareTitle').textContent,
        summary: document.getElementById('compareSummary').textContent,
        added: document.querySelectorAll('.compare-diff .diff-added').length,
        exchange: document.getElementById('exchangeView').hidden,
        fills: document.getElementById('compareView').getBoundingClientRect().height > 400,
    }));
    assert.match(comparison.title, /^Compare #\d+ and #\d+$/);
    assert.match(comparison.summary, /\d+ differences?/);
    assert.ok(comparison.added > 0, 'the response bodies are diffed');
    assert.equal(comparison.exchange, true);
    assert.equal(comparison.fills, true, 'the comparison fills the inspector');
    await page.click('#compareClose');
    assert.equal(await page.evaluate(() => document.getElementById('compareView').hidden), true);
});

uiTest('the fixture editor saves a route fixture that the next response uses', { width: 1280, height: 900 }, async (page) => {
    await page.waitFor(() => [...document.querySelectorAll('#routesList button')].some(button => /\/team/.test(button.textContent)), { message: 'the Team route is listed' });
    await page.evaluate(() => [...document.querySelectorAll('#routesList button')].find(button => /\/team/.test(button.textContent)).id ||= 'teamRoute');
    await page.click('#teamRoute');
    await page.click('#editFixtureButton');
    await page.waitFor(() => document.querySelector('#fixtureTree input[aria-label="Value of lead"]'), { message: 'the tree shows the fixture' });
    const layout = await page.evaluate(() => ({
        details: document.getElementById('routeDetails').hidden,
        fills: document.getElementById('fixtureEditor').getBoundingClientRect().height > 400,
        overflow: document.getElementById('fixtureTree').scrollWidth > document.getElementById('fixtureTree').clientWidth,
    }));
    assert.deepEqual(layout, { details: true, fills: true, overflow: false });

    await page.click('#fixtureTree input[aria-label="Value of lead"]');
    await page.evaluate(() => document.activeElement.select());
    await page.type('Linus');
    await page.press('Tab');
    await page.waitFor(() => document.getElementById('fixtureStatus').textContent === 'Unsaved changes.', { message: 'the edit is pending' });
    await page.evaluate(() => {
        const size = document.querySelector('#fixtureTree select[aria-label="Type of size"]');
        size.value = 'string';
        size.dispatchEvent(new Event('change', { bubbles: true }));
    });
    await page.click('#fixtureSave');
    await page.waitFor(() => /^Saved/.test(document.getElementById('fixtureStatus').textContent), { message: 'the fixture saves' });

    const response = await (await fetch(mockURL + '/team')).text();
    assert.match(response, /"lead": "Linus"/);
    assert.match(response, /"size": 3\b/, 'size renders unquoted from the template; the fixture now holds "3"');

    // The save reloads the routes; the route stays selected with the editor open.
    await page.waitFor(async () => {
        const state = await (await fetch('state')).json();
        return state.revision > 1;
    }, { message: 'the routes reload' });
    await page.waitFor(() => !/previous revision/.test(document.getElementById('routeSummary').textContent) && !document.getElementById('fixtureEditor').hidden, { message: 'the edited route stays selected', timeout: 8000 });

    await page.click('#fixtureJsonTab');
    const text = await page.evaluate(() => document.getElementById('fixtureText').value);
    assert.equal(text, '{\n  "lead": "Linus",\n  "size": "3"\n}\n');
});

uiTest('route switches turn a route off and back on', { width: 1280, height: 900 }, async (page) => {
    await page.waitFor(() => [...document.querySelectorAll('#routesList button')].some(button => /List users/.test(button.textContent)), { message: 'the List users route is listed' });
    await page.evaluate(() => [...document.querySelectorAll('#routesList button')].find(button => /List users/.test(button.textContent)).id ||= 'listRoute');
    await page.click('#listRoute');
    await page.click('#switchButton');
    await page.waitFor(() => !document.getElementById('switchForm').hidden, { message: 'the switches form opens' });
    const layout = await page.evaluate(() => {
        const form = document.getElementById('switchForm').getBoundingClientRect();
        const panel = document.getElementById('routeView').getBoundingClientRect();
        return { fits: form.right <= panel.right + 1 && form.width > 200, focused: document.activeElement.id };
    });
    assert.deepEqual(layout, { fits: true, focused: 'switchOff' });

    await page.click('#switchOff');
    await page.click('#switchApply');
    await page.waitFor(() => /^Switched/.test(document.getElementById('switchStatusText').textContent), { message: 'the switch applies' });
    await page.waitFor(() => [...document.querySelectorAll('#routesList button')].find(button => /List users/.test(button.textContent))?.querySelector('.route-switch')?.textContent === 'off', { message: 'the route list shows the switch' });
    assert.equal((await fetch(mockURL + '/users')).status, 404, 'the switched-off route steps aside');
    const state = await (await fetch(adminURL + 'state')).json();
    assert.equal(state.switches, 1);

    await page.click('#switchClear');
    await page.waitFor(() => /^Switched back on/.test(document.getElementById('switchStatusText').textContent), { message: 'the switch clears' });
    await page.waitFor(() => !document.querySelector('#routesList .route-switch'), { message: 'the badge goes' });
    assert.equal((await fetch(mockURL + '/users')).status, 200);
    assert.equal(await page.evaluate(() => document.getElementById('switchOff').checked), false);
});

uiTest('route search narrows and groups the routes list', { width: 1280, height: 900 }, async (page) => {
    await page.waitFor(() => document.querySelectorAll('#routesList .route-button').length === 4, { message: 'four routes' });
    await page.click('#routeSearch');
    await page.type('post user');
    await page.waitFor(() => document.querySelectorAll('#routesList .route-button').length === 1, { message: 'one route matches' });
    assert.equal(await page.evaluate(() => document.getElementById('routeCount').textContent), '1 of 4');
    await page.press('Escape');
    await page.waitFor(() => document.querySelectorAll('#routesList .route-button').length === 4, { message: 'Escape clears the search' });
    await page.evaluate(() => {
        const select = document.getElementById('routeGroup');
        select.value = 'file';
        select.dispatchEvent(new Event('change'));
    });
    const groups = await page.evaluate(() => [...document.querySelectorAll('#routesList .route-group')].map(li => li.textContent));
    assert.equal(groups.length, 1);
    assert.match(groups[0], /api\.http · 4$/);
});

uiTest('an unmatched request highlights what each near miss needed', { width: 1280, height: 900 }, async (page) => {
    await traffic(['POST', '/users', { title: 'x' }]);
    await page.waitFor(() => document.querySelectorAll('#requestTableBody tr[data-id]').length === 1);
    await page.click('#requestTableBody tr[data-id]:first-child');
    await page.waitFor(() => document.querySelector('#mismatchDetails .near-miss'), { message: 'a near-miss card' });
    const card = await page.evaluate(() => ({
        open: document.getElementById('mismatchPanel').open,
        marks: [...document.querySelectorAll('#mismatchDetails .near-miss:first-child mark')].map(mark => mark.textContent),
    }));
    assert.equal(card.open, true, 'the explanation opens for an unmatched request');
    assert.ok(card.marks.includes('JSON name'), `the failed @json.name is highlighted (${card.marks})`);
    await page.evaluate(() => [...document.querySelectorAll('#mismatchDetails .near-miss:first-child button')].find(b => b.textContent === 'Similar misses').id ||= 'similarMisses');
    await page.click('#similarMisses');
    assert.equal(await page.evaluate(() => document.getElementById('filterInput').value), 'near:"Create user"');
    await page.evaluate(() => [...document.querySelectorAll('#mismatchDetails .near-miss:first-child button')].find(b => b.textContent === 'Inspect route').id ||= 'inspectRoute');
    await page.click('#inspectRoute');
    await page.waitFor(() => !document.getElementById('routeView').hidden, { message: 'the route sheet shows' });
});
