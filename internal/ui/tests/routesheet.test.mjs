import test from 'node:test';
import assert from 'node:assert/strict';
import { formatRoute } from '../static/inspector.mjs';
import { parseRouteSheet } from '../static/routesheet.mjs';

test('parseRouteSheet reads formatRoute text into a sign, facts, sections and a body', () => {
    const text = formatRoute({
        method: 'POST', path: '/v1/payments', query: 'dry=1', source: 'pay.http', line: 85, revision: 2, hits: 1, port: 8080,
        status: 201, delay: '0s', fault: 'reset 25%', rateLimit: '20/1s', matchHeaders: { Authorization: ['*'] },
        matchJson: { 'card.number': '~[0-9]+' }, headers: { Location: ['/x'] }, body: '{\n  "id": 1\n}\n\n{"second": true}',
    }, "curl 'http://localhost:8080/v1/payments'");
    const sheet = parseRouteSheet(text);
    assert.equal(sheet.method, 'POST');
    assert.equal(sheet.target, '/v1/payments?dry=1');
    assert.deepEqual(sheet.facts.slice(0, 4), [
        { label: 'Source', value: 'pay.http:85, revision 2' },
        { label: 'Matched', value: '1 request' },
        { label: 'Port', value: '8080' },
        { label: 'Response', value: '201, delay 0s, fault reset 25%' },
    ]);
    assert.deepEqual(sheet.sections.map(section => section.heading),
        ['Sample request', 'Request header requirements', 'Body requirements', 'Response headers', 'Variables']);
    assert.deepEqual(sheet.sections[1].lines, ['Authorization: *']);
    assert.deepEqual(sheet.sections[4].lines, ['(none)']);
    assert.equal(sheet.bodyLabel, 'Inline response body');
    assert.equal(sheet.body, '{\n  "id": 1\n}\n\n{"second": true}', 'blank lines in the body stay in the body');
});

test('parseRouteSheet handles file bodies and any-method routes', () => {
    const sheet = parseRouteSheet(formatRoute({ method: '*', path: '/files/*', file: 'data/big.bin', status: 200 }));
    assert.equal(sheet.method, '*');
    assert.equal(sheet.bodyLabel, 'Body file: data/big.bin');
    assert.equal(sheet.body, '(read from file when requested)');
});
