(async () => {
  const maxNodes = %d, maxBytes = %d;
  const walker = document.createTreeWalker(document, NodeFilter.SHOW_ALL);
  let nodes = 0;
  while (walker.nextNode()) {
    if (++nodes > maxNodes) return { nodes, tooLarge: true, html: "" };
  }

  // Loading still passes through the browser's request and transfer budgets.
  // Bound both the count and wait; broken images cannot stall article capture.
  const originals = [...document.querySelectorAll("article img,main img")].slice(0, 32);
  await Promise.all(originals.map(img => {
    img.loading = "eager";
    return Promise.race([
      img.decode().catch(() => {}),
      new Promise(resolve => setTimeout(resolve, 4000))
    ]);
  }));

  const clone = document.documentElement.cloneNode(true);
  const copies = clone.querySelectorAll("article img,main img");
  let imageBytes = 0;
  originals.forEach((img, i) => {
    try {
      if (!img.complete || !img.naturalWidth) return;
      const vector = /\.svg(?:[?#]|$)/i.test(img.currentSrc || img.src);
      const scale = Math.min(vector ? Infinity : 1, 1200 / img.naturalWidth, 1600 / img.naturalHeight);
      const canvas = document.createElement("canvas");
      canvas.width = Math.max(1, Math.round(img.naturalWidth * scale));
      canvas.height = Math.max(1, Math.round(img.naturalHeight * scale));
      canvas.getContext("2d").drawImage(img, 0, 0, canvas.width, canvas.height);
      const data = canvas.toDataURL("image/png");
      if (data.length > 1400000 || imageBytes + data.length > 4000000) return;
      imageBytes += data.length;
      copies[i].setAttribute("src", data);
      copies[i].removeAttribute("srcset");
    } catch (_) {
      // Cross-origin images without canvas permission cannot be embedded.
    }
  });
  const html = clone.outerHTML;
  const tooLarge = new TextEncoder().encode(html).length > maxBytes;
  return { nodes, tooLarge, html: tooLarge ? "" : html };
})()
