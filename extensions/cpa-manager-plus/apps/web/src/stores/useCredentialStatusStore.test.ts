import { beforeEach, describe, expect, it } from 'vitest';
import {
  applyCredentialStatus,
  findCredentialStatus,
  useCredentialStatusStore,
} from './useCredentialStatusStore';

describe('useCredentialStatusStore', () => {
  beforeEach(() => {
    useCredentialStatusStore.getState().clear();
    useCredentialStatusStore.getState().activateScope('test');
  });

  it('keeps credentials with the same file name separated by auth index', () => {
    useCredentialStatusStore.getState().replaceSnapshots([
      { name: 'shared.json', auth_index: 'auth-1', disabled: false, status: 'active' },
      { name: 'shared.json', auth_index: 'auth-2', disabled: true, status: 'disabled' },
    ]);

    const snapshots = useCredentialStatusStore.getState().snapshots;
    expect(
      findCredentialStatus(snapshots, { name: 'shared.json', authIndex: 'auth-1' })?.disabled
    ).toBe(false);
    expect(
      findCredentialStatus(snapshots, { name: 'shared.json', authIndex: 'auth-2' })?.disabled
    ).toBe(true);
  });

  it('applies a remotely refreshed status to a stale page snapshot', () => {
    useCredentialStatusStore.getState().replaceSnapshots([
      {
        name: 'codex.json',
        auth_index: 'auth-1',
        disabled: true,
        unavailable: true,
        status: 'disabled',
        status_message: 'forbidden',
      },
    ]);

    const result = applyCredentialStatus(
      { name: 'codex.json', auth_index: 'auth-1', disabled: false, status: 'active' },
      useCredentialStatusStore.getState().snapshots
    );

    expect(result.disabled).toBe(true);
    expect(result.unavailable).toBe(true);
    expect(result.status).toBe('disabled');
    expect(result.statusMessage).toBe('forbidden');
  });

  it('does not replace an existing snapshot when an idempotent patch is applied', () => {
    useCredentialStatusStore.getState().replaceSnapshots([
      {
        name: 'codex.json',
        auth_index: 'auth-1',
        disabled: true,
        unavailable: true,
        status: 'disabled',
        status_message: 'forbidden',
      },
    ]);

    useCredentialStatusStore
      .getState()
      .patchStatus({ name: 'codex.json', authIndex: 'auth-1' }, true);

    const snapshot = findCredentialStatus(useCredentialStatusStore.getState().snapshots, {
      name: 'codex.json',
      authIndex: 'auth-1',
    });
    expect(snapshot?.unavailable).toBe(true);
    expect(snapshot?.statusMessage).toBe('forbidden');
  });
});
