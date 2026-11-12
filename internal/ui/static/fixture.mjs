// Editing a route's @fixture JSON file: parsing with error positions, and
// immutable edits addressed by a path of object keys and array indexes.
// Pure functions so the editor's logic can be unit tested without the page.

// Fixtures with more nodes than this are edited as JSON text only.
export const maxTreeNodes = 2000;

// parseFixture returns { value } or { error, line, column } for JSON text.
// editableFixture reports whether the fixture editor can open a @fixture
// file: CSV (.csv) and NDJSON (.ndjson, .jsonl) fixtures are read-only.
export function editableFixture(name = '') {
    return !/\.(csv|ndjson|jsonl)$/i.test(name);
}

export function parseFixture(text) {
    try {
        return { value: JSON.parse(text) };
    } catch (error) {
        const source = String(text);
        const message = String(error.message || error);
        let line = null, column = null;
        const lineMatch = message.match(/line (\d+) column (\d+)/);
        const positionMatch = message.match(/position (\d+)/);
        if (lineMatch) {
            line = Number(lineMatch[1]); column = Number(lineMatch[2]);
        } else if (positionMatch) {
            const before = source.slice(0, Number(positionMatch[1]));
            line = before.split('\n').length;
            column = before.length - before.lastIndexOf('\n');
        } else if (/end of (JSON )?input/i.test(message)) {
            const lines = source.split('\n');
            line = lines.length; column = lines[lines.length - 1].length + 1;
        }
        const where = line ? ` at line ${line}, column ${column}` : '';
        return { error: `Invalid JSON${where}: ${message.replace(/\s*\(line \d+ column \d+\)/, '')}`, line, column };
    }
}

// losslessIssue returns why parsing text and saving it with formatFixture
// would change its data, or '' when it would not. JavaScript numbers lose
// integers beyond 2^53 and the spelling of numbers such as 1.0 or 1e3,
// objects put integer-like keys ("1", "20") first, and a repeated key keeps
// only its last value. text must be valid JSON. The tree view refuses such
// files so a save cannot silently rewrite them; the JSON view saves the text
// as written.
export function losslessIssue(text) {
    const stack = [];
    for (let i = 0; i < text.length;) {
        const c = text[i];
        if (c === '"') {
            let j = i + 1;
            while (j < text.length && text[j] !== '"') j += text[j] === '\\' ? 2 : 1;
            const top = stack[stack.length - 1];
            if (top?.keys && top.expectKey) {
                top.keys.push(JSON.parse(text.slice(i, j + 1)));
                top.expectKey = false;
            }
            i = j + 1;
            continue;
        }
        if (c === '-' || (c >= '0' && c <= '9')) {
            let j = i;
            while (j < text.length && /[-+.eE0-9]/.test(text[j])) j++;
            const number = text.slice(i, j);
            const saved = String(Number(number));
            if (saved !== number) return `The number ${number} would be saved as ${saved}.`;
            i = j;
            continue;
        }
        if (c === '{') stack.push({ keys: [], expectKey: true });
        else if (c === '[') stack.push({});
        else if (c === ',' && stack[stack.length - 1]?.keys) stack[stack.length - 1].expectKey = true;
        else if (c === '}' || c === ']') {
            const issue = keyIssue(stack.pop().keys);
            if (issue) return issue;
        }
        i++;
    }
    return '';
}

function keyIssue(keys) {
    if (!keys) return '';
    const seen = new Set();
    for (const key of keys) {
        if (seen.has(key)) return `The field "${key}" appears more than once; only its last value would be kept.`;
        seen.add(key);
    }
    const order = Object.keys(Object.fromEntries(keys.map(key => [key, true])));
    const moved = order.findIndex((key, index) => key !== keys[index]);
    if (moved >= 0) return `The field "${order[moved]}" would move to the front of its object.`;
    return '';
}

// formatFixture returns the text a fixture is saved as.
export const formatFixture = (value) => JSON.stringify(value, null, 2) + '\n';

export function valueType(value) {
    if (value === null) return 'null';
    if (Array.isArray(value)) return 'array';
    return typeof value === 'object' ? 'object' : typeof value;
}

export function countNodes(value) {
    if (value && typeof value === 'object') {
        let count = 1;
        for (const child of Object.values(value)) count += countNodes(child);
        return count;
    }
    return 1;
}

