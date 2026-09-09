'use strict';
const $ = id => document.getElementById(id);
(async () => {
 const config = await chrome.storage.local.get(['server', 'key', 'bookmarkSync', 'syncStatus']);
 $('bookmark-sync').checked = Boolean(config.bookmarkSync);
 $('sync-status').textContent = config.syncStatus || '';
 $('server').value = config.server || '';
 $('key').value = config.key || '';
 if (config.server) {
  $('library').href = config.server;
  $('library').hidden = false;
  $('disconnect').hidden = false;
 }
})();
$('setup').addEventListener('submit', async event => {
 event.preventDefault();
 const button = event.currentTarget.querySelector('button');
 let server, origin;
 try {
  const url = new URL($('server').value.trim());
  if (!['https:', 'http:'].includes(url.protocol) || url.username || url.password || url.search || url.hash || !['', '/'].includes(url.pathname)) throw new Error();
  server = url.origin;
  origin = url.protocol + '//' + url.hostname + '/*';
 } catch {
  $('status').textContent = 'Invalid server address.';
  return;
 }
 const key = $('key').value.trim();
 if (!key) { $('status').textContent = 'Access key required.'; return; }
 button.disabled = true;
 try {
  // The permission request must happen before any await, during the submit gesture.
  const granted = await chrome.permissions.request({origins: [origin]});
  if (!granted) { $('status').textContent = 'Server access not permitted.'; return; }
  $('status').textContent = 'Connecting…';
  const response = await fetch(server + '/api/v1/session', {
   credentials: 'omit', redirect: 'error', headers: {Authorization: 'Bearer ' + key}, signal: AbortSignal.timeout(15000)
  });
  if (!response.ok) throw new Error(response.status === 401 ? 'Access key rejected.' : 'Server returned HTTP ' + response.status + '.');
  const body = await response.json();
  if (body.authenticated !== true) throw new Error('This address is not an Attic server.');
  await chrome.storage.local.set({server, key});
  chrome.runtime.sendMessage({type: 'reconcile'}).catch(() => {});
  // Remove any old or unsuccessfully configured server grants after connecting.
  const permissions = await chrome.permissions.getAll();
  const unused = (permissions.origins || []).filter(granted => granted !== origin);
  if (unused.length) await chrome.permissions.remove({origins: unused});
  $('status').textContent = 'Connected.';
  $('library').href = server;
  $('library').hidden = false;
  $('disconnect').hidden = false;
 } catch (error) {
  $('status').textContent = error.name === 'TimeoutError' ? 'Connection timed out.' : error instanceof TypeError ? 'Cannot reach Attic.' : error.message;
 } finally { button.disabled = false; }
});
$('disconnect').addEventListener('click', async () => {
 try {
  await chrome.storage.local.remove(['server', 'key', 'pendingSaves', 'pendingBookmarks', 'syncStatus', 'captureStatus']);
  await snapshots.clear();
  await chrome.storage.local.set({bookmarkSync: false});
  $('bookmark-sync').checked = false;
  const permissions = await chrome.permissions.getAll();
  if (permissions.origins?.length) await chrome.permissions.remove({origins: permissions.origins});
  $('key').value = '';
  $('library').hidden = true;
  $('disconnect').hidden = true;
  $('status').textContent = 'Disconnected.';
 } catch { $('status').textContent = 'Could not disconnect.'; }
});

$('bookmark-sync').addEventListener('change', async () => {
 try {
  const enabled = $('bookmark-sync').checked;
  if (enabled && !await chrome.permissions.request({permissions: ['bookmarks']})) {
   $('bookmark-sync').checked = false;
   throw new Error('Bookmark access not permitted.');
  }
  await chrome.storage.local.set({bookmarkSync: enabled});
  if (!enabled) {
   await chrome.storage.local.remove('pendingBookmarks');
   await chrome.permissions.remove({permissions: ['bookmarks']});
  }
  $('sync-status').textContent = enabled ? 'Importing browser bookmarks…' : 'Automatic saving is off.';
  await chrome.runtime.sendMessage({type: 'reconcile'});
  const state = await chrome.storage.local.get(['syncStatus']);
  if (enabled) $('sync-status').textContent = state.syncStatus || 'Waiting for connection.';
 } catch (error) { $('sync-status').textContent = error.message; }
});
$('sync-now').addEventListener('click', async () => {
 $('sync-status').textContent = 'Checking bookmarks…';
 await chrome.runtime.sendMessage({type: 'reconcile'});
 const state = await chrome.storage.local.get(['syncStatus']);
 $('sync-status').textContent = state.syncStatus || 'Not connected.';
});
chrome.storage.onChanged.addListener(changes => {
 if (changes.syncStatus) $('sync-status').textContent = changes.syncStatus.newValue || '';
});
