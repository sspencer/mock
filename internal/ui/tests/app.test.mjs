import test from 'node:test';
import assert from 'node:assert/strict';

// Small DOM adapter exercises application event wiring without a browser install.
// Layout and native accessibility behavior still require browser verification.
class Element extends EventTarget {
    constructor(tag='div') { super(); this.style={setProperty:(key,value)=>{this.attributes[key]=value;}}; this.tagName=tag; this.children=[]; this.dataset={}; this.attributes={}; this.classes=new Set(); this.textContent=''; this.value=''; this.classList={add:(...xs)=>xs.forEach(x=>this.classes.add(x)),remove:(...xs)=>xs.forEach(x=>this.classes.delete(x)),contains:x=>this.classes.has(x),toggle:(x,on)=>{on ??= !this.classes.has(x);on?this.classes.add(x):this.classes.delete(x);return on;}}; }
    setAttribute(k,v){this.attributes[k]=v;}
    getAttribute(k){return this.attributes[k];}
    removeAttribute(k){delete this.attributes[k];}
    append(...xs){xs.forEach(x=>this.appendChild(x));}
    appendChild(x){return this.insertBefore(x,null);}
    insertBefore(x,next){x.remove();const at=next?this.children.indexOf(next):this.children.length;this.children.splice(at,0,x);x.parent=this;return x;}
    remove(){if(this.parent){this.parent.children.splice(this.parent.children.indexOf(this),1);this.parent=null;}}
    replaceChildren(...xs){for(const child of [...this.children])child.remove();this.append(...xs);}
    get firstChild(){return this.children[0]||null;}
    get nextSibling(){return this.parent?.children[this.parent.children.indexOf(this)+1]||null;}
    insertRow(){return this.appendChild(new Element('tr'));}
    insertCell(){return this.appendChild(new Element('td'));}
    closest(selector){if(selector==='table')return table;return this.tagName==='tr'?this:this.parent?.closest(selector);}
    querySelector(selector){return this.querySelectorAll(selector)[0]||new Element();}
    querySelectorAll(selector){return this.children.flatMap(child=>[...((child.tagName===selector || child.tagName==='tr' && selector==='tr[data-id]' && child.dataset.id)?[child]:[]),...child.querySelectorAll(selector)]);}
    contains(element){return element===this||this.children.some(child=>child.contains(element));}
    focus(){document.activeElement=this;}
    scrollIntoView(){}
    setPointerCapture(){}
    getBoundingClientRect(){return {left:0,top:0,width:1000,height:600};}
    get isConnected(){return !!this.parent;}
    matches(){return false;}
}
const table=new Element('table');
const elements=new Map();
const element=id=>{if(!elements.has(id))elements.set(id,new Element());return elements.get(id);};
const doc=new EventTarget();doc.getElementById=element;doc.querySelector=()=>element('panel');doc.createElement=tag=>new Element(tag);doc.body=new Element();doc.documentElement=new Element();doc.activeElement=null;
element('mock-config').textContent='{"version":"test"}';table.append(element('requestTableBody'));
globalThis.document=doc;
globalThis.window={matchMedia:()=>({matches:false})};
globalThis.location={hostname:'localhost'};
globalThis.localStorage={getItem(){throw new Error('storage blocked');},setItem(){throw new Error('storage blocked');}};
let source;globalThis.EventSource=class extends EventTarget{constructor(){super();source=this;}};
const frames=[];globalThis.requestAnimationFrame=fn=>frames.push(fn);
const realInterval=globalThis.setInterval;globalThis.setInterval=()=>0;
let posted=[]; let postedBodies=[]; let resendReply={ok:true,id:0}; let routesData=[]; let stateData={revision:1,routeCount:0,lastSuccess:'2026-09-06T00:00:00Z'};
let fixtureFetch=null;
globalThis.fetch=async(url,options)=>{if(String(url).startsWith('fixture'))return fixtureFetch(url,options);if(options?.method==='POST'){posted.push(url);postedBodies.push(options.body);}if(url==='resend')return {ok:resendReply.ok,status:resendReply.ok?200:400,text:async()=>'bad request',json:async()=>({id:resendReply.id})};return {ok:true,headers:{get:name=>name==='X-Mock-Session'?'two':'500'},json:async()=>url==='state'?stateData:routesData};};
await import('../static/app.mjs');
globalThis.setInterval=realInterval;
const flush=()=>{while(frames.length)frames.shift()();};
const event=(id,session='one')=>({id,session,request:{method:'GET',url:`/x/${id}`,time:'00:00',details:`request ${id}`},response:{status:200,statusText:'OK',time:'1ms',details:`response ${id}`}});
const send=(id,session)=>source.onmessage({data:JSON.stringify(event(id,session))});
const click=id=>element(id).dispatchEvent(new Event('click'));
const control=(kind,id,session='one')=>source.dispatchEvent(new MessageEvent(kind,{data:JSON.stringify({id,session})}));

