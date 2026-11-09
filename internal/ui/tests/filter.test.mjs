import test from 'node:test';
import assert from 'node:assert/strict';
import { filterTerms, matchesFilter, routeFilter } from '../static/filter.mjs';

const ev = (method, status, extra = {}) => ({ request: { method, url: '/x' }, response: { status }, match: {}, ...extra });
const route = name => ({ match: { route: { name } } });
const show = (text, event, haystack = '') => matchesFilter(filterTerms(text), event, haystack);

test('status classes, methods and match state', () => {
    assert.ok(show('4xx', ev('GET', 404)));
    assert.ok(!show('4xx', ev('GET', 500)));
    assert.ok(show('5XX post', ev('POST', 502)));
    assert.ok(!show('get', ev('POST', 200)));
    assert.ok(show('unmatched', ev('GET', 404)));
    assert.ok(!show('unmatched', ev('GET', 200, route('A'))));
    assert.ok(show('matched', ev('GET', 200, route('A'))));
    assert.ok(!show('unmatched', ev('POST', 200, { callback: { trigger: 1 } })));
    assert.ok(show('-2xx', ev('GET', 404)));
    assert.ok(!show('-2xx', ev('GET', 204)));
});

test('route terms match the answering route by name', () => {
    assert.ok(show('route:"User detail"', ev('GET', 200, route('User detail'))));
    assert.ok(show('route:users', ev('GET', 200, route('Users'))));
    assert.ok(!show('route:user', ev('GET', 200, route('Users'))));
    assert.ok(!show('route:a', ev('GET', 404)));
    assert.deepEqual(filterTerms('route:'), []);
    assert.deepEqual(filterTerms('route:"open'), [{ negate: false, prefix: 'route', value: 'open' }]);
});

test('other terms search the text', () => {
    assert.ok(show('users proxied', ev('GET', 200), 'get /users 200 proxied'));
    assert.ok(!show('orders', ev('GET', 200), 'get /users 200'));
    assert.ok(show('', ev('GET', 200)));
});

test('routeFilter quotes names that need it', () => {
    assert.equal(routeFilter('Users'), 'route:Users');
    assert.equal(routeFilter('User detail'), 'route:"User detail"');
    assert.equal(routeFilter('Say "hi"'), 'route:"Say hi"');
    assert.ok(show(routeFilter('User detail'), ev('GET', 200, route('User detail'))));
});
