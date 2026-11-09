// A minimal Chrome DevTools Protocol client for the browser-level UI tests.
// It launches headless Chrome with --remote-debugging-pipe (no port, no
// WebSocket, no npm dependencies) and drives pages with real input events.

import { spawn, spawnSync } from 'node:child_process';
import { existsSync, mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

// findChrome returns the path of a Chrome or Chromium binary: $CHROME_BIN,
// else a well-known install location or a binary on $PATH, else null.
export function findChrome() {
    if (process.env.CHROME_BIN) return process.env.CHROME_BIN;
    const known = [
        '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
        '/Applications/Chromium.app/Contents/MacOS/Chromium',
        'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe',
    ];
    for (const path of known) if (existsSync(path)) return path;
    for (const name of ['google-chrome', 'google-chrome-stable', 'chromium', 'chromium-browser']) {
        const found = spawnSync('which', [name], { encoding: 'utf8' });
        if (found.status === 0 && found.stdout.trim()) return found.stdout.trim();
    }
    return null;
}

// Browser is a running headless Chrome.
export class Browser {
    static async launch(executable) {
        const profile = mkdtempSync(join(tmpdir(), 'mock-chrome-'));
        const args = [
            '--headless=new', '--remote-debugging-pipe', `--user-data-dir=${profile}`,
            '--no-first-run', '--no-default-browser-check', '--disable-extensions',
            '--disable-gpu', '--hide-scrollbars', '--mute-audio', '--disable-background-networking',
        ];
        // CI containers and Ubuntu's AppArmor profile block Chrome's sandbox.
        if (process.platform === 'linux' && (process.env.CI || process.getuid?.() === 0)) args.push('--no-sandbox');
        args.push('about:blank');
        const child = spawn(executable, args, { stdio: ['ignore', 'ignore', 'pipe', 'pipe', 'pipe'] });
        let stderr = '';
        child.stderr.on('data', chunk => { stderr = (stderr + chunk).slice(-4000); });
        const browser = new Browser(child, profile, () => stderr);
        await browser.send('Target.setDiscoverTargets', { discover: false });
        return browser;
    }

    constructor(child, profile, stderr) {
        this.child = child;
        this.profile = profile;
        this.stderr = stderr;
        this.nextId = 1;
        this.pending = new Map();
        this.listeners = new Set();
        this.buffer = '';
        this.closed = new Promise(resolve => child.once('exit', resolve));
        child.stdio[4].setEncoding('utf8');
        child.stdio[4].on('data', chunk => this.receive(chunk));
        child.once('exit', () => {
            for (const { reject } of this.pending.values()) reject(new Error(`Chrome exited: ${this.stderr()}`));
            this.pending.clear();
        });
    }

    receive(chunk) {
        this.buffer += chunk;
        let end;
        while ((end = this.buffer.indexOf('\0')) >= 0) {
            const message = JSON.parse(this.buffer.slice(0, end));
            this.buffer = this.buffer.slice(end + 1);
            if (message.id && this.pending.has(message.id)) {
                const { resolve, reject, method } = this.pending.get(message.id);
                this.pending.delete(message.id);
                if (message.error) reject(new Error(`${method}: ${message.error.message}`));
                else resolve(message.result);
            } else if (message.method) {
                for (const listener of this.listeners) listener(message);
            }
        }
    }

    send(method, params = {}, sessionId) {
        const id = this.nextId++;
        const message = { id, method, params };
        if (sessionId) message.sessionId = sessionId;
        return new Promise((resolve, reject) => {
            this.pending.set(id, { resolve, reject, method });
            this.child.stdio[3].write(JSON.stringify(message) + '\0');
        });
    }

    // newPage opens a page in a fresh browser context, so storage
    // (theme, divider sizes) never leaks between tests.
    async newPage({ width = 1280, height = 800, mobile = false, colorScheme = 'light' } = {}) {
        const { browserContextId } = await this.send('Target.createBrowserContext');
        const { targetId } = await this.send('Target.createTarget', { url: 'about:blank', browserContextId });
        const { sessionId } = await this.send('Target.attachToTarget', { targetId, flatten: true });
        const page = new Page(this, sessionId, browserContextId);
        await page.send('Page.enable');
        await page.send('Runtime.enable');
        await page.send('Log.enable');
        await page.setViewport(width, height, mobile);
        await page.send('Emulation.setEmulatedMedia', { features: [{ name: 'prefers-color-scheme', value: colorScheme }] });
        return page;
    }

    async close() {
        try { await this.send('Browser.close'); } catch { /* already gone */ }
        const timer = setTimeout(() => this.child.kill('SIGKILL'), 3000);
        await this.closed;
        clearTimeout(timer);
        rmSync(this.profile, { recursive: true, force: true });
    }
}

const keyCodes = { Enter: 13, Escape: 27, End: 35, Home: 36, ArrowLeft: 37, ArrowUp: 38, ArrowRight: 39, ArrowDown: 40, Tab: 9, '/': 191, ' ': 32 };

// Page is one tab. Errors logged by the page (console.error, uncaught
// exceptions, failed loads) collect in page.errors.
export class Page {
    constructor(browser, sessionId, browserContextId) {
        this.browser = browser;
        this.sessionId = sessionId;
        this.browserContextId = browserContextId;
        this.errors = [];
        this.listener = (message) => {
            if (message.sessionId !== this.sessionId) return;
            const { method, params } = message;
            if (method === 'Runtime.exceptionThrown') this.errors.push(params.exceptionDetails.exception?.description || params.exceptionDetails.text);
            if (method === 'Runtime.consoleAPICalled' && params.type === 'error') this.errors.push(params.args.map(arg => arg.value ?? arg.description).join(' '));
            if (method === 'Log.entryAdded' && params.entry.level === 'error') this.errors.push(`${params.entry.text} ${params.entry.url || ''}`.trim());
        };
        browser.listeners.add(this.listener);
    }

    send(method, params = {}) {
        return this.browser.send(method, params, this.sessionId);
    }

    waitForEvent(name, timeout = 10000) {
        return new Promise((resolve, reject) => {
            const timer = setTimeout(() => { this.browser.listeners.delete(listener); reject(new Error(`timed out waiting for ${name}`)); }, timeout);
            const listener = (message) => {
                if (message.sessionId !== this.sessionId || message.method !== name) return;
                clearTimeout(timer);
                this.browser.listeners.delete(listener);
                resolve(message.params);
            };
            this.browser.listeners.add(listener);
        });
    }

    async goto(url) {
        const loaded = this.waitForEvent('Page.loadEventFired');
        const { errorText } = await this.send('Page.navigate', { url });
        if (errorText) throw new Error(`navigate ${url}: ${errorText}`);
        await loaded;
    }

    async reload() {
        const loaded = this.waitForEvent('Page.loadEventFired');
        await this.send('Page.reload');
        await loaded;
    }

    setViewport(width, height, mobile = false) {
        return this.send('Emulation.setDeviceMetricsOverride', { width, height, deviceScaleFactor: 1, mobile });
    }

    // evaluate runs a function in the page and returns its JSON result.
    async evaluate(fn, ...args) {
        const expression = `(${fn})(...${JSON.stringify(args)})`;
        const { result, exceptionDetails } = await this.send('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true });
        if (exceptionDetails) throw new Error(`evaluate: ${exceptionDetails.exception?.description || exceptionDetails.text}`);
        return result.value;
    }

    // waitFor polls fn in the page until it returns a truthy value.
    async waitFor(fn, { timeout = 5000, message } = {}, ...args) {
        const deadline = Date.now() + timeout;
        let last;
        while (Date.now() < deadline) {
            last = await this.evaluate(fn, ...args);
            if (last) return last;
            await new Promise(resolve => setTimeout(resolve, 50));
        }
        throw new Error(`timed out: ${message || fn}`);
    }

    // center scrolls a selector's element into view and returns its center.
    async center(selector) {
        const point = await this.evaluate((selector) => {
            const element = document.querySelector(selector);
            if (!element) return null;
            element.scrollIntoView({ block: 'center', inline: 'center' });
            const box = element.getBoundingClientRect();
            return { x: box.left + box.width / 2, y: box.top + box.height / 2 };
        }, selector);
        if (!point) throw new Error(`no element matches ${selector}`);
        return point;
    }

    async mouse(type, x, y, extra = {}) {
        await this.send('Input.dispatchMouseEvent', { type, x, y, button: type === 'mouseMoved' ? 'none' : 'left', buttons: extra.buttons ?? 0, clickCount: extra.clickCount ?? 0 });
    }

    async click(selector, clickCount = 1) {
        const { x, y } = await this.center(selector);
        await this.mouse('mouseMoved', x, y);
        for (let count = 1; count <= clickCount; count++) {
            await this.mouse('mousePressed', x, y, { clickCount: count, buttons: 1 });
            await this.mouse('mouseReleased', x, y, { clickCount: count });
        }
    }

    // drag presses on a selector's element and moves the mouse by dx, dy.
    async drag(selector, dx, dy) {
        const { x, y } = await this.center(selector);
        await this.mouse('mouseMoved', x, y);
        await this.mouse('mousePressed', x, y, { clickCount: 1, buttons: 1 });
        for (let step = 1; step <= 5; step++) await this.send('Input.dispatchMouseEvent', { type: 'mouseMoved', x: x + dx * step / 5, y: y + dy * step / 5, button: 'left', buttons: 1 });
        await this.mouse('mouseReleased', x + dx, y + dy, { clickCount: 1 });
    }

    async press(key, modifiers = 0) {
        const code = keyCodes[key] ?? key.toUpperCase().charCodeAt(0);
        const text = key.length === 1 ? key : undefined;
        await this.send('Input.dispatchKeyEvent', { type: 'keyDown', key, windowsVirtualKeyCode: code, text, modifiers });
        await this.send('Input.dispatchKeyEvent', { type: 'keyUp', key, windowsVirtualKeyCode: code, modifiers });
    }

    async type(text) {
        await this.send('Input.insertText', { text });
    }

    async screenshot() {
        const { data } = await this.send('Page.captureScreenshot', { format: 'png' });
        return Buffer.from(data, 'base64');
    }

    async close() {
        this.browser.listeners.delete(this.listener);
        await this.browser.send('Target.disposeBrowserContext', { browserContextId: this.browserContextId }).catch(() => {});
    }
}
