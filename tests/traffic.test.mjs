import test from 'node:test';
import assert from 'node:assert/strict';
import { PendingTraffic, streamLabel } from '../static/traffic.mjs';
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
