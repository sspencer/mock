export function clampPercent(value, fallback, min = 20, max = 80) {
    const number = Number(value);
    return value == null || value === '' || !Number.isFinite(number) ? fallback : Math.max(min, Math.min(max, number));
}

// A splitter is both a pointer target and a keyboard-operable ARIA separator.
// initial is the default percentage, or a function that computes it from the
// current layout; it applies when nothing is saved and on double-click.
export function attachSplitter(handle, container, { axis, property, key, initial, min = 20, max = 80 }, preferences) {
    const fallback = () => {
        const computed = typeof initial === 'function' ? initial() : initial;
        return Number.isFinite(computed) ? computed : 50;
    };
    let value = clampPercent(preferences.get(key), null, min, max) ?? clampPercent(fallback(), 50, min, max);
    const set = (next, persist = true) => {
        value = clampPercent(next, value, min, max);
        container.style.setProperty(property, `${value}%`);
        handle.setAttribute('aria-valuenow', String(Math.round(value)));
        handle.setAttribute('aria-valuetext', `${Math.round(value)} percent`);
        if (persist) preferences.set(key, String(value));
    };
    handle.setAttribute('aria-valuemin', String(min));
    handle.setAttribute('aria-valuemax', String(max));
    set(value, false);
    let dragging = false;
    handle.addEventListener('pointerdown', event => {
        if (event.button !== 0) return;
        event.preventDefault();
        dragging = true;
        handle.setPointerCapture(event.pointerId);
        handle.classList.add('dragging');
    });
    handle.addEventListener('pointermove', event => {
        if (!dragging) return;
        const rect = container.getBoundingClientRect();
        const size = axis === 'x' ? rect.width : rect.height;
        if (!size) return;
        const offset = axis === 'x' ? event.clientX - rect.left : event.clientY - rect.top;
        set(offset / size * 100);
    });
    const stop = () => { dragging = false; handle.classList.remove('dragging'); };
    handle.addEventListener('pointerup', stop);
    handle.addEventListener('pointercancel', stop);
    handle.addEventListener('lostpointercapture', stop);
    handle.addEventListener('dblclick', () => set(fallback()));
    handle.addEventListener('keydown', event => {
        const lower = axis === 'x' ? 'ArrowLeft' : 'ArrowUp';
        const upper = axis === 'x' ? 'ArrowRight' : 'ArrowDown';
        if (![lower, upper, 'Home', 'End'].includes(event.key)) return;
        event.preventDefault();
        set(event.key === 'Home' ? min : event.key === 'End' ? max : value + (event.key === lower ? -1 : 1) * (event.shiftKey ? 10 : 2));
    });
}

// alignedTrafficHeight returns the --traffic-height percentage that makes the
// panel below the traffic table as tall as responseHeight. It returns fallback
// when either height is unknown, as when the inspector is hidden on narrow screens.
export function alignedTrafficHeight(panelHeight, responseHeight, fallback) {
    if (!(panelHeight > 0) || !(responseHeight > 0)) return fallback;
    return 100 - (responseHeight / panelHeight) * 100;
}
