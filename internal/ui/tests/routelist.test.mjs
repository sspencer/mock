import test from 'node:test';
import assert from 'node:assert/strict';
import { groupRoutes, routeCountLabel, routeMatchesSearch } from '../static/routelist.mjs';

const routes = [
    { method: 'GET', path: '/users', name: 'Users', source: 'users.http', port: 0, tags: ['users'] },
    { method: 'POST', path: '/orders', query: 'dry=1', name: 'Create order', source: 'orders.http', port: 9000, tags: ['orders', 'billing'] },
    { method: 'GET', path: '/health', name: 'Health', source: 'users.http', port: 0 },
];

test('routeMatchesSearch needs every word somewhere in the route', () => {
    assert.ok(routeMatchesSearch(routes[0], ''));
    assert.ok(routeMatchesSearch(routes[1], 'post ORDER'));
    assert.ok(routeMatchesSearch(routes[1], 'billing dry'));
    assert.ok(routeMatchesSearch(routes[2], 'users.http'));
    assert.ok(!routeMatchesSearch(routes[0], 'users post'));
});

test('groupRoutes groups by file, port or tag', () => {
    const labels = groups => groups.map(g => [g.label, g.routes.map(r => r.name)]);
    assert.deepEqual(labels(groupRoutes(routes)), [['', ['Users', 'Create order', 'Health']]]);
    assert.deepEqual(groupRoutes([], 'file'), []);
    assert.deepEqual(groupRoutes([]), []);
    assert.deepEqual(labels(groupRoutes(routes, 'file')), [['users.http', ['Users', 'Health']], ['orders.http', ['Create order']]]);
    assert.deepEqual(labels(groupRoutes(routes, 'port')), [['default port', ['Users', 'Health']], ['port 9000', ['Create order']]]);
    assert.deepEqual(labels(groupRoutes(routes, 'tag')), [
        ['@tag billing', ['Create order']], ['@tag orders', ['Create order']], ['@tag users', ['Users']], ['untagged', ['Health']],
    ]);
});

test('routeCountLabel shows the share while searching', () => {
    assert.equal(routeCountLabel(4, 4), '4');
    assert.equal(routeCountLabel(1, 4), '1 of 4');
});
