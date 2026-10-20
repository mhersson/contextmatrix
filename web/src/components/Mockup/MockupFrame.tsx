import { useEffect, useRef } from 'react';
import { MOCKUP_FRAME_PATH, RENDER_MESSAGE, SETTINGS_MESSAGE, type FrameMessage, type MockupSettings } from './frameProtocol';

interface MockupFrameProps {
  source: string;
  settings?: MockupSettings;
  title: string;
}

/** Runs model-written HTML in CM's static shell, sealed by the sandbox. A new
 *  source mounts a fresh frame; the page never listens to what it posts. */
export function MockupFrame({ source, settings, title }: MockupFrameProps) {
  return <SealedFrame key={source} source={source} settings={settings} title={title} />;
}

function SealedFrame({ source, settings, title }: MockupFrameProps) {
  const frameRef = useRef<HTMLIFrameElement>(null);
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
    <iframe
      ref={frameRef}
      className="mk-frame"
      title={title}
      src={MOCKUP_FRAME_PATH}
      sandbox="allow-scripts"
      referrerPolicy="no-referrer"
      onLoad={onLoad}
    />
  );
}
