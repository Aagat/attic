'use strict';
const saving = new Set();
chrome.runtime.onInstalled.addListener(() => {
 chrome.contextMenus.removeAll(() => chrome.contextMenus.create({id:'save-link',title:'Save link to Attic',contexts:['link'],targetUrlPatterns:['http://*/*','https://*/*']}));
});
async function badge(tabId, text, title, color='#233c32') {
 if(tabId===undefined)return;
 await Promise.all([chrome.action.setBadgeText({tabId,text}),chrome.action.setBadgeBackgroundColor({tabId,color}),chrome.action.setTitle({tabId,title})]);
}
async function save(raw, tabId) {
 if(saving.has(tabId))return;
 saving.add(tabId);
 try {
  const {server,key}=await chrome.storage.local.get(['server','key']);
  if(!server||!key){await chrome.runtime.openOptionsPage();return;}
  let url;try{url=new URL(raw);}catch{throw new Error('Open an article page first.');}
  if(!['https:','http:'].includes(url.protocol))throw new Error('Only web article links can be saved.');
  const endpoint=new URL(server);
  if(!await chrome.permissions.contains({origins:[endpoint.protocol+'//'+endpoint.hostname+'/*']})){await chrome.runtime.openOptionsPage();return;}
  await badge(tabId,'…','Saving to Attic…');
  const response=await fetch(server+'/api/v1/jobs',{
   method:'POST',credentials:'omit',redirect:'error',signal:AbortSignal.timeout(20000),
   headers:{Authorization:'Bearer '+key,'Content-Type':'application/json','Idempotency-Key':crypto.randomUUID()},body:JSON.stringify({url:url.href})
  });
  if(response.status===401)throw new Error('Access key rejected. Open extension options to reconnect.');
  if(!response.ok)throw new Error('Attic could not save this link (HTTP '+response.status+'). Click to retry.');
  await badge(tabId,'✓','Saved to Attic. Your PDF is being prepared.');
 }catch(error){
  const message=error.name==='TimeoutError'?'Attic timed out. Check your connection and library before retrying.':error instanceof TypeError?'Cannot reach Attic. Check your server address and network connection.':error.message;
  await badge(tabId,'!',message,'#9d4531');
 }finally{saving.delete(tabId);}
}
chrome.action.onClicked.addListener(tab => save(tab.url,tab.id));
chrome.contextMenus.onClicked.addListener((info,tab)=>{if(info.menuItemId==='save-link')save(info.linkUrl,tab?.id);});
