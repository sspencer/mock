// Drafts a .http section from a captured request, so a request can be pasted
// into a request file as a starting point: a new route for a request no route
// matched, or a more specific variant of the route that answered it.

// The names @query.*, @form.* and @header.* accept (the .http variable name rule).
const VARIABLE_NAME = /^[A-Za-z_][A-Za-z0-9_.-]*$/;
// @json.* paths split at dots, so a top-level key with a dot cannot be named.
const JSON_KEY = /^[A-Za-z_][A-Za-z0-9_-]*$/;
// Each matcher kind is capped so a large body or query does not flood the draft.
const MAX_MATCHERS = 8;

// routePath turns a captured path into a literal route path: "*" segments are
// wildcards and ":name" segments are parameters in routes, so escape them.
export function routePath(path) {
    return (path || '/').split('/').map(segment => segment
        .replaceAll('*', '%2A')
        .replaceAll('{', '%7B')
        .replaceAll('}', '%7D')
        .replace(/^:/, '%3A')).join('/');
}

function requestPort(host) {
    const match = /:(\d+)$/.exec(host || '');
    return match ? Number(match[1]) : 0;
}

function header(headers, name) {
    const key = Object.keys(headers || {}).find(k => k.toLowerCase() === name.toLowerCase());
    return key ? headers[key][0] || '' : '';
}

function bodyText(body) {
    if (!body || body.encoding || body.truncated || body.error) return '';
    return body.text || '';
}

// jsonMatchers requires each top-level field of a JSON object body.
function jsonMatchers(text) {
    let value;
    try { value = JSON.parse(text); } catch { return []; }
    if (!value || typeof value !== 'object' || Array.isArray(value)) return [];
    return Object.entries(value)
        .filter(([key, field]) => JSON_KEY.test(key) && field !== null)
        .slice(0, MAX_MATCHERS)
        .map(([key]) => `@json.${key} = *`);
}

function formMatchers(text) {
    const names = [...new Set([...new URLSearchParams(text).keys()])];
    return names.filter(name => VARIABLE_NAME.test(name)).slice(0, MAX_MATCHERS).map(name => `@form.${name} = *`);
}

function queryMatchers(query) {
    const names = [...new Set([...new URLSearchParams(query).keys()])];
    return names.filter(name => VARIABLE_NAME.test(name)).slice(0, MAX_MATCHERS).map(name => `@query.${name} = *`);
}

// A body can be pasted inline unless a line would end the section ("###") or
// its first line would read as a file reference ("< path").
function inlineSafe(text) {
    return !/^###/m.test(text) && !text.startsWith('< ');
}

// buildRouteDraft returns a .http section for a request event's request and
// response. Matchers only require what the request had (fields present, not
// their values), so the draft also answers similar requests. A response that
// came from --upstream, or from the route that matched, is used as the draft's
// response; otherwise it is a placeholder 200 JSON body. A matched request's
// draft starts with a comment naming that route, since the draft only wins if
// it is placed above it. mockPort is the default port; when it differs from
// the port the request arrived on, the draft sets @port.
// internal/cli/draft.go ports this for unmatched requests; keep them in step.
export function buildRouteDraft(event, { mockPort = 0 } = {}) {
    const request = event?.request;
    if (!request?.method) throw new Error('This request has no captured method.');
    const [path, query = ''] = (request.url || '/').split(/\?(.*)/s);
    const lines = [];

    const response = event.response || {};
    const upstreamBody = bodyText(response.body);
    const useUpstream = usableResponse(event);
    const status = useUpstream ? response.status : 200;
    lines.push(`### [${status}] ${request.method} ${routePath(path)}`);
    const route = event.match?.route;
    if (route) lines.push(`# Variant of "${route.name}" (${route.source}:${route.line}): place it above that route so it matches first.`);

    const port = requestPort(request.host);
    if (port && mockPort && port !== mockPort) lines.push(`@port = ${port}`);
    lines.push(...queryMatchers(query));
    if (header(request.headers, 'Authorization')) lines.push('@header.Authorization = *');
    const requestBody = bodyText(request.body);
    if (requestBody.trim()) {
        const contentType = header(request.headers, 'Content-Type').toLowerCase();
        const matchers = contentType.includes('x-www-form-urlencoded') ? formMatchers(requestBody) : jsonMatchers(requestBody);
        lines.push(...matchers);
    }
    // Captured bodies are served as captured, not as templates.
    if (useUpstream && upstreamBody.includes('{{')) lines.push('@template = off');
    lines.push(`${request.method} ${routePath(path)}`);

    if (useUpstream) {
        const contentType = header(response.headers, 'Content-Type');
        if (contentType) lines.push(`Content-Type: ${contentType}`);
        if (upstreamBody) lines.push('', upstreamBody.replace(/\s+$/, ''));
    } else {
        lines.push('Content-Type: application/json', '', '{}');
    }
    return lines.join('\n') + '\n';
}

// usableUpstream reports whether an event's response came from --upstream and
// can be pasted as a draft's response.
function usableUpstream(event) {
    return Boolean(event.match?.upstream) && usableCapture(event);
}

// usableResponse also accepts the response of the route that matched.
function usableResponse(event) {
    return Boolean(event.match?.upstream || event.match?.route) && usableCapture(event);
}

function usableCapture(event) {
    const response = event.response || {};
    return response.status > 0 && !response.fault &&
        !response.body?.encoding && !response.body?.truncated && inlineSafe(bodyText(response.body));
}

// buildRouteDrafts drafts one section per distinct method and path among the
// events no route answered (callbacks are skipped), oldest first. For each
// method and path the first event with a usable upstream response is used, else
// the first event. It returns the sections joined by blank lines and how many
// there are; text is "" when there are none.
export function buildRouteDrafts(events, options = {}) {
    const groups = new Map();
    for (const event of events || []) {
        if (!event?.request?.method || event.match?.route || event.callback) continue;
        const path = (event.request.url || '/').split('?')[0];
        const key = `${event.request.method} ${path}`;
        const current = groups.get(key);
        if (!current || (!usableUpstream(current) && usableUpstream(event))) groups.set(key, event);
    }
    const sections = [...groups.values()].map(event => buildRouteDraft(event, options));
    return { text: sections.join('\n'), count: sections.length };
}
