import test from 'node:test';
import assert from 'node:assert/strict';
import { buildCurl, formatMessage, formatRoute, bodyNotice } from '../static/inspector.mjs';
import { attachSplitter, clampPercent } from '../static/layout.mjs';

const message = {
    method: 'POST', url: '/users?q=one', scheme: 'https', host: 'example.test', httpVersion: 'HTTP/1.1',
    headers: { 'Content-Type': ['application/json'], 'Content-Length': ['7'], 'X-Name': ["O'Brien"] },
    body: { text: '{"a":1}', capturedSize: 7, size: 7, truncated: false },
};

test('Pretty formats valid JSON while Raw preserves body bytes', () => {
    assert.match(formatMessage(message, true, true), /\{\n  "a": 1\n\}/);
    assert.ok(formatMessage(message, false, true).endsWith('{"a":1}'));
    const truncated = { ...message, body: { ...message.body, text: '{"a":', truncated: true } };
    assert.ok(formatMessage(truncated, true, true).endsWith('{"a":'));
    assert.match(bodyNotice(truncated.body), /Truncated/);
});

test('Copy cURL quotes shell metacharacters and omits transport-managed headers', () => {
    const command = buildCurl(message);
    assert.ok(command.includes("'X-Name: O'\\''Brien'"));
    assert.ok(command.startsWith("printf '%s' '{\"a\":1}' | curl"));
    assert.ok(command.includes("--data-binary @-"));
    assert.ok(buildCurl({ ...message, body: { ...message.body, text: "@private-file" } }).startsWith("printf '%s' '@private-file' | curl"));
    assert.ok(command.includes("'https://example.test/users?q=one'"));
    assert.ok(!command.includes('Content-Length'));
    assert.throws(() => buildCurl({ ...message, body: { ...message.body, truncated: true } }), /not fully captured/);
    assert.throws(() => buildCurl({ ...message, body: { ...message.body, encoding: 'base64' } }), /Binary/);
});

test('Route view includes source, requirements, file and template', () => {
    const text = formatRoute({ method: 'GET', path: '/x', name: 'Example', source: 'test.http', line: 3, revision: 2, status: 201, delay: '1s', file: 'body.json', matchHeaders: { Authorization: ['*'] }, body: '{"x":1}' });
    for (const value of ['test.http:3', 'Authorization: *', '201', '1s', 'body.json', '{"x":1}']) assert.ok(text.includes(value));
});

test('Splitter persists clamped pointer and keyboard changes and restores defaults', () => {
    class Handle extends EventTarget {
        values = {};
        classList = { add() {}, remove() {} };
        setAttribute(k, v) { this.values[k] = v; }
        setPointerCapture() {}
    }
    const handle = new Handle();
    const styles = {}; const saved = {};
    const container = { style: { setProperty: (k, v) => styles[k] = v }, getBoundingClientRect: () => ({ width: 1000, height: 500, left: 100, top: 0 }) };
    attachSplitter(handle, container, { axis: 'x', property: '--width', key: 'width', initial: 45 }, { get: () => 'invalid', set: (k, v) => saved[k] = v });
    const emit = (type, values) => { const event = new Event(type, { cancelable: true }); Object.assign(event, values); handle.dispatchEvent(event); };
    assert.equal(styles['--width'], '45%');
    emit('keydown', { key: 'ArrowRight', shiftKey: true });
    assert.equal(styles['--width'], '55%');
    emit('pointerdown', { button: 0, pointerId: 1 });
    emit('pointermove', { clientX: 1100 });
    assert.equal(saved.width, '80');
    emit('pointerup', {});
    emit('dblclick', {});
    assert.equal(saved.width, '45');
    assert.equal(clampPercent(null, 45), 45);
    assert.equal(clampPercent(-100, 45), 20);
});
