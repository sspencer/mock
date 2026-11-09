// Parses the traffic filter. The filter is a list of terms separated by
// spaces, and a request is shown when it matches every term:
//
//   2xx 3xx 4xx 5xx      a status class
//   unmatched, matched   whether a route answered (callbacks are neither)
//   GET, post, ...       an HTTP method (case-insensitive)
//   route:NAME           answered by the route with this name; quote a name
//                        with spaces: route:"User detail"
//   anything else        text in the method, URL, status, badge or fault
//
// Terms are case-insensitive. "-term" negates a term, such as -2xx.

const METHODS = new Set(['get', 'head', 'post', 'put', 'patch', 'delete', 'options', 'trace', 'connect']);

// filterTerms splits text into terms; double quotes keep spaces in a term.
export function filterTerms(text) {
    const terms = [];
    const pattern = /(-?)(?:([a-z]+):)?(?:"([^"]*)"?|(\S+))/gi;
    for (const match of (text || '').matchAll(pattern)) {
        const [, negate, prefix, quoted, plain] = match;
        terms.push({ negate: negate === '-', prefix: (prefix || '').toLowerCase(), value: (quoted ?? plain ?? '').toLowerCase() });
    }
    // "route:" alone is a prefix with nothing after it yet; ignore it while typing.
    return terms.filter(term => term.prefix ? term.value : term.value !== 'route:');
}

// routeFilter returns the filter text that shows one route's requests.
export function routeFilter(name) {
    return /[\s"]/.test(name) || !name ? `route:"${(name || '').replaceAll('"', '')}"` : `route:${name}`;
}

// statusClass is "2xx" for 200..299 and so on; "" without a status.
const statusClass = (status) => status >= 100 && status < 600 ? `${Math.floor(status / 100)}xx` : '';

function termMatches(term, event, haystack) {
    const { prefix, value } = term;
    if (prefix === 'route') return (event.match?.route?.name || '').toLowerCase() === value && !event.callback;
    if (prefix) return haystack.includes(`${prefix}:${value}`);
    if (/^[1-5]xx$/.test(value)) return statusClass(Number(event.response?.status)) === value;
    if (value === 'unmatched') return !event.match?.route && !event.callback;
    if (value === 'matched') return Boolean(event.match?.route) && !event.callback;
    if (METHODS.has(value)) return (event.request?.method || '').toLowerCase() === value;
    return haystack.includes(value);
}

// matchesFilter reports whether event matches every term. haystack is the
// lower-case text that plain terms search.
export function matchesFilter(terms, event, haystack) {
    return terms.every(term => termMatches(term, event, haystack) !== term.negate);
}
