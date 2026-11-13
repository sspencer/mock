// Saved traffic filters: the recent filters and the pinned ones are kept in
// localStorage (through the page's preferences object, which never throws),
// and the current filter is kept in the page URL's #filter= fragment so a
// view can be bookmarked or shared. Pure functions; app.mjs applies them.

export const RECENT_KEY = 'recentFilters';
export const PINNED_KEY = 'pinnedFilters';
export const MAX_RECENT = 8;
export const MAX_PINNED = 20;

// readList parses a stored list, keeping only non-empty strings.
export function readList(json, max) {
    try {
        const list = JSON.parse(json || '[]');
        if (!Array.isArray(list)) return [];
        return [...new Set(list.filter(item => typeof item === 'string' && item.trim()).map(item => item.trim()))].slice(0, max);
    } catch {
        return [];
    }
}

// remember puts text first in a recent list, without duplicates.
export function remember(list, text, max = MAX_RECENT) {
    const value = (text || '').trim();
    if (!value) return list;
    return [value, ...list.filter(item => item !== value)].slice(0, max);
}

// togglePin pins text, or unpins it when it is pinned already.
export function togglePin(pinned, text, max = MAX_PINNED) {
    const value = (text || '').trim();
    if (!value) return pinned;
    if (pinned.includes(value)) return pinned.filter(item => item !== value);
    return [...pinned, value].slice(-max);
}

// suggestions lists the filters to offer: pinned ones first, then recent
// ones that are not pinned.
export function suggestions(pinned, recent) {
    return [
        ...pinned.map(value => ({ value, label: 'Pinned' })),
        ...recent.filter(value => !pinned.includes(value)).map(value => ({ value, label: 'Recent' })),
    ];
}

// filterFromHash reads the filter from a location hash such as
// "#filter=4xx%20POST"; "" when there is none.
export function filterFromHash(hash) {
    const params = new URLSearchParams((hash || '').replace(/^#/, ''));
    return (params.get('filter') || '').trim();
}

// hashWithFilter returns the hash that records text, keeping any other
// fragment parameters; "" clears it.
export function hashWithFilter(hash, text) {
    const params = new URLSearchParams((hash || '').replace(/^#/, ''));
    const value = (text || '').trim();
    if (value) params.set('filter', value);
    else params.delete('filter');
    const encoded = params.toString().replaceAll('+', '%20');
    return encoded ? `#${encoded}` : '';
}
