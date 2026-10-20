export const MOCKUP_FRAME_PATH = '/mockup-frame.html';
export const RENDER_MESSAGE = 'cm-mockup-render';
export const SETTINGS_MESSAGE = 'cm-mockup-settings';
export const SETTINGS_EVENT = 'mockup:settings';

export type MockupSettings = Readonly<Record<string, string>>;

export type FrameMessage =
  | { type: typeof RENDER_MESSAGE; source: string; settings?: MockupSettings }
  | { type: typeof SETTINGS_MESSAGE; settings: MockupSettings };
