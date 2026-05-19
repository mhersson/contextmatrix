import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import React from 'react';
import { api } from '../api/client';
import { useImageUpload } from './useImageUpload';

// Spy on api.uploadImage. Re-installed per-test because afterEach calls
// vi.restoreAllMocks() which removes the spy entirely.
let uploadMock = vi.spyOn(api, 'uploadImage');

beforeEach(() => {
  uploadMock = vi.spyOn(api, 'uploadImage');
});

afterEach(() => {
  vi.restoreAllMocks();
});

function pngFile(name = 'a.png'): File {
  return new File([new Uint8Array([0x89, 0x50, 0x4e, 0x47])], name, { type: 'image/png' });
}

function textFile(name = 'a.txt'): File {
  return new File(['hello'], name, { type: 'text/plain' });
}

describe('useImageUpload', () => {
  it('routes a successful upload through onInsert with the returned URL', async () => {
    uploadMock.mockResolvedValueOnce({ id: 'abc0123456789def', url: '/api/images/abc0123456789def' });
    const onInsert = vi.fn();
    const { result } = renderHook(() => useImageUpload(onInsert));

    const file = pngFile();
    await act(async () => {
      result.current.handleFileSelect({
        target: { files: [file], value: '' },
      } as unknown as React.ChangeEvent<HTMLInputElement>);
      // Let the async upload settle.
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(onInsert).toHaveBeenCalledWith('/api/images/abc0123456789def');
    expect(result.current.uploading).toBe(false);
    expect(result.current.uploadError).toBeNull();
  });

  it('surfaces an upload failure on uploadError', async () => {
    // Use Promise.reject directly so the value flows verbatim through await
    // — some mock wrappers re-shape non-Error rejections, which would hide
    // the type guard in useImageUpload.
    uploadMock.mockImplementationOnce(() => Promise.reject({ error: 'image exceeds 10 MB limit', code: 'CONTENT_TOO_LARGE' }));
    const onInsert = vi.fn();
    const { result } = renderHook(() => useImageUpload(onInsert));

    await act(async () => {
      result.current.handleFileSelect({
        target: { files: [pngFile()], value: '' },
      } as unknown as React.ChangeEvent<HTMLInputElement>);
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(onInsert).not.toHaveBeenCalled();
    expect(result.current.uploadError).toBeTruthy();
  });

  it('only enqueues files whose MIME is image/*; text files are dropped', async () => {
    const onInsert = vi.fn();
    const { result } = renderHook(() => useImageUpload(onInsert));

    await act(async () => {
      result.current.handleFileSelect({
        target: { files: [textFile()], value: '' },
      } as unknown as React.ChangeEvent<HTMLInputElement>);
      await Promise.resolve();
    });

    expect(uploadMock).not.toHaveBeenCalled();
    expect(onInsert).not.toHaveBeenCalled();
  });

  it('resets the input value so re-picking the same file fires onChange again', async () => {
    uploadMock.mockResolvedValue({ id: 'aaaaaaaaaaaaaaaa', url: '/api/images/aaaaaaaaaaaaaaaa' });
    const onInsert = vi.fn();
    const { result } = renderHook(() => useImageUpload(onInsert));

    const fakeTarget = { files: [pngFile()], value: 'first.png' };
    await act(async () => {
      result.current.handleFileSelect({
        target: fakeTarget,
      } as unknown as React.ChangeEvent<HTMLInputElement>);
      await Promise.resolve();
    });

    expect(fakeTarget.value).toBe('');
  });

  it('handlePaste filters non-image clipboard entries and preventDefaults when at least one image is found', () => {
    const onInsert = vi.fn();
    const { result } = renderHook(() => useImageUpload(onInsert));

    const items = [
      { kind: 'string', type: 'text/plain', getAsFile: () => null },
      { kind: 'file', type: 'application/pdf', getAsFile: () => textFile() },
      { kind: 'file', type: 'image/png', getAsFile: () => pngFile() },
    ];

    const preventDefault = vi.fn();

    act(() => {
      result.current.handlePaste({
        clipboardData: { items },
        preventDefault,
      } as unknown as React.ClipboardEvent<HTMLTextAreaElement>);
    });

    // At least one image item present → preventDefault must fire so the
    // textarea doesn't also insert the pasted alt-text representation.
    expect(preventDefault).toHaveBeenCalledTimes(1);
  });

  it('uploading stays true while any of multiple concurrent uploads is still in flight', async () => {
    let resolveA: (v: { id: string; url: string }) => void = () => {};
    let resolveB: (v: { id: string; url: string }) => void = () => {};
    const promiseA = new Promise<{ id: string; url: string }>((r) => {
      resolveA = r;
    });
    const promiseB = new Promise<{ id: string; url: string }>((r) => {
      resolveB = r;
    });
    uploadMock.mockImplementationOnce(() => promiseA).mockImplementationOnce(() => promiseB);

    const onInsert = vi.fn();
    const { result } = renderHook(() => useImageUpload(onInsert));

    act(() => {
      result.current.handleFileSelect({
        target: { files: [pngFile('a.png'), pngFile('b.png')], value: '' },
      } as unknown as React.ChangeEvent<HTMLInputElement>);
    });

    await waitFor(() => expect(result.current.uploading).toBe(true));

    // Resolve only the first upload; the counter must keep uploading=true
    // because the second is still pending.
    await act(async () => {
      resolveA({ id: '1111111111111111', url: '/api/images/1111111111111111' });
    });
    // Yield once more to let the await in uploadAndInsert settle.
    await act(async () => {
      await Promise.resolve();
    });
    expect(result.current.uploading).toBe(true);
    expect(onInsert).toHaveBeenCalledWith('/api/images/1111111111111111');

    // Resolve the second; now uploading flips to false.
    await act(async () => {
      resolveB({ id: '2222222222222222', url: '/api/images/2222222222222222' });
    });
    await waitFor(() => expect(result.current.uploading).toBe(false));
  });

  it('unmount mid-upload cancels the fetch and does not invoke onInsert', async () => {
    let resolveUpload: (v: { id: string; url: string }) => void = () => {};
    let receivedSignal: AbortSignal | undefined;
    uploadMock.mockImplementationOnce((_file, signal) => {
      receivedSignal = signal;
      return new Promise<{ id: string; url: string }>((r) => {
        resolveUpload = r;
      });
    });

    const onInsert = vi.fn();
    const { result, unmount } = renderHook(() => useImageUpload(onInsert));

    act(() => {
      result.current.handleFileSelect({
        target: { files: [pngFile()], value: '' },
      } as unknown as React.ChangeEvent<HTMLInputElement>);
    });

    await waitFor(() => expect(result.current.uploading).toBe(true));
    expect(receivedSignal).toBeDefined();
    expect(receivedSignal?.aborted).toBe(false);

    // Unmount mid-upload; the hook must abort the signal it handed to fetch.
    unmount();
    expect(receivedSignal?.aborted).toBe(true);

    // Resolve the upload *after* unmount; onInsert must not fire because the
    // post-abort guard short-circuits before the splice.
    await act(async () => {
      resolveUpload({ id: '3333333333333333', url: '/api/images/3333333333333333' });
      await Promise.resolve();
    });
    expect(onInsert).not.toHaveBeenCalled();
  });

  it('handlePaste does not call preventDefault when the clipboard has no image files', () => {
    const onInsert = vi.fn();
    const { result } = renderHook(() => useImageUpload(onInsert));

    const items = [
      { kind: 'string', type: 'text/plain', getAsFile: () => null },
      { kind: 'file', type: 'application/pdf', getAsFile: () => textFile() },
    ];

    const preventDefault = vi.fn();

    act(() => {
      result.current.handlePaste({
        clipboardData: { items },
        preventDefault,
      } as unknown as React.ClipboardEvent<HTMLTextAreaElement>);
    });

    expect(preventDefault).not.toHaveBeenCalled();
  });

  it('handleDragOver no-ops without preventDefault when the payload is not files', () => {
    const onInsert = vi.fn();
    const { result } = renderHook(() => useImageUpload(onInsert));
    const preventDefault = vi.fn();

    act(() => {
      result.current.handleDragOver({
        dataTransfer: { types: ['text/plain'] },
        preventDefault,
      } as unknown as React.DragEvent<HTMLDivElement>);
    });

    expect(preventDefault).not.toHaveBeenCalled();
  });

  it('handleDragOver calls preventDefault when the payload includes Files', () => {
    const onInsert = vi.fn();
    const { result } = renderHook(() => useImageUpload(onInsert));
    const preventDefault = vi.fn();

    act(() => {
      result.current.handleDragOver({
        dataTransfer: { types: ['Files'] },
        preventDefault,
      } as unknown as React.DragEvent<HTMLDivElement>);
    });

    expect(preventDefault).toHaveBeenCalledTimes(1);
  });

  it('still uploads after StrictMode dual-effect runs (controller re-created in effect)', async () => {
    // Regression: previously the AbortController was constructed at `useRef`
    // init. In dev StrictMode the first cleanup nulled the ref and the
    // second setup didn't re-create it, so every subsequent upload short-
    // circuited at the `if (!controller) return` guard. Wrapping renderHook
    // in <React.StrictMode> reproduces the dual-effect; the upload must
    // still go through.
    uploadMock.mockResolvedValueOnce({ id: 'strict0000000000', url: '/api/images/strict0000000000' });
    const onInsert = vi.fn();
    const { result } = renderHook(() => useImageUpload(onInsert), {
      wrapper: ({ children }) => React.createElement(React.StrictMode, null, children),
    });

    await act(async () => {
      result.current.handleFileSelect({
        target: { files: [pngFile()], value: '' },
      } as unknown as React.ChangeEvent<HTMLInputElement>);
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(uploadMock).toHaveBeenCalled();
    expect(onInsert).toHaveBeenCalledWith('/api/images/strict0000000000');
  });
});
