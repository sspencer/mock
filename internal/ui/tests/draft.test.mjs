import test from 'node:test';
import assert from 'node:assert/strict';
import { buildRouteDraft, routePath } from '../static/draft.mjs';

const body = text => ({ text, size: text.length, capturedSize: text.length, truncated: false });
const unmatched = (request, extra = {}) => ({
    request: { method: 'GET', url: '/', host: 'localhost:8080', headers: {}, body: body(''), ...request },
    response: { status: 404, headers: {}, body: body('') },
    match: { revision: 1 },
    ...extra,
});

test('a plain GET drafts a placeholder route with query matchers', () => {
    assert.equal(buildRouteDraft(unmatched({ url: '/users/42?page=2&page=3&sort=name' }), { mockPort: 8080 }),
        '### [200] GET /users/42\n@query.page = *\n@query.sort = *\nGET /users/42\nContent-Type: application/json\n\n{}\n');
});

test('JSON bodies require their top-level fields', () => {
    const draft = buildRouteDraft(unmatched({
        method: 'POST', url: '/users',
        headers: { 'content-type': ['application/json'], Authorization: ['Bearer x'] },
        body: body('{"name":"Alex","age":3,"nick":null,"a.b":1,"tags":[]}'),
    }));
    assert.equal(draft, '### [200] POST /users\n@header.Authorization = *\n@json.name = *\n@json.age = *\n@json.tags = *\nPOST /users\nContent-Type: application/json\n\n{}\n');
});

test('non-object or invalid JSON bodies add no matchers', () => {
    for (const text of ['[1,2]', 'not json', '"x"']) {
        assert.doesNotMatch(buildRouteDraft(unmatched({ method: 'POST', body: body(text) })), /@json/);
    }
});

test('form bodies require their field names', () => {
    const draft = buildRouteDraft(unmatched({
        method: 'POST', url: '/login',
        headers: { 'Content-Type': ['application/x-www-form-urlencoded; charset=utf-8'] },
        body: body('user=a&pass=b&user=c&bad%20name=1'),
    }));
    assert.match(draft, /\n@form\.user = \*\n@form\.pass = \*\nPOST \/login\n/);
    assert.doesNotMatch(draft, /bad/);
});

test('matchers are capped per kind', () => {
    const query = Array.from({ length: 12 }, (_, i) => `q${i}=1`).join('&');
    const fields = Object.fromEntries(Array.from({ length: 12 }, (_, i) => [`f${i}`, i]));
    const draft = buildRouteDraft(unmatched({ method: 'PUT', url: `/x?${query}`, body: body(JSON.stringify(fields)) }));
    assert.equal(draft.match(/@query\./g).length, 8);
    assert.equal(draft.match(/@json\./g).length, 8);
});

test('route paths escape wildcard, parameter and placeholder syntax', () => {
    assert.equal(routePath('/files/*/:id/{{x}}/a:b'), '/files/%2A/%3Aid/%7B%7Bx%7D%7D/a:b');
    assert.equal(routePath(''), '/');
    assert.match(buildRouteDraft(unmatched({ url: '/a/*' })), /^### \[200\] GET \/a\/%2A\nGET \/a\/%2A\n/);
});

test('@port is set when the request arrived on a non-default port', () => {
    assert.match(buildRouteDraft(unmatched({ host: 'localhost:9090' }), { mockPort: 8080 }), /\n@port = 9090\n/);
    assert.doesNotMatch(buildRouteDraft(unmatched({ host: 'localhost:8080' }), { mockPort: 8080 }), /@port/);
    assert.doesNotMatch(buildRouteDraft(unmatched({ host: 'localhost' }), { mockPort: 8080 }), /@port/);
});

test('a proxied request reuses the upstream response', () => {
    const event = unmatched({ url: '/repos' }, {
        response: { status: 201, headers: { 'Content-Type': ['application/json'] }, body: body('{"id":1,"t":"{{x}}"}\n\n') },
        match: { revision: 1, upstream: 'https://api.example.test' },
    });
    assert.equal(buildRouteDraft(event),
        '### [201] GET /repos\n@template = off\nGET /repos\nContent-Type: application/json\n\n{"id":1,"t":"{{x}}"}\n');
});

test('unusable upstream responses fall back to the placeholder', () => {
    const upstream = response => unmatched({}, { response: { status: 200, headers: {}, body: body(''), ...response }, match: { upstream: 'http://u' } });
    const cases = [
        { body: { ...body('AAEC'), encoding: 'base64' } },
        { body: { ...body('{"a":'), truncated: true } },
        { body: body('ok\n### not a section') },
        { body: body('< ./file.json') },
        { status: 0, fault: 'reset' },
    ];
    for (const response of cases) {
        assert.match(buildRouteDraft(upstream(response)), /^### \[200\] GET \/\nGET \/\nContent-Type: application\/json\n\n\{\}\n$/);
    }
});

test('an event without a method cannot be drafted', () => {
    assert.throws(() => buildRouteDraft({ request: {} }), /no captured method/);
    assert.throws(() => buildRouteDraft(undefined), /no captured method/);
});
