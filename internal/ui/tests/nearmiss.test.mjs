import test from 'node:test';
import assert from 'node:assert/strict';
import { candidateRoute, failedLabel, reasonParts } from '../static/nearmiss.mjs';

test('reasonParts names the requirement that failed', () => {
    assert.deepEqual(reasonParts('Query role: expected "admin"; received "user"'), { subject: 'Query role', detail: 'expected "admin"; received "user"' });
    assert.deepEqual(reasonParts('Header X-Tenant: missing'), { subject: 'Header X-Tenant', detail: 'missing' });
    assert.deepEqual(reasonParts('Path parameter id: expected ~^\\d+$; received "x"'), { subject: 'Path parameter id', detail: 'expected ~^\\d+$; received "x"' });
    assert.deepEqual(reasonParts('JSON body: expected a JSON body; received none'), { subject: 'JSON body', detail: 'expected a JSON body; received none' });
    assert.deepEqual(reasonParts('Body: expected ~abc'), { subject: 'Body', detail: 'expected ~abc' });
    assert.equal(reasonParts('Expected method POST; received GET').subject, 'Method');
    assert.equal(reasonParts('Served on port 8081; request arrived on port 8080').subject, 'Port');
    assert.equal(reasonParts('Path "/a" does not match "/b"').subject, 'Path');
    assert.deepEqual(reasonParts('something else'), { subject: '', detail: 'something else' });
});

test('candidateRoute finds the route by source, line and name', () => {
    const routes = [{ name: 'A', source: 'a.http', line: 1 }, { name: 'B', source: 'a.http', line: 5 }];
    assert.equal(candidateRoute(routes, { name: 'B', source: 'a.http', line: 5 }), routes[1]);
    assert.equal(candidateRoute(routes, { name: 'B', source: 'a.http', line: 9 }), null);
    assert.equal(candidateRoute(undefined, { name: 'B' }), null);
});

test('failedLabel counts reasons', () => {
    assert.equal(failedLabel({ reasons: ['x'] }), '1 requirement failed');
    assert.equal(failedLabel({ reasons: ['x', 'y'] }), '2 requirements failed');
});
