"use strict";
const $ = id => document.getElementById(id);
let items = [], total = 0, signedIn = false, requestVersion = 0, listRequest, listActive = false, searchTimer, selectedItem;
const reader = new AtticReader(api);
const itemPath = item => '/items/' + encodeURIComponent(item.id);
function notice(text, error = false) {
  $('notice').textContent = text;
  $('notice').classList.toggle('error', error);
}
function showLogin() {
  signedIn = false;
  requestVersion++;
  listRequest?.abort();
  $('library').hidden = true; $('login').hidden = false; $('signout').hidden = true;
  reader.close(); $('item-details').close();
}
function showLibrary() {
  signedIn = true;
  $('login').hidden = true; $('library').hidden = false; $('signout').hidden = false;
}
async function api(path, options = {}) {
  const {format = 'json', ...requestOptions} = options;
  const response = await fetch('/api/v1' + path, {
    credentials: 'same-origin', ...requestOptions,
    headers: {...(options.body && !(options.body instanceof FormData) ? {'Content-Type':'application/json'} : {}), ...options.headers},
  });
  if (!response.ok) {
    let info; try { info = await response.json(); } catch {}
    if (response.status === 401) showLogin();
    throw new Error(info?.error?.message || (typeof info?.error === 'string' ? info.error : '') || 'The request could not be completed. Please try again.');
  }
  return response.status === 204 ? null : format === 'blob' ? response.blob() : response.json();
}
function element(tag, text, className) {
  const node = document.createElement(tag);
  if (text !== undefined) node.textContent = text;
  if (className) node.className = className;
  return node;
}
function action(label, fn, style = 'secondary') {
  const button = element('button', label, style); button.type = 'button';
  button.addEventListener('click', async () => {
    button.disabled = true;
    try { await fn(); } catch (error) { notice(error.message, true); }
    finally { button.disabled = false; }
  });
  return button;
}
function link(label, href) {
  const node = element('a', label); node.href = href; node.target = '_blank'; node.rel = 'noreferrer'; return node;
}
const readable = value => String(value || 'not requested').replaceAll('_', ' ');
function render() {
  const cards = [];
  for (const item of items) {
    const card = element('article', undefined, 'article'), info = element('div', undefined, 'article-info');
    let domain = 'PDF'; try { domain = new URL(item.url).hostname; } catch {}
    const top = element('div', undefined, 'article-top');
    top.append(element('span', domain, 'domain'), element('span', new Date(item.saved_at).toLocaleDateString()));
    info.append(top, element('h3', item.title || item.url || 'Untitled document'));
    if (item.snippet) info.append(element('p', item.snippet, 'snippet'));
    else if (item.notes) info.append(element('p', item.notes));
    const statuses = element('div', undefined, 'item-statuses');
    for (const [label, value] of [['Archive', item.kind === 'pdf' ? 'original preserved' : item.capture_status], ['PDF', item.pdf_status], ['Kindle', item.delivery_status], ['Search', item.index_status]]) {
      statuses.append(element('span', label + ': ' + readable(value), 'badge ' + (['failed','blocked'].includes(value) ? 'failed' : ['pending','queued','processing'].includes(value) ? 'active' : '')));
    }
    info.append(statuses);
    if (item.kind === 'pdf' && item.text_available === false) info.append(element('p', 'No extractable text; filename and notes are searchable.', 'hint'));
    if (item.folders?.length) info.append(element('p', item.folders.join(' · '), 'hint'));
    if (item.tags?.length) info.append(element('p', item.tags.join(' · '), 'tags'));
    const actions = element('div', undefined, 'actions');
    if (item.has_pdf && item.job_id) actions.append(action('Read PDF', () => reader.open(item.job_id)));
    actions.append(action('Details', () => openDetails(item)));
    actions.append(action('Send to Kindle', async () => {
      await api(itemPath(item) + '/send', {method:'POST', headers:{'Idempotency-Key':crypto.randomUUID()}});
      notice('Kindle delivery requested.'); await refresh();
    }));
    card.append(info, actions); cards.push(card);
  }
  if (!cards.length) {
    const empty = element('div', undefined, 'empty');
    empty.append(element('h3', 'Nothing here yet.'), element('p', 'Save a link or PDF, or try another search.'));
    cards.push(empty);
  }
  $('articles').replaceChildren(...cards);
  $('item-count').textContent = '(' + total + ')';
  $('more').hidden = items.length >= total;
}
async function refresh(more = false) {
  if (!signedIn) return;
  const version = ++requestVersion;
  listActive = true;
  listRequest?.abort(); listRequest = new AbortController();
  const wanted = more ? 50 : Math.max(50, items.length);
  const params = new URLSearchParams({limit:String(Math.min(100, wanted)), offset:String(more ? items.length : 0)});
  for (const [key,id] of [['q','search'],['domain','filter-domain'],['tag','filter-tag'],['capture_status','filter-capture'],['from','filter-from'],['to','filter-to']]) {
    if ($(id).value.trim()) params.set(key, $(id).value.trim());
  }
  try {
    const page = await api('/items?' + params, {signal:listRequest.signal});
    if (version !== requestVersion || !signedIn) return;
    const received = [...page.items];
    while (!more && received.length < wanted && received.length < page.total) {
      params.set('offset', String(received.length));
      params.set('limit', String(Math.min(100, wanted - received.length)));
      const remaining = await api('/items?' + params, {signal:listRequest.signal});
      if (version !== requestVersion || !signedIn) return;
      if (!remaining.items.length) break;
      received.push(...remaining.items);
    }
    const next = more ? [...items, ...received] : received;
    const changed = JSON.stringify(items) !== JSON.stringify(next) || total !== page.total;
    items = next; total = page.total;
    $('index-status').textContent = page.index_status && !['ready','indexed','available'].includes(page.index_status) ? 'Search: ' + readable(page.index_status) + '. Saved content is preserved while indexing catches up.' : '';
    if (page.storage_bytes !== undefined) $('storage-usage').textContent = 'Stored content: ' + (page.storage_bytes / 1048576).toFixed(1) + ' MB';
    if (changed || !$('articles').children.length) render();
  } catch (error) { if (error.name !== 'AbortError') notice(error.message, true); }
  finally { if (version === requestVersion) listActive = false; }
}
async function openDetails(item) {
  selectedItem = await api(itemPath(item));
  $('edit-title').value = selectedItem.title || '';
  $('edit-notes').value = selectedItem.notes || '';
  $('edit-tags').value = (selectedItem.tags || []).join(', ');
  $('edit-status').textContent = '';
  $('suggested-tags').textContent = (selectedItem.classification ? selectedItem.classification + ' · ' : '') + (selectedItem.suggested_tags?.length ? 'AI suggested tags: ' + selectedItem.suggested_tags.join(', ') : 'Classification: ' + readable(selectedItem.enrichment_status));
  $('accept-tags').hidden = !selectedItem.suggested_tags?.length;
  $('capture-list').replaceChildren();
  for (const capture of selectedItem.captures || []) {
    const row = element('div', undefined, 'capture-version');
    const path = '/api/v1' + itemPath(item) + '/captures/' + encodeURIComponent(capture.id);
    row.append(element('p', new Date(capture.created_at).toLocaleString() + ' · ' + readable(capture.status)));
    if (['complete', 'partial'].includes(capture.status)) row.append(link('Read saved page', path + '?view=reader'));
    row.append(link('Original layout', path));
    if (capture.missing?.length) row.append(element('span', capture.missing.length + ' missing resources', 'hint'));
    $('capture-list').append(row);
  }
  if (!$('capture-list').children.length) $('capture-list').append(element('p', item.kind === 'pdf' ? 'The original PDF is preserved.' : 'No saved page is available yet.'));
  const actions = [];
  if (selectedItem.enrichment_status === 'failed') actions.push(action('Retry classification', async () => {
    await api(itemPath(item) + '/enrich', {method:'POST'}); $('item-details').close(); notice('Classification queued again.'); await refresh();
  }));
  if (item.url) actions.push(link('Original page ↗', item.url));
  if (item.kind !== 'pdf') actions.push(action('Capture again', async () => {
    await api(itemPath(item) + '/recapture', {method:'POST'}); $('item-details').close(); notice('A new capture was requested. Previous versions are preserved.'); await refresh();
  }));
  actions.push(action('Remove from Attic', async () => {
    if (!confirm('Permanently remove this item and its saved content from Attic? Your browser bookmarks remain unchanged.')) return;
    await api(itemPath(item), {method:'DELETE'}); $('item-details').close(); notice('Removed from Attic.'); await refresh();
  }, 'quiet'));
  $('item-actions').replaceChildren(...actions);
  $('item-details').showModal();
}
$('close-details').addEventListener('click', () => $('item-details').close());
$('accept-tags').addEventListener('click', () => {
  const current = $('edit-tags').value.split(',').map(v => v.trim()).filter(Boolean);
  $('edit-tags').value = [...new Set([...current, ...selectedItem.suggested_tags])].join(', ');
});
$('edit-form').addEventListener('submit', async event => {
  event.preventDefault();
  try {
    await api(itemPath(selectedItem), {method:'PUT', body:JSON.stringify({title:$('edit-title').value.trim(), notes:$('edit-notes').value, tags:$('edit-tags').value.split(',').map(v => v.trim()).filter(Boolean)})});
    $('edit-status').textContent = 'Changes saved.'; await refresh();
  } catch (error) { $('edit-status').textContent = error.message; }
});
$('login-form').addEventListener('submit', async event => {
  event.preventDefault(); const button = event.currentTarget.querySelector('button'); button.disabled = true;
  $('login-error').textContent = '';
  try {
    await api('/session', {method:'POST', headers:{Authorization:'Bearer ' + $('access-key').value.trim()}});
    $('access-key').value = ''; showLibrary(); await refresh();
  } catch (error) { $('login-error').textContent = error.message; }
  finally { button.disabled = false; }
});
$('signout').addEventListener('click', async () => {
  try { await api('/session', {method:'DELETE'}); items = []; total = 0; $('articles').replaceChildren(); showLogin(); }
  catch (error) { notice(error.message, true); }
});
$('submit-form').addEventListener('submit', async event => {
  event.preventDefault();
  const buttons = [...event.currentTarget.querySelectorAll('button')]; buttons.forEach(b => b.disabled = true);
  const intent = event.submitter?.value || 'bookmark';
  try {
    await api('/items', {method:'POST', headers:{'Idempotency-Key':crypto.randomUUID()}, body:JSON.stringify({url:$('article-url').value.trim(), action:intent})});
    $('article-url').value = ''; history.replaceState(null, '', location.pathname);
    notice(intent === 'kindle' ? 'Saved. Kindle delivery requested.' : 'Bookmarked. Attic will preserve a copy.');
  } catch (error) { notice(error.message, true); }
  finally { await refresh(); buttons.forEach(b => b.disabled = false); }
});
for (const [formID, fileID, endpoint] of [['upload-form','pdf-file','/items/upload'],['import-form','bookmark-file','/items/import'],['restore-form','restore-file','/items/restore']]) {
  $(formID).addEventListener('submit', async event => {
    event.preventDefault(); const button = event.currentTarget.querySelector('button'); button.disabled = true;
    try {
      const body = new FormData(); body.append('file', $(fileID).files[0]);
      if (formID === 'upload-form') body.append('action', $('upload-action').value);
      notice('Saving your file…');
      const result = await api(endpoint, {method:'POST', body, headers:{'Idempotency-Key':crypto.randomUUID()}});
      const errors = Array.isArray(result.errors) ? result.errors.length : result.errors || 0;
      notice(result.imported !== undefined ? `${result.imported} imported · ${result.merged || 0} merged · ${result.skipped || 0} skipped${errors ? ' · ' + errors + ' errors; retry this import to continue.' : ''}` : result.restored !== undefined ? result.restored + ' items restored.' : 'Document saved.', Boolean(errors));
      $(fileID).value = ''; await refresh();
    } catch (error) { notice(error.message, true); }
    finally { await refresh(); button.disabled = false; }
  });
}
$('export').addEventListener('click', async () => {
  $('export').disabled = true;
  try {
    const blob = await api('/items/export', {format:'blob'}), url = URL.createObjectURL(blob);
    const download = element('a'); download.href = url; download.download = 'attic-export.zip'; download.click();
    setTimeout(() => URL.revokeObjectURL(url), 60000);
  } catch (error) { notice(error.message, true); }
  finally { $('export').disabled = false; }
});
for (const id of ['search','filter-domain','filter-tag','filter-capture','filter-from','filter-to']) {
  $(id).addEventListener('input', () => {
    clearTimeout(searchTimer); searchTimer = setTimeout(() => { items = []; refresh(); }, 250);
  });
}
$('more').addEventListener('click', () => refresh(true));
const params = new URLSearchParams(location.search), shared = AtticShare.parse(params);
if (shared) { $('article-url').value = shared; notice('Shared link ready. Choose Bookmark or Send to Kindle.'); }
else if (['url','text','title'].some(key => params.has(key))) notice('No web link was found in the shared content. Paste a link to save it.', true);
if ('serviceWorker' in navigator && isSecureContext) navigator.serviceWorker.register('/sw.js').catch(() => {});
(async () => { try { await api('/session'); showLibrary(); await refresh(); } catch { showLogin(); } })();
setInterval(() => { if (signedIn && !document.hidden && !listActive) refresh(); }, 4000);
