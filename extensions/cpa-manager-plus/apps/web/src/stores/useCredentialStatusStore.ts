import { create } from 'zustand';
import type { AuthFileItem } from '@/types/authFile';

export type CredentialStatusSnapshot = {
  key: string;
  name: string;
  authIndex: string;
  disabled: boolean;
  unavailable: boolean;
  status: string;
  statusMessage: string;
  state: string;
  updatedAt: string | number | null;
};

type CredentialIdentity = {
  name?: string | null;
  authIndex?: string | number | null;
};

type CredentialStatusState = {
  scope: string;
  revision: number;
  snapshots: Record<string, CredentialStatusSnapshot>;
  activateScope: (scope: string) => void;
  replaceSnapshots: (files: AuthFileItem[]) => void;
  upsertSnapshots: (files: AuthFileItem[]) => void;
  patchStatus: (identity: CredentialIdentity, disabled: boolean) => void;
  removeFiles: (names: string[]) => void;
  clear: () => void;
};

const readText = (value: unknown): string =>
  typeof value === 'string' || typeof value === 'number' ? String(value).trim() : '';

const readBoolean = (value: unknown): boolean => {
  if (typeof value === 'boolean') return value;
  if (typeof value === 'number') return value !== 0;
  const normalized = readText(value).toLowerCase();
  return normalized === 'true' || normalized === '1' || normalized === 'yes' || normalized === 'on';
};

const readName = (file: AuthFileItem): string => readText(file.name ?? file.id);

const readAuthIndex = (file: AuthFileItem): string =>
  readText(file['auth_index'] ?? file.authIndex ?? file['auth-index']);

const buildKey = (name: string, authIndex: string): string =>
  authIndex ? `index:${authIndex}` : `name:${name}`;

const toSnapshot = (file: AuthFileItem): CredentialStatusSnapshot | null => {
  const name = readName(file);
  const authIndex = readAuthIndex(file);
  if (!name && !authIndex) return null;
  const status = readText(file.status);
  return {
    key: buildKey(name, authIndex),
    name,
    authIndex,
    disabled: readBoolean(file.disabled) || status.toLowerCase() === 'disabled',
    unavailable: readBoolean(file.unavailable),
    status,
    statusMessage: readText(file['status_message'] ?? file.statusMessage),
    state: readText(file.state),
    updatedAt:
      (file['updated_at'] as string | number | null | undefined) ??
      (file.updatedAt as string | number | null | undefined) ??
      (file.modified as string | number | null | undefined) ??
      null,
  };
};

const snapshotsFromFiles = (files: AuthFileItem[]): Record<string, CredentialStatusSnapshot> => {
  const snapshots: Record<string, CredentialStatusSnapshot> = {};
  files.forEach((file) => {
    const snapshot = toSnapshot(file);
    if (snapshot) snapshots[snapshot.key] = snapshot;
  });
  return snapshots;
};

const snapshotEqual = (
  left: CredentialStatusSnapshot | undefined,
  right: CredentialStatusSnapshot
): boolean =>
  Boolean(left) &&
  left?.name === right.name &&
  left.authIndex === right.authIndex &&
  left.disabled === right.disabled &&
  left.unavailable === right.unavailable &&
  left.status === right.status &&
  left.statusMessage === right.statusMessage &&
  left.state === right.state &&
  left.updatedAt === right.updatedAt;

const snapshotRecordsEqual = (
  left: Record<string, CredentialStatusSnapshot>,
  right: Record<string, CredentialStatusSnapshot>
): boolean => {
  const leftKeys = Object.keys(left);
  const rightKeys = Object.keys(right);
  if (leftKeys.length !== rightKeys.length) return false;
  return rightKeys.every((key) => snapshotEqual(left[key], right[key]));
};

