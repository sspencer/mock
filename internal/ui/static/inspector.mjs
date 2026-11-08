// Rendering and copying use captured metadata rather than parsing display text.
export function bodyNotice(body) {
    if (!body) return '';
    const notes = [];
    if (body.encoding === 'base64') notes.push('Binary body shown as base64');
    if (body.truncated) notes.push(`Truncated: ${body.capturedSize} of ${body.size < 0 ? 'unknown' : body.size} bytes captured`);
    if (body.error) notes.push(`Capture error: ${body.error}`);
    return notes.join(' · ');
}

export function formatMessage(message, pretty, request = false) {
    if (!message?.headers || !message.body) return message?.details || '';
    const first = request
        ? `${message.method} ${message.url} ${message.httpVersion}`
        : `${message.httpVersion} ${message.status} ${message.statusText}`;
    const headers = Object.entries(message.headers).flatMap(([name, values]) => values.map(value => `${name}: ${value}`));
    let text = message.body.text || '';
    if (pretty && !message.body.encoding && !message.body.truncated && !message.body.error && text) {
        try { text = JSON.stringify(JSON.parse(text), null, 2); } catch { /* Preserve non-JSON bodies. */ }
    }
    return [first, ...headers].join('\n') + (text ? `\n\n${text}` : '');
}

export function shellQuote(value) {
    return "'" + String(value).replaceAll("'", "'\\''") + "'";
}

export function buildCurl(request) {
    if (!request?.scheme || !request.host) throw new Error('This request has no captured URL.');
    const body = request.body;
    if (body?.truncated || body?.error) throw new Error('Cannot copy a complete cURL command: the request body was not fully captured.');
    if (body?.encoding === 'base64') throw new Error('Binary request: use HAR export to retrieve the captured base64 bytes.');
    const parts = ['curl', '--request', shellQuote(request.method), '--url', shellQuote(`${request.scheme}://${request.host}${request.url}`)];
    const managed = new Set(['host', 'content-length', 'transfer-encoding', 'connection', 'expect', 'accept-encoding']);
    for (const [name, values] of Object.entries(request.headers || {})) {
        if (!managed.has(name.toLowerCase())) {
            for (const value of values) parts.push('--header', shellQuote(`${name}: ${value}`));
        }
    }
    if (body?.capturedSize > 0) {
        parts.push('--data-binary', '@-');
        return `printf '%s' ${shellQuote(body.text)} | ${parts.join(' ')}`;
    }
    return parts.join(' ');
}

// Parameter names that look numeric get an integer; anything else gets a word.
const NUMERIC_PARAM = /^(?:id|ids|num|number|count|page|limit|offset|index|year|age|size)$|(?:_id|Id|ID)$/;

export function sampleParam(name) {
    return NUMERIC_PARAM.test(name) ? '1' : 'example';
}

// Builds a curl command for the mock listener that can be pasted into a shell.
// `mock check` builds the same sample request in Go (internal/cli/sample.go);
// keep the two in step.
// `origin` is the scheme, host, and port the mock routes are served from.
// Matcher values: "*" (present), "!" (absent), "~pattern" (regexp),
// "!text" and "!~pattern" (present but not that), else exact. A sample
// request uses exact values as written and a placeholder otherwise; a
// placeholder may not satisfy a regexp.
export const isSampleExact = value => value !== '*' && !value.startsWith('!') && !value.startsWith('~');

// Describes a matcher value for the route details panel.
export function describeMatcher(value) {
    if (value === '!') return '! (must be absent)';
    if (value.startsWith('!~')) return `${value} (present, not matching the regular expression)`;
    if (value.startsWith('!')) return `${value} (present, not ${value.slice(1)})`;
    if (value.startsWith('~')) return `${value} (regular expression)`;
    return value;
}

// Splits a @json path such as items.*.sku or meta["user.id"] into
// { name, any, quoted } segments, like restclient.ParseJSONPath. Returns null
// for a malformed path.
export function jsonPathSegments(path) {
    const segments = [];
    const pattern = /\.?\["([^"\s]+)"\]|(?:^|\.)([A-Za-z0-9_-]+|\*)/y;
    let match;
    while (pattern.lastIndex < path.length && (match = pattern.exec(path))) {
        if (match[1] !== undefined) segments.push({ name: match[1], any: false, quoted: true });
        else segments.push({ name: match[2], any: match[2] === '*', quoted: false });
    }
    return segments.length && pattern.lastIndex === path.length ? segments : null;
}

