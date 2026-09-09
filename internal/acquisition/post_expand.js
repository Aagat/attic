(async () => {
  if (!/^(?:www\.|mobile\.)?(?:x|twitter)\.com$/.test(location.hostname)) return false;
  const id = location.pathname.match(/\/status\/(\d+)(?:\/|$)/)?.[1];
  if (!id) return false;
  function control() {
    for (const article of document.querySelectorAll('article[data-testid="tweet"]')) {
      // The first timestamp is this post's permalink; later ones can be quotes.
      const time = article.querySelector('a[href*="/status/"] time');
      const link = time?.closest('a');
      if (!link || time.closest('article') !== article) continue;
      const ownID = new URL(link.href, location.href).pathname.match(/\/status\/(\d+)(?:\/|$)/)?.[1];
      if (ownID !== id) continue;
      const button = article.querySelector('[data-testid="tweet-text-show-more-link"]');
      if (button && button.closest('article') === article && button.getClientRects().length && !button.disabled) return button;
    }
  }
  for (let attempt = 0; attempt < 3; attempt++) {
    const button = control();
    if (!button) return false;
    button.click();
    await new Promise(resolve => setTimeout(resolve, 700));
  }
  return Boolean(control());
})()
