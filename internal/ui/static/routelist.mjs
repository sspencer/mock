// The routes list's search box and grouping, as pure functions over the
// routes GET /routes returns.

// routeMatchesSearch reports whether a route contains every word of query,
// ignoring case, in its method, path, query, name, source file or tags.
export function routeMatchesSearch(route, query = '') {
    const words = query.toLowerCase().split(/\s+/).filter(Boolean);
    if (!words.length) return true;
    const text = [route.method, route.path, route.query, route.name, route.source, ...(route.tags || [])]
        .filter(Boolean).join(' ').toLowerCase();
    return words.every(word => text.includes(word));
}

export const GROUPINGS = ['', 'file', 'port', 'tag'];

// groupRoutes splits routes into labeled groups, keeping load order inside
// each: by source file or by port in order of first appearance, or by tag in
// name order with untagged routes last (a route with two tags is in both).
// With no grouping it is one group labeled ''.
export function groupRoutes(routes, by = '') {
    if (!GROUPINGS.includes(by) || !by) return routes.length ? [{ label: '', routes }] : [];
    const groups = new Map();
    const add = (label, route) => {
        if (!groups.has(label)) groups.set(label, []);
        groups.get(label).push(route);
    };
    for (const route of routes) {
        if (by === 'file') add(route.source || 'unknown file', route);
        else if (by === 'port') add(route.port ? `port ${route.port}` : 'default port', route);
        else if (route.tags?.length) for (const tag of route.tags) add(`@tag ${tag}`, route);
    }
    let out = [...groups].map(([label, list]) => ({ label, routes: list }));
    if (by === 'tag') {
        out.sort((a, b) => a.label.localeCompare(b.label));
        const untagged = routes.filter(route => !route.tags?.length);
        if (untagged.length) out.push({ label: 'untagged', routes: untagged });
    }
    return out;
}

// routeCountLabel is the routes count, "12", or "3 of 12" while searching.
export function routeCountLabel(shown, total) {
    return shown === total ? String(total) : `${shown} of ${total}`;
}
