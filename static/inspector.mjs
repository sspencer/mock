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

export function formatRoute(route) {
    const responseHeaders = Object.entries(route.headers || {}).flatMap(([name, values]) => values.map(value => `${name}: ${value}`)).join('\n');
    const matchHeaders = Object.entries(route.matchHeaders || {}).flatMap(([name, values]) => values.map(value => `${name}: ${value}`)).join('\n');
    const variables = Object.entries(route.variables || {}).map(([key, value]) => `$${key}=${value}`).join('\n');
    return [
        `${route.method} ${route.path}${route.query ? '?' + route.query : ''}`,
        `Source: ${route.source || '(unknown)'}:${route.line || '?'} · revision ${route.revision}`,
        `Response: ${route.status} · delay ${route.delay || '0s'}`,
        `Request header requirements\n${matchHeaders || '(none)'}`,
        `Response headers\n${responseHeaders || '(automatic)'}`,
        `Variables\n${variables || '(none)'}`,
        route.file ? `Body file: ${route.file}${route.body ? '\nInline body takes precedence over this file.' : ''}` : 'Inline response body',
        route.body || (route.file ? '(read from file when requested)' : '(empty body)'),
        route.bodyTruncated ? '[Template preview truncated after 65536 bytes]' : '',
    ].filter(Boolean).join('\n\n');
}