// Builds a sample JSON body from @json matchers. Exact values are used as
// written (JSON literals such as 42 or true stay unquoted); "*" and regexp
// values become "example"; "!" fields are left out. Numeric path segments
// make arrays. Returns '' when there is nothing to send or paths conflict.
export function sampleJSONBody(matchJson) {
    const entries = Object.entries(matchJson || {}).filter(([, value]) => value !== '!');
    if (!entries.length) return '';
    const literal = value => {
        if (!isSampleExact(value)) return 'example';
        try { return JSON.parse(value); } catch { return value; }
    };
    const isIndex = segment => /^(0|[1-9][0-9]*)$/.test(segment);
    let root;
    try {
        for (const [path, value] of entries) {
            // A "*" segment accepts any element, so the sample puts the value first.
            const parsed = jsonPathSegments(path);
            if (!parsed) throw new Error('bad path');
            const segments = parsed.map(({ name, any }) => any ? '0' : name);
            const index = i => !parsed[i].quoted && isIndex(segments[i]);
            if (root === undefined) root = index(0) ? [] : {};
            let node = root;
            segments.forEach((segment, i) => {
                const last = i === segments.length - 1;
                if (Array.isArray(node) !== index(i)) throw new Error('conflict');
                if (last) { node[segment] = literal(value); return; }
                const child = node[segment] ?? (index(i + 1) ? [] : {});
                if (typeof child !== 'object' || child === null) throw new Error('conflict');
                node[segment] = child;
                node = child;
            });
        }
    } catch { return ''; }
    // Sparse arrays from indexes like roles.1 serialize their holes as null.
    return JSON.stringify(root);
}

// Builds a sample URL-encoded body from @form matchers, like the query string.
export function sampleFormBody(matchForm) {
    const form = new URLSearchParams();
    for (const [name, values] of Object.entries(matchForm || {})) {
        for (const value of values) {
            if (value !== '!') form.append(name, isSampleExact(value) ? value : 'example');
        }
    }
    return form.toString();
}

export function buildRouteCurl(route, origin) {
    const matchPath = route.matchPath || {};
    const path = String(route.path)
        .replace(/(^|\/):([^/]+)/g, (_, slash, name) =>
            slash + (name in matchPath && isSampleExact(matchPath[name]) ? encodeURIComponent(matchPath[name]) : sampleParam(name)))
        // A trailing * or *name wildcard matches the rest of the path.
        .replace(/(^|\/)\*([A-Za-z0-9_]*)$/, (_, slash, name) =>
            slash + (name in matchPath && isSampleExact(matchPath[name]) ? matchPath[name].split('/').map(encodeURIComponent).join('/') : 'example'));
    // Required @query.* parameters are appended to the declared query as written.
    const extra = new URLSearchParams();
    for (const [name, values] of Object.entries(route.matchQuery || {})) {
        for (const value of values) {
            if (value !== '!') extra.append(name, isSampleExact(value) ? value : sampleParam(name));
        }
    }
    const queryText = [route.query, extra.toString()].filter(Boolean).join('&');
    const url = `${origin}${path}${queryText ? '?' + queryText : ''}`;
    const parts = ['curl'];
    if (origin.startsWith('https:')) parts.push('--insecure');
    if (route.method === 'HEAD') parts.push('--head');
    else if (route.method !== 'GET') parts.push('--request', route.method);
    for (const [name, values] of Object.entries(route.matchHeaders || {})) {
        for (const value of values) {
            if (value !== '!') parts.push('--header', shellQuote(`${name}: ${isSampleExact(value) ? value : 'example'}`));
        }
    }
    const cookies = Object.entries(route.matchCookies || {}).flatMap(([name, values]) =>
        values.filter(value => value !== '!').map(value => `${name}=${isSampleExact(value) ? value : 'example'}`));
    if (cookies.length) parts.push('--cookie', shellQuote(cookies.join('; ')));
    const json = sampleJSONBody(route.matchJson);
    const form = sampleFormBody(route.matchForm);
    const body = json || form || (route.matchBody && isSampleExact(route.matchBody) ? route.matchBody : '');
    // curl sends --data-raw as application/x-www-form-urlencoded by default.
    if (json && !Object.keys(route.matchHeaders || {}).some(name => name.toLowerCase() === 'content-type')) {
        parts.push('--header', shellQuote('Content-Type: application/json'));
    }
    if (body) parts.push('--data-raw', shellQuote(body));
    parts.push(shellQuote(url));
    return parts.join(' ');
}

