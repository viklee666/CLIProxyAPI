import { authFilesApi } from '@/services/api/authFiles';
import { refreshCredentialStatuses } from '@/services/credentialStatusSync';
import type { AuthFileItem } from '@/types/authFile';

const pendingForbiddenDisables = new Map<string, Promise<boolean>>();

const readText = (value: unknown): string =>
  typeof value === 'string' || typeof value === 'number' ? String(value).trim() : '';

export const disableCredentialAfterForbidden = async (
  file: AuthFileItem,
  status: number | undefined
): Promise<boolean> => {
  if (status !== 403 || file.disabled === true) return false;
  const name = readText(file.name);
  const authIndex = readText(file['auth_index'] ?? file.authIndex ?? file['auth-index']);
  if (!name) return false;
  const key = `${name}\u0000${authIndex}`;
  const existing = pendingForbiddenDisables.get(key);
  if (existing) return existing;

  const task = authFilesApi
    .setStatusWithFallback(name, true, authIndex || undefined)
    .then(() => {
      void refreshCredentialStatuses();
      return true;
    })
    .catch(() => false)
    .finally(() => {
      pendingForbiddenDisables.delete(key);
    });
  pendingForbiddenDisables.set(key, task);
  return task;
};
