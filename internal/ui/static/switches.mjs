// Route switches: turn a route off, force its status, or add a delay or a
// fault while mock runs, through the admin /faults endpoint. Pure helpers,
// so the form logic can be unit tested without the page.

export const FAULT_KINDS = ['reset', 'truncate', 'hang', 'empty', 'garbage'];

// switchFields returns the form's values for a route: its own switch if it
// has one, else empty fields (the route's own settings apply).
export function switchFields(route) {
    const own = route?.switch || {};
    const [kind = '', share = ''] = (own.fault || '').split(/\s+/);
    return {
        off: Boolean(own.off),
        status: own.status ? String(own.status) : '',
        delay: own.delay || '',
        fault: kind === 'none' ? 'none' : kind,
        share: share ? share.replace(/%$/, '') : '100',
    };
}

// switchPayload turns the form's values into a POST /faults body for the
// route, or an error to show. Empty fields are left out, so the route keeps
// its own setting for them.
export function switchPayload(route, fields) {
    const payload = { route: route.id };
    if (fields.off) payload.off = true;
    const status = String(fields.status ?? '').trim();
    if (status) {
        const code = Number(status);
        if (!Number.isInteger(code) || code < 200 || code > 999) return { error: 'Status must be a whole number from 200 to 999.' };
        payload.status = code;
    }
    const delay = String(fields.delay ?? '').trim();
    if (delay) payload.delay = delay;
    const fault = String(fields.fault ?? '').trim();
    if (fault === 'none') {
        payload.fault = 'none';
    } else if (fault) {
        if (!FAULT_KINDS.includes(fault)) return { error: `Unknown fault "${fault}".` };
        const share = String(fields.share ?? '').trim() || '100';
        const percent = Number(share);
        if (!Number.isFinite(percent) || percent < 0 || percent > 100) return { error: 'The fault share must be a percentage from 0 to 100.' };
        payload.fault = percent === 100 ? fault : `${fault} ${percent}%`;
    }
    if (Object.keys(payload).length === 1) return { error: 'Choose at least one switch, or use Switch back on to remove them.' };
    return { payload };
}

// switchBadge describes the badge a route gets in the route list while a
// switch (its own, or one for all routes) applies to it, or null.
export function switchBadge(route) {
    if (!route?.override) return null;
    const text = route.switch?.off ? 'off' : 'switched';
    return { text, title: `Switched at run time: ${route.override}` };
}
