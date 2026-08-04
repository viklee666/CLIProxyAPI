import { useEffect, useState, type ReactElement } from 'react';
import { Navigate, useLocation } from 'react-router-dom';
import { LoadingSpinner } from '@/components/ui/LoadingSpinner';
import { useTenantAuthStore } from '@/stores';

export function TenantProtectedRoute({ children }: { children: ReactElement }) {
  const location = useLocation();
  const isAuthenticated = useTenantAuthStore((state) => state.isAuthenticated);
  const apiBase = useTenantAuthStore((state) => state.apiBase);
  const token = useTenantAuthStore((state) => state.token);
  const restoreSession = useTenantAuthStore((state) => state.restoreSession);
  const [checkedSession, setCheckedSession] = useState<string | null>(null);
  const sessionKey = `${apiBase}\u0000${token}`;
  const canRestore = !isAuthenticated && Boolean(apiBase && token);
  const checking = canRestore && checkedSession !== sessionKey;

  useEffect(() => {
    if (!checking) return;

    let active = true;
    void restoreSession().finally(() => {
      if (active) setCheckedSession(sessionKey);
    });

    return () => {
      active = false;
    };
  }, [checking, restoreSession, sessionKey]);

  if (checking) {
    return (
      <div className="main-content">
        <LoadingSpinner />
      </div>
    );
  }

  if (!isAuthenticated) {
    return <Navigate to="/login" replace state={{ from: location }} />;
  }

  return children;
}
