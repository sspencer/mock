import test from 'node:test';
import assert from 'node:assert/strict';
import { noSelection, selectRequest, selectRoute, selectCompare, selectedRequestId, selectedRoute, afterEventsChanged, afterRoutesLoaded } from '../static/selection.mjs';

test('a selection is exactly one kind', () => {
 assert.equal(selectedRequestId(selectRequest(3)),3);assert.equal(selectedRoute(selectRequest(3)),null);
 const route={id:'1:0'};
 assert.equal(selectedRoute(selectRoute(route)),route);assert.equal(selectedRequestId(selectRoute(route)),null);
 assert.equal(selectedRequestId(noSelection),null);assert.equal(selectedRoute(noSelection),null);
});
test('a request deselects once it is no longer displayed', () => {
 const selection=selectRequest(3);
 assert.equal(afterEventsChanged(selection,id=>id===3),selection);
 assert.equal(afterEventsChanged(selection,()=>false),noSelection);
 const route=selectRoute({id:'1:0'});assert.equal(afterEventsChanged(route,()=>false),route);
});
test('a route follows reloads by id and goes stale when its id disappears', () => {
 const old=selectRoute({id:'1:0',name:'old'});
 const refreshed=afterRoutesLoaded(old,[{id:'1:0',name:'new'}]);
 assert.deepEqual(refreshed,{kind:'route',route:{id:'1:0',name:'new'},stale:false});
 const stale=afterRoutesLoaded(refreshed,[{id:'2:0'}]);
 assert.equal(stale.stale,true);assert.equal(stale.route.name,'new');
 assert.equal(afterRoutesLoaded(stale,[]),stale);
 assert.equal(afterRoutesLoaded(stale,[{id:'1:0'}]).stale,false);
 const request=selectRequest(1);assert.equal(afterRoutesLoaded(request,[]),request);
});
test('a followed route stays selected when a reload changes its id', () => {
 const route={id:'1:2',name:'Users',source:'api.http',line:9};
 const follow={source:'api.http',line:9,name:'Users'};
 const next=afterRoutesLoaded(selectRoute(route),[{id:'2:0',name:'Other',source:'api.http',line:3},{id:'2:1',name:'Users',source:'api.http',line:9}],follow);
 assert.equal(next.route.id,'2:1');assert.equal(next.stale,false);
 assert.equal(afterRoutesLoaded(selectRoute(route),[{id:'2:1',name:'Renamed',source:'api.http',line:9}],follow).stale,true);
});
test('a comparison lasts while both events exist, even when filtered out', () => {
 const compare=selectCompare(1,2);
 assert.equal(selectedRequestId(compare),null);
 assert.equal(afterEventsChanged(compare,()=>false,()=>true),compare);
 assert.equal(afterEventsChanged(compare,()=>true,id=>id===1),noSelection);
});
