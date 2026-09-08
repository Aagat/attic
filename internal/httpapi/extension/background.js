'use strict';

const alarmName = 'attic-reconcile';
let running;
let again = false;
let queueMutation = Promise.resolve();
function updateSaves(change) {
  const result = queueMutation.then(async () => {
    const state = await chrome.storage.local.get(['pendingSaves']);
    const pending = state.pendingSaves || {};
    change(pending);
    await chrome.storage.local.set({pendingSaves: pending});
  });
  queueMutation = result.catch(() => {});
  return result;
}

function webURL(raw) {
  const url = new URL(raw);
  if (!['http:', 'https:'].includes(url.protocol)) throw new Error('Only web links can be saved.');
  return url.href;
}

async function request(config, path, body, requestID) {
  if (!config.server || !config.key) throw new Error('Connect to Attic in extension settings first.');
  const endpoint = new URL(config.server);
  if (!await chrome.permissions.contains({origins: [endpoint.protocol + '//' + endpoint.hostname + '/*']})) {
    throw new Error('Reconnect to Attic in extension settings to allow server access.');
  }
  const response = await fetch(config.server + path, {
    method: 'POST', credentials: 'omit', redirect: 'error', signal: AbortSignal.timeout(20000),
    headers: {Authorization: 'Bearer ' + config.key, 'Content-Type': 'application/json', 'Idempotency-Key': requestID || crypto.randomUUID()},
    body: JSON.stringify(body),
  });
  if (response.status === 401) throw new Error('Access key rejected. Reconnect in extension settings.');
  if (!response.ok) throw new Error('Attic returned HTTP ' + response.status + '. The save will retry.');
  return response.json();
}

async function enqueueSave(message) {
  const config = await chrome.storage.local.get(['server', 'key', 'pendingSaves']);
  if (!config.server || !config.key) throw new Error('Connect to Attic in extension settings first.');
  const id = message.requestID || crypto.randomUUID();
  const item = {url: webURL(message.url), title: message.title || '', action: message.action === 'kindle' ? 'kindle' : 'bookmark'};
  await updateSaves(pending => { pending[id] = item; });
  await reconcile();
  const state = await chrome.storage.local.get(['pendingSaves', 'syncStatus']);
  return {queued: Boolean(state.pendingSaves?.[id]), message: state.pendingSaves?.[id] ? 'Saved in this browser; Attic will retry when connected.' : 'Saved to Attic.'};
}

// One owner serializes durable queues, bookmark scans, and network retries.
function reconcile() {
  if (running) { again = true; return running; }
  running = (async () => {
    do {
      again = false;
      try { await syncOnce(); }
      catch (error) { await chrome.storage.local.set({syncStatus: 'Waiting to retry: ' + error.message}); }
    } while (again);
  })().finally(() => { running = undefined; });
  return running;
}

async function syncOnce() {
  const config = await chrome.storage.local.get(['server', 'key', 'bookmarkSync', 'clientID', 'pendingBookmarks', 'pendingSaves', 'knownBookmarks', 'syncServer']);
  if (!config.server || !config.key) return;
  const clientID = config.clientID || crypto.randomUUID();
  if (!config.clientID) await chrome.storage.local.set({clientID});
  const pending = config.pendingBookmarks || {};
  const known = config.syncServer === config.server ? (config.knownBookmarks || {}) : {};
  if (config.bookmarkSync && await chrome.permissions.contains({permissions: ['bookmarks']})) {
    function collect(nodes, folders = []) {
      for (const node of nodes) {
        if (node.url) {
          try {
            const item = {url: webURL(node.url), title: node.title || '', action: 'bookmark', source: {
              client_id: clientID, node_id: node.id, folder: folders.join('/'),
              saved_at: new Date(node.dateAdded || Date.now()).toISOString(),
            }};
            if (known[node.id] !== JSON.stringify(item)) pending[node.id] = item;
          } catch { /* Browser-internal bookmarks are not web pages. */ }
        } else collect(node.children || [], node.title ? [...folders, node.title] : folders);
      }
    }
    collect(await chrome.bookmarks.getTree());
    await chrome.storage.local.set({pendingBookmarks: pending});
  }
  // Manual saves remain retryable even when bookmark ingestion is disabled.
  for (const [id, item] of Object.entries(config.pendingSaves || {})) {
    await request(config, '/api/v1/items', item, id);
    await updateSaves(pending => { delete pending[id]; });
  }
  if (config.bookmarkSync) {
    const ids = Object.keys(pending);
    for (let offset = 0; offset < ids.length; offset += 100) {
      const batch = ids.slice(offset, offset + 100);
      const result = await request(config, '/api/v1/items/import', {bookmarks: batch.map(id => pending[id])});
      if (result.errors?.length || (typeof result.errors === 'number' && result.errors > 0)) {
        throw new Error('Some browser bookmarks could not be imported.');
      }
      for (const id of batch) { known[id] = JSON.stringify(pending[id]); delete pending[id]; }
      await chrome.storage.local.set({pendingBookmarks: pending, knownBookmarks: known, syncServer: config.server});
    }
  }
  await chrome.storage.local.set({syncStatus: 'Up to date · ' + new Date().toLocaleString()});
}

chrome.runtime.onInstalled.addListener(() => {
  chrome.contextMenus.removeAll(() => {
    for (const action of ['bookmark', 'kindle']) chrome.contextMenus.create({
      id: action, title: action === 'bookmark' ? 'Bookmark in Attic' : 'Send to Kindle via Attic',
      contexts: ['link', 'page'], documentUrlPatterns: ['http://*/*', 'https://*/*'],
    });
  });
  chrome.alarms.create(alarmName, {periodInMinutes: 1});
  reconcile();
});
chrome.runtime.onStartup.addListener(() => { chrome.alarms.create(alarmName, {periodInMinutes: 1}); reconcile(); });
chrome.alarms.onAlarm.addListener(alarm => { if (alarm.name === alarmName) reconcile(); });
chrome.runtime.onMessage.addListener((message, sender, reply) => {
  if (sender.id !== chrome.runtime.id) return;
  const operation = message.type === 'save' ? enqueueSave(message) : message.type === 'reconcile' ? reconcile() : undefined;
  if (!operation) return;
  operation.then(result => reply({ok: true, ...result}), error => reply({ok: false, error: error.message}));
  return true;
});
chrome.contextMenus.onClicked.addListener(async (info, tab) => {
  if (!['bookmark', 'kindle'].includes(info.menuItemId)) return;
  let text, title;
  try {
    const result = await enqueueSave({url: info.linkUrl || info.pageUrl || tab?.url, title: info.linkUrl ? '' : tab?.title, action: info.menuItemId});
    text = result.queued ? '…' : '✓'; title = result.message;
  } catch (error) { text = '!'; title = error.message; }
  if (tab?.id !== undefined) {
    await chrome.action.setBadgeText({tabId: tab.id, text});
    await chrome.action.setTitle({tabId: tab.id, title});
  }
});
let bookmarkListeners = false;
function listenToBookmarks() {
  if (bookmarkListeners || !chrome.bookmarks) return;
  for (const event of ['onCreated', 'onChanged', 'onMoved', 'onImportEnded']) {
    chrome.bookmarks[event].addListener(() => reconcile());
  }
  bookmarkListeners = true;
}
listenToBookmarks();
chrome.permissions.onAdded.addListener(() => { listenToBookmarks(); reconcile(); });

