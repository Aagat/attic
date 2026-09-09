// Apply app-style gestures only to installed touch-device windows.
export function configureMobilePWA() {
  const installed = matchMedia("(display-mode: standalone)");
  const touch = matchMedia("(pointer: coarse)");
  const viewport = document.querySelector<HTMLMetaElement>(
    'meta[name="viewport"]',
  );
  const originalViewport = viewport?.content || "";
  let enabled = false;
  function update() {
    enabled =
      touch.matches &&
      (installed.matches ||
        (navigator as Navigator & { standalone?: boolean }).standalone ===
          true);
    document.documentElement.classList.toggle("mobile-pwa", enabled);
    if (viewport)
      viewport.content =
        originalViewport +
        (enabled ? ", maximum-scale=1, user-scalable=no" : "");
  }
  // iOS can ignore viewport zoom limits. Cancel its page-zoom gestures while
  // leaving single-finger scrolling and the PDF's explicit zoom controls alone.
  const preventGesture = (event: Event) => {
    if (enabled) event.preventDefault();
  };
  document.addEventListener("gesturestart", preventGesture, { passive: false });
  document.addEventListener("gesturechange", preventGesture, {
    passive: false,
  });
  document.addEventListener(
    "touchmove",
    (event) => {
      if (enabled && event.touches.length > 1) event.preventDefault();
    },
    { passive: false },
  );
  installed.addEventListener("change", update);
  touch.addEventListener("change", update);
  update();
}
