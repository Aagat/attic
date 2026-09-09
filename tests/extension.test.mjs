import test from 'node:test';
import assert from 'node:assert/strict';
import vm from 'node:vm';
import {readFileSync} from 'node:fs';
import {webcrypto} from 'node:crypto';
const source = readFileSync(new URL('../internal/httpapi/extension/background.js', import.meta.url), 'utf8');
function extension({state = {}, tree = [], blobs = new Map(), response = async () => ({ok: true, status: 200, json: async () => ({errors: []})})} = {}) {
  const calls = [], events = {};
  state = {server: 'https://attic.example', key: 'private-key', ...state};
  const event = name => ({addListener: fn => {events[name] = fn;}});
  const context = vm.createContext({URL, URLSearchParams, FormData, Blob, importScripts: () => {}, snapshots: {get: async id => blobs.get(id), remove: async id => blobs.delete(id)}, AbortSignal, crypto: webcrypto, Date, fetch: async (url, init) => {
    calls.push({url, init}); return response(url, init);
  }, chrome: {
    tabs: {create: async tab => {calls.push({tab});}},
    runtime: {getURL: path => 'chrome-extension://extension/' + path, id: 'extension', onInstalled: event('install'), onStartup: event('startup'), onMessage: event('message')},
    storage: {local: {get: async keys => Object.fromEntries(keys.filter(k => k in state).map(k => [k, structuredClone(state[k])])),
      set: async values => Object.assign(state, structuredClone(values))}},
    permissions: {contains: async () => true, onAdded: event('permissionAdded')},
    bookmarks: {getTree: async () => structuredClone(tree), ...Object.fromEntries(['onCreated','onChanged','onMoved','onImportEnded'].map(name => [name,event(name)]))},
    alarms: {create: () => {}, onAlarm: event('alarm')},
    contextMenus: {removeAll: fn => fn(), create: () => {}, onClicked: event('menu')},
    action: {setBadgeText: async () => {}, setTitle: async () => {}},
  }});
  vm.runInContext(source, context);
  return {state, calls, events, blobs, async send(message) {
    return new Promise(resolve => events.message(message, {id: 'extension'}, resolve));
  }};
}
const bookmark = (id = '1') => ({id, url: 'https://example.com/article?meaningful=1', title: 'Article', dateAdded: 1700000000000});
test('both manual actions preserve title and the selected delivery intent', async () => {
  for (const action of ['bookmark','kindle']) {
    const app = extension(); const result = await app.send({type:'save',url:'https://example.com',title:'Example',action,requestID:'stable-key'});
    assert.equal(result.ok,true); assert.equal(result.queued,false);
    assert.equal(app.calls[0].url,'https://attic.example/api/v1/items');
    assert.deepEqual(JSON.parse(app.calls[0].init.body),{url:'https://example.com/',title:'Example',action});
    assert.equal(app.calls[0].init.headers['Idempotency-Key'],'stable-key');
    assert.equal(app.calls[0].init.credentials,'omit'); assert.equal(app.calls[0].init.redirect,'error');
  }
});
test('offline manual delivery retries with the same idempotency key after restart', async () => {
  const first = extension({response: async () => {throw new Error('Offline');}});
  const result = await first.send({type:'save',url:'https://example.com',action:'kindle',requestID:'delivery-1'});
  assert.equal(result.queued,true);
  const resumed = extension({state:first.state}); await resumed.send({type:'reconcile'});
  assert.equal(resumed.calls[0].init.headers['Idempotency-Key'],'delivery-1');
  assert.deepEqual(resumed.state.pendingSaves,{});
});
test('opt-in reconciliation imports existing bookmarks in batches with folder, date and stable identity', async () => {
  const app = extension({state:{bookmarkSync:true},tree:[{title:'Research',children:Array.from({length:105},(_,i)=>bookmark(String(i)))}]});
  await app.send({type:'reconcile'});
  assert.equal(app.calls.length,2);
  const batch=JSON.parse(app.calls[0].init.body).bookmarks;
  assert.equal(batch.length,100); assert.equal(batch[0].source.folder,'Research');
  assert.equal(batch[0].source.saved_at,'2023-11-14T22:13:20.000Z');
  assert.equal(batch[0].action,'bookmark');
  const client=app.state.clientID;
  await app.send({type:'reconcile'});
  assert.equal(app.calls.length,2);
  assert.equal(app.state.clientID,client);
});
test('bookmark access is opt-in and browser-internal bookmarks are excluded', async () => {
  const app=extension({tree:[bookmark()]}); await app.send({type:'reconcile'}); assert.equal(app.calls.length,0);
  app.state.bookmarkSync=true;
  const internal=extension({state:app.state,tree:[{id:'x',url:'chrome://settings',title:'Settings'}]});
  await internal.send({type:'reconcile'}); assert.equal(internal.calls.length,0);
});
test('failed bookmark ingestion survives restart and browser deletion without issuing deletes', async () => {
  const app=extension({state:{bookmarkSync:true},tree:[bookmark()],response:async()=>({ok:false,status:503})});
  await app.send({type:'reconcile'}); assert.ok(app.state.pendingBookmarks['1']);
  const resumed=extension({state:app.state,tree:[]});await resumed.send({type:'reconcile'});
  assert.equal(resumed.calls.length,1); assert.equal(resumed.calls[0].init.method,'POST');
  assert.equal(JSON.parse(resumed.calls[0].init.body).bookmarks[0].source.node_id,'1');
  assert.deepEqual(resumed.state.pendingBookmarks,{});
});
test('partial import errors retain the batch for retry',async()=>{
 const app=extension({state:{bookmarkSync:true},tree:[bookmark()],response:async()=>({ok:true,status:200,json:async()=>({errors:[{index:0,error:'temporary'}]})})});
 await app.send({type:'reconcile'});assert.ok(app.state.pendingBookmarks['1']);assert.match(app.state.syncStatus,/Waiting to retry/);
});
test('internal pages and missing configuration are rejected before creating a pending save',async()=>{
 for(const app of [extension(),extension({state:{server:''}})]){
  const result=await app.send({type:'save',url:'chrome://settings'});assert.equal(result.ok,false);assert.equal(app.calls.length,0);assert.equal(app.state.pendingSaves,undefined);
 }
});
const optionsSource=readFileSync(new URL('../internal/httpapi/extension/options.js',import.meta.url),'utf8');
function options({granted=true, response={ok:true,json:async()=>({authenticated:true})}}={}) {
 const elements={}, calls=[], removed=[];let config={server:'https://old.example',key:'old-key'};
 for(const id of ['server','key','library','disconnect','status','setup','bookmark-sync','sync-status','sync-now'])elements[id]={value:'',hidden:true,handlers:{},addEventListener(event,fn){this.handlers[event]=fn;}};
 const button={disabled:false};
 const context=vm.createContext({URL,AbortSignal,Error,TypeError,snapshots:{clear:async()=>{}},document:{getElementById:id=>elements[id]},fetch:async(url,init)=>{calls.push({url,init});return response;},chrome:{runtime:{sendMessage:async()=>({ok:true})},storage:{onChanged:{addListener:()=>{}},local:{get:async()=>config,set:async v=>{Object.assign(config,v);},remove:async keys=>{for(const key of [keys].flat())delete config[key];}}},permissions:{request:async value=>{calls.push({permission:value});if(granted instanceof Error)throw granted;return granted;},getAll:async()=>({origins:['https://old.example/*','http://localhost/*']}),remove:async value=>removed.push(value)}}});
 vm.runInContext(optionsSource,context);
 return {elements,calls,removed,button,get config(){return config;},async submit(server='http://localhost:18080',key='new-key'){await new Promise(r=>setImmediate(r));elements.server.value=server;elements.key.value=key;await elements.setup.handlers.submit({preventDefault(){},currentTarget:{querySelector:()=>button}});}};
}
test('setup uses a host grant while retaining the configured port for requests',async()=>{
 const app=options();await app.submit();assert.equal(app.calls[0].permission.origins[0],'http://localhost/*');assert.equal(app.calls[1].url,'http://localhost:18080/api/v1/session');assert.equal(app.config.server,'http://localhost:18080');assert.equal(app.removed[0].origins[0],'https://old.example/*');assert.equal(app.removed[0].origins.length,1);assert.equal(app.button.disabled,false);
});
test('denied permissions never send the access key',async()=>{
 const app=options({granted:false});await app.submit();assert.equal(app.calls.length,1);assert.equal(app.config.key,'old-key');assert.equal(app.button.disabled,false);
});
test('permission API errors remain visible and re-enable setup',async()=>{
 const app=options({granted:new Error('Permission unavailable')});await app.submit();assert.equal(app.elements.status.textContent,'Permission unavailable');assert.equal(app.button.disabled,false);
});
test('failed authentication preserves the previous working configuration',async()=>{
 const app=options({response:{ok:false,status:401}});await app.submit();assert.equal(app.config.key,'old-key');assert.match(app.elements.status.textContent,/Access key rejected/);
});
test('setup rejects embedded credentials and paths before requesting permission',async()=>{
 for(const server of ['https://user:password@example.com','https://example.com/attic']){const app=options();await app.submit(server);assert.equal(app.calls.length,0);}
});
test('disconnect removes stored credentials and host permissions',async()=>{
 const app=options();await app.submit();await app.elements.disconnect.handlers.click();assert.equal(app.config.key,undefined);assert.equal(app.elements.key.value,'');assert.equal(app.elements.library.hidden,true);assert.equal(app.removed.at(-1).origins.length,2);
});

