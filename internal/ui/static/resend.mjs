// Resend replays a logged request through POST /resend, optionally edited.
// These helpers turn an event into the editor's fields and the fields back
// into the endpoint's JSON body.

// Headers the server recomputes for the replayed request.
const skippedHeaders = new Set(['host', 'content-length', 'transfer-encoding', 'connection']);

// resendDraft returns the editor fields for a logged request:
// { method, url, host, headers, body, encoding, bodyEditable }, or
// { error } when the request cannot be replayed faithfully.
export function resendDraft(event) {
    const request = event?.request;
    if (!request) return { error: 'Select a request to resend.' };
    if (event.callback) return { error: 'Callbacks are sent by mock, not to it, so they cannot be resent.' };
    const body = request.body || {};
    if (body.truncated || body.error) {
        return { error: 'The request body was not captured completely, so it cannot be resent.' };
    }
    const lines = [];
    for (const [name, values] of Object.entries(request.headers || {})) {
        if (skippedHeaders.has(name.toLowerCase())) continue;
        for (const value of [].concat(values)) lines.push(`${name}: ${value}`);
    }
    const binary = body.encoding === 'base64';
    return {
        method: request.method || 'GET',
        url: request.url || '/',
        host: request.host || '',
        headers: lines.join('\n'),
        body: body.text || '',
        encoding: binary ? 'base64' : '',
        bodyEditable: !binary,
    };
}

// parseHeaderLines reads "Name: value" lines into { Name: [values] }. Blank
// lines are skipped; a line without a colon or name throws.
export function parseHeaderLines(text) {
    const headers = {};
    for (const [index, raw] of String(text || '').split('\n').entries()) {
        const line = raw.replace(/\r$/, '');
        if (!line.trim()) continue;
        const colon = line.indexOf(':');
        const name = colon > 0 ? line.slice(0, colon).trim() : '';
        if (!name || /\s/.test(name)) throw new Error(`Header line ${index + 1} must look like "Name: value".`);
        (headers[name] ||= []).push(line.slice(colon + 1).trim());
    }
    return headers;
}

// buildResend returns the POST /resend body for the edited fields. A binary
// body is sent back unchanged as base64.
export function buildResend(draft, fields, from) {
    const method = String(fields.method || '').trim().toUpperCase();
    if (!/^[A-Z][A-Z0-9_-]*$/.test(method)) throw new Error('Enter a method such as GET or POST.');
    const url = String(fields.url || '').trim();
    if (!url.startsWith('/')) throw new Error('Enter a path that starts with /, such as /users?page=2.');
    const payload = { from, method, url, host: draft.host, headers: parseHeaderLines(fields.headers) };
    if (draft.bodyEditable) {
        if (fields.body) payload.body = fields.body;
    } else if (draft.body) {
        payload.body = draft.body;
        payload.encoding = draft.encoding;
    }
    return payload;
}
