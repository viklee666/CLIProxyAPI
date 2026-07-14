import { useEffect } from 'react';
import { useAuthStore, useLanguageStore, useThemeStore, useVisualEffectsStore } from '@/stores';
import {
  activateCredentialStatusSyncScope,
  clearCredentialStatusSync,
  refreshCredentialStatuses,
} from '@/services/credentialStatusSync';
import { sha256Hex } from '@/utils/apiKeyHash';

const CREDENTIAL_STATUS_REFRESH_INTERVAL_MS = 15_000;

export function AppLifecycle() {
  const initializeTheme = useThemeStore((state) => state.initializeTheme);
  const initializeVisualEffects = useVisualEffectsStore((state) => state.initializeVisualEffects);
  const language = useLanguageStore((state) => state.language);
  const setLanguage = useLanguageStore((state) => state.setLanguage);
  const connectionStatus = useAuthStore((state) => state.connectionStatus);
  const apiBase = useAuthStore((state) => state.apiBase);
  const managementKey = useAuthStore((state) => state.managementKey);

  useEffect(() => {
    const cleanupTheme = initializeTheme();
    return cleanupTheme;
  }, [initializeTheme]);

  useEffect(() => {
    initializeVisualEffects();
  }, [initializeVisualEffects]);

  useEffect(() => {
    setLanguage(language);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []); // 仅用于首屏同步 i18n 语言

  useEffect(() => {
    document.documentElement.lang = language;
  }, [language]);

  useEffect(() => {
    if (connectionStatus !== 'connected' || !apiBase || !managementKey) {
      clearCredentialStatusSync();
      return;
    }

    activateCredentialStatusSyncScope(sha256Hex(`${apiBase}\u0000${managementKey}`));
    void refreshCredentialStatuses({ forceFull: true });
    const interval = window.setInterval(() => {
      void refreshCredentialStatuses();
    }, CREDENTIAL_STATUS_REFRESH_INTERVAL_MS);
    const refreshVisible = () => {
      if (document.visibilityState === 'visible') {
        void refreshCredentialStatuses();
      }
    };
    window.addEventListener('focus', refreshVisible);
    document.addEventListener('visibilitychange', refreshVisible);
    return () => {
      window.clearInterval(interval);
      window.removeEventListener('focus', refreshVisible);
      document.removeEventListener('visibilitychange', refreshVisible);
    };
  }, [apiBase, connectionStatus, managementKey]);

  return null;
}