test('dashboard starts with unavailable storage and batches stable traffic rows',async()=>{
 await Promise.resolve();source.onopen();send(1);send(2);flush();
 const rows=element('requestTableBody').children;assert.deepEqual(rows.map(r=>r.dataset.id),['2','1']);
 const first=rows[0];send(3);flush();assert.equal(element('requestTableBody').children[1],first);
});
test('pause buffers events and resume retains them',()=>{
 click('pauseButton');send(4);flush();assert.equal(element('requestTableBody').children.length,3);
 assert.match(element('streamStatus').textContent,/1 new/);
 click('pauseButton');flush();assert.equal(element('requestTableBody').children[0].dataset.id,'4');
});
test('restart and clear controls remove stale traffic while preserving newer events',()=>{
 control('clear',4);send(3);send(5);flush();assert.deepEqual(element('requestTableBody').children.map(r=>r.dataset.id),['5']);
 control('reset',0,'two');send(1,'two');flush();assert.deepEqual(element('requestTableBody').children.map(r=>r.dataset.id),['1']);
});
test('evicting selected traffic clears its inspector',()=>{
 const row=element('requestTableBody').children[0];row.focus();const key=new Event('keydown',{bubbles:true});Object.defineProperty(key,'target',{value:row});Object.defineProperty(key,'key',{value:'Enter'});element('requestTableBody').dispatchEvent(key);
 assert.equal(element('requestDetails').textContent,'request 1');
 for(let id=2;id<=1002;id++)send(id,'two');flush();assert.equal(element('requestDetails').textContent,'');assert.equal(element('requestTableBody').children.length,1000);
});


test('inspector toggles Pretty/Raw, shows match metadata, and copies original body', async () => {
 const sample=event(1003,'two');
 sample.request={...sample.request,scheme:'https',host:'example.test',httpVersion:'HTTP/1.1',headers:{},body:{text:'{"name":"Alex"}',size:15,capturedSize:15,truncated:false}};
 sample.response={...sample.response,httpVersion:'HTTP/1.1',headers:{},body:{text:'{"ok":true}',size:11,capturedSize:11,truncated:false}};
 sample.match={revision:2,route:{name:'User detail',source:'users.http',line:12}};
 source.onmessage({data:JSON.stringify(sample)});flush();
 const row=element('requestTableBody').children[0];
 const key=new Event('keydown');Object.defineProperty(key,'target',{value:row});Object.defineProperty(key,'key',{value:'Enter'});element('requestTableBody').dispatchEvent(key);
 assert.match(element('matchSummary').textContent,/users.http:12 · revision 2/);
 assert.equal(element('copyRouteDraftButton').hidden,false);
 assert.match(element('copyRouteDraftButton').title,/variant of User detail/);
 assert.ok(element('requestDetails').textContent.includes('\n  "name": "Alex"'));
 click('requestRaw');assert.ok(element('requestDetails').textContent.endsWith('{"name":"Alex"}'));
 let copied='';Object.defineProperty(globalThis,'navigator',{configurable:true,value:{clipboard:{writeText:async text=>{copied=text;}}}});
 click('copyRequestBody');await Promise.resolve();assert.equal(copied,'{"name":"Alex"}');
 click('copyCurlButton');await Promise.resolve();assert.ok(copied.includes('https://example.test/x/1003'));
 click('backToList');assert.equal(element('workspace').classList.contains('show-inspector'),false);
});

