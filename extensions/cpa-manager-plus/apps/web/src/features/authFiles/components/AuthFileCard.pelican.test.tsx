import { act } from 'react';
import { create, type ReactTestRenderer } from 'react-test-renderer';
import { describe, expect, it, vi } from 'vitest';
import { Button } from '@/components/ui/Button';
import { AuthFileCard } from './AuthFileCard';

vi.mock('react-i18next', () => ({ initReactI18next: { type: '3rdParty', init: () => {} }, useTranslation: () => ({ t: (key: string) => key }) }));

describe('single-credential Pelican action', () => {
  it('retains the exact auth index when credential files share a name', () => {
    const file = { name: 'shared.json', type: 'custom', authIndex: 'exact-child' };
    const onPelicanTest = vi.fn();
    let renderer!: ReactTestRenderer;
    act(() => { renderer = create(<AuthFileCard file={file} compact selected={false} resolvedTheme="light" disableControls={false}
      deleting={null} statusUpdating={{}} statusBarCache={new Map()} onShowModels={vi.fn()} onDownload={vi.fn()}
      onOpenPrefixProxyEditor={vi.fn()} onDelete={vi.fn()} onToggleStatus={vi.fn()} onToggleSelect={vi.fn()} onPelicanTest={onPelicanTest} />); });
    act(() => renderer.root.findAllByType(Button).find((button) => button.props['aria-label'] === 'ai_providers.pelican_button')!.props.onClick());
    expect(onPelicanTest).toHaveBeenCalledWith(file);
    act(() => renderer.unmount());
  });
});
