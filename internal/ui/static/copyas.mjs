// Copy a captured request in other client formats, beside Copy cURL:
// fetch, HTTPie, Go net/http, and a REST Client request (the request half of
// an .http section). Each takes the captured request ({ scheme, host, url,
// method, headers, body }) and returns text, or throws when the request
// cannot be copied completely.

import { shellQuote } from './inspector.mjs';

export const COPY_FORMATS = [
    { id: 'fetch', label: 'fetch' },
    { id: 'httpie', label: 'HTTPie' },
    { id: 'go', label: 'Go net/http' },
    { id: 'http', label: 'REST Client' },
];

// Headers the client sets itself; copying them would be wrong or rejected.
const MANAGED = new Set(['host', 'content-length', 'transfer-encoding', 'connection', 'expect', 'accept-encoding']);

function parts(request) {
    if (!request?.scheme || !request.host) throw new Error('This request has no captured URL.');
    const body = request.body;
    if (body?.truncated || body?.error) throw new Error('Cannot copy a complete request: the request body was not fully captured.');
    if (body?.encoding === 'base64') throw new Error('Binary request: use HAR export to retrieve the captured base64 bytes.');
    const headers = [];
    for (const [name, values] of Object.entries(request.headers || {})) {
        if (MANAGED.has(name.toLowerCase())) continue;
        for (const value of values) headers.push([name, value]);
    }
    return {
        method: request.method || 'GET',
        url: `${request.scheme}://${request.host}${request.url}`,
        headers,
        body: body?.capturedSize > 0 ? body.text : '',
    };
}

export function buildFetch(request) {
    const { method, url, headers, body } = parts(request);
    const options = [`  method: ${JSON.stringify(method)},`];
    if (headers.length) {
        // Repeated headers are joined, as the Headers class would.
        const merged = new Map();
        for (const [name, value] of headers) merged.set(name, merged.has(name) ? `${merged.get(name)}, ${value}` : value);
        options.push('  headers: {');
        for (const [name, value] of merged) options.push(`    ${JSON.stringify(name)}: ${JSON.stringify(value)},`);
        options.push('  },');
    }
    if (body) options.push(`  body: ${JSON.stringify(body)},`);
    return `await fetch(${JSON.stringify(url)}, {\n${options.join('\n')}\n});`;
}

export function buildHTTPie(request) {
    const { method, url, headers, body } = parts(request);
    const words = ['http', '--ignore-stdin'];
    if (body) words.splice(1, 1, '--raw', shellQuote(body));
    words.push(method, shellQuote(url));
    for (const [name, value] of headers) {
        // "Name:" with nothing after it removes a header in HTTPie; "Name;"
        // sends it empty.
        words.push(shellQuote(value === '' ? `${name};` : `${name}:${value}`));
    }
    return words.join(' ');
}

function goString(text) {
    if (!text.includes('`') && !/[\r\u0000-\u0008\u000b\u000c\u000e-\u001f]/.test(text)) return '`' + text + '`';
    return JSON.stringify(text);
}

export function buildGo(request) {
    const { method, url, headers, body } = parts(request);
    const constants = { GET: 'MethodGet', POST: 'MethodPost', PUT: 'MethodPut', PATCH: 'MethodPatch', DELETE: 'MethodDelete', HEAD: 'MethodHead', OPTIONS: 'MethodOptions' };
    const methodExpr = constants[method] ? `http.${constants[method]}` : JSON.stringify(method);
    const bodyExpr = body ? `strings.NewReader(${goString(body)})` : 'nil';
    const lines = [
        `req, err := http.NewRequest(${methodExpr}, ${JSON.stringify(url)}, ${bodyExpr})`,
        'if err != nil {',
        '\treturn err',
        '}',
    ];
    for (const [name, value] of headers) lines.push(`req.Header.Add(${JSON.stringify(name)}, ${JSON.stringify(value)})`);
    lines.push(
        'resp, err := http.DefaultClient.Do(req)',
        'if err != nil {',
        '\treturn err',
        '}',
        'defer resp.Body.Close()',
    );
    return lines.join('\n');
}

export function buildRESTClient(request) {
    const { method, url, headers, body } = parts(request);
    const lines = [`${method} ${url}`];
    for (const [name, value] of headers) lines.push(`${name}: ${value}`);
    if (body) {
        if (/^###/m.test(body)) throw new Error('The body has a line starting with ###, which would start a new request in a .http file.');
        lines.push('', body);
    }
    return lines.join('\n');
}

const BUILDERS = { fetch: buildFetch, httpie: buildHTTPie, go: buildGo, http: buildRESTClient };

// buildRequestAs formats request in one of COPY_FORMATS.
export function buildRequestAs(format, request) {
    const build = BUILDERS[format];
    if (!build) throw new Error(`Unknown format ${format}.`);
    return build(request);
}
