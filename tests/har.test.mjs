import test from 'node:test';
import assert from 'node:assert/strict';
import { buildHAR } from '../static/har.mjs';
const fixture = () => ({ request: { method: 'POST', scheme: 'https', host: 'localhost:8443', url: '/x?q=%E2%9C%93', httpVersion: 'HTTP/2.0', headers: { 'Content-Type': ['text/plain'] }, startedAt: '2026-09-05T00:00:00Z', body: { text: 'é\n', size: 3, capturedSize: 3, truncated: false } }, response: { status: 200, statusText: 'OK', elapsedMs: 12, httpVersion: 'HTTP/2.0', headers: {}, body: { text: 'AAE=', encoding: 'base64', size: 2, capturedSize: 2, truncated: false } } });
test('HAR preserves TLS, byte sizes, raw body endings and binary encoding', () => {
 const entry = buildHAR([fixture()], 'test').log.entries[0];
 assert.equal(entry.request.url, 'https://localhost:8443/x?q=%E2%9C%93');
 assert.equal(entry.request.bodySize, 3);
 assert.equal(entry.request.postData.text, 'é\n');
 assert.equal(entry.response.content.encoding, 'base64');
 assert.equal(entry.time, Object.values(entry.timings).filter(n => n >= 0).reduce((a,b)=>a+b,0));
});
test('HAR separates truncation metadata and unknown request sizes', () => {
 const event=fixture();event.request.body.size=-1;event.request.body.truncated=true;event.response.body.truncated=true;event.response.body.size=100000;
 const entry=buildHAR([event]).log.entries[0];
 assert.equal(entry.request.bodySize,-1);assert.equal(entry.request._truncated,true);
 assert.equal(entry.response.content.size,100000);assert.equal(entry.response.content._capturedSize,2);
 assert.equal(entry.response.content.text,'AAE=');
});
