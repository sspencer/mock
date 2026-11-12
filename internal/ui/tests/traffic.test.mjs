import test from 'node:test';
import assert from 'node:assert/strict';
import { PendingTraffic, TrafficStream, streamLabel, requestHost, eventBadge } from '../static/traffic.mjs';
test('pause retains bounded traffic and reports overflow', () => {
 const pending=new PendingTraffic(2);
 for(let id=1;id<=3;id++)pending.add({id});
 assert.equal(pending.dropped,1);
 assert.deepEqual(pending.drain().map(e=>e.id),[2,3]);
 assert.equal(pending.events.size,0);
});
test('clear removes only traffic through server cursor; reset clears all', () => {
 const pending=new PendingTraffic();pending.add({id:1});pending.add({id:3});pending.clearThrough(2);
 assert.deepEqual(pending.drain(),[{id:3}]);pending.add({id:4});pending.clearThrough();assert.equal(pending.events.size,0);
});
test('connection errors remain visible when pausing and resuming', () => {
 assert.equal(streamLabel(false,false,0,0),'Reconnecting');
 assert.equal(streamLabel(false,true,5,0),'Reconnecting');
 assert.match(streamLabel(true,true,5,2),/5 new.*2 older omitted/);
});

test('request host shows only when mock listens on several ports', () => {
 assert.equal(requestHost({ host: 'localhost:9090' }, true), 'localhost:9090');
 assert.equal(requestHost({ host: 'localhost:9090' }, false), '');
 assert.equal(requestHost({}, true), '');
});

const ev=(id,session='one')=>({id,session});
test('stream starts offline and live, and connection is independent of pause', () => {
 const stream=new TrafficStream();
 assert.equal(stream.connection,'offline');assert.equal(stream.display,'live');assert.equal(stream.label(),'Reconnecting');
 stream.opened();assert.equal(stream.label(),'Listening');
 stream.pause();stream.failed();assert.equal(stream.label(),'Reconnecting');
 stream.opened();assert.match(stream.label(),/^Paused/);
});
test('live messages show; paused messages wait and drain in id order on resume', () => {
 const stream=new TrafficStream();
 assert.deepEqual(stream.message(ev(1)),{clearThrough:Infinity,show:[ev(1)]});
 assert.deepEqual(stream.message(ev(2)),{show:[ev(2)]});
 stream.pause();assert.deepEqual(stream.message(ev(4)).show,[]);stream.message(ev(3));
 assert.deepEqual(stream.resume().show.map(e=>e.id),[3,4]);assert.equal(stream.display,'live');
});
test('a new session starts a new epoch and drops pending traffic', () => {
 const stream=new TrafficStream();stream.message(ev(5));stream.cleared({session:'one',id:5});stream.pause();stream.message(ev(6));
 const effect=stream.message(ev(1,'two'));
 assert.equal(effect.clearThrough,Infinity);assert.equal(stream.session,'two');assert.equal(stream.clearedThrough,0);
 assert.deepEqual(stream.resume().show.map(e=>e.id),[1]);
});
test('clear boundaries only move forward and drop older events', () => {
 const stream=new TrafficStream();stream.message(ev(1));
 assert.equal(stream.cleared({session:'one',id:4}).clearThrough,4);
 assert.deepEqual(stream.message(ev(3)).show,[]);
 assert.equal(stream.clearConfirmed('one',2).clearThrough,4);
 assert.deepEqual(stream.message(ev(5)).show,[ev(5)]);
});
test('a clear confirmed by another session is stale and ignored', () => {
 const stream=new TrafficStream();stream.message(ev(1));
 assert.equal(stream.clearConfirmed('two',9),null);assert.equal(stream.clearedThrough,0);
 assert.equal(new TrafficStream().clearConfirmed('any',3).clearThrough,3);
});
test('reset explains history gaps and a later clear removes the notice', () => {
 const stream=new TrafficStream();stream.opened();
 assert.equal(stream.reset({session:'one',reason:'history-gap'}).clearThrough,Infinity);
 assert.match(stream.label(),/History gap/);
 stream.reset({session:'two',reason:'server-restarted'});assert.equal(stream.label(),'Listening');
 stream.reset({session:'two',reason:'history-gap'});stream.cleared({session:'two',id:1});assert.equal(stream.label(),'Listening');
});

test('callback rows always show their host and get a badge', () => {
    assert.equal(requestHost({ host: 'hooks.example.com' }, false, true), 'hooks.example.com');
    const callback = { callback: { trigger: 7 }, match: { route: { name: 'Pay' } } };
    assert.deepEqual(eventBadge(callback), { text: 'callback', title: 'Sent by Pay after request #7' });
    const failed = { callback: { trigger: 7, error: 'timed out after 10s' }, match: { route: { name: 'Pay' } } };
    assert.equal(eventBadge(failed).title, 'Sent by Pay after request #7 · failed: timed out after 10s');
    assert.deepEqual(eventBadge({ match: { upstream: 'http://api' } }), { text: 'proxied', title: 'Forwarded to http://api' });
    assert.equal(eventBadge({ match: {} }), null);
    assert.deepEqual(eventBadge({ resendOf: 4, match: {} }), { text: 'resent', title: 'Resent from request #4 from the admin UI' });
    assert.equal(eventBadge({ resendOf: 0, match: { upstream: 'http://api' } }).title, 'Resent from the admin UI and forwarded to http://api');
});

test('numbered callbacks and retries get their own badge text', () => {
    const second = { callback: { trigger: 7, index: 2 }, match: { route: { name: 'Pay' } } };
    assert.deepEqual(eventBadge(second), { text: 'callback', title: 'Callback 2 sent by Pay after request #7' });
    const retry = { callback: { trigger: 7, attempt: 3, error: 'connection refused' }, match: { route: { name: 'Pay' } } };
    assert.deepEqual(eventBadge(retry), { text: 'retry', title: 'Sent by Pay after request #7 · attempt 3 · failed: connection refused' });
});
