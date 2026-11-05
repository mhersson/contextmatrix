import { useEffect, useMemo, useRef, useState, type CSSProperties, type RefObject } from 'react';
import { MOCKUP_FRAME_PATH, RENDER_MESSAGE, SETTINGS_MESSAGE, type FrameMessage, type MockupSettings } from './frameProtocol';
import { viewportWidth } from './viewport';

interface MockupFrameProps {
  source: string;
  settings?: MockupSettings;
  title: string;
}

/** Runs model-written HTML in CM's static shell, sealed by the sandbox. A new
 *  source mounts a fresh frame; the page never listens to what it posts. A source
 *  that declares a viewport wider than the pane is drawn at that width and scaled
 *  down to fit, read from the source text and never from the frame. */
export function MockupFrame({ source, settings, title }: MockupFrameProps) {
  return <SealedFrame key={source} source={source} settings={settings} title={title} />;
}

interface PaneSize {
  width: number;
  height: number;
}

/** The content box of `ref`'s element, zero until the browser has reported it. */
function usePaneSize(ref: RefObject<HTMLElement | null>): PaneSize {
  const [size, setSize] = useState<PaneSize>({ width: 0, height: 0 });
  useEffect(() => {
    const pane = ref.current;
    if (!pane) return;
    const observer = new ResizeObserver((entries) => {
      const { width, height } = entries[entries.length - 1].contentRect;
      setSize((prev) => (prev.width === width && prev.height === height ? prev : { width, height }));
    });
    observer.observe(pane);
    return () => observer.disconnect();
  }, [ref]);
  return size;
}

/** Inline size for a frame drawn `declared` px wide in a pane: scaled down from its top left corner,
 *  tall enough that the scaled frame fills the pane. None when the pane already holds it. */
function scaledStyle(declared: number | null, pane: PaneSize): CSSProperties | undefined {
  if (declared === null || pane.width <= 0 || declared <= pane.width) return undefined;
  const scale = pane.width / declared;
  return {
    width: `${declared}px`,
    height: `${pane.height / scale}px`,
    transform: `scale(${scale})`,
    transformOrigin: '0 0',
  };
}

function SealedFrame({ source, settings, title }: MockupFrameProps) {
  const paneRef = useRef<HTMLDivElement>(null);
  const frameRef = useRef<HTMLIFrameElement>(null);
  const pane = usePaneSize(paneRef);
  const declared = useMemo(() => viewportWidth(source), [source]);
  const rendered = useRef(false);
  const latest = useRef(settings);

  // Target origin "*": the sandboxed frame's origin is opaque.
  const post = (message: FrameMessage) => frameRef.current?.contentWindow?.postMessage(message, '*');

  // Writing the shell's document fires load again; only the first one renders.
  const onLoad = () => {
    if (rendered.current) return;
    rendered.current = true;
    post(
      latest.current === undefined
        ? { type: RENDER_MESSAGE, source }
        : { type: RENDER_MESSAGE, source, settings: latest.current },
    );
  };

  useEffect(() => {
    latest.current = settings;
    if (!rendered.current || settings === undefined) return;
    frameRef.current?.contentWindow?.postMessage({ type: SETTINGS_MESSAGE, settings } satisfies FrameMessage, '*');
  }, [settings]);

  return (
    <div ref={paneRef} className="mk-frame">
      <iframe
        ref={frameRef}
        className="mk-frame-doc"
        style={scaledStyle(declared, pane)}
        title={title}
        src={MOCKUP_FRAME_PATH}
        sandbox="allow-scripts"
        referrerPolicy="no-referrer"
        onLoad={onLoad}
      />
    </div>
  );
}