export const useCredentialStatusStore = create<CredentialStatusState>((set) => ({
  scope: '',
  revision: 0,
  snapshots: {},
  activateScope: (scope) =>
    set((state) => {
      const normalized = scope.trim();
      if (state.scope === normalized) return state;
      return { scope: normalized, revision: state.revision + 1, snapshots: {} };
    }),
  replaceSnapshots: (files) =>
    set((state) => {
      const snapshots = snapshotsFromFiles(files);
      if (snapshotRecordsEqual(state.snapshots, snapshots)) return state;
      return { snapshots, revision: state.revision + 1 };
    }),
  upsertSnapshots: (files) =>
    set((state) => {
      const additions = snapshotsFromFiles(files);
      const entries = Object.entries(additions);
      if (entries.length === 0) return state;
      let changed = false;
      const snapshots = { ...state.snapshots };
      entries.forEach(([key, snapshot]) => {
        if (snapshotEqual(snapshots[key], snapshot)) return;
        snapshots[key] = snapshot;
        changed = true;
      });
      return changed ? { snapshots, revision: state.revision + 1 } : state;
    }),
  patchStatus: (identity, disabled) =>
    set((state) => {
      const name = readText(identity.name);
      const authIndex = readText(identity.authIndex);
      let changed = false;
      let matched = false;
      const snapshots = { ...state.snapshots };
      Object.entries(snapshots).forEach(([key, snapshot]) => {
        const matches = authIndex
          ? snapshot.authIndex === authIndex
          : name && snapshot.name === name;
        if (!matches) return;
        matched = true;
        if (snapshot.disabled === disabled) return;
        snapshots[key] = {
          ...snapshot,
          disabled,
          unavailable: disabled ? snapshot.unavailable : false,
          status: disabled ? 'disabled' : 'active',
          statusMessage: disabled ? 'disabled via management API' : '',
          updatedAt: Date.now(),
        };
        changed = true;
      });
      if (!matched && (name || authIndex)) {
        const key = buildKey(name, authIndex);
        snapshots[key] = {
          key,
          name,
          authIndex,
          disabled,
          unavailable: false,
          status: disabled ? 'disabled' : 'active',
          statusMessage: disabled ? 'disabled via management API' : '',
          state: '',
          updatedAt: Date.now(),
        };
        changed = true;
      }
      return changed ? { snapshots, revision: state.revision + 1 } : state;
    }),
  removeFiles: (names) =>
    set((state) => {
      const deleted = new Set(names.map((name) => name.trim()).filter(Boolean));
      if (deleted.size === 0) return state;
      const snapshots = Object.fromEntries(
        Object.entries(state.snapshots).filter(([, snapshot]) => !deleted.has(snapshot.name))
      );
      if (Object.keys(snapshots).length === Object.keys(state.snapshots).length) return state;
      return { snapshots, revision: state.revision + 1 };
    }),
  clear: () =>
    set((state) =>
      Object.keys(state.snapshots).length === 0 && state.scope === ''
        ? state
        : { scope: '', snapshots: {}, revision: state.revision + 1 }
    ),
}));

export const findCredentialStatus = (
  snapshots: Record<string, CredentialStatusSnapshot>,
  identity: CredentialIdentity
): CredentialStatusSnapshot | undefined => {
  const name = readText(identity.name);
  const authIndex = readText(identity.authIndex);
  if (authIndex && snapshots[`index:${authIndex}`]) return snapshots[`index:${authIndex}`];
  if (name && snapshots[`name:${name}`]) return snapshots[`name:${name}`];
  if (!name) return undefined;
  const matches = Object.values(snapshots).filter((snapshot) => snapshot.name === name);
  return matches.length === 1 ? matches[0] : undefined;
};

export const applyCredentialStatus = (
  file: AuthFileItem,
  snapshots: Record<string, CredentialStatusSnapshot>
): AuthFileItem => {
  const snapshot = findCredentialStatus(snapshots, {
    name: readName(file),
    authIndex: readAuthIndex(file),
  });
  if (!snapshot) return file;
  if (
    file.disabled === snapshot.disabled &&
    file.unavailable === snapshot.unavailable &&
    readText(file.status) === snapshot.status &&
    readText(file['status_message'] ?? file.statusMessage) === snapshot.statusMessage
  ) {
    return file;
  }
  return {
    ...file,
    disabled: snapshot.disabled,
    unavailable: snapshot.unavailable,
    status: snapshot.status,
    statusMessage: snapshot.statusMessage,
    status_message: snapshot.statusMessage,
    ...(snapshot.state ? { state: snapshot.state } : {}),
    ...(snapshot.updatedAt !== null
      ? { updatedAt: snapshot.updatedAt, updated_at: snapshot.updatedAt }
      : {}),
  };
};

export const applyCredentialStatuses = (
  files: AuthFileItem[],
  snapshots: Record<string, CredentialStatusSnapshot>
): AuthFileItem[] => files.map((file) => applyCredentialStatus(file, snapshots));
