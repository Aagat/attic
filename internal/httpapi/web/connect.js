'use strict';
const $=id=>document.getElementById(id);
$('server-address').value=location.origin;
$('save-endpoint').value=location.origin+'/api/v1/items';
$('https-note').textContent=isSecureContext?'This address supports secure app installation.':'You are using HTTP. Open Attic through HTTPS to install it as an Android share target.';
for(const [button,field] of [['copy-server','server-address'],['copy-endpoint','save-endpoint']]){
 $(button).addEventListener('click',async()=>{try{await navigator.clipboard.writeText($(field).value);$('setup-status').textContent='Copied.';}catch{$(field).select();$('setup-status').textContent='Select and copy the highlighted address.';}});
}
let installPrompt;
window.addEventListener('beforeinstallprompt',event=>{event.preventDefault();installPrompt=event;$('install').hidden=false;});
$('install').addEventListener('click',async()=>{if(!installPrompt)return;await installPrompt.prompt();installPrompt=null;$('install').hidden=true;});
if('serviceWorker' in navigator&&isSecureContext)navigator.serviceWorker.register('/sw.js').catch(()=>{});
