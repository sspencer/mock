import test from 'node:test';
import assert from 'node:assert/strict';
import { switchBadge, switchFields, switchPayload } from '../static/switches.mjs';

const route = { id: '3:1', name: 'Orders' };

test('switchFields reads the route\'s own switch, or starts empty', () => {
    assert.deepEqual(switchFields(route), { off: false, status: '', delay: '', fault: '', share: '100' });
    assert.deepEqual(switchFields({ ...route, switch: { off: true, status: 503, delay: '2s', fault: 'reset 25%' } }),
        { off: true, status: '503', delay: '2s', fault: 'reset', share: '25' });
    assert.equal(switchFields({ ...route, switch: { fault: 'none' } }).fault, 'none');
});

test('switchPayload builds a POST /faults body', () => {
    assert.deepEqual(switchPayload(route, { off: true, status: '', delay: '', fault: '', share: '100' }), { payload: { route: '3:1', off: true } });
    assert.deepEqual(switchPayload(route, { off: false, status: ' 503 ', delay: '1s..2s', fault: 'truncate', share: '50' }),
        { payload: { route: '3:1', status: 503, delay: '1s..2s', fault: 'truncate 50%' } });
    assert.deepEqual(switchPayload(route, { fault: 'hang', share: '' }).payload, { route: '3:1', fault: 'hang' });
    assert.deepEqual(switchPayload(route, { fault: 'none' }).payload, { route: '3:1', fault: 'none' });
    assert.match(switchPayload(route, { status: '99' }).error, /200 to 999/);
    assert.match(switchPayload(route, { fault: 'reset', share: '150' }).error, /0 to 100/);
    assert.match(switchPayload(route, { fault: 'explode' }).error, /Unknown fault/);
    assert.match(switchPayload(route, {}).error, /at least one/);
});

test('switchBadge marks switched routes', () => {
    assert.equal(switchBadge(route), null);
    assert.deepEqual(switchBadge({ ...route, override: 'off', switch: { off: true } }), { text: 'off', title: 'Switched at run time: off' });
    assert.equal(switchBadge({ ...route, override: 'delay 2s (all routes)' }).text, 'switched');
});
