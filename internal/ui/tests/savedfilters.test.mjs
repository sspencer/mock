import test from 'node:test';
import assert from 'node:assert/strict';
import { readList, remember, togglePin, suggestions, filterFromHash, hashWithFilter } from '../static/savedfilters.mjs';

test('stored lists survive junk', () => {
    assert.deepEqual(readList('["4xx", "", 3, " 4xx ", "POST"]', 8), ['4xx', 'POST']);
    assert.deepEqual(readList('{"a":1}', 8), []);
    assert.deepEqual(readList('not json', 8), []);
    assert.deepEqual(readList(null, 8), []);
    assert.deepEqual(readList('["a","b","c"]', 2), ['a', 'b']);
});

test('recent filters move to the front and are capped', () => {
    assert.deepEqual(remember(['a', 'b', 'c'], ' b '), ['b', 'a', 'c']);
    assert.deepEqual(remember(['a', 'b'], 'c', 2), ['c', 'a']);
    assert.deepEqual(remember(['a'], '  '), ['a']);
});

test('pinning toggles and suggestions put pinned filters first', () => {
    let pinned = togglePin([], 'unmatched');
    pinned = togglePin(pinned, 'route:x');
    assert.deepEqual(pinned, ['unmatched', 'route:x']);
    assert.deepEqual(togglePin(pinned, 'unmatched'), ['route:x']);
    assert.deepEqual(suggestions(['unmatched'], ['4xx', 'unmatched']), [
        { value: 'unmatched', label: 'Pinned' },
        { value: '4xx', label: 'Recent' },
    ]);
});

test('the filter round-trips through the URL fragment', () => {
    const hash = hashWithFilter('', 'route:"User detail" -2xx');
    assert.equal(hash, '#filter=route%3A%22User%20detail%22%20-2xx');
    assert.equal(filterFromHash(hash), 'route:"User detail" -2xx');
    assert.equal(hashWithFilter('#x=1&filter=a', ''), '#x=1');
    assert.equal(hashWithFilter('#filter=a', ' '), '');
    assert.equal(filterFromHash('#a+b'), '');
    assert.equal(filterFromHash(''), '');
});
