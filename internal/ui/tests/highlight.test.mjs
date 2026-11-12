import test from 'node:test';
import assert from 'node:assert/strict';
import { highlightMessage, highlightJSON, tokenClass, maxHighlightBody } from '../static/highlight.mjs';

const joined = tokens => tokens.map(token => token.text).join('');
const kinds = tokens => tokens.filter(token => token.kind).map(token => [token.text, token.kind]);

test('a request is split into verb, target, version, header names and JSON', () => {
    const text = 'POST /users?x=1 HTTP/1.1\nContent-Type: application/json\nX-Time: 10:30\n\n{\n  "name": "Ada: \\"x\\"",\n  "age": -3.5e2,\n  "ok": true,\n  "none": null\n}';
    const tokens = highlightMessage(text);
    assert.equal(joined(tokens), text);
    assert.deepEqual(kinds(tokens), [
        ['POST', 'verb verb-post'], [' /users?x=1', 'target'], [' HTTP/1.1', 'version'],
        ['Content-Type', 'name'], ['X-Time', 'name'],
        ['"name"', 'name'], ['"Ada: \\"x\\""', 'string'], ['"age"', 'name'], ['-3.5e2', 'number'],
        ['"ok"', 'name'], ['true', 'literal'], ['"none"', 'name'], ['null', 'literal'],
    ]);
});

test('a response start line carries its status class', () => {
    const text = 'HTTP/1.1 404 Not Found\nContent-Type: text/plain\n\n404 page not found';
    const tokens = highlightMessage(text);
    assert.equal(joined(tokens), text);
    assert.deepEqual(kinds(tokens), [['HTTP/1.1 ', 'version'], ['404 Not Found', 'status status-4xx'], ['Content-Type', 'name']]);
});

test('text that is not an HTTP message stays plain', () => {
    assert.deepEqual(highlightMessage('request 1'), [{ text: 'request 1', kind: '' }]);
    assert.deepEqual(highlightMessage(''), []);
    assert.equal(kinds(highlightMessage('BREW /pot HTTP/1.1'))[0][1], 'verb verb-other');
});

test('a very large body is left uncolored', () => {
    const body = `[${'1,'.repeat(maxHighlightBody / 2)}1]`;
    const tokens = highlightMessage(`HTTP/1.1 200 OK\n\n${body}`);
    assert.deepEqual(tokens.at(-1), { text: body, kind: '' });
});

test('highlightJSON keeps broken JSON intact', () => {
    const text = '{"a": [1, "two", fals';
    assert.equal(joined(highlightJSON(text)), text);
});

test('tokenClass prefixes every part', () => {
    assert.equal(tokenClass('verb verb-get'), 'hl-verb hl-verb-get');
    assert.equal(tokenClass(''), '');
});
