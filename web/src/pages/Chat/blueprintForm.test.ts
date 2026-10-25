import { describe, it, expect, vi } from 'vitest';
import {
  EMPTY_BLUEPRINT_FORM,
  capSentence,
  createLabel,
  disabledReason,
  needReason,
  plainLanguageKey,
  readPlainLanguage,
  rememberPlainLanguage,
  unattendedDefault,
  withMode,
  withUnattended,
  type BlueprintMode,
} from './blueprintForm';

describe('blueprintForm', () => {
  it('blocks a blueprint without projects and nothing else', () => {
    expect(disabledReason(true, EMPTY_BLUEPRINT_FORM)).not.toBeNull();
    expect(disabledReason(true, { ...EMPTY_BLUEPRINT_FORM, projects: ['alpha'] })).toBeNull();
    expect(disabledReason(false, EMPTY_BLUEPRINT_FORM)).toBeNull();
  });

  it('blocks a cap on an unpriced model, never without a cap or a project', () => {
    const picked = { ...EMPTY_BLUEPRINT_FORM, projects: ['alpha'] };
    expect(disabledReason(true, { ...picked, capUSD: 5 }, true)).not.toBeNull();
    expect(disabledReason(true, { ...picked, capUSD: 5 }, false)).toBeNull();
    expect(disabledReason(true, picked, true)).toBeNull();
    expect(disabledReason(false, { ...picked, capUSD: 5 }, true)).toBeNull();
    expect(disabledReason(true, { ...EMPTY_BLUEPRINT_FORM, capUSD: 5 }, true)).toBe(disabledReason(true, EMPTY_BLUEPRINT_FORM));
  });

  it('refuses a blank need and one over 64 KiB, counted in bytes', () => {
    expect(needReason('')).not.toBeNull();
    expect(needReason('  \n ')).not.toBeNull();
    expect(needReason('x')).toBeNull();
    expect(needReason('x'.repeat(65536))).toBeNull();
    expect(needReason('x'.repeat(65537))).not.toBeNull();
    // Bytes, not characters: 21846 euro signs are 65538 bytes.
    expect(needReason('€'.repeat(21846))).not.toBeNull();
  });

  it('names the cap amount only when there is a cap', () => {
    expect(capSentence(5)).toContain('$5');
    expect(capSentence(0)).not.toContain('$');
  });

  it('labels yolo differently from the other modes and the creating state', () => {
    expect(createLabel(true, 'yolo', false)).not.toBe(createLabel(true, 'default', false));
    expect(createLabel(true, 'auto', false)).toBe(createLabel(true, 'default', false));
    expect(createLabel(true, 'default', true)).not.toBe(createLabel(false, 'default', true));
  });
});

describe('the remembered plain-language choice', () => {
  it('is kept per identity and starts off without one', () => {
    expect(readPlainLanguage('human:alice')).toBe(false);
    rememberPlainLanguage('human:alice', true);
    expect(readPlainLanguage('human:alice')).toBe(true);
    expect(readPlainLanguage('human:bob')).toBe(false);
    rememberPlainLanguage('human:alice', false);
    expect(readPlainLanguage('human:alice')).toBe(false);
  });

  it('keeps nothing without an identity', () => {
    rememberPlainLanguage(null, true);
    expect(readPlainLanguage(null)).toBe(false);
  });

  it('reads junk as off', () => {
    localStorage.setItem(plainLanguageKey('human:alice'), 'yes');
    expect(readPlainLanguage('human:alice')).toBe(false);
  });

  it('starts off and never throws when the browser refuses its storage', () => {
    const denied = () => {
      throw new Error('denied');
    };
    vi.stubGlobal('localStorage', { getItem: denied, setItem: denied, removeItem: denied, clear: denied });
    try {
      expect(() => rememberPlainLanguage('human:alice', true)).not.toThrow();
      expect(readPlainLanguage('human:alice')).toBe(false);
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it.each<[BlueprintMode, boolean]>([
    ['default', false],
    ['auto', false],
    ['auto+emit', false],
    ['yolo', true],
  ])('defaults Unattended for %s to %s', (mode, want) => {
    expect(unattendedDefault(mode)).toBe(want);
  });

  it('follows the mode for Unattended until it is touched', () => {
    const yolo = withMode(EMPTY_BLUEPRINT_FORM, 'yolo');
    expect(yolo.mode).toBe('yolo');
    expect(yolo.unattended).toBe(true);
    expect(withMode(yolo, 'auto').unattended).toBe(false);
  });

  it('keeps a touched Unattended across mode changes', () => {
    const off = withUnattended(EMPTY_BLUEPRINT_FORM, false);
    expect(off.unattendedTouched).toBe(true);
    expect(withMode(off, 'yolo').unattended).toBe(false);
    const on = withUnattended(EMPTY_BLUEPRINT_FORM, true);
    expect(withMode(on, 'auto').unattended).toBe(true);
  });
});