test('simultaneous manual saves do not overwrite the durable queue', async () => {
 const app=extension({response:async()=>{throw new Error('offline');}});
 await Promise.all(['a','b','c'].map(requestID=>app.send({type:'save',url:'https://example.com/'+requestID,requestID})));
 assert.deepEqual(Object.keys(app.state.pendingSaves).sort(),['a','b','c']);
});
test('browser title edits and moves reconcile without resending unchanged bookmarks',async()=>{
 const tree=[{title:'Before',children:[bookmark()]}];const app=extension({state:{bookmarkSync:true},tree});
 await app.send({type:'reconcile'});tree[0].title='After';tree[0].children[0].title='Edited';
 await app.send({type:'reconcile'});
 const item=JSON.parse(app.calls[1].init.body).bookmarks[0];assert.equal(item.title,'Edited');assert.equal(item.source.folder,'After');
 await app.send({type:'reconcile'});assert.equal(app.calls.length,2);
});

test('capture survives popup close and worker restart, uploads before Kindle, and retries delivery without recapturing', async () => {
 const first = extension();
 const saved = await first.send({type:'save',url:'https://example.com/',tabID:7,action:'kindle',requestID:'capture-1'});
 assert.equal(saved.queued,true); assert.equal(first.calls[0].tab.active,false);
 assert.match(first.calls[0].tab.url,/capture.html/); assert.equal(first.calls.length,1);
 const blobs = new Map([['capture-1', new Blob(['MHTML'])]]);
 const resumed = extension({state:first.state,blobs,response:async(url)=>{
   if(url.endsWith('/send'))throw new Error('connection lost after acceptance');
   return {ok:true,status:200,json:async()=>({id:'item-1'})};
 }});
 await resumed.send({type:'captureFinished',requestID:'capture-1'});
 await new Promise(resolve => setImmediate(resolve));
 assert.deepEqual(resumed.calls.map(call=>call.url.split('/api/v1')[1]),['/items','/items/item-1/capture','/items/item-1/send']);
 assert.equal(JSON.parse(resumed.calls[0].init.body).action,'bookmark');
 assert.equal(await resumed.calls[1].init.body.get('snapshot').text(),'MHTML');
 assert.equal(resumed.calls[1].init.headers['Content-Type'],undefined);
 assert.equal(resumed.state.pendingSaves['capture-1'].uploaded,true);
 const final = extension({state:resumed.state,blobs}); await final.send({type:'reconcile'});
 assert.equal(final.calls.length,1); assert.match(final.calls[0].url,/item-1\/send$/);
 assert.equal(final.calls[0].init.headers['Idempotency-Key'],'capture-1');
 assert.deepEqual(final.state.pendingSaves,{});assert.equal(blobs.size,0);
});
test('failed browser capture falls back to URL saving and does not strand delivery',async()=>{
 const app=extension();await app.send({type:'save',url:'https://example.com/',tabID:2,action:'kindle',requestID:'fallback'});
 await app.send({type:'captureFinished',requestID:'fallback',error:'Capture not supported.'});
 await new Promise(resolve => setImmediate(resolve));
 assert.equal(JSON.parse(app.calls[1].init.body).action,'kindle');
 assert.match(app.state.captureStatus,/Capture not supported/);assert.deepEqual(app.state.pendingSaves,{});
});
test('transient capture upload errors cannot start Kindle delivery',async()=>{
 const app=extension({state:{pendingSaves:{capture:{url:'https://example.com/',title:'',action:'kindle'}}},blobs:new Map([['capture',new Blob(['MHTML'])]]),response:async(url)=>({ok:!url.endsWith('/capture'),status:url.endsWith('/capture')?503:200,json:async()=>({id:'item'})})});
 await app.send({type:'reconcile'});assert.equal(app.calls.length,2);assert.equal(app.state.pendingSaves.capture.uploaded,undefined);assert.equal(app.blobs.size,1);
});
const captureSource=readFileSync(new URL('../internal/httpapi/extension/capture.js',import.meta.url),'utf8');
async function captureDocument({navigated=false,failure=false}={}) {
 const calls=[];let reads=0;
 const context=vm.createContext({URLSearchParams,setTimeout,clearTimeout,location:{search:'?id=request&tab=7&url=https%3A%2F%2Fexample.com%2F'},snapshots:{put:async(id,blob)=>calls.push({stored:id,size:blob.size})},chrome:{
  tabs:{get:async()=>({url:++reads>1&&navigated?'https://different.example/':'https://example.com/'}),getCurrent:async()=>({id:8}),remove:async id=>calls.push({closed:id})},
  pageCapture:{saveAsMHTML:(_,callback)=>callback(failure?undefined:new Blob(['MHTML']))},
  runtime:{sendMessage:async message=>calls.push({message})},
 }});
 await vm.runInContext(captureSource,context);return calls;
}
test('capture document persists bytes before acknowledgement and closes its helper tab',async()=>{
 const calls=await captureDocument();assert.equal(calls[0].stored,'request');assert.equal(calls[1].message.type,'captureFinished');assert.equal(calls[1].message.error,undefined);assert.equal(calls[2].closed,8);
});
test('navigation races and unavailable capture never upload misleading content and close the helper',async()=>{
 for(const options of [{navigated:true},{failure:true}]){
  const calls=await captureDocument(options);assert.equal(calls.length,2);assert.ok(calls[0].message.error);assert.equal(calls[1].closed,8);
 }
});
test('a helper finishing after disconnect removes its orphaned snapshot without saving or sending',async()=>{
 const blobs=new Map([['cancelled',new Blob(['private page'])]]);
 const app=extension({state:{server:undefined,key:undefined,pendingSaves:{}},blobs});
 const result=await app.send({type:'captureFinished',requestID:'cancelled'});
 assert.equal(result.ok,true);assert.equal(blobs.size,0);assert.equal(app.calls.length,0);
 assert.deepEqual(app.state.pendingSaves,{});
});

