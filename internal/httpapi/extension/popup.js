'use strict';
const $ = id => document.getElementById(id);
(async () => {
  let saved, readerURL;
  const disable = value => { $('bookmark').disabled = $('kindle').disabled = value; };
  disable(true);
  const [tab] = await chrome.tabs.query({active: true, currentWindow: true});
  $('title').textContent = tab?.title || tab?.url || 'No web page selected.';
  if (!/^https?:\/\//.test(tab?.url || '')) {
    $('status').textContent = 'Select a web page.';
    return;
  }
  for (const action of ['bookmark', 'kindle']) $(action).addEventListener('click', async () => {
    if (action === 'bookmark' && saved) {
      await chrome.tabs.create({url: readerURL});
      return;
    }
    disable(true);
    $('status').textContent = saved ? 'Sending…' : 'Saving…';
    try {
      const result = await chrome.runtime.sendMessage({type: 'save', action, itemID: saved?.id, tabID: tab?.id, url: tab?.url, title: tab?.title, requestID: crypto.randomUUID()});
      if (!result.ok) throw new Error(result.error);
      $('status').textContent = result.message;
      if (saved) $('bookmark').disabled = false;
    } catch (error) {
      $('status').textContent = error.message;
      disable(false);
    }
  });
  $('status').textContent = 'Checking Attic…';
  try {
    const result = await chrome.runtime.sendMessage({type: 'lookup', url: tab.url});
    if (!result.ok) throw new Error(result.error);
    saved = result.item;
    readerURL = result.readerURL;
    if (saved) {
      $('bookmark').textContent = 'Open in Attic';
      const delivered = ['accepted', 'sent'].includes(saved.delivery_status);
      $('kindle').textContent = delivered ? 'Resend to Kindle' : 'Send to Kindle';
      const capture = {complete: 'Preserved', partial: 'Preserved', failed: 'Capture failed', blocked: 'Needs browser capture', queued: 'Capture queued', capturing: 'Preserving', not_captured: 'Not captured'}[saved.capture_status];
      const delivery = delivered ? 'Email Sent' : {failed: 'Delivery failed', uncertain: 'Outcome uncertain', unconfirmed: 'Outcome uncertain', pending: 'Preparing document', queued: 'Preparing document', sending: 'Preparing document', retrying: 'Preparing document'}[saved.delivery_status];
      $('status').textContent = ['Already in Attic', capture, delivery].filter(Boolean).join(' · ');
    } else $('status').textContent = '';
  } catch (error) { $('status').textContent = error.message; }
  disable(false);
})();
