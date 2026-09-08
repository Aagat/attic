'use strict';
const $ = id => document.getElementById(id);
let jobs = new Map(), cursor = '', loadedMore = false, filter = 'all', signedIn = false, refreshing = false;
let previewFile = null, previewGeneration = 0;
const pdfPreview=new AtticPDFPreview();
const active = job => ['queued','processing','delivering'].includes(job.status);
const failed = job => ['failed','delivery_failed','cancelled'].includes(job.status);
const stages = {queued:'Waiting in the queue',fetching:'Opening the article or an archive',extracting:'Finding the article',ai_analyzing:'Checking the source content',formatting:'Typesetting and checking the PDF',persisting:'Saving your PDF',delivering:'Sending to Kindle'};
const failures = {access_denied:'The original website and available archive sources could not provide a readable article.',paywall_detected:'The article is paywalled, and no usable archive copy was found.',pdf_quality_failed:'The PDF did not pass its quality checks. It has not been published.',format_failed:'This article could not be turned into a PDF.',fetch_failed:'The article could not be retrieved.',unsupported_content:'This page does not appear to be a readable article.',ai_auth_failed:'The server’s ChatGPT connection needs attention.'};
function notice(text, error=false) { $('notice').textContent=text; $('notice').classList.toggle('error',error); }
function showLogin() { signedIn=false; $('library').hidden=true; $('login').hidden=false; $('signout').hidden=true; closePreview(); }
function showLibrary() { signedIn=true; $('login').hidden=true; $('library').hidden=false; $('signout').hidden=false; }
async function api(path, options={}) {
 const response=await fetch('/api/v1'+path,{credentials:'same-origin',...options,headers:{...(options.body?{'Content-Type':'application/json'}:{}),...options.headers}});
 if(!response.ok) { let info;try{info=await response.json();}catch{} if(response.status===401)showLogin(); throw new Error(info?.error?.message||'The request could not be completed. Please try again.'); }
 return response.status===204 ? null : response.json();
}
function element(tag,text,className) { const node=document.createElement(tag); if(text!==undefined)node.textContent=text; if(className)node.className=className; return node; }
function action(label,fn,style='secondary') { const button=element('button',label,style);button.type='button';button.addEventListener('click',async()=>{button.disabled=true;try{await fn();}catch(error){notice(error.message,true);}finally{button.disabled=false;}});return button; }
function render() {
 const query=$('search').value.trim().toLowerCase();
 const all=[...jobs.values()].sort((a,b)=>b.created_at.localeCompare(a.created_at)||b.id.localeCompare(a.id));
 const shown=all.filter(job=>(filter==='all'||(filter==='ready'&&job.has_artifact)||(filter==='active'&&active(job))||(filter==='failed'&&failed(job)))&&(!query||`${job.title||''} ${job.source_host||''}`.toLowerCase().includes(query)));
 $('articles').replaceChildren();
 if(!shown.length) {const empty=element('div',undefined,'empty');empty.append(element('h3',jobs.size?'Nothing here yet.':'Your next good read starts here.'),element('p',jobs.size?'Try another filter, or load more articles.':'Paste an article link above. We’ll take care of the formatting.'));$('articles').append(empty);}
 for(const job of shown) {
  const card=element('article',undefined,'article'),info=element('div',undefined,'article-info'),top=element('div',undefined,'article-top');
  top.append(element('span',job.source_host||'Article','domain'),element('span',new Date(job.created_at).toLocaleDateString(undefined,{month:'short',day:'numeric'})));
  const label=job.has_artifact?'Ready to read':active(job)?(stages[job.stage||job.status]||'Working on it'):failed(job)?'Needs attention':job.status;
  top.append(element('span',label,'badge '+(active(job)?'active':failed(job)?'failed':'')));
  info.append(top,element('h3',job.title||job.source_host||'New article'));
  if(active(job)){const progress=element('div',undefined,'progress');const bar=element('span');bar.style.width=({queued:8,fetching:20,extracting:35,ai_analyzing:55,formatting:80,persisting:95}[job.stage||job.status]||10)+'%';progress.append(bar);info.append(progress);}
  if(failed(job))info.append(element('p',failures[job.failure_category]||'This article could not be completed. You can retry or remove it.'));
  const actions=element('div',undefined,'actions');
  if(job.has_artifact)actions.append(action('Read PDF',()=>openPreview(job)));
  if(failed(job))actions.append(action('Try again',async()=>{const next=await api('/jobs/'+encodeURIComponent(job.id)+'/retry',{method:'POST'});notice('Article queued again.');await refresh();}));
  actions.append(action(active(job)?'Cancel':'Remove',async()=>{if(!confirm(active(job)?'Cancel this article?':'Remove this article and its PDF?'))return;await api('/jobs/'+encodeURIComponent(job.id),{method:'DELETE'});jobs.delete(job.id);render();notice('Article removed.');},'quiet'));
  card.append(info,actions);$('articles').append(card);
 }
 $('more').hidden=!cursor;
}
async function refresh(more=false) {
 if(!signedIn||refreshing)return;refreshing=true;
 try {
  const page=await api('/jobs?limit=50'+(more&&cursor?'&cursor='+encodeURIComponent(cursor):''));
  let changed=more||jobs.size===0;
  for(const job of page.items){if(JSON.stringify(jobs.get(job.id))!==JSON.stringify(job))changed=true;jobs.set(job.id,job);}
  if(more||!loadedMore)cursor=page.next_cursor||'';
  if(more)loadedMore=true;
  if(changed)render();
 } catch(error){notice(error.message,true);} finally{refreshing=false;}
}
function closePreview() {
 previewGeneration++;
 if($('reader').open)$('reader').close();
 pdfPreview.close();previewFile=null;
}
async function openPreview(job) {
 closePreview();$('archive-source').hidden=true;const generation=previewGeneration;$('reader-title').textContent=job.title||'Article';$('reader-meta').textContent=job.source_host||'';$('reader-status').textContent='Opening PDF…';$('kindle-help').hidden=true;$('share').hidden=true;$('reader').showModal();
 try {
  const detail=await api('/jobs/'+encodeURIComponent(job.id));
  if(generation!==previewGeneration)return;
  $('reader-title').textContent=detail.title||'Article';
  $('reader-meta').textContent=[detail.metadata?.author,detail.metadata?.site_name,detail.metadata?.publication_date?.slice(0,10)].filter(Boolean).join(' · ');
  $('original').href=detail.submitted_url;
  const archived=/^https?:\/\/(?:web\.archive\.org|archive\.(?:ph|is|today|md|fo|li|vn))\//i.test(detail.canonical_url||'');
  $('archive-source').hidden=!archived;$('archive-source').href=archived?detail.canonical_url:'#';
  const path='/api/v1/jobs/'+encodeURIComponent(job.id)+'/artifact';$('download').href=path;
  const response=await fetch(path,{credentials:'same-origin'});
  if(!response.ok)throw new Error('Could not open this PDF. Try downloading it again.');
  const blob=await response.blob();if(generation!==previewGeneration||!$('reader').open)return;
  previewFile=new File([blob],detail.artifact?.filename||'article.pdf',{type:'application/pdf'});
  $('share').hidden=!(navigator.canShare&&navigator.canShare({files:[previewFile]}));
  await pdfPreview.load(blob);
 }catch(error){if(generation===previewGeneration)$('reader-status').textContent=error.message;}
}
$('login-form').addEventListener('submit',async event=>{event.preventDefault();const button=event.currentTarget.querySelector('button');button.disabled=true;$('login-error').textContent='';try{await api('/session',{method:'POST',headers:{Authorization:'Bearer '+$('access-key').value.trim()}});$('access-key').value='';showLibrary();await refresh();}catch(error){$('login-error').textContent=error.message;}finally{button.disabled=false;}});
$('signout').addEventListener('click',async()=>{try{await api('/session',{method:'DELETE'});jobs.clear();cursor='';loadedMore=false;showLogin();}catch(error){notice(error.message,true);}});
$('submit-form').addEventListener('submit',async event=>{event.preventDefault();const button=event.currentTarget.querySelector('button');button.disabled=true;try{const next=await api('/jobs',{method:'POST',body:JSON.stringify({url:$('article-url').value.trim()})});$('article-url').value='';history.replaceState(null,'',location.pathname);filter='all';document.querySelectorAll('[data-filter]').forEach(b=>b.setAttribute('aria-pressed',String(b.dataset.filter===filter)));notice('Saved. Your PDF is on its way.');await refresh();}catch(error){notice(error.message,true);}finally{button.disabled=false;}});
$('search').addEventListener('input',render);
$('more').addEventListener('click',()=>refresh(true));
for(const button of document.querySelectorAll('[data-filter]'))button.addEventListener('click',()=>{filter=button.dataset.filter;document.querySelectorAll('[data-filter]').forEach(b=>b.setAttribute('aria-pressed',String(b===button)));render();});
$('close-reader').addEventListener('click',closePreview);
$('reader').addEventListener('cancel',event=>{event.preventDefault();closePreview();});
$('kindle').addEventListener('click',()=>{$('kindle-help').hidden=!$('kindle-help').hidden;});
$('share').addEventListener('click',async()=>{if(!previewFile)return;try{await navigator.share({files:[previewFile],title:$('reader-title').textContent});}catch(error){if(error.name!=='AbortError')$('reader-status').textContent='Sharing is unavailable. Download the PDF and use Send to Kindle instead.';}});
const params=new URLSearchParams(location.search);
const shared=AtticShare.parse(params);
if(shared){$('article-url').value=shared;notice('Shared link ready. Tap Save article to add it to your library.');}
else if(['url','text','title'].some(key=>params.has(key))){notice('No web link was found in the shared content. Paste an article link to save it.',true);}
if('serviceWorker' in navigator&&isSecureContext)navigator.serviceWorker.register('/sw.js').catch(()=>{});
(async()=>{try{await api('/session');showLibrary();await refresh();}catch{showLogin();}})();
setInterval(()=>{if(signedIn&&!document.hidden)refresh();},4000);
