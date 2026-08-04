import { useState } from 'react';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { tenantAuthApi } from '@/services/api';
import { useNotificationStore, useTenantAuthStore } from '@/stores';
import styles from './TenantPanel.module.scss';

export function TenantAccountPage() {
  const showNotification = useNotificationStore((state) => state.showNotification);
  const logout = useTenantAuthStore((state) => state.logout);
  const tenant = useTenantAuthStore((state) => state.tenant);
  const [currentPassword, setCurrentPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [saving, setSaving] = useState(false);

  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (newPassword.length < 6) {
      showNotification('新密码至少需要 6 位', 'error');
      return;
    }
    if (newPassword !== confirmPassword) {
      showNotification('两次输入的新密码不一致', 'error');
      return;
    }
    setSaving(true);
    try {
      await tenantAuthApi.changePassword(currentPassword, newPassword);
      showNotification('密码已修改，请使用新密码重新登录', 'success');
      await logout();
    } catch (saveError) {
      showNotification(saveError instanceof Error ? saveError.message : '修改密码失败', 'error');
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <div>
          <h1>账户</h1>
          <p>{tenant?.display_name || '当前租户'}</p>
        </div>
      </header>
      <section className={styles.summaryBlock}>
        <h2>修改密码</h2>
        <form className={styles.inlineForm} onSubmit={submit}>
          <Input
            label="当前密码"
            type="password"
            value={currentPassword}
            autoComplete="current-password"
            onChange={(event) => setCurrentPassword(event.target.value)}
          />
          <Input
            label="新密码"
            type="password"
            value={newPassword}
            autoComplete="new-password"
            onChange={(event) => setNewPassword(event.target.value)}
          />
          <Input
            label="确认新密码"
            type="password"
            value={confirmPassword}
            autoComplete="new-password"
            onChange={(event) => setConfirmPassword(event.target.value)}
          />
          <Button type="submit" loading={saving}>
            更新密码
          </Button>
        </form>
      </section>
    </div>
  );
}
