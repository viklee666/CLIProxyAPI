import { act } from 'react';
import { create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { Button } from '@/components/ui/Button';
import { NativeProviderLimits } from './NativeProviderLimits';

const { getNativeProviderLimits } = vi.hoisted(() => ({ getNativeProviderLimits: vi.fn(async (kind: string) => [
  { name: `${kind} first`, apiKey: `${kind}-secret`, authIndex: 'collision' },
  { name: `${kind} second`, apiKey: `${kind}-secret-2`, authIndex: 'collision' },
]) }));
vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock('@/services/api', () => ({ providersApi: { getNativeProviderLimits } }));
vi.mock('@/stores', () => ({ useNotificationStore: () => ({ showNotification: vi.fn() }) }));
let renderer: ReactTestRenderer | undefined;
afterEach(() => { if (renderer) act(() => renderer?.unmount()); });

describe('native provider intelligence-test entry points', () => {
  it('publishes xAI and Meta targets for global selection and opens individual entries', async () => {
    const onPelicanTargetsChange = vi.fn();
    const onPelicanTest = vi.fn();
    await act(async () => { renderer = create(<NativeProviderLimits disabled={false} onPelicanTargetsChange={onPelicanTargetsChange} onPelicanTest={onPelicanTest} />); });
    const targets = onPelicanTargetsChange.mock.calls[0][0];
    expect(targets.map((target: { selector: unknown }) => target.selector)).toEqual([
      { provider_kind: 'xai', config_index: 0 }, { provider_kind: 'xai', config_index: 1 },
      { provider_kind: 'meta', config_index: 0 }, { provider_kind: 'meta', config_index: 1 },
    ]);
    const buttons = renderer!.root.findAllByType(Button).filter((button) => button.props.children === 'ai_providers.pelican_button');
    expect(buttons).toHaveLength(4);
    act(() => buttons[3].props.onClick());
    expect(onPelicanTest).toHaveBeenCalledWith([targets[3]]);
    expect(JSON.stringify(targets)).not.toContain('secret');
  });
});
