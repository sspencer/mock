import test from 'node:test';
import assert from 'node:assert/strict';
import { resendDraft, parseHeaderLines, buildResend } from '../static/resend.mjs';

const request = (overrides = {}) => ({
    request: {
        method: 'POST', url: '/orders?x=1', host: 'localhost:8080',
        headers: { Host: ['localhost:8080'], 'Content-Length': ['2'], Accept: ['a', 'b'] },
        body: { text: '{}', size: 2, capturedSize: 2, truncated: false },
        ...overrides,
    },
});

test('resendDraft copies the request without recomputed headers', () => {
    assert.deepEqual(resendDraft(request()), {
        method: 'POST', url: '/orders?x=1', host: 'localhost:8080', headers: 'Accept: a\nAccept: b',
        body: '{}', encoding: '', bodyEditable: true,
    });
    const binary = resendDraft(request({ body: { text: 'AAE=', encoding: 'base64', size: 2, capturedSize: 2 } }));
    assert.equal(binary.bodyEditable, false);
    assert.equal(binary.encoding, 'base64');
});

test('resendDraft refuses requests it cannot replay faithfully', () => {
    assert.match(resendDraft(undefined).error, /Select/);
    assert.match(resendDraft({ ...request(), callback: { trigger: 1 } }).error, /Callbacks/);
    assert.match(resendDraft(request({ body: { text: 'x', truncated: true } })).error, /not captured completely/);
});

test('parseHeaderLines reads repeated headers and rejects malformed lines', () => {
    assert.deepEqual(parseHeaderLines('A: 1\r\n\nA: 2\nB:x:y'), { A: ['1', '2'], B: ['x:y'] });
    assert.throws(() => parseHeaderLines('no colon'), /line 1/);
    assert.throws(() => parseHeaderLines('ok: 1\nBad Name: 2'), /line 2/);
});

test('buildResend validates the fields and keeps binary bodies unchanged', () => {
    const draft = resendDraft(request());
    assert.deepEqual(buildResend(draft, { method: ' get ', url: '/a', headers: '', body: '' }, 3),
        { from: 3, method: 'GET', url: '/a', host: 'localhost:8080', headers: {} });
    assert.throws(() => buildResend(draft, { method: 'G E T', url: '/a' }, 3), /method/);
    assert.throws(() => buildResend(draft, { method: 'GET', url: 'http://x/' }, 3), /starts with \//);
    const binary = resendDraft(request({ body: { text: 'AAE=', encoding: 'base64' } }));
    assert.deepEqual(buildResend(binary, { method: 'PUT', url: '/b', headers: '', body: 'ignored' }, 4),
        { from: 4, method: 'PUT', url: '/b', host: 'localhost:8080', headers: {}, body: 'AAE=', encoding: 'base64' });
});
