'use strict';
// Only the public offline page is cached. Library data, sessions and PDFs stay online.
const cacheName='attic-offline-v1';
self.addEventListener('install',event=>{event.waitUntil(caches.open(cacheName).then(cache=>cache.add('/offline.html')).then(()=>self.skipWaiting()));});
self.addEventListener('activate',event=>{event.waitUntil(caches.keys().then(keys=>Promise.all(keys.filter(key=>key.startsWith('attic-offline-')&&key!==cacheName).map(key=>caches.delete(key)))).then(()=>self.clients.claim()));});
self.addEventListener('fetch',event=>{
 if(event.request.mode==='navigate'&&event.request.method==='GET'&&new URL(event.request.url).origin===self.location.origin){event.respondWith(fetch(event.request).catch(()=>caches.match('/offline.html')));}
});