test('unmatched requests copy a draft route', async () => {
 const sample=event(1004,'two');
 sample.request={...sample.request,method:'POST',url:'/orders?expand=items',host:'localhost:8080',headers:{'Content-Type':['application/json']},body:{text:'{"sku":"A1","qty":2}',size:20,capturedSize:20,truncated:false}};
 sample.response={...sample.response,status:404,statusText:'Not Found',headers:{},body:{text:'',size:0,capturedSize:0,truncated:false}};
 sample.match={revision:2};
 source.onmessage({data:JSON.stringify(sample)});flush();
 const row=element('requestTableBody').children[0];
 const key=new Event('keydown');Object.defineProperty(key,'target',{value:row});Object.defineProperty(key,'key',{value:'Enter'});element('requestTableBody').dispatchEvent(key);
 assert.equal(element('copyRouteDraftButton').hidden,false);
 let copied='';Object.defineProperty(globalThis,'navigator',{configurable:true,value:{clipboard:{writeText:async text=>{copied=text;}}}});
 click('copyRouteDraftButton');await Promise.resolve();
 assert.equal(copied,'### [200] POST /orders\n@query.expand = *\n@json.sku = *\n@json.qty = *\nPOST /orders\nContent-Type: application/json\n\n{}\n');
});

test('routes open their configuration and copy a sample request', async () => {
 routesData=[{id:'2:0',revision:2,name:'Create user',method:'POST',path:'/users',port:8080,source:'users.http',line:1,status:201,delay:'1s',headers:{},matchHeaders:{},variables:{},body:'{"ok":true}',hits:3}];
 click('refreshRoutesButton');await new Promise(resolve=>setImmediate(resolve));
 const button=element('routesList').querySelector('button');button.dispatchEvent(new Event('click'));
 assert.equal(element('routeView').hidden,false);
 assert.match(element('routeDetails').textContent,/POST \/users/);
 assert.equal(element('routeTitle').textContent,'Create user');
 assert.equal(button.parent.children.at(-1).textContent,'3');
 assert.match(element('routeDetails').textContent,/Matched: 3 requests/);
 assert.match(element('routeSummary').textContent,/users.http:1/);
 let copied='';Object.defineProperty(globalThis,'navigator',{configurable:true,value:{clipboard:{writeText:async text=>{copied=text;}}}});
 click('copyRouteCurlButton');await Promise.resolve();assert.equal(copied,"curl --request POST 'http://localhost:8080/users'");
 routesData=[{...routesData[0],id:'3:0',revision:3}];
 click('refreshRoutesButton');await new Promise(resolve=>setImmediate(resolve));
 assert.match(element('routeSummary').textContent,/previous revision/);
 assert.equal(element('routeView').hidden,false);
});

test('request column hides the host on a single-port server',()=>{
 control('reset',0,'hosts');
 const withHost={...event(1,'hosts'),request:{...event(1,'hosts').request,host:'localhost:8080',url:'/token'}};
 source.onmessage({data:JSON.stringify(withHost)});flush();
 const row=element('requestTableBody').children[0];
 const spans=row.children[2].children;
 assert.equal(spans.length,2);assert.equal(spans[1].textContent,' /token');
 assert.equal(row.title,'GET /token · 1ms');
});

test('the filter understands status classes and route hit counts filter traffic',()=>{
 control('reset',0,'filters');
 const make=(id,status,routeName,method='GET')=>({...event(id,'filters'),request:{...event(id,'filters').request,method},response:{...event(id,'filters').response,status},match:routeName?{revision:1,route:{name:routeName}}:{revision:1}});
 for(const e of [make(1,200,'Create user','POST'),make(2,404),make(3,500,'Boom'),make(4,201,'Create user','POST')])source.onmessage({data:JSON.stringify(e)});
 flush();
 const ids=()=>element('requestTableBody').children.map(r=>r.dataset.id).filter(Boolean);
 const filter=text=>{element('filterInput').value=text;element('filterInput').dispatchEvent(new Event('input'));};
 filter('4xx');assert.deepEqual(ids(),['2']);
 filter('unmatched');assert.deepEqual(ids(),['2']);
 filter('post -201');assert.deepEqual(ids(),['1']);
 filter('');
 const hits=element('routesList').children[0].children.at(-1);
 hits.dispatchEvent(new Event('click'));
 assert.equal(element('filterInput').value,'route:"Create user"');assert.deepEqual(ids(),['4','1']);
 hits.dispatchEvent(new Event('click'));
 assert.equal(element('filterInput').value,'');assert.equal(ids().length,4);
});

