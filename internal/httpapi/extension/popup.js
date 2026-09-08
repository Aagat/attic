'use strict';
const $ = id => document.getElementById(id);
(async () => {
  const [tab] = await chrome.tabs.query({active: true, currentWindow: true});
  $('title').textContent = tab?.title || tab?.url || 'Open a web page to save it.';
  for (const action of ['bookmark', 'kindle']) $(''+action).addEventListener('click', async () => {
    $('bookmark').disabled = $('kindle').disabled = true;
    $('status').textContent = 'Saving…';
    try {
      const result = await chrome.runtime.sendMessage({type: 'save', action, url: tab?.url, title: tab?.title, requestID: crypto.randomUUID()});
      if (!result.ok) throw new Error(result.error);
      $('status').textContent = result.message;
    } catch (error) {
      $('status').textContent = error.message;
      $('bookmark').disabled = $('kindle').disabled = false;
    }
  });
})();
