// HAR is built from captured bytes and metadata, never from formatted UI text.
export function buildHAR(events, version = 'dev') {
    const headers = values => Object.entries(values || {}).flatMap(([name, entries]) => entries.map(value => ({ name, value })));
    const header = (values, name) => values.find(value => value.name.toLowerCase() === name.toLowerCase())?.value || '';
    const entries = events.map(event => {
        const request = event.request;
        const response = event.response;
        const reqHeaders = headers(request.headers);
        const resHeaders = headers(response.headers);
        const reqBody = request.body;
        const resBody = response.body;
        const url = new URL(request.url, `${request.scheme}://${request.host}`).href;
        const elapsed = Math.max(0, response.elapsedMs);
        const requestData = {
            method: request.method, url, httpVersion: request.httpVersion,
            cookies: [], headers: reqHeaders,
            queryString: [...new URL(url).searchParams].map(([name, value]) => ({ name, value })),
            headersSize: -1, bodySize: reqBody.size,
            _truncated: reqBody.truncated, _captureError: reqBody.error || undefined,
        };
        // HAR postData has no standard binary encoding field. Preserve binary
        // request captures in an explicit extension instead of pretending UTF-8.
        if (reqBody.capturedSize > 0) {
            if (reqBody.encoding === 'base64') {
                requestData._bodyBase64 = reqBody.text;
            } else {
                requestData.postData = { mimeType: header(reqHeaders, 'Content-Type') || 'text/plain', text: reqBody.text };
            }
        }
        return {
            startedDateTime: request.startedAt, time: elapsed,
            request: requestData,
            response: {
                status: response.status, statusText: response.statusText,
                httpVersion: response.httpVersion, cookies: [], headers: resHeaders,
                content: { size: resBody.size, mimeType: header(resHeaders, 'Content-Type') || 'application/octet-stream',
                    text: resBody.text, encoding: resBody.encoding || undefined,
                    _capturedSize: resBody.capturedSize, _truncated: resBody.truncated },
                redirectURL: header(resHeaders, 'Location'), headersSize: -1, bodySize: resBody.size,
            },
            cache: {},
            // Only total server time is measured; do not fabricate phase timings.
            timings: { send: -1, wait: elapsed, receive: -1 },
            comment: 'Server-side capture; wait contains total handler time. Cookies are retained in headers.',
        };
    });
    return { log: { version: '1.2', creator: { name: 'mock', version }, entries } };
}