test('current-page lookup uses authorized exact URL search without creating a save', async () => {
 const item={id:'existing',capture_status:'complete',delivery_status:'sent'};
 const app=extension({response:async()=>({ok:true,status:200,json:async()=>({items:[item]})})});
 const result=await app.send({type:'lookup',url:'https://example.com/article?x=1#part'});
 assert.deepEqual(result.item,item);
 assert.equal(result.readerURL,'https://attic.example/items/existing');
 const call=app.calls[0];
 assert.equal(new URL(call.url).searchParams.get('url'),'https://example.com/article?x=1#part');
 assert.equal(call.init.method,'GET');assert.equal(call.init.body,undefined);
 assert.equal(call.init.headers.Authorization,'Bearer private-key');
 assert.equal(call.init.headers['Idempotency-Key'],undefined);
 assert.equal(app.state.pendingSaves,undefined);
});
test('lookup handles no match, unauthorized requests and non-web pages',async()=>{
 const app=extension({response:async()=>({ok:true,status:200,json:async()=>({items:[]})})});
 assert.equal((await app.send({type:'lookup',url:'https://example.com'})).item,undefined);
 assert.equal((await app.send({type:'lookup',url:'chrome://settings'})).ok,false);
 assert.equal(app.calls.length,1);
 const rejected=extension({response:async()=>({ok:false,status:401})});
 assert.equal((await rejected.send({type:'lookup',url:'https://example.com'})).error,'Access key rejected.');
});
test('existing item Kindle delivery skips capture and preserves delivery identity across restart',async()=>{
 const first=extension({response:async()=>{throw new Error('Offline');}});
 const result=await first.send({type:'save',itemID:'saved-item',url:'https://example.com',tabID:7,action:'kindle',requestID:'resend-1'});
 assert.equal(result.queued,true);
 assert.equal(first.calls.length,1);
 assert.equal(first.calls[0].url,'https://attic.example/api/v1/items/saved-item/send');
 assert.equal(first.state.pendingSaves['resend-1'].capturing,undefined);
 const resumed=extension({state:first.state});await resumed.send({type:'reconcile'});
 assert.equal(resumed.calls.length,1);
 assert.equal(resumed.calls[0].url,'https://attic.example/api/v1/items/saved-item/send');
 assert.equal(resumed.calls[0].init.headers['Idempotency-Key'],'resend-1');
 assert.deepEqual(resumed.state.pendingSaves,{});
});
