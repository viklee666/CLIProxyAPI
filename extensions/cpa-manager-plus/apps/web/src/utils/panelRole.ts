export type PanelRole = 'admin' | 'tenant';

// The server still owns authorization. This only selects the appropriate
// panel experience for the two independently hosted entry paths.
export const resolvePanelRole = (): PanelRole => {
  if (typeof window === 'undefined') {
    return 'admin';
  }
  return /\/user\/?$/i.test(window.location.pathname) ? 'tenant' : 'admin';
};
