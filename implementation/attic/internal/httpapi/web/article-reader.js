"use strict";
(() => {
  // Owns a single canvas and PDF worker. No browser PDF plugin or blob iframe.
  class PDFPreview {
    constructor() {
      this.viewer = document.getElementById("pdf-viewer");
      this.canvas = document.getElementById("pdf-canvas");
      this.status = document.getElementById("reader-status");
      this.input = document.getElementById("pdf-page");
      this.generation = 0;
      this.renderVersion = 0;
      this.zoom = 1;
      document.getElementById("pdf-prev").onclick = () =>
        this.go(this.number - 1);
      document.getElementById("pdf-next").onclick = () =>
        this.go(this.number + 1);
      this.input.onchange = () => this.go(Number(this.input.value));
      document.getElementById("pdf-smaller").onclick = () =>
        this.setZoom(this.zoom / 1.25);
      document.getElementById("pdf-larger").onclick = () =>
        this.setZoom(this.zoom * 1.25);
      this.observer = new ResizeObserver(() => {
        // Height changes (status, browser chrome, help panel) do not change page scale.
        // The last requested width also suppresses our own canvas/layout notifications.
        const width = this.viewer.clientWidth;
        if (!this.doc || width <= 0 || width === this.renderWidth) return;
        clearTimeout(this.resizeTimer);
        this.resizeTimer = setTimeout(() => {
          if (
            this.doc &&
            this.viewer.clientWidth > 0 &&
            this.viewer.clientWidth !== this.renderWidth
          )
            this.render();
        }, 150);
      });
      this.observer.observe(this.viewer);
    }
    close() {
      this.generation++;
      this.renderVersion++;
      this.renderWidth = 0;
      clearTimeout(this.resizeTimer);
      if (this.renderTask) this.renderTask.cancel();
      this.renderTask = null;
      const task = this.loadingTask;
      this.loadingTask = null;
      this.doc = null;
      if (task) task.destroy().catch(() => {});
      this.canvas.width = 0;
      this.canvas.height = 0;
      delete this.canvas.dataset.page;
      document.getElementById("pdf-controls").hidden = true;
      this.viewer.hidden = true;
    }
    async load(blob) {
      this.close();
      const generation = this.generation;
      const pdfjs = await import("/pdfjs/pdf.mjs");
      const data = await blob.arrayBuffer();
      if (generation !== this.generation) return;
      pdfjs.GlobalWorkerOptions.workerSrc = "/pdfjs/pdf.worker.mjs";
      this.loadingTask = pdfjs.getDocument({
        data,
        isEvalSupported: false,
        isOffscreenCanvasSupported: false,
        cMapUrl: "/pdfjs/cmaps/",
        cMapPacked: true,
        standardFontDataUrl: "/pdfjs/standard_fonts/",
        wasmUrl: "/pdfjs/wasm/",
      });
      const doc = await this.loadingTask.promise;
      if (generation !== this.generation) return;
      this.doc = doc;
      this.number = 1;
      this.zoom = 1;
      this.input.max = doc.numPages;
      document.getElementById("pdf-total").textContent = String(doc.numPages);
      document.getElementById("pdf-controls").hidden = false;
      this.viewer.hidden = false;
      await this.render();
    }
    go(number) {
      if (!this.doc) return;
      this.number = Math.max(
        1,
        Math.min(this.doc.numPages, Math.round(number) || 1),
      );
      this.viewer.scrollTop = 0;
      this.render();
    }
    setZoom(zoom) {
      this.zoom = Math.max(1, Math.min(2.5, zoom));
      this.render();
    }
    async render() {
      if (!this.doc || this.viewer.clientWidth <= 0) return;
      const width = this.viewer.clientWidth;
      this.renderWidth = width;
      const doc = this.doc,
        generation = this.generation,
        version = ++this.renderVersion,
        number = this.number;
      const oldTask = this.renderTask;
      if (oldTask) {
        oldTask.cancel();
        try {
          await oldTask.promise;
        } catch {}
      }
      if (generation !== this.generation || version !== this.renderVersion)
        return;
      this.status.textContent = "Rendering page " + number + "…";
      try {
        const page = await doc.getPage(number);
        if (generation !== this.generation || version !== this.renderVersion)
          return;
        const base = page.getViewport({ scale: 1 });
        const cssScale = (Math.max(1, width - 24) / base.width) * this.zoom;
        const css = page.getViewport({ scale: cssScale });
        // Cap the canvas at four megapixels, including on high-density phones.
        const ratio = Math.min(
          devicePixelRatio || 1,
          2,
          Math.sqrt(4000000 / (css.width * css.height)),
        );
        const viewport = page.getViewport({ scale: cssScale * ratio });
        this.canvas.width = Math.ceil(viewport.width);
        this.canvas.height = Math.ceil(viewport.height);
        this.canvas.style.width = css.width + "px";
        this.canvas.style.height = css.height + "px";
        this.renderTask = page.render({
          canvasContext: this.canvas.getContext("2d"),
          viewport,
        });
        await this.renderTask.promise;
        if (generation !== this.generation || version !== this.renderVersion)
          return;
        this.renderTask = null;
        this.canvas.dataset.page = String(number);
        this.canvas.setAttribute(
          "aria-label",
          "PDF page " + number + " of " + doc.numPages,
        );
        this.input.value = number;
        document.getElementById("pdf-prev").disabled = number === 1;
        document.getElementById("pdf-next").disabled = number === doc.numPages;
        document.getElementById("pdf-smaller").disabled = this.zoom <= 1;
        document.getElementById("pdf-larger").disabled = this.zoom >= 2.5;
        this.status.textContent = "";
        page.cleanup();
      } catch (error) {
        if (
          generation === this.generation &&
          version === this.renderVersion &&
          error.name !== "RenderingCancelledException"
        )
          this.status.textContent =
            "Preview could not be rendered. You can still download or share the PDF.";
      }
    }
  }

  // The library only opens or closes an article. This module owns everything
  // attached to that reading session, including pending requests and PDF resources.
  class ArticleReader {
    #request;
    #pdf = new PDFPreview();
    #session = null;
    #file = null;
    #dialog = document.getElementById("reader");

    constructor(request) {
      this.#request = request;
      document
        .getElementById("close-reader")
        .addEventListener("click", () => this.close());
      this.#dialog.addEventListener("cancel", (event) => {
        event.preventDefault();
        this.close();
      });
      document.getElementById("kindle").addEventListener("click", () => {
        const help = document.getElementById("kindle-help");
        help.hidden = !help.hidden;
      });
      document
        .getElementById("share")
        .addEventListener("click", () => this.#share());
    }

    close() {
      this.#session?.abort();
      this.#session = null;
      this.#file = null;
      this.#pdf.close();
      if (this.#dialog.open) this.#dialog.close();
      for (const id of ["download", "original", "archive-source"]) {
        const link = document.getElementById(id);
        link.removeAttribute("href");
        link.hidden = true;
      }
      document.getElementById("share").hidden = true;
    }

    async open(jobID) {
      this.close();
      const session = new AbortController();
      this.#session = session;
      const current = () =>
        this.#session === session && !session.signal.aborted;
      const title = document.getElementById("reader-title");
      const status = document.getElementById("reader-status");
      title.textContent = "Article";
      document.getElementById("reader-meta").textContent = "";
      document.getElementById("kindle-help").hidden = true;
      status.textContent = "Opening PDF…";
      this.#dialog.showModal();

      try {
        const path = "/jobs/" + encodeURIComponent(jobID);
        const detail = await this.#request(path, { signal: session.signal });
        if (!current()) return;
        title.textContent = detail.title || "Article";
        document.getElementById("reader-meta").textContent = [
          detail.metadata?.author,
          detail.metadata?.site_name,
          detail.metadata?.publication_date?.slice(0, 10),
        ]
          .filter(Boolean)
          .join(" · ");
        const original = document.getElementById("original");
        original.href = detail.submitted_url;
        original.hidden = false;
        const archived =
          /^https?:\/\/(?:web\.archive\.org|archive\.(?:ph|is|today|md|fo|li|vn))\//i.test(
            detail.canonical_url || "",
          );
        const archive = document.getElementById("archive-source");
        archive.hidden = !archived;
        if (archived) archive.href = detail.canonical_url;
        const download = document.getElementById("download");
        download.href = "/api/v1" + path + "/artifact";
        download.hidden = false;

        const blob = await this.#request(path + "/artifact", {
          signal: session.signal,
          format: "blob",
        });
        if (!current()) return;
        this.#file = new File(
          [blob],
          detail.artifact?.filename || "article.pdf",
          { type: "application/pdf" },
        );
        document.getElementById("share").hidden = !(
          navigator.canShare && navigator.canShare({ files: [this.#file] })
        );
        await this.#pdf.load(blob);
      } catch (error) {
        if (current())
          status.textContent = error.message || "Could not open this PDF.";
      }
    }

    async #share() {
      if (!this.#file) return;
      const session = this.#session;
      try {
        await navigator.share({
          files: [this.#file],
          title: document.getElementById("reader-title").textContent,
        });
      } catch (error) {
        if (session === this.#session && error.name !== "AbortError") {
          document.getElementById("reader-status").textContent =
            "Sharing is unavailable. Download the PDF and use Send to Kindle instead.";
        }
      }
    }
  }
  globalThis.AtticReader = ArticleReader;
})();