// hitsLabel describes how many requests a route has answered.
export function hitsLabel(hits = 0) {
    return hits === 1 ? '1 request' : `${hits} requests`;
}

export function formatRoute(route, sampleCurl = '') {
    const responseHeaders = Object.entries(route.headers || {}).flatMap(([name, values]) => values.map(value => `${name}: ${value}`)).join('\n');
    const describe = describeMatcher;
    const matchHeaders = Object.entries(route.matchHeaders || {}).flatMap(([name, values]) => values.map(value => `${name}: ${describe(value)}`)).join('\n');
    const matchQuery = Object.entries(route.matchQuery || {}).flatMap(([name, values]) => values.map(value => `${name}: ${describe(value)}`)).join('\n');
    const matchPath = Object.entries(route.matchPath || {}).map(([name, value]) => `:${name}: ${describe(value)}`).join('\n');
    const matchCookies = Object.entries(route.matchCookies || {}).flatMap(([name, values]) => values.map(value => `${name}: ${describe(value)}`)).join('\n');
    const matchBody = [
        route.matchBody ? `body: ${describe(route.matchBody)}` : '',
        ...Object.entries(route.matchJson || {}).map(([path, value]) => `json ${path}: ${describe(value)}`),
        ...Object.entries(route.matchXml || {}).map(([path, value]) => `xml ${path}: ${describe(value)}`),
        ...Object.entries(route.matchForm || {}).flatMap(([name, values]) => values.map(value => `form ${name}: ${describe(value)}`)),
    ].filter(Boolean).join('\n');
    const variables = Object.entries(route.variables || {}).map(([key, value]) => `@${key} = ${value}`).join('\n');
    return [
        `${route.method} ${route.path}${route.query ? '?' + route.query : ''}`,
        sampleCurl ? `Sample request\n${sampleCurl}` : '',
        `Source: ${route.source || '(unknown)'}:${route.line || '?'} · revision ${route.revision}`,
        `Matched: ${hitsLabel(route.hits)}`,
        route.port ? `Port: ${route.port}` : '',
        route.proxy ? `Proxy: ${route.proxy}${route.delay && route.delay !== '0s' ? ` · delay ${route.delay}` : ''}` : `Response: ${route.status} · delay ${route.delay || '0s'}${route.stream ? ` · stream ${route.stream}` : ''}${route.fault ? ` · fault ${route.fault}` : ''}${route.compress ? ` · compress ${route.compress}` : ''}`,
        route.fixture ? `Fixture: ${route.fixture}` : '',
        route.callback ? `Callback: ${route.callback}` : '',
        route.rateLimit ? `Rate limit: ${route.rateLimit} (then 429 with Retry-After)` : '',
        `Request header requirements\n${matchHeaders || '(none)'}`,
        matchQuery ? `Query requirements\n${matchQuery}` : '',
        matchPath ? `Path parameter requirements\n${matchPath}` : '',
        matchCookies ? `Cookie requirements\n${matchCookies}` : '',
        matchBody ? `Body requirements\n${matchBody}` : '',
        `Response headers\n${responseHeaders || '(automatic)'}`,
        `Variables\n${variables || '(none)'}`,
        route.file ? `Body file: ${route.file}` : 'Inline response body',
        route.body || (route.file ? '(read from file when requested)' : '(empty body)'),
        route.bodyTruncated ? '[Template preview truncated after 65536 bytes]' : '',
    ].filter(Boolean).join('\n\n');
}
