import { useCallback, useEffect, useRef } from "react";

// Let the surrounding page scroll; resize again when images or edits change content.
export function useContentFrameHeight() {
  const disconnect = useRef<() => void>(() => {});
  useEffect(() => () => disconnect.current(), []);
  return useCallback((frame: HTMLIFrameElement) => {
    disconnect.current();
    const doc = frame.contentDocument;
    if (!doc?.body) return;
    let task = 0;
    const resize = () => {
      cancelAnimationFrame(task);
      task = requestAnimationFrame(() => {
        const style = frame.contentWindow!.getComputedStyle(doc.body);
        const height = Math.ceil(
          Math.max(
            doc.body.scrollHeight,
            doc.body.getBoundingClientRect().height,
          ) +
            (parseFloat(style.marginTop) || 0) +
            (parseFloat(style.marginBottom) || 0) +
            2,
        );
        frame.style.height = `${height}px`;
      });
    };
    const observer = new ResizeObserver(resize);
    observer.observe(doc.body);
    doc.addEventListener("load", resize, true);
    resize();
    disconnect.current = () => {
      observer.disconnect();
      cancelAnimationFrame(task);
      doc.removeEventListener("load", resize, true);
    };
  }, []);
}
