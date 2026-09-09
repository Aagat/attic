'use strict';
// Blobs stay in IndexedDB, shared by extension documents and its service worker.
const snapshots = (() => {
  async function operation(mode, action) {
    const db = await new Promise((resolve, reject) => {
      const request = indexedDB.open('attic-page-captures', 1);
      request.onupgradeneeded = () => request.result.createObjectStore('snapshots');
      request.onsuccess = () => resolve(request.result);
      request.onerror = () => reject(request.error);
    });
    try {
      return await new Promise((resolve, reject) => {
        const transaction = db.transaction('snapshots', mode);
        const request = action(transaction.objectStore('snapshots'));
        transaction.oncomplete = () => resolve(request.result);
        transaction.onerror = () => reject(transaction.error);
        transaction.onabort = () => reject(transaction.error || new Error('Capture storage interrupted.'));
      });
    } finally { db.close(); }
  }
  return {
    clear: () => operation('readwrite', store => store.clear()),
    get: id => operation('readonly', store => store.get(id)),
    put: (id, blob) => operation('readwrite', store => store.put(blob, id)),
    remove: id => operation('readwrite', store => store.delete(id)),
  };
})();
