import { act, type ReactNode } from 'react';
import { create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Drawer } from './Drawer';

vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock('react-dom', () => ({ createPortal: (children: ReactNode) => children }));
let renderer: ReactTestRenderer | undefined;

beforeEach(() => {
  vi.useFakeTimers();
  vi.stubGlobal('window', { setTimeout, clearTimeout });
  vi.stubGlobal('HTMLElement', class {});
  vi.stubGlobal('document', {
    activeElement: null,
    body: { style: { overflow: '' } },
    documentElement: { style: { overflow: '' } },
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  });
});
afterEach(() => {
  if (renderer) act(() => renderer?.unmount());
  renderer = undefined;
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe('Drawer close lifecycle', () => {
  it('notifies close start immediately and completion only after animation', async () => {
    const onCloseStart = vi.fn();
    const onClose = vi.fn();
    await act(async () => { renderer = create(<Drawer open onCloseStart={onCloseStart} onClose={onClose}>content</Drawer>); });
    act(() => renderer!.root.findByType('button').props.onClick());
    expect(onCloseStart).toHaveBeenCalledOnce();
    expect(onClose).not.toHaveBeenCalled();
    act(() => renderer!.root.findByType('button').props.onClick());
    expect(onCloseStart).toHaveBeenCalledOnce();
    act(() => vi.advanceTimersByTime(279));
    expect(onClose).not.toHaveBeenCalled();
    act(() => vi.advanceTimersByTime(1));
    expect(onClose).toHaveBeenCalledOnce();
  });

  it('keeps existing behavior when close-start notification is omitted', async () => {
    const onClose = vi.fn();
    await act(async () => { renderer = create(<Drawer open onClose={onClose}>content</Drawer>); });
    act(() => renderer!.root.findByType('button').props.onClick());
    expect(onClose).not.toHaveBeenCalled();
    act(() => vi.advanceTimersByTime(280));
    expect(onClose).toHaveBeenCalledOnce();
  });
});
