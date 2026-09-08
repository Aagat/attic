import test from 'node:test';
import assert from 'node:assert/strict';
import vm from 'node:vm';
import {readFileSync} from 'node:fs';
import {webcrypto} from 'node:crypto';
const source=readFileSync(new URL('../internal/httpapi/extension/background.js',import.meta.url),'utf8');
function extension({config={server:'https://attic.example',key:'private-key'},response=async()=>({status:202,ok:true})}={}){
 const calls=[],badges=[],titles=[],events={};let options=0;
 const context=vm.createContext({URL,AbortSignal,crypto:webcrypto,Set,Error,TypeError,fetch:async(url,init)=>{calls.push({url,init});return response();},chrome:{
  runtime:{onInstalled:{addListener:f=>events.install=f},openOptionsPage:async()=>{options++;}},
  storage:{local:{get:async()=>config}},permissions:{contains:async()=>true},
  contextMenus:{removeAll:f=>f(),create:()=>{},onClicked:{addListener:f=>events.menu=f}},
  action:{onClicked:{addListener:f=>events.click=f},setBadgeText:async value=>badges.push(value),setBadgeBackgroundColor:async()=>{},setTitle:async value=>titles.push(value)}
 }});
 vm.runInContext(source,context);
 return {events,calls,badges,titles,get options(){return options;}};
}
test('toolbar submits only the current URL to the configured server',async()=>{
 const app=extension();await app.events.click({id:4,url:'https://publisher.example/article?x=1'});
 assert.equal(app.calls.length,1);const {url,init}=app.calls[0];assert.equal(url,'https://attic.example/api/v1/jobs');
 assert.deepEqual(JSON.parse(init.body),{url:'https://publisher.example/article?x=1'});
 assert.equal(init.headers.Authorization,'Bearer private-key');assert.ok(init.headers['Idempotency-Key']);
 assert.equal(init.credentials,'omit');assert.equal(init.redirect,'error');assert.equal(app.badges.at(-1).text,'✓');
});
test('unconfigured extension opens setup without sending a URL',async()=>{
 const app=extension({config:{}});await app.events.click({id:1,url:'https://example.com'});assert.equal(app.options,1);assert.equal(app.calls.length,0);
});
test('rejected key is not presented as a successful save',async()=>{
 const app=extension({response:async()=>({status:401,ok:false})});await app.events.click({id:1,url:'https://example.com'});assert.equal(app.badges.at(-1).text,'!');assert.match(app.titles.at(-1).title,/Access key rejected/);
});
test('browser internal pages never reach the server',async()=>{
 const app=extension();await app.events.click({id:1,url:'chrome://settings'});assert.equal(app.calls.length,0);assert.equal(app.badges.at(-1).text,'!');
});
test('repeated click while saving does not duplicate the request',async()=>{
 let release;const app=extension({response:()=>new Promise(resolve=>release=resolve)});
 const first=app.events.click({id:1,url:'https://example.com'});await new Promise(resolve=>setImmediate(resolve));
 await app.events.click({id:1,url:'https://example.com'});assert.equal(app.calls.length,1);release({status:202,ok:true});await first;
});
test('link context menu submits the link rather than its containing page',async()=>{
 const app=extension();app.events.menu({menuItemId:'save-link',linkUrl:'https://publisher.example/linked'},{id:1,url:'https://search.example/'});
 await new Promise(resolve=>setImmediate(resolve));assert.equal(JSON.parse(app.calls[0].init.body).url,'https://publisher.example/linked');
});

const optionsSource=readFileSync(new URL('../internal/httpapi/extension/options.js',import.meta.url),'utf8');
function options({granted=true, response={ok:true,json:async()=>({authenticated:true})}}={}) {
 const elements={}, calls=[], removed=[];let config={server:'https://old.example',key:'old-key'};
 for(const id of ['server','key','library','disconnect','status','setup'])elements[id]={value:'',hidden:true,handlers:{},addEventListener(event,fn){this.handlers[event]=fn;}};
 const button={disabled:false};
 const context=vm.createContext({URL,AbortSignal,Error,TypeError,document:{getElementById:id=>elements[id]},fetch:async(url,init)=>{calls.push({url,init});return response;},chrome:{storage:{local:{get:async()=>config,set:async v=>{config=v;},remove:async()=>{config={};}}},permissions:{request:async value=>{calls.push({permission:value});if(granted instanceof Error)throw granted;return granted;},getAll:async()=>({origins:['https://old.example/*','http://localhost/*']}),remove:async value=>removed.push(value)}}});
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