export function getAt(root, path) {
    return path.reduce((value, key) => value?.[key], root);
}

// update returns a copy of root with the value at path replaced by
// change(value). Only the containers along the path are copied.
function update(root, path, change) {
    if (!path.length) return change(root);
    const [key, ...rest] = path;
    const copy = Array.isArray(root) ? [...root] : { ...root };
    copy[key] = update(root[key], rest, change);
    return copy;
}

export function setAt(root, path, value) {
    return update(root, path, () => value);
}

export function removeAt(root, path) {
    if (!path.length) throw new Error('The top-level value cannot be removed.');
    return update(root, path.slice(0, -1), (parent) => {
        const key = path[path.length - 1];
        if (Array.isArray(parent)) return parent.filter((_, index) => index !== key);
        const copy = { ...parent };
        delete copy[key];
        return copy;
    });
}

// duplicateAt inserts a copy of an array element right after it.
export function duplicateAt(root, path) {
    const key = path[path.length - 1];
    return update(root, path.slice(0, -1), (parent) => {
        if (!Array.isArray(parent)) throw new Error('Only array items can be duplicated.');
        return [...parent.slice(0, key + 1), structuredClone(parent[key]), ...parent.slice(key + 1)];
    });
}

// renameAt renames an object member, keeping its position.
export function renameAt(root, path, name) {
    const key = path[path.length - 1];
    if (name === key) return root;
    if (!name) throw new Error('A field name cannot be empty.');
    return update(root, path.slice(0, -1), (parent) => {
        if (Array.isArray(parent) || !parent || typeof parent !== 'object') throw new Error('Only object fields can be renamed.');
        if (Object.hasOwn(parent, name)) throw new Error(`There is already a field named "${name}".`);
        return Object.fromEntries(Object.entries(parent).map(([k, v]) => [k === key ? name : k, v]));
    });
}

// addChild appends to the object or array at path: a new "field" member
// with an empty string, or a copy of the array's last item (else "").
export function addChild(root, path) {
    const target = getAt(root, path);
    if (Array.isArray(target)) {
        const item = target.length ? structuredClone(target[target.length - 1]) : '';
        return setAt(root, path, [...target, item]);
    }
    if (target && typeof target === 'object') {
        let name = 'field';
        for (let n = 2; Object.hasOwn(target, name); n++) name = `field${n}`;
        return setAt(root, path, { ...target, [name]: '' });
    }
    throw new Error('Fields can only be added to an object or an array.');
}

// convertValue turns an editor's text into a value of the chosen type:
// { value } or { error }.
export function convertValue(text, type) {
    switch (type) {
    case 'string': return { value: String(text) };
    case 'number': {
        const trimmed = String(text).trim();
        const value = Number(trimmed);
        if (!trimmed || !Number.isFinite(value)) return { error: 'Enter a number, such as 42 or 1.5.' };
        return { value };
    }
    case 'boolean':
        if (text === 'true' || text === true) return { value: true };
        if (text === 'false' || text === false) return { value: false };
        return { error: 'Choose true or false.' };
    case 'null': return { value: null };
    case 'object': return { value: {} };
    case 'array': return { value: [] };
    }
    return { error: `Unknown type ${type}.` };
}

// retype converts a value to another type, keeping what carries over: a
// number's digits as a string, a numeric string as a number, and so on.
export function retype(value, type) {
    if (valueType(value) === type) return value;
    if (type === 'string') return value == null || typeof value === 'object' ? '' : String(value);
    if (type === 'number') { const n = Number(value); return typeof value !== 'object' && Number.isFinite(n) && value !== '' ? n : 0; }
    if (type === 'boolean') return value === true || value === 'true' || (typeof value === 'number' && value !== 0);
    return convertValue('', type).value;
}

// pathLabel names a path for labels and messages: users[0].name.
export function pathLabel(path) {
    if (!path.length) return 'top level';
    return path.map((key, index) => {
        if (typeof key === 'number') return `[${key}]`;
        if (/^[A-Za-z_$][\w$]*$/.test(key)) return index ? `.${key}` : key;
        return `[${JSON.stringify(key)}]`;
    }).join('');
}
