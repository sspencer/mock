// The inspector shows at most one thing. Selection is one of:
//
//   { kind: 'none' }
//   { kind: 'request', id }             a request-log event
//   { kind: 'route', route, stale }     a configured route; stale once a reload
//                                       drops its id (route ids include the revision)
//   { kind: 'compare', a, b }           two request-log events side by side
//
// Transitions are pure so they can be unit tested without the page.
export const noSelection = Object.freeze({ kind: 'none' });
export const selectRequest = (id) => ({ kind: 'request', id });
export const selectRoute = (route) => ({ kind: 'route', route, stale: false });
export const selectCompare = (a, b) => ({ kind: 'compare', a, b });

export const selectedRequestId = (selection) => selection.kind === 'request' ? selection.id : null;
export const selectedRoute = (selection) => selection.kind === 'route' ? selection.route : null;

// A selected request that is no longer displayed (evicted, cleared, filtered
// out) deselects. A comparison lasts while both events are still in the log
// (exists), even if the filter hides them. Routes are unaffected.
export function afterEventsChanged(selection, isDisplayed, exists = isDisplayed) {
    if (selection.kind === 'request' && !isDisplayed(selection.id)) return noSelection;
    if (selection.kind === 'compare' && !(exists(selection.a) && exists(selection.b))) return noSelection;
    return selection;
}

// A selected route follows the freshly loaded list: it is replaced by the
// route with the same id, or kept (for display) and marked stale.
export function afterRoutesLoaded(selection, routes) {
    if (selection.kind !== 'route') return selection;
    const current = routes.find(route => route.id === selection.route.id);
    if (current) return selectRoute(current);
    return selection.stale ? selection : { ...selection, stale: true };
}
