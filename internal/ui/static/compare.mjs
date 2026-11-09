// Compare two logged requests side by side: the overview (method, URL,
// status, route), query parameters, headers and bodies. Pure functions so
// the diff can be unit tested without the page.

// Bodies with more line pairs than this are compared for equality only.
const maxDiffCells = 4_000_000;

// fieldRows returns { name, a, b, same } for each name in either record,
// sorted by name. Values are arrays of strings.
export function fieldRows(left = {}, right = {}) {
    const names = new Map();
    for (const name of [...Object.keys(left || {}), ...Object.keys(right || {})]) {
        const key = name.toLowerCase();
        if (!names.has(key)) names.set(key, name);
    }
    const lookup = (record, key) => {
        for (const [name, values] of Object.entries(record || {})) {
            if (name.toLowerCase() === key) return [].concat(values).map(String);
        }
        return null;
    };
    return [...names.entries()]
        .sort(([x], [y]) => x.localeCompare(y))
        .map(([key, name]) => {
            const a = lookup(left, key);
            const b = lookup(right, key);
            return { name, a, b, same: JSON.stringify(a) === JSON.stringify(b) };
        });
}

// queryParams reads a request URL's query string into { name: [values] }.
export function queryParams(url) {
    const query = String(url || '').split('#')[0].split('?').slice(1).join('?');
    const params = {};
    if (!query) return params;
    for (const [name, value] of new URLSearchParams(query)) (params[name] ||= []).push(value);
    return params;
}

const pathOf = (url) => String(url || '').split('?')[0];

// sortKeys returns value with object keys sorted at every depth, so that
// JSON bodies differing only in key order compare equal.
function sortKeys(value) {
    if (Array.isArray(value)) return value.map(sortKeys);
    if (value && typeof value === 'object') {
        return Object.fromEntries(Object.keys(value).sort().map(key => [key, sortKeys(value[key])]));
    }
    return value;
}

// bodyText returns the text a body is compared as: pretty JSON with sorted
// keys when both sides parse, else the captured text.
function bodyLines(body, asJSON) {
    const text = body?.text || '';
    if (!text) return [];
    if (asJSON) return JSON.stringify(sortKeys(JSON.parse(text)), null, 2).split('\n');
    return text.split('\n');
}

const parsesAsJSON = (body) => {
    if (!body?.text || body.encoding === 'base64') return false;
    try { JSON.parse(body.text); return true; } catch { return false; }
};

// diffLines returns a line diff: [{ op: ' ' | '-' | '+', text }], where '-'
// lines are only in a and '+' lines only in b. It uses a longest common
// subsequence table, so callers must bound the input size.
export function diffLines(a, b) {
    const n = a.length, m = b.length;
    const lcs = Array.from({ length: n + 1 }, () => new Uint32Array(m + 1));
    for (let i = n - 1; i >= 0; i--) {
        for (let j = m - 1; j >= 0; j--) {
            lcs[i][j] = a[i] === b[j] ? lcs[i + 1][j + 1] + 1 : Math.max(lcs[i + 1][j], lcs[i][j + 1]);
        }
    }
    const out = [];
    let i = 0, j = 0;
    while (i < n && j < m) {
        if (a[i] === b[j]) { out.push({ op: ' ', text: a[i] }); i++; j++; }
        else if (lcs[i + 1][j] >= lcs[i][j + 1]) out.push({ op: '-', text: a[i++] });
        else out.push({ op: '+', text: b[j++] });
    }
    while (i < n) out.push({ op: '-', text: a[i++] });
    while (j < m) out.push({ op: '+', text: b[j++] });
    return out;
}

// compareBodies returns { same, lines?, note? }. Binary, truncated or very
// large bodies get a note instead of a line diff.
export function compareBodies(a, b) {
    const textA = a?.text || '', textB = b?.text || '';
    const binary = a?.encoding === 'base64' || b?.encoding === 'base64';
    const same = textA === textB && (a?.encoding || '') === (b?.encoding || '');
    const truncated = a?.truncated || b?.truncated;
    const suffix = truncated ? ' Only the captured part is compared.' : '';
    if (!textA && !textB) return { same: true, lines: [], note: 'Neither request has a body.' };
    if (binary) return { same, note: (same ? 'The binary bodies are identical.' : 'The binary bodies differ.') + suffix };
    const asJSON = parsesAsJSON(a) && parsesAsJSON(b);
    const left = bodyLines(a, asJSON), right = bodyLines(b, asJSON);
    if (left.length * right.length > maxDiffCells) {
        return { same, note: (same ? 'The bodies are identical.' : 'The bodies differ; they are too large to compare line by line.') + suffix };
    }
    const lines = diffLines(left, right);
    const equal = lines.every(line => line.op === ' ');
    let note = suffix.trim();
    if (asJSON && equal && !same) note = `The JSON bodies are equivalent (they differ only in formatting or key order).${suffix}`;
    return { same: equal, lines, note };
}

function routeLabel(event) {
    if (event?.callback) return 'callback';
    const route = event?.match?.route;
    if (route) return `${route.name} (${route.source}:${route.line})`;
    return event?.match?.upstream ? `none (proxied to ${event.match.upstream})` : 'none (unmatched)';
}

function statusLabel(event) {
    const response = event?.response || {};
    if (response.fault === 'reset') return 'reset (no response)';
    return `${response.status ?? ''} ${response.statusText || ''}`.trim();
}

// compareEvents returns the comparison of two events:
// { overview, query, requestHeaders, requestBody, responseHeaders, responseBody }
// where the row lists come from fieldRows and the bodies from compareBodies.
export function compareEvents(a, b) {
    const one = (value) => value == null || value === '' ? null : [String(value)];
    const overview = [
        ['Method', a?.request?.method, b?.request?.method],
        ['Host', a?.request?.host, b?.request?.host],
        ['Path', pathOf(a?.request?.url), pathOf(b?.request?.url)],
        ['Status', statusLabel(a), statusLabel(b)],
        ['Route', routeLabel(a), routeLabel(b)],
        ['Revision', a?.match?.revision, b?.match?.revision],
        ['Time', a?.request?.time, b?.request?.time],
        ['Duration', a?.response?.time, b?.response?.time],
    ].map(([name, x, y]) => {
        const left = one(x), right = one(y);
        return { name, a: left, b: right, same: JSON.stringify(left) === JSON.stringify(right) };
    });
    return {
        overview,
        query: fieldRows(queryParams(a?.request?.url), queryParams(b?.request?.url)),
        requestHeaders: fieldRows(a?.request?.headers, b?.request?.headers),
        requestBody: compareBodies(a?.request?.body, b?.request?.body),
        responseHeaders: fieldRows(a?.response?.headers, b?.response?.headers),
        responseBody: compareBodies(a?.response?.body, b?.response?.body),
    };
}

// differenceCount counts the rows and body sections that differ, for a
// summary line. Time, Duration and the Date header nearly always differ, so
// they are not counted.
export function differenceCount(comparison) {
    const rows = [...comparison.overview.filter(row => row.name !== 'Time' && row.name !== 'Duration'),
        ...comparison.query, ...comparison.requestHeaders,
        ...comparison.responseHeaders.filter(row => row.name.toLowerCase() !== 'date')];
    return rows.filter(row => !row.same).length
        + (comparison.requestBody.same ? 0 : 1) + (comparison.responseBody.same ? 0 : 1);
}
