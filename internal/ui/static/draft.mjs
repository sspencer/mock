// Drafts a .http section from a captured request that no route matched, so an
// unmatched request can be pasted into a request file as a starting point.

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
// came from --upstream is used as the draft's response; otherwise it is a
// placeholder 200 JSON body. mockPort is the default port; when it differs from
// the port the request arrived on, the draft sets @port.
export function buildRouteDraft(event, { mockPort = 0 } = {}) {
    const request = event?.request;
    if (!request?.method) throw new Error('This request has no captured method.');
    const [path, query = ''] = (request.url || '/').split(/\?(.*)/s);
    const lines = [];

    const response = event.response || {};
    const upstreamBody = bodyText(response.body);
    const useUpstream = Boolean(event.match?.upstream) && response.status > 0 && !response.fault &&
        !response.body?.encoding && !response.body?.truncated && inlineSafe(upstreamBody);
    const status = useUpstream ? response.status : 200;
    lines.push(`### [${status}] ${request.method} ${routePath(path)}`);

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
    // Upstream bodies are served as captured, not as templates.
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