test('resend edits a request, posts it to the admin server and selects the new event', async () => {
 control('reset',0,'resend');
 const original={...event(1,'resend'),request:{...event(1,'resend').request,method:'POST',url:'/users',host:'localhost:8080',headers:{'Content-Type':['application/json'],'Content-Length':['13'],Host:['localhost:8080']},body:{text:'{"name":"Al"}',size:13,capturedSize:13,truncated:false}},match:{revision:1,route:{name:'Create'}}};
 source.onmessage({data:JSON.stringify(original)});flush();
 element('filterInput').value='';element('filterInput').dispatchEvent(new Event('input'));flush();
 const row=element('requestTableBody').children[0];
 const key=new Event('keydown');Object.defineProperty(key,'target',{value:row});Object.defineProperty(key,'key',{value:'Enter'});element('requestTableBody').dispatchEvent(key);
 assert.equal(element('resendButton').hidden,false);
 click('resendButton');
 assert.equal(element('resendForm').hidden,false);
 assert.equal(element('resendMethod').value,'POST');assert.equal(element('resendUrl').value,'/users');
 assert.equal(element('resendHeaders').value,'Content-Type: application/json');
 assert.equal(element('resendBody').value,'{"name":"Al"}');
 element('resendBody').value='{"name":"Bo"}';
 element('resendHeaders').value='not a header';
 element('resendForm').dispatchEvent(new Event('submit'));await new Promise(resolve=>setImmediate(resolve));
 assert.match(element('resendStatus').textContent,/Name: value/);
 element('resendHeaders').value='Content-Type: application/json\nX-Try: 2';
 resendReply={ok:true,id:2};posted=[];postedBodies=[];
 element('resendForm').dispatchEvent(new Event('submit'));await new Promise(resolve=>setImmediate(resolve));
 assert.deepEqual(posted,['resend']);
 assert.deepEqual(JSON.parse(postedBodies[0]),{from:1,method:'POST',url:'/users',host:'localhost:8080',headers:{'Content-Type':['application/json'],'X-Try':['2']},body:'{"name":"Bo"}'});
 assert.equal(element('resendForm').hidden,true);
 source.onmessage({data:JSON.stringify({...original,id:2,resendOf:1,request:{...original.request,details:'request 2'}})});flush();
 assert.equal(element('requestTableBody').children[0].classes.has('selected'),true);
 click('resendButton');resendReply={ok:false,id:0};
 element('resendForm').dispatchEvent(new Event('submit'));await new Promise(resolve=>setImmediate(resolve));
 assert.equal(element('resendStatus').textContent,'bad request');
 assert.equal(element('resendForm').hidden,false);
 click('resendCancel');assert.equal(element('resendForm').hidden,true);
});

