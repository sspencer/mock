// Bounded pending traffic keeps Pause a display control, not a capture switch.
export class PendingTraffic {
    constructor(limit = 200) { this.limit = limit; this.events = new Map(); this.dropped = 0; }
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
