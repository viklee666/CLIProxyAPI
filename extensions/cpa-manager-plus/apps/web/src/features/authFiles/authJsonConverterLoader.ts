import type * as AuthJsonConverter from './sessionAuthConverter';

let converterPromise: Promise<typeof AuthJsonConverter> | null = null;

export const loadAuthJsonConverter = () => {
  converterPromise ??= import('./sessionAuthConverter');
  return converterPromise;
};
