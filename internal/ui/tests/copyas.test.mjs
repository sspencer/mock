import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { COPY_FORMATS, buildRequestAs, buildFetch, buildHTTPie, buildGo, buildRESTClient } from '../static/copyas.mjs';

const post = {
    scheme: 'http', host: '127.0.0.1:8080', url: '/users?x=1', method: 'POST',
    headers: { 'Content-Type': ['application/json'], 'Content-Length': ['13'], 'X-Tag': ['a', 'b'] },
    body: { text: '{"name":"Al"}', capturedSize: 13 },
};
const get = { scheme: 'http', host: 'localhost:8080', url: '/', method: 'GET', headers: { Empty: [''] } };

test('fetch joins repeated headers and keeps the body', () => {
    assert.equal(buildFetch(post), [
        'await fetch("http://127.0.0.1:8080/users?x=1", {',
        '  method: "POST",',
        '  headers: {',
        '    "Content-Type": "application/json",',
        '    "X-Tag": "a, b",',
        '  },',
        '  body: "{\\"name\\":\\"Al\\"}",',
        '});',
    ].join('\n'));
});

test('HTTPie sends the raw body and empty headers with ;', () => {
    assert.equal(buildHTTPie(post), `http --raw '{"name":"Al"}' POST 'http://127.0.0.1:8080/users?x=1' 'Content-Type:application/json' 'X-Tag:a' 'X-Tag:b'`);
    assert.equal(buildHTTPie(get), `http --ignore-stdin GET 'http://localhost:8080/' 'Empty;'`);
});

test('Go uses method constants and a raw string body', () => {
    const code = buildGo(post);
    assert.match(code, /^req, err := http.NewRequest\(http.MethodPost, "http:\/\/127.0.0.1:8080\/users\?x=1", strings.NewReader\(`\{"name":"Al"\}`\)\)/);
    assert.match(code, /req.Header.Add\("X-Tag", "b"\)/);
    assert.doesNotMatch(code, /Content-Length/);
    assert.match(buildGo({ ...get, method: 'PURGE' }), /http.NewRequest\("PURGE", "http:\/\/localhost:8080\/", nil\)/);
    assert.match(buildGo({ ...post, body: { text: 'a`b', capturedSize: 3 } }), /strings.NewReader\("a`b"\)/);
});

test('REST Client request is the request half of an .http section', () => {
    assert.equal(buildRESTClient(post), 'POST http://127.0.0.1:8080/users?x=1\nContent-Type: application/json\nX-Tag: a\nX-Tag: b\n\n{"name":"Al"}');
    assert.throws(() => buildRESTClient({ ...post, body: { text: 'a\n### b', capturedSize: 7 } }), /###/);
});

test('incomplete or binary bodies and unknown formats are refused', () => {
    assert.throws(() => buildRequestAs('fetch', { ...post, body: { truncated: true } }), /not fully captured/);
    assert.throws(() => buildRequestAs('go', { ...post, body: { encoding: 'base64', capturedSize: 3 } }), /Binary/);
    assert.throws(() => buildRequestAs('fetch', { method: 'GET' }), /no captured URL/);
    assert.throws(() => buildRequestAs('xml', post), /Unknown format/);
});

test('the Copy as menu offers every format', () => {
    const html = readFileSync(new URL('../static/index.html', import.meta.url), 'utf8');
    for (const format of COPY_FORMATS) assert.match(html, new RegExp(`<option value="${format.id}">${format.label}</option>`));
});
