import { act, type ReactNode } from 'react';
import { create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Button } from '@/components/ui/Button';
import { PelicanTestDrawer } from './PelicanTestDrawer';
import { runPelicanTest, type PelicanTestResult, type PelicanTestTarget } from './pelicanTest';
import type * as PelicanModule from './pelicanTest';

vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock('@/components/ui/Drawer', () => ({ Drawer: ({ children, footer }: { children: ReactNode; footer: ReactNode }) => <div>{children}{footer}</div> }));
vi.mock('@/services/api/authFiles', () => ({ authFilesApi: { getModelsForAuthFile: vi.fn(async () => []) } }));
vi.mock('./pelicanTest', async (original) => ({ ...await original<typeof PelicanModule>(), runPelicanTest: vi.fn() }));

const targets: PelicanTestTarget[] = [
  { id: 'a', label: 'Provider A', subtitle: '', authIndex: 'a', modelNames: ['model-a'] },
  { id: 'b', label: 'Credential B', subtitle: '', authIndex: 'b', modelNames: ['model-b'] },
];
let renderer: ReactTestRenderer;
const runButton = () => renderer.root.findAllByType(Button).find((button) => button.props.children?.[1] === 'ai_providers.pelican_run')!;
const checkboxes = () => renderer.root.findAllByType('input').filter((input) => input.props.type === 'checkbox');

beforeEach(() => { vi.mocked(runPelicanTest).mockReset(); });
afterEach(() => { if (renderer) act(() => renderer.unmount()); });

describe('Pelican selected batch execution', () => {
  it('runs only selected targets with their individual models and supports selecting all', async () => {
    vi.mocked(runPelicanTest).mockImplementation(async (target, model) => ({ id: target.id, status: 'success', model, answer: `answer ${target.id}` }));
    await act(async () => { renderer = create(<PelicanTestDrawer open targets={targets} actionsDisabled={false} onClose={() => {}} />); });
    act(() => checkboxes()[2].props.onChange());
    await act(async () => { await runButton().props.onClick(); });
    expect(vi.mocked(runPelicanTest).mock.calls.map(([target, model]) => [target.id, model])).toEqual([['a', 'model-a']]);
    act(() => checkboxes()[0].props.onChange());
    await act(async () => { await runButton().props.onClick(); });
    expect(vi.mocked(runPelicanTest).mock.calls.slice(1).map(([target, model]) => [target.id, model])).toEqual([['a', 'model-a'], ['b', 'model-b']]);
    expect(renderer.root.findAllByType('pre').map((node) => node.children.join(''))).toEqual(['answer a', 'answer b']);
  });

  it('aborts on cancel, stops queued targets and ignores a late response', async () => {
    let resolve!: (value: PelicanTestResult) => void;
    vi.mocked(runPelicanTest).mockImplementation(() => new Promise((done) => { resolve = done; }));
    await act(async () => { renderer = create(<PelicanTestDrawer open targets={targets} actionsDisabled={false} onClose={() => {}} />); });
    act(() => runButton().props.onClick());
    const signal = vi.mocked(runPelicanTest).mock.calls[0][4];
    act(() => renderer.root.findAllByType(Button).find((button) => button.props.children === 'ai_providers.pelican_cancel')!.props.onClick());
    expect(signal?.aborted).toBe(true);
    await act(async () => { resolve({ id: 'a', status: 'success', model: 'model-a', answer: 'stale answer' }); });
    expect(runPelicanTest).toHaveBeenCalledTimes(1);
    expect(renderer.root.findAllByType('pre')).toHaveLength(0);
    const statuses = renderer.root.findAll((node) => node.props.role === 'status').map((node) => node.children.join(''));
    expect(statuses.every((status) => status.includes('pelican_status_cancelled'))).toBe(true);
  });

  it('does not run with no selection', async () => {
    await act(async () => { renderer = create(<PelicanTestDrawer open targets={targets} actionsDisabled={false} onClose={() => {}} />); });
    act(() => checkboxes()[0].props.onChange());
    expect(runButton().props.disabled).toBe(true);
    act(() => runButton().props.onClick());
    expect(runPelicanTest).not.toHaveBeenCalled();
  });
});
