import test from 'node:test';
import assert from 'node:assert/strict';

// Small DOM adapter exercises application event wiring without a browser install.
// Layout and native accessibility behavior still require browser verification.
class Element extends EventTarget {
    constructor(tag='div') { super(); this.tagName=tag; this.children=[]; this.dataset={}; this.attributes={}; this.classes=new Set(); this.textContent=''; this.value=''; this.classList={add:(...xs)=>xs.forEach(x=>this.classes.add(x)),remove:(...xs)=>xs.forEach(x=>this.classes.delete(x)),contains:x=>this.classes.has(x),toggle:(x,on)=>{on ??= !this.classes.has(x);on?this.classes.add(x):this.classes.delete(x);return on;}}; }
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
    querySelectorAll(selector){return this.children.flatMap(child=>[...(child.tagName==='tr' && (selector==='tr'||selector==='tr[data-id]' && child.dataset.id)?[child]:[]),...child.querySelectorAll(selector)]);}
    contains(element){return element===this||this.children.some(child=>child.contains(element));}
    focus(){document.activeElement=this;}
    scrollIntoView(){}
    matches(){return false;}
}
const table=new Element('table');
const elements=new Map();
const element=id=>{if(!elements.has(id))elements.set(id,new Element());return elements.get(id);};
const doc=new EventTarget();doc.getElementById=element;doc.querySelector=()=>element('panel');doc.createElement=tag=>new Element(tag);doc.body=new Element();doc.documentElement=new Element();doc.activeElement=null;
element('mock-config').textContent='{"version":"test"}';table.append(element('requestTableBody'));
globalThis.document=doc;
globalThis.window={matchMedia:()=>({matches:false})};
globalThis.localStorage={getItem(){throw new Error('storage blocked');},setItem(){throw new Error('storage blocked');}};
let source;globalThis.EventSource=class extends EventTarget{constructor(){super();source=this;}};
const frames=[];globalThis.requestAnimationFrame=fn=>frames.push(fn);
const realInterval=globalThis.setInterval;globalThis.setInterval=()=>0;
globalThis.fetch=async()=>({ok:true,json:async()=>[]});
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
 for(let id=2;id<=202;id++)send(id,'two');flush();assert.equal(element('requestDetails').textContent,'');assert.equal(element('requestTableBody').children.length,200);
});
