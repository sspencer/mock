// Bounded pending traffic keeps Pause a display control, not a capture switch.
export class PendingTraffic {
    constructor(limit = 1000) { this.limit = limit; this.events = new Map(); this.dropped = 0; }
    add(event) {
        this.events.set(event.id, event);
        while (this.events.size > this.limit) {
            this.events.delete(this.events.keys().next().value);
            this.dropped++;
        }
    }
    clearThrough(cursor = Infinity) {
        for (const id of this.events.keys()) if (id <= cursor) this.events.delete(id);
        if (cursor === Infinity) this.dropped = 0;
    }
    drain() {
        const events = [...this.events.values()].sort((a, b) => a.id - b.id);
        this.events.clear();
        return events;
    }
}

export function streamLabel(connected, paused, pending, dropped) {
    if (!connected) return 'Reconnecting';
    if (!paused) return dropped ? `Listening · ${dropped} older requests omitted` : 'Listening';
    return `Paused · ${pending} new${dropped ? ` · ${dropped} older omitted` : ''}`;
}

// requestHost is the host and port shown before a request's path. It is empty
// unless mock listens on more than one port, where it tells the services apart.
export function requestHost(request, multiPort, callback = false) {
    // A callback goes to another server, so its host always matters.
    return ((multiPort || callback) && request?.host) || '';
}

// eventBadge labels a request-log row that the mock did not simply answer:
// an outbound @callback, or a request forwarded to an upstream. Returns
// { text, title } or null.
export function eventBadge(event) {
    if (event?.callback) {
        const route = event.match?.route?.name || 'a route';
        const outcome = event.callback.error ? ` · failed: ${event.callback.error}` : '';
        return { text: 'callback', title: `Sent by ${route} after request #${event.callback.trigger}${outcome}` };
    }
    if (event?.resendOf != null) {
        const proxied = event.match?.upstream ? ` and forwarded to ${event.match.upstream}` : '';
        return { text: 'resent', title: `${event.resendOf ? `Resent from request #${event.resendOf}` : 'Resent'} from the admin UI${proxied}` };
    }
    if (event?.match?.upstream) return { text: 'proxied', title: `Forwarded to ${event.match.upstream}` };
    return null;
}

// TrafficStream is the request-log stream client's state machine. Two
// independent states plus one epoch:
//
//   connection: 'offline' <-> 'online'   (EventSource onerror / onopen)
//   display:    'live'    <-> 'paused'   (Pause button; paused events wait in pending)
//   epoch:      { session, clearedThrough } - events at or below clearedThrough,
//               or from another session, belong to an earlier epoch and are dropped.
//
// Each transition returns the effect for the page to apply:
//   { clearThrough?: number, show: event[] }
// where clearThrough removes displayed events with id <= cursor (Infinity = all).
export class TrafficStream {
    constructor(limit = 1000) {
        this.connection = 'offline';
        this.display = 'live';
        this.session = null;
        this.clearedThrough = 0;
        this.notice = '';
        this.pending = new PendingTraffic(limit);
    }
    get connected() { return this.connection === 'online'; }
    get paused() { return this.display === 'paused'; }
    label() {
        return streamLabel(this.connected, this.paused, this.pending.events.size, this.pending.dropped) + this.notice;
    }

    opened() { this.connection = 'online'; }
    failed() { this.connection = 'offline'; }

    pause() { this.display = 'paused'; return { show: [] }; }
    resume() { this.display = 'live'; return { show: this.pending.drain() }; }

    // The server started a new epoch: it restarted, or the client's cursor fell
    // outside retained history.
    reset({ session, reason }) {
        this.notice = reason === 'history-gap' ? ' · History gap; showing retained traffic' : '';
        return this.#startEpoch(session);
    }
    // The server cleared its log through id (pushed over the stream).
    cleared({ session, id }) {
        this.notice = '';
        this.session = session;
        return this.#clearThrough(id);
    }
    // POST /clear succeeded. A response from another session is stale (the
    // server restarted mid-request) and changes nothing: returns null.
    clearConfirmed(session, cursor) {
        if (this.session && session !== this.session) return null;
        return this.#clearThrough(cursor);
    }
    message(event) {
        let effect = { show: [] };
        if (this.session !== event.session) effect = this.#startEpoch(event.session);
        if (event.id > this.clearedThrough) {
            if (this.paused) this.pending.add(event);
            else effect.show.push(event);
        }
        return effect;
    }

    #startEpoch(session) {
        this.session = session;
        this.clearedThrough = 0;
        this.pending.clearThrough();
        return { clearThrough: Infinity, show: [] };
    }
    #clearThrough(cursor) {
        this.clearedThrough = Math.max(this.clearedThrough, cursor);
        this.pending.clearThrough(this.clearedThrough);
        return { clearThrough: this.clearedThrough, show: [] };
    }
}
