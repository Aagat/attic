'use strict';

// Omnibox owns short-lived search requests; nothing enters the durable save queue.
(() => {
  let generation = 0, timer, controller;
  const suggestions = new Map();
  const escape = value => String(value).replace(/[&<>"']/g, character => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&apos;'}[character]));
  const defaultSuggestion = description => chrome.omnibox.setDefaultSuggestion({description}).catch(() => {});
  function cancel() {
    generation++;
    clearTimeout(timer);
    controller?.abort();
    suggestions.clear();
  }
  async function connection() {
    const config = await chrome.storage.local.get(['server', 'key']);
    if (!config.server || !config.key) throw new Error('Connect to Attic in extension settings');
    const server = new URL(config.server);
    if (!['http:', 'https:'].includes(server.protocol) || server.username || server.password || server.pathname !== '/' || server.search || server.hash) {
      throw new Error('Reconnect to Attic in extension settings');
    }
    if (!await chrome.permissions.contains({origins: [server.protocol + '//' + server.hostname + '/*']})) {
      throw new Error('Reconnect to Attic in extension settings');
    }
    return {server: server.origin, key: config.key};
  }
  chrome.omnibox.onInputStarted.addListener(() => {
    cancel();
    defaultSuggestion('Search your Attic library');
  });
  chrome.omnibox.onInputChanged.addListener((text, suggest) => {
    cancel();
    const current = generation;
    const query = text.trim().slice(0, 1000);
    defaultSuggestion(query ? 'Search Attic for <match>' + escape(query) + '</match>' : 'Search your Attic library');
    suggest([]);
    if (!query) return;
    timer = setTimeout(async () => {
      const request = new AbortController();
      controller = request;
      const timeout = setTimeout(() => request.abort(), 5000);
      try {
        const config = await connection();
        if (current !== generation) return;
        const params = new URLSearchParams({q: query, limit: '5'});
        const response = await fetch(config.server + '/api/v1/items?' + params, {
          credentials: 'omit', redirect: 'error', signal: request.signal,
          headers: {Authorization: 'Bearer ' + config.key},
        });
        if (!response.ok) throw new Error(response.status === 401 ? 'Reconnect to Attic in extension settings' : 'Suggestions unavailable — press Enter to search Attic');
        const data = await response.json();
        if (!Array.isArray(data.items)) throw new Error('Suggestions unavailable — press Enter to search Attic');
        if (current !== generation) return;
        const results = [];
        for (const item of data.items.slice(0, 5)) {
          if (typeof item.id !== 'string' || !item.id || item.id.length > 200) continue;
          const content = config.server + '/items/' + encodeURIComponent(item.id);
          if (suggestions.has(content)) continue;
          let host = '';
          try { host = new URL(item.url).hostname; } catch { /* Uploaded PDFs have no URL. */ }
          const title = typeof item.title === 'string' && item.title.trim() ? item.title.slice(0, 200) : 'Untitled item';
          suggestions.set(content, content);
          results.push({content, description: escape(title) + (host ? ' <dim>' + escape(host) + '</dim>' : '')});
        }
        suggest(results);
      } catch (error) {
        if (current === generation) {
          suggest([]);
          defaultSuggestion(escape(error.name === 'AbortError' ? 'Suggestions timed out — press Enter to search Attic' : error.message));
        }
      } finally {
        clearTimeout(timeout);
      }
    }, 200);
  });
  chrome.omnibox.onInputCancelled.addListener(cancel);
  chrome.omnibox.onInputEntered.addListener((text, disposition) => {
    const selected = suggestions.get(text);
    cancel();
    (async () => {
      const config = await connection();
      const url = selected && new URL(selected).origin === config.server
        ? selected
        : config.server + (text.trim() ? '/?' + new URLSearchParams({q: text.trim().slice(0, 1000)}) : '/');
      if (disposition === 'newForegroundTab' || disposition === 'newBackgroundTab') {
        await chrome.tabs.create({url, active: disposition === 'newForegroundTab'});
      } else {
        await chrome.tabs.update({url});
      }
    })().catch(() => chrome.runtime.openOptionsPage().catch(() => {}));
  });
  chrome.storage.onChanged.addListener((changes, area) => {
    if (area === 'local' && (changes.server || changes.key)) cancel();
  });
})();
