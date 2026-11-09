import test from 'node:test';
import assert from 'node:assert/strict';
import { fieldRows, queryParams, diffLines, compareBodies, compareEvents, differenceCount } from '../static/compare.mjs';

test('fieldRows pairs names case-insensitively and sorts them', () => {
 assert.deepEqual(fieldRows({Accept:['a'],'X-B':['1']},{accept:['a'],'X-A':'2'}),[
  {name:'Accept',a:['a'],b:['a'],same:true},
  {name:'X-A',a:null,b:['2'],same:false},
  {name:'X-B',a:['1'],b:null,same:false},
 ]);
 assert.deepEqual(fieldRows(undefined,null),[]);
});

test('queryParams reads repeated values and ignores fragments', () => {
 assert.deepEqual(queryParams('/x?a=1&b=two%20words&a=3#frag'),{a:['1','3'],b:['two words']});
 assert.deepEqual(queryParams('/x'),{});
});

test('diffLines keeps common lines and marks removals and additions', () => {
 assert.deepEqual(diffLines(['a','b','c'],['a','x','c','d']),[
  {op:' ',text:'a'},{op:'-',text:'b'},{op:'+',text:'x'},{op:' ',text:'c'},{op:'+',text:'d'},
 ]);
 assert.deepEqual(diffLines([],['z']),[{op:'+',text:'z'}]);
});

test('compareBodies treats JSON key order and formatting as equivalent', () => {
 const result=compareBodies({text:'{"b":1,"a":[1,2]}'},{text:'{\n  "a": [1, 2],\n  "b": 1\n}'});
 assert.equal(result.same,true);assert.match(result.note,/equivalent/);
 const changed=compareBodies({text:'{"a":1}'},{text:'{"a":2}'});
 assert.equal(changed.same,false);
 assert.deepEqual(changed.lines.filter(line=>line.op!==' ').map(line=>line.op+line.text.trim()),['-"a": 1','+"a": 2']);
});

test('compareBodies notes empty, binary and truncated bodies', () => {
 assert.deepEqual(compareBodies(undefined,{text:''}),{same:true,lines:[],note:'Neither request has a body.'});
 assert.deepEqual(compareBodies({text:'AAE=',encoding:'base64'},{text:'AAE=',encoding:'base64'}),{same:true,note:'The binary bodies are identical.'});
 assert.equal(compareBodies({text:'AAE=',encoding:'base64'},{text:'hi'}).same,false);
 const truncated=compareBodies({text:'one\ntwo',truncated:true},{text:'one\nthree'});
 assert.equal(truncated.same,false);assert.match(truncated.note,/captured part/);
});

test('compareEvents and differenceCount ignore timing and the Date header', () => {
 const base={id:1,request:{method:'GET',host:'localhost:8080',url:'/users?page=1',time:'10:00',headers:{Accept:['*/*']}},response:{status:200,statusText:'OK',time:'1ms',headers:{Date:['Mon'],'Content-Type':['application/json']},body:{text:'{"id":1}'}},match:{revision:1,route:{name:'Users',source:'api.http',line:3}}};
 const same={...base,id:2,request:{...base.request,time:'10:01'},response:{...base.response,time:'5ms',headers:{...base.response.headers,Date:['Tue']}}};
 assert.equal(differenceCount(compareEvents(base,same)),0);
 const other={...same,request:{...same.request,url:'/users?page=2'},response:{...same.response,status:404,statusText:'Not Found',body:{text:'{"id":2}'}},match:{revision:1}};
 const comparison=compareEvents(base,other);
 assert.deepEqual(comparison.overview.filter(row=>!row.same).map(row=>row.name),['Status','Route','Time','Duration']);
 assert.deepEqual(comparison.overview.find(row=>row.name==='Route').b,['none (unmatched)']);
 assert.equal(differenceCount(comparison),4); // status, route, query page, response body
});
