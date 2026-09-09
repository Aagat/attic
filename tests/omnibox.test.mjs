import test from 'node:test';
import assert from 'node:assert/strict';
import vm from 'node:vm';
import {readFileSync} from 'node:fs';
const source=readFileSync(new URL('../internal/httpapi/extension/omnibox.js',import.meta.url),'utf8');
function extension({response=async()=>({ok:true,json:async()=>({items:[]})}),allowed=true,state={server:'http://attic.example:18080',key:'private-key'}}={}) {
 const events={},calls=[],defaults=[],results=[],timers=new Map();let next=0;
 const event=name=>({addListener:fn=>events[name]=fn});
 vm.runInNewContext(source,{URL,URLSearchParams,AbortController,setTimeout:(fn,delay)=>{const id=++next;timers.set(id,{fn,delay});return id;},clearTimeout:id=>timers.delete(id),fetch:async(url,init)=>{calls.push({url,init});return response(url,init);},chrome:{
  omnibox:{...Object.fromEntries(['onInputStarted','onInputChanged','onInputCancelled','onInputEntered'].map(n=>[n,event(n)])),setDefaultSuggestion:async value=>defaults.push(value)},
  storage:{local:{get:async()=>({...state})},onChanged:event('storage')},
  permissions:{contains:async()=>allowed},
  tabs:{update:async tab=>calls.push({update:tab}),create:async tab=>calls.push({create:tab})},
  runtime:{openOptionsPage:async()=>calls.push({options:true})},
 }});
 return {events,calls,defaults,results,state,
  change(text){events.onInputChanged(text,items=>results.push(JSON.parse(JSON.stringify(items))));},
  async search(){const timer=[...timers].find(([,t])=>t.delay===200);if(timer){timers.delete(timer[0]);await timer[1].fn();}},
  async enter(text,disposition='currentTab'){events.onInputEntered(text,disposition);await new Promise(resolve=>setImmediate(resolve));},
 };
}
test('keyword is a and worker includes omnibox module',()=>{
 const manifest=JSON.parse(readFileSync(new URL('../internal/httpapi/extension/manifest.json',import.meta.url)));
 assert.equal(manifest.omnibox.keyword,'a');
 assert.match(readFileSync(new URL('../internal/httpapi/extension/background.js',import.meta.url),'utf8'),/importScripts\([^)]*'omnibox.js'/);
});
test('suggestions come from authenticated Attic search with escaped titles',async()=>{
 const app=extension({response:async()=>({ok:true,json:async()=>({items:[{id:'saved/1',title:'A <match> & "quote"',url:'https://publisher.example/article'}]})})});
 app.change('books & notes');await app.search();
 assert.equal(app.calls[0].url,'http://attic.example:18080/api/v1/items?q=books+%26+notes&limit=5');
 assert.equal(app.calls[0].init.headers.Authorization,'Bearer private-key');
 assert.equal(app.calls[0].init.credentials,'omit');assert.equal(app.calls[0].init.redirect,'error');
 assert.equal(app.results.at(-1)[0].description,'A &lt;match&gt; &amp; &quot;quote&quot; <dim>publisher.example</dim>');
 await app.enter(app.results.at(-1)[0].content);
 assert.equal(app.calls.at(-1).update.url,'http://attic.example:18080/items/saved%2F1');
});
test('a nonempty query stays pending until search results arrive',async()=>{
 const app=extension({response:async()=>({ok:true,json:async()=>({items:[{id:'match',title:'Match'}]})})});
 app.change('books');
 assert.equal(app.results.length,0,'an empty callback would prematurely complete Chromium suggestions');
 await app.search();
 assert.equal(app.results.length,1);
 assert.equal(app.results[0][0].description,'Match');
 app.change('');
 assert.deepEqual(app.results.at(-1),[]);
});
test('typing is debounced and stale responses cannot replace new results',async()=>{
 let resolveFirst;
 const app=extension({response:async url=>url.includes('q=old')?await new Promise(resolve=>resolveFirst=resolve):({ok:true,json:async()=>({items:[{id:'new',title:'New'}]})})});
 app.change('unused');app.change('old');const old=app.search();await new Promise(resolve=>setImmediate(resolve));
 app.change('new');await app.search();
 assert.equal(app.calls[0].init.signal.aborted,true);
 resolveFirst({ok:true,json:async()=>({items:[{id:'old',title:'Old'}]})});await old;
 assert.equal(app.calls.length,2);assert.equal(app.results.at(-1)[0].description,'New');
 app.events.onInputCancelled();await app.enter('query');
 assert.equal(app.calls.at(-1).update.url,'http://attic.example:18080/?q=query');
});
test('cancellation and disconnect suppress in-flight suggestions',async()=>{
 for(const stop of [app=>app.events.onInputCancelled(),app=>app.events.storage({key:{newValue:''}},'local')]){
  let finish;
  const app=extension({response:async()=>await new Promise(resolve=>finish=resolve)});
  app.change('query');const pending=app.search();await new Promise(resolve=>setImmediate(resolve));stop(app);
  finish({ok:true,json:async()=>({items:[{id:'stale',title:'Stale'}]})});await pending;
  assert.deepEqual(app.results,[]);
 }
});
test('Enter preserves requested tab disposition and treats arbitrary URLs as queries',async()=>{
 for(const disposition of ['currentTab','newForegroundTab','newBackgroundTab']){
  const app=extension();await app.enter('https://evil.example/?key=secret',disposition);
  const call=app.calls.at(-1);const tab=call.update||call.create;
  assert.equal(new URL(tab.url).origin,'http://attic.example:18080');
  assert.equal(new URL(tab.url).searchParams.get('q'),'https://evil.example/?key=secret');
  if(disposition!=='currentTab')assert.equal(tab.active,disposition==='newForegroundTab');
 }
});
test('missing setup or denied permission never sends credentials; unavailable search stays usable',async()=>{
 for(const config of [{state:{}},{allowed:false}]){
  const app=extension(config);app.change('query');await app.search();assert.equal(app.calls.length,0);await app.enter('query');assert.equal(app.calls.at(-1).options,true);
 }
 const app=extension({response:async()=>({ok:false,status:503})});app.change('query');await app.search();
 assert.deepEqual(app.results.at(-1),[]);assert.match(app.defaults.at(-1).description,/unavailable/);
 await app.enter('query');assert.equal(app.calls.at(-1).update.url,'http://attic.example:18080/?q=query');
});
