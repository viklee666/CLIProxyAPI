import { NavLink } from 'react-router-dom';
import { Button } from '@/components/ui/Button';
import { INLINE_LOGO_JPEG } from '@/assets/logoInline';
import { TenantRoutes } from '@/router/TenantRoutes';
import { useTenantAuthStore } from '@/stores';
import styles from './TenantLayout.module.scss';

const navItems = [
  { path: '/', label: '仪表盘', end: true },
  { path: '/ai-providers', label: 'AI 提供商' },
  { path: '/client-groups', label: '客户端分组' },
  { path: '/client-keys', label: 'API Key' },
  { path: '/monitoring', label: '请求监控' },
  { path: '/usage-analytics', label: '用量分析' },
  { path: '/account', label: '账户' },
];

export function TenantLayout() {
  const tenant = useTenantAuthStore((state) => state.tenant);
  const logout = useTenantAuthStore((state) => state.logout);

  return (
    <div className={styles.shell}>
      <aside className={styles.sidebar}>
        <div className={styles.brand}>
          <img src={INLINE_LOGO_JPEG} alt="CPA" />
          <span>用户面板</span>
        </div>
        <nav className={styles.nav} aria-label="用户面板导航">
          {navItems.map((item) => (
            <NavLink
              key={item.path}
              to={item.path}
              end={item.end}
              className={({ isActive }) => (isActive ? styles.navActive : undefined)}
            >
              {item.label}
            </NavLink>
          ))}
        </nav>
        <div className={styles.account}>
          <strong>{tenant?.display_name || '租户'}</strong>
          <span>独立会话</span>
          <Button variant="secondary" size="sm" onClick={() => void logout()}>
            退出登录
          </Button>
        </div>
      </aside>
      <main className={styles.content}>
        <TenantRoutes />
      </main>
    </div>
  );
}
