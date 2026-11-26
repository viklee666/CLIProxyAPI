import { act, type ReactNode } from 'react';
import { create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Button } from '@/components/ui/Button';
import { Drawer } from '@/components/ui/Drawer';
import { PelicanTestDrawer } from './PelicanTestDrawer';
import { candyPrompt, pelicanPrompt, resolvePelicanTestTarget, runPelicanTest, type PelicanTestResult, type PelicanTestTarget, type ResolvedPelicanTestTarget } from './pelicanTest';
import type * as PelicanModule from './pelicanTest';

vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock('@/components/ui/Drawer', () => ({ Drawer: ({ children, footer }: { children: ReactNode; footer: ReactNode }) => <div>{children}{footer}</div> }));
vi.mock('./pelicanTest', async (original) => ({ ...await original<typeof PelicanModule>(), runPelicanTest: vi.fn(), resolvePelicanTestTarget: vi.fn() }));

const targets: PelicanTestTarget[] = [
  { id: 'a', label: 'Provider A', subtitle: '', selector: { auth_id: 'auth-a' } },
  { id: 'b', label: 'Credential B', subtitle: '', selector: { auth_id: 'auth-b' } },
];
let renderer: ReactTestRenderer;
const runButton = () => renderer.root.findAllByType(Button).find((button) => button.props.children?.[1] === 'ai_providers.pelican_run')!;
const checkboxes = () => renderer.root.findAllByType('input').filter((input) => input.props.type === 'checkbox');
const modeSelect = () => renderer.root.findAllByType('select').find((node) => node.props['aria-label'] === 'ai_providers.pelican_benchmark')!;
const resolved = (target: PelicanTestTarget): ResolvedPelicanTestTarget => ({ ...target, authId: `auth-${target.id}`, authIndex: 'collision', name: 'shared.json', modelNames: [`model-${target.id}`, `other-${target.id}`] });

beforeEach(() => {
  vi.mocked(runPelicanTest).mockReset();
  vi.mocked(resolvePelicanTestTarget).mockReset().mockImplementation(async (target) => resolved(target));
});
afterEach(() => { if (renderer) act(() => renderer.unmount()); });

describe('Pelican selected batch execution', () => {
  it('runs only selected targets with their individual models and supports selecting all', async () => {
    vi.mocked(runPelicanTest).mockImplementation(async (target, model) => ({ id: target.id, status: 'success', model, answer: `answer ${target.id}` }));
    await act(async () => { renderer = create(<PelicanTestDrawer open targets={targets} actionsDisabled={false} onClose={() => {}} />); });
    act(() => renderer.root.findAllByType('select').find((node) => node.props['aria-label'] === 'Provider A · ai_providers.pelican_model')!.props.onChange({ target: { value: 'other-a' } }));
    act(() => checkboxes()[2].props.onChange());
    await act(async () => { await runButton().props.onClick(); });
    expect(vi.mocked(runPelicanTest).mock.calls.map(([target, model]) => [target.authId, model])).toEqual([['auth-a', 'other-a']]);
    act(() => checkboxes()[0].props.onChange());
    await act(async () => { await runButton().props.onClick(); });
    expect(vi.mocked(runPelicanTest).mock.calls.slice(1).map(([target, model]) => [target.authId, model])).toEqual([['auth-a', 'other-a'], ['auth-b', 'model-b']]);
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

  it('aborts at close start and ignores late model discovery without changing the close callback', async () => {
    // ES2022 has no Promise.withResolvers; keep the deferred resolver local.
    let finish!: (value: ResolvedPelicanTestTarget) => void;
    vi.mocked(resolvePelicanTestTarget).mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
    const onClose = vi.fn();
    await act(async () => { renderer = create(<PelicanTestDrawer open targets={targets} actionsDisabled={false} onClose={onClose} />); });
    const close = renderer.root.findByType(Drawer).props.onClose;
    const signal = vi.mocked(resolvePelicanTestTarget).mock.calls[0][1];
    act(() => renderer.root.findByType(Drawer).props.onCloseStart());
    expect(signal?.aborted).toBe(true);
    expect(onClose).not.toHaveBeenCalled();
    await act(async () => { finish(resolved(targets[0])); });
    expect(renderer.root.findByType(Drawer).props.onClose).toBe(close);
    expect(resolvePelicanTestTarget).toHaveBeenCalledTimes(1);
    expect(runButton().props.disabled).toBe(true);
    act(() => close());
    expect(onClose).toHaveBeenCalledOnce();
  });

  it('aborts active generation before the close animation finishes', async () => {
    vi.mocked(runPelicanTest).mockImplementation(() => new Promise(() => {}));
    await act(async () => { renderer = create(<PelicanTestDrawer open targets={targets} actionsDisabled={false} onClose={() => {}} />); });
    act(() => runButton().props.onClick());
    act(() => renderer.root.findByType(Drawer).props.onCloseStart());
    expect(vi.mocked(runPelicanTest).mock.calls[0][4]?.aborted).toBe(true);
    expect(runButton().props.disabled).toBe(true);
  });

  it('exposes three editable modes and keeps the candy reference UI-only', async () => {
    vi.mocked(runPelicanTest).mockImplementation(async (target, model) => ({ id: target.id, model, status: 'success', answer: '<html><body>21</body></html>' }));
    await act(async () => { renderer = create(<PelicanTestDrawer open targets={targets} actionsDisabled={false} onClose={() => {}} />); });
    expect(modeSelect().findAllByType('option').map((option) => option.props.value)).toEqual(['animation', 'candy', 'custom']);
    expect(renderer.root.findByType('textarea').props.value).toBe(pelicanPrompt);
    act(() => modeSelect().props.onChange({ target: { value: 'candy' } }));
    expect(renderer.root.findByType('textarea').props.value).toBe(candyPrompt);
    expect(renderer.root.findAllByType('p').some((node) => node.children.includes('ai_providers.pelican_candy_reference'))).toBe(true);
    await act(async () => { runButton().props.onClick(); });
    expect(vi.mocked(runPelicanTest).mock.calls.every((call) => call[3] === candyPrompt)).toBe(true);
    expect(renderer.root.findAllByType('iframe')).toHaveLength(0);
    act(() => modeSelect().props.onChange({ target: { value: 'custom' } }));
    expect(renderer.root.findByType('textarea').props.value).toBe('');
    const custom = '创建一个章鱼的 HTML 动画。';
    act(() => renderer.root.findByType('textarea').props.onChange({ target: { value: custom } }));
    await act(async () => { runButton().props.onClick(); });
    expect(vi.mocked(runPelicanTest).mock.calls.slice(2).every((call) => call[3] === custom)).toBe(true);
    expect(renderer.root.findAllByType('iframe')).toHaveLength(2);
    expect(renderer.root.findAllByType('iframe').every((node) => node.props.sandbox === 'allow-scripts')).toBe(true);
  });

  it('blocks execution when exact identity resolution is ambiguous', async () => {
    vi.mocked(resolvePelicanTestTarget).mockRejectedValue(new Error('Ambiguous credential'));
    await act(async () => { renderer = create(<PelicanTestDrawer open targets={targets} actionsDisabled={false} onClose={() => {}} />); });
    expect(runButton().props.disabled).toBe(true);
    act(() => runButton().props.onClick());
    expect(runPelicanTest).not.toHaveBeenCalled();
  });
});
