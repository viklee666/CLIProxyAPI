import type { RequestLimits } from '@/types';

export const requestLimits = (value: RequestLimits) => ({
  concurrencyLimit: value.concurrencyLimit ?? 0,
  rpmLimit: value.rpmLimit ?? 0,
});

export const validRequestLimits = (value: RequestLimits) =>
  Object.values(requestLimits(value)).every((limit) => Number.isSafeInteger(limit) && limit >= 0);

export const requestLimitsChanged = (left: RequestLimits, right: RequestLimits) =>
  (left.concurrencyLimit ?? 0) !== (right.concurrencyLimit ?? 0) ||
  (left.rpmLimit ?? 0) !== (right.rpmLimit ?? 0);
