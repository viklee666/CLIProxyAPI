import { useEffect, useState } from 'react';
import { Navigate, useLocation, useNavigate } from 'react-router-dom';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { INLINE_LOGO_JPEG } from '@/assets/logoInline';
import { useTenantAuthStore } from '@/stores';
import { detectApiBaseFromLocation } from '@/utils/connection';
import styles from '@/features/login/LoginPage.module.scss';

type RedirectState = { from?: { pathname?: string } };

export function TenantLoginPage() {
  const navigate = useNavigate();
  const location = useLocation();
  const isAuthenticated = useTenantAuthStore((state) => state.isAuthenticated);
  const login = useTenantAuthStore((state) => state.login);
  const restoreSession = useTenantAuthStore((state) => state.restoreSession);
  const remembered = useTenantAuthStore((state) => state.rememberSession);
  const [password, setPassword] = useState('');
  const [rememberSession, setRememberSession] = useState(remembered);
  const [loading, setLoading] = useState(false);
  const [restoring, setRestoring] = useState(true);
  const [error, setError] = useState('');

  useEffect(() => {
    let active = true;
    void restoreSession()
      .then((restored) => {
        if (active && restored) {
          const target = (location.state as RedirectState | null)?.from?.pathname || '/';
          navigate(target, { replace: true });
        }
      })
      .finally(() => {
        if (active) setRestoring(false);
      });
    return () => {
      active = false;
    };
  }, [location.state, navigate, restoreSession]);

  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!password.trim() || loading) return;
    setLoading(true);
    setError('');
    try {
      await login({
        apiBase: detectApiBaseFromLocation(),
        password,
        rememberSession,
      });
      const target = (location.state as RedirectState | null)?.from?.pathname || '/';
      navigate(target, { replace: true });
    } catch (loginError) {
      setError(loginError instanceof Error ? loginError.message : 'Login failed');
    } finally {
      setLoading(false);
    }
  };

  if (isAuthenticated && !restoring) {
    const target = (location.state as RedirectState | null)?.from?.pathname || '/';
    return <Navigate to={target} replace />;
  }

  return (
    <main className={styles.container}>
      <div className={styles.formPanel}>
        <div className={styles.formContent}>
          <section className={styles.loginCard} aria-busy={restoring}>
            <div className={styles.cardBranding}>
              <img src={INLINE_LOGO_JPEG} alt="CPA" className={styles.logo} />
              <h1>用户面板</h1>
              <p>使用管理员提供的密码登录</p>
            </div>
            {!restoring ? (
              <form className={styles.loginForm} onSubmit={submit}>
                <Input
                  label="密码"
                  type="password"
                  value={password}
                  autoComplete="current-password"
                  autoFocus
                  onChange={(event) => setPassword(event.target.value)}
                  disabled={loading}
                />
                <label className={styles.toggleLabel}>
                  <input
                    type="checkbox"
                    checked={rememberSession}
                    onChange={(event) => setRememberSession(event.target.checked)}
                  />{' '}
                  记住登录状态
                </label>
                {error ? <div className={styles.errorBox}>{error}</div> : null}
                <Button type="submit" loading={loading} fullWidth>
                  登录
                </Button>
              </form>
            ) : (
              <div className={styles.splashContent}>
                <div className={styles.splashLoader}>
                  <div className={styles.splashLoaderBar} />
                </div>
              </div>
            )}
          </section>
        </div>
      </div>
    </main>
  );
}
