import test from 'node:test';
import assert from 'node:assert/strict';
import {
    addChild, convertValue, countNodes, duplicateAt, formatFixture, getAt, losslessIssue,
    parseFixture, pathLabel, removeAt, renameAt, retype, setAt, valueType,
} from '../static/fixture.mjs';

test('parseFixture returns the value or an error with a position', () => {
    assert.deepEqual(parseFixture('{"a": [1, 2]}'), { value: { a: [1, 2] } });
    const bad = parseFixture('{\n  "a": 1,\n  oops\n}');
    assert.equal(bad.line, 3);
    assert.ok(bad.column >= 1);
    assert.match(bad.error, /^Invalid JSON at line 3, column \d+: /);
    const truncated = parseFixture('{"a": ');
    assert.equal(truncated.line, 1);
    assert.match(truncated.error, /^Invalid JSON at line 1/);
});

test('formatFixture writes two-space JSON with a trailing newline', () => {
    assert.equal(formatFixture({ a: [1] }), '{\n  "a": [\n    1\n  ]\n}\n');
});

test('valueType and countNodes describe values', () => {
    assert.deepEqual([null, [], {}, 1, 'x', true].map(valueType), ['null', 'array', 'object', 'number', 'string', 'boolean']);
    assert.equal(countNodes({ a: [1, 2], b: { c: null } }), 6);
    assert.equal(countNodes(3), 1);
});

test('setAt and removeAt copy only the containers on the path', () => {
    const root = { users: [{ name: 'Ada' }, { name: 'Bob' }], meta: { page: 1 } };
    const next = setAt(root, ['users', 1, 'name'], 'Grace');
    assert.equal(getAt(next, ['users', 1, 'name']), 'Grace');
    assert.equal(root.users[1].name, 'Bob');
    assert.equal(next.meta, root.meta);
    assert.equal(next.users[0], root.users[0]);
    assert.deepEqual(removeAt(next, ['users', 0]).users, [{ name: 'Grace' }]);
    assert.deepEqual(removeAt(root, ['meta']), { users: root.users });
    assert.throws(() => removeAt(root, []), /top-level/);
    assert.equal(setAt(root, [], 5), 5);
});

test('duplicateAt copies an array item after itself', () => {
    const root = { list: [{ id: 1 }, { id: 2 }] };
    const next = duplicateAt(root, ['list', 0]);
    assert.deepEqual(next.list, [{ id: 1 }, { id: 1 }, { id: 2 }]);
    assert.notEqual(next.list[0], next.list[1]);
    assert.throws(() => duplicateAt(root, ['list']), /array items/);
});

test('renameAt keeps the field order and refuses duplicates', () => {
    const root = { a: 1, b: 2, c: 3 };
    assert.deepEqual(Object.keys(renameAt(root, ['b'], 'z')), ['a', 'z', 'c']);
    assert.equal(renameAt(root, ['b'], 'b'), root);
    assert.throws(() => renameAt(root, ['b'], 'a'), /already a field named "a"/);
    assert.throws(() => renameAt(root, ['b'], ''), /cannot be empty/);
    assert.throws(() => renameAt({ list: [1] }, ['list', 0], 'x'), /object fields/);
});

test('addChild adds a unique field or copies the last array item', () => {
    assert.deepEqual(addChild({ field: 1 }, []), { field: 1, field2: '' });
    assert.deepEqual(addChild({ list: [{ id: 1 }] }, ['list']).list, [{ id: 1 }, { id: 1 }]);
    assert.deepEqual(addChild([], []), ['']);
    assert.throws(() => addChild({ a: 1 }, ['a']), /object or an array/);
});

test('convertValue checks text against the chosen type', () => {
    assert.deepEqual(convertValue('42', 'number'), { value: 42 });
    assert.deepEqual(convertValue(' 1.5 ', 'number'), { value: 1.5 });
    assert.match(convertValue('abc', 'number').error, /Enter a number/);
    assert.match(convertValue('', 'number').error, /Enter a number/);
    assert.deepEqual(convertValue('false', 'boolean'), { value: false });
    assert.deepEqual(convertValue('42', 'string'), { value: '42' });
    assert.deepEqual(convertValue('x', 'null'), { value: null });
    assert.deepEqual(convertValue('', 'array'), { value: [] });
});

test('retype carries values across types where it can', () => {
    assert.equal(retype(42, 'string'), '42');
    assert.equal(retype('7', 'number'), 7);
    assert.equal(retype('x', 'number'), 0);
    assert.equal(retype('true', 'boolean'), true);
    assert.equal(retype({ a: 1 }, 'string'), '');
    assert.deepEqual(retype('x', 'object'), {});
    assert.equal(retype(null, 'null'), null);
});

test('pathLabel names a path', () => {
    assert.equal(pathLabel([]), 'top level');
    assert.equal(pathLabel(['users', 0, 'name']), 'users[0].name');
    assert.equal(pathLabel([2, 'first name']), '[2]["first name"]');
});

test('losslessIssue flags data the tree view would rewrite', () => {
    assert.equal(losslessIssue('{"a": [1, -2.5, 3e-7, "1.0", {"b": null}], "c": "x\\"y"}'), '');
    assert.equal(losslessIssue('{"id": 12345678901234567890}'), 'The number 12345678901234567890 would be saved as 12345678901234567000.');
    assert.match(losslessIssue('[1.0]'), /1\.0 would be saved as 1\b/);
    assert.match(losslessIssue('[1e3]'), /1e3 would be saved as 1000/);
    assert.match(losslessIssue('{"a": 1, "b": {"a": 2, "a": 3}}'), /"a" appears more than once/);
    assert.match(losslessIssue('{"name": "x", "10": 1}'), /"10" would move to the front/);
    assert.equal(losslessIssue('{"1": 1, "2": 2, "name": "x"}'), '');
    assert.equal(losslessIssue('{"a": {"x": 1}, "b": {"x": 2}}'), '');
});
