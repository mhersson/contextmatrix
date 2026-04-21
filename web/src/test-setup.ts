import '@testing-library/jest-dom';

// jsdom does not implement ResizeObserver; provide a no-op stub so components
// that use it (e.g. VirtualLogList) mount cleanly under tests that don't
// themselves override the global.
if (typeof globalThis.ResizeObserver === 'undefined') {
  class NoopResizeObserver {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  (globalThis as unknown as { ResizeObserver: typeof NoopResizeObserver }).ResizeObserver =
    NoopResizeObserver;
}