test('compare picks one request, compares it with another, swaps and closes', () => {
 control('reset',0,'compare');
 const a={...event(1,'compare'),request:{...event(1,'compare').request,url:'/users?page=1',headers:{Accept:['*/*']}}};
 const b={...event(2,'compare'),request:{...event(2,'compare').request,url:'/users?page=2',headers:{Accept:['*/*']}},response:{...event(2,'compare').response,status:404,statusText:'Not Found'}};
 source.onmessage({data:JSON.stringify(a)});source.onmessage({data:JSON.stringify(b)});flush();
 element('filterInput').value='';element('filterInput').dispatchEvent(new Event('input'));flush();
 const choose=id=>{const row=element('requestTableBody').children.find(r=>r.dataset.id===String(id));const key=new Event('keydown');Object.defineProperty(key,'target',{value:row});Object.defineProperty(key,'key',{value:'Enter'});element('requestTableBody').dispatchEvent(key);};
 choose(1);
 assert.equal(element('compareButton').hidden,false);assert.equal(element('compareButton').textContent,'Compare…');
 click('compareButton');assert.equal(element('compareButton').textContent,'Cancel compare');
 choose(2);assert.equal(element('compareButton').textContent,'Compare with #1');
 click('compareButton');
 assert.equal(element('compareView').hidden,false);assert.equal(element('exchangeView').hidden,true);
 assert.equal(element('compareTitle').textContent,'Compare #1 and #2');
 assert.match(element('compareSummary').textContent,/2 differences/);
 click('compareSwap');assert.equal(element('compareTitle').textContent,'Compare #2 and #1');
 click('compareOnlyDiffs');assert.equal(element('compareOnlyDiffs').getAttribute('aria-pressed'),'false');
 click('compareClose');
 assert.equal(element('compareView').hidden,true);assert.equal(element('exchangeView').hidden,false);
 assert.equal(element('requestDetails').textContent.includes('request 1'),true);
});
test('the fixture editor edits a route fixture as a tree or JSON and saves it', async () => {
 const tick=()=>new Promise(resolve=>setImmediate(resolve));
 const route={id:'4:0',revision:4,name:'List users',method:'GET',path:'/users',port:8080,source:'users.http',line:7,status:200,fixture:'users.json',headers:{},matchHeaders:{},variables:{},hits:0};
 const puts=[];let putReply={status:200,body:{version:'v2'}};
 fixtureFetch=async(url,options)=>{
  if(options?.method==='PUT'){puts.push(JSON.parse(options.body));return {ok:putReply.status===200,status:putReply.status,json:async()=>putReply.body,text:async()=>putReply.body.error};}
  assert.equal(url,'fixture?route=4%3A0');
  return {ok:true,status:200,json:async()=>({file:'/srv/users.json',fixture:'users.json',content:'{"name":"Ada","count":1,"tags":["a"]}',version:'v1'})};
 };
 routesData=[route];click('refreshRoutesButton');await tick();
 element('routesList').querySelector('button').dispatchEvent(new Event('click'));
 assert.equal(element('editFixtureButton').hidden,false);
 assert.equal(element('fixtureEditor').hidden,true);
 click('editFixtureButton');await tick();
 assert.equal(element('fixtureEditor').hidden,false);
 assert.equal(element('routeDetails').hidden,true);
 assert.equal(element('fixtureFile').textContent,'users.json');
 const inputs=()=>element('fixtureTree').querySelectorAll('input');
 assert.deepEqual(inputs().map(input=>input.value),['name','Ada','count','1','tags','a']);
 const change=(input,value)=>{input.value=value;input.dispatchEvent(new Event('change'));};
 change(inputs()[1],'Grace');
 assert.equal(element('fixtureStatus').textContent,'Unsaved changes.');
 change(inputs()[3],'abc');
 assert.match(element('fixtureStatus').textContent,/count: Enter a number/);
 assert.equal(element('fixtureStatus').classes.has('error'),true);
 change(inputs()[2],'name');
 assert.match(element('fixtureStatus').textContent,/already a field named "name"/);
 element('fixtureTree').querySelectorAll('button').find(button=>button.title==='Duplicate tags[0]').dispatchEvent(new Event('click'));
 assert.deepEqual(inputs().map(input=>input.value),['name','Grace','count','1','tags','a','a']);
 click('fixtureJsonTab');
 assert.equal(element('fixtureText').hidden,false);
 assert.equal(element('fixtureText').value,'{\n  "name": "Grace",\n  "count": 1,\n  "tags": [\n    "a",\n    "a"\n  ]\n}\n');
 element('fixtureText').value='{"name": ';element('fixtureText').dispatchEvent(new Event('input'));
 click('fixtureTreeTab');
 assert.match(element('fixtureStatus').textContent,/^Invalid JSON at line 1/);
 assert.equal(element('fixtureTreeTab').getAttribute('aria-pressed'),'false');
 element('fixtureText').value='{"name":"Lin"}';element('fixtureText').dispatchEvent(new Event('input'));
 click('fixtureFormat');
 assert.equal(element('fixtureText').value,'{\n  "name": "Lin"\n}\n');
 click('fixtureSave');await tick();
 assert.deepEqual(puts.at(-1),{file:'/srv/users.json',content:'{\n  "name": "Lin"\n}\n',version:'v1'});
 assert.match(element('fixtureStatus').textContent,/^Saved/);
 putReply={status:409,body:{error:'the file changed on disk since it was opened; reload it to see the changes',version:'v9'}};
 click('fixtureSave');await tick();
 assert.equal(puts.at(-1).version,'v2');
 assert.match(element('fixtureStatus').textContent,/^Not saved: the file changed on disk.*edits are kept/);
 // The save reloads the routes; the edited route stays selected and open.
 routesData=[{...route,id:'5:0',revision:5}];click('refreshRoutesButton');await tick();
 assert.doesNotMatch(element('routeSummary').textContent,/previous revision/);
 assert.equal(element('fixtureEditor').hidden,false);
 click('fixtureClose');
 assert.equal(element('fixtureEditor').hidden,true);
 assert.equal(element('routeDetails').hidden,false);
 // Reopening resumes the draft instead of reading the file again.
 fixtureFetch=async()=>{throw new Error('unexpected read');};
 click('editFixtureButton');await tick();
 assert.equal(element('fixtureEditor').hidden,false);
 assert.equal(element('fixtureText').value,'{\n  "name": "Lin"\n}\n');
});
