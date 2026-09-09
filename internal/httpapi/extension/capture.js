'use strict';
(async () => {
  const params = new URLSearchParams(location.search);
  const requestID = params.get('id');
  let error;
  try {
    const tabId = Number(params.get('tab'));
    const expected = params.get('url');
    const before = await chrome.tabs.get(tabId);
    if (before.url !== expected) throw new Error('The page changed before it could be captured.');
    const blob = await new Promise((resolve, reject) => {
      const timeout = setTimeout(() => reject(new Error('Browser capture timed out.')), 90000);
      chrome.pageCapture.saveAsMHTML({tabId}, result => {
        clearTimeout(timeout);
        if (chrome.runtime.lastError) reject(new Error(chrome.runtime.lastError.message));
        else resolve(result);
      });
    });
    const after = await chrome.tabs.get(tabId);
    if (after.url !== expected) throw new Error('The page changed while it was being captured.');
    if (!blob?.size) throw new Error('The browser could not capture this page.');
    if (blob.size > 32 * 1024 * 1024) throw new Error('This browser copy exceeds the 32 MB upload limit.');
    await snapshots.put(requestID, blob);
  } catch (cause) { error = cause.message; }
  // The worker acknowledges persistence before starting network work.
  await chrome.runtime.sendMessage({type: 'captureFinished', requestID, error}).catch(() => {});
  const self = await chrome.tabs.getCurrent();
  if (self?.id !== undefined) await chrome.tabs.remove(self.id);
})();
