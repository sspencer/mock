// Near-miss help for an unmatched request: each candidate route's reasons
// split into the requirement that failed (highlighted) and the detail.

// reasonParts splits a near-miss reason into its subject, such as
// "Query role" or "Method", and the rest. Reasons that name no subject come
// back with subject ''.
export function reasonParts(reason = '') {
    const fixed = [
        [/^Served on port /, 'Port'],
        [/^Path "/, 'Path'],
        [/^Expected method /, 'Method'],
        [/^Request query is malformed/, 'Query'],
    ];
    for (const [pattern, subject] of fixed) {
        if (pattern.test(reason)) return { subject, detail: reason };
    }
    const match = /^((?:Query|Path parameter|Header|Cookie|JSON|XML|Form) [^:]+|Body): (.*)$/s.exec(reason);
    return match ? { subject: match[1], detail: match[2] } : { subject: '', detail: reason };
}

// candidateRoute finds the configured route a near-miss candidate names, by
// source line and name, or null when the routes changed since.
export function candidateRoute(routes, candidate) {
    return (routes || []).find(route => route.source === candidate.source && route.line === candidate.line && route.name === candidate.name) || null;
}

// failedLabel says how many requirements a candidate failed.
export function failedLabel(candidate) {
    const count = candidate.reasons?.length || 0;
    return count === 1 ? '1 requirement failed' : `${count} requirements failed`;
}
