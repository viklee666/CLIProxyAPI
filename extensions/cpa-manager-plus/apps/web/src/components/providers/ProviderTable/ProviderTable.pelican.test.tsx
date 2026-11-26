import { act } from 'react';
import { create, type ReactTestRenderer } from 'react-test-renderer';
import { describe, expect, it, vi } from 'vitest';
import { Button } from '@/components/ui/Button';
import { ProviderTable } from './ProviderTable';
import { buildProviderRows } from './rowData';

vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key }) }));

describe('single-provider Pelican action', () => {
  it('passes the clicked provider identity even when its original index differs from display order', () => {
    const rows = buildProviderRows({ gemini: [], claude: [], vertex: [], openai: [], usageByProvider: new Map(),
      codex: [{ name: 'first', apiKey: 'a', authIndex: 'auth-a' }, { name: 'second', apiKey: 'b', authIndex: 'auth-b' }],
    });
    const onPelicanTest = vi.fn();
    let renderer!: ReactTestRenderer;
    act(() => { renderer = create(<ProviderTable rows={[rows[1], rows[0]]} loading={false} actionsDisabled={false} toggleDisabled={false}
      resolvedTheme="light" emptyState={null} onShowDetail={vi.fn()} onEdit={vi.fn()} onDelete={vi.fn()} onToggle={vi.fn()}
      onPriorityChange={vi.fn()} onPelicanTest={onPelicanTest} />); });
    act(() => renderer.root.findAllByType(Button).filter((button) => button.props['aria-label'] === 'ai_providers.pelican_button')[0].props.onClick());
    expect(onPelicanTest).toHaveBeenCalledWith(rows[1]);
    act(() => renderer.unmount());
  });
});
