import { useCallback, useEffect, useState } from 'react';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import { useHeaderRefresh } from '@/hooks/useHeaderRefresh';
import {
  tenantsApi,
  type TenantProfile,
  type TenantProviderAdminView,
} from '@/services/api';
import { useNotificationStore } from '@/stores';
import styles from '@/features/clientAccess/ClientAccessPage.module.scss';

type TenantForm = {
  displayName: string;
  enabled: boolean;
};

type PasswordDisclosure = {
  title: string;
  password: string;
};

const emptyForm = (): TenantForm => ({ displayName: '', enabled: true });

const formatDate = (value: string) => {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? '-' : date.toLocaleString();
};

export function TenantManagementPage() {
  const showNotification = useNotificationStore((state) => state.showNotification);
  const showConfirmation = useNotificationStore((state) => state.showConfirmation);
  const [tenants, setTenants] = useState<TenantProfile[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [editing, setEditing] = useState<TenantProfile | null>(null);
  const [form, setForm] = useState<TenantForm>(emptyForm);
  const [formOpen, setFormOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [passwordDisclosure, setPasswordDisclosure] = useState<PasswordDisclosure | null>(null);
  const [providerTenant, setProviderTenant] = useState<TenantProfile | null>(null);
  const [providers, setProviders] = useState<TenantProviderAdminView[]>([]);
  const [providerLoading, setProviderLoading] = useState(false);
  const [providerError, setProviderError] = useState('');

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      setTenants(await tenantsApi.list());
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : '加载租户失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);
  useHeaderRefresh(load);

  const openCreate = () => {
    setEditing(null);
    setForm(emptyForm());
    setFormOpen(true);
  };

  const openEdit = (tenant: TenantProfile) => {
    setEditing(tenant);
    setForm({ displayName: tenant.display_name, enabled: tenant.enabled });
    setFormOpen(true);
  };

  const save = async () => {
    const displayName = form.displayName.trim();
    if (!displayName) {
      showNotification('请填写租户名称', 'error');
      return;
    }

    setSaving(true);
    try {
      if (editing) {
        await tenantsApi.update(editing.id, {
          display_name: displayName,
          enabled: form.enabled,
        });
        showNotification('租户已保存', 'success');
      } else {
        const result = await tenantsApi.create({ display_name: displayName });
        setPasswordDisclosure({
          title: `已创建 ${result.tenant.display_name}`,
          password: result.password,
        });
        showNotification('租户已创建', 'success');
      }
      setFormOpen(false);
      await load();
    } catch (saveError) {
      showNotification(saveError instanceof Error ? saveError.message : '保存租户失败', 'error');
    } finally {
      setSaving(false);
    }
  };

  const resetPassword = (tenant: TenantProfile) => {
    showConfirmation({
      title: '重置租户密码',
      message: `确认重置 ${tenant.display_name} 的密码？当前会话将立即失效。`,
      variant: 'danger',
      onConfirm: async () => {
        try {
          const result = await tenantsApi.resetPassword(tenant.id);
          setPasswordDisclosure({ title: `已重置 ${tenant.display_name} 的密码`, password: result.password });
          showNotification('密码已重置', 'success');
        } catch (resetError) {
          showNotification(resetError instanceof Error ? resetError.message : '重置密码失败', 'error');
        }
      },
    });
  };

  const remove = (tenant: TenantProfile) => {
    showConfirmation({
      title: '删除租户',
      message: `确认删除 ${tenant.display_name}？其提供商、分组、API Key 和会话都会被删除。`,
      variant: 'danger',
      onConfirm: async () => {
        try {
          await tenantsApi.delete(tenant.id);
          showNotification('租户已删除', 'success');
          await load();
        } catch (removeError) {
          showNotification(removeError instanceof Error ? removeError.message : '删除租户失败', 'error');
        }
      },
    });
  };

  const openProviders = async (tenant: TenantProfile) => {
    setProviderTenant(tenant);
    setProviders([]);
    setProviderError('');
    setProviderLoading(true);
    try {
      setProviders(await tenantsApi.listProviders(tenant.id));
    } catch (loadError) {
      setProviderError(loadError instanceof Error ? loadError.message : '加载提供商失败');
    } finally {
      setProviderLoading(false);
    }
  };

  const copyPassword = async () => {
    if (!passwordDisclosure) return;
    try {
      await navigator.clipboard.writeText(passwordDisclosure.password);
      showNotification('密码已复制', 'success');
    } catch {
      showNotification('复制失败，请手动保存密码', 'error');
    }
  };

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <div>
          <h1>租户管理</h1>
          <p>创建和维护非管理员用户，并查看其已脱敏的上游提供商配置。</p>
        </div>
        <Button onClick={openCreate}>创建租户</Button>
      </header>

      <div className={styles.tableWrap}>
        {loading ? <div className={styles.loading}>正在加载...</div> : null}
        {!loading && error ? <div className={styles.error}>{error}</div> : null}
        {!loading && !error && tenants.length === 0 ? (
          <div className={styles.empty}>尚未创建租户。</div>
        ) : null}
        {!loading && !error && tenants.length > 0 ? (
          <table className={`${styles.table} ${styles.groupsTable}`}>
            <thead>
              <tr>
                <th>名称</th>
                <th>状态</th>
                <th>创建时间</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {tenants.map((tenant) => (
                <tr key={tenant.id}>
                  <td>
                    <div className={styles.name}>{tenant.display_name}</div>
                    <div className={styles.muted}>ID: {tenant.id}</div>
                  </td>
                  <td>
                    <span
                      className={`${styles.badge} ${
                        tenant.enabled ? styles.badgeActive : styles.badgeDisabled
                      }`}
                    >
                      {tenant.enabled ? '已启用' : '已停用'}
                    </span>
                  </td>
                  <td className={styles.muted}>{formatDate(tenant.created_at)}</td>
                  <td>
                    <div className={styles.actions}>
                      <Button size="xs" variant="secondary" onClick={() => void openProviders(tenant)}>
                        提供商
                      </Button>
                      <Button size="xs" variant="secondary" onClick={() => resetPassword(tenant)}>
                        重置密码
                      </Button>
                      <Button size="xs" variant="secondary" onClick={() => openEdit(tenant)}>
                        编辑
                      </Button>
                      <Button size="xs" variant="danger" onClick={() => remove(tenant)}>
                        删除
                      </Button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : null}
      </div>

      <Modal
        open={formOpen}
        onClose={() => setFormOpen(false)}
        title={editing ? '编辑租户' : '创建租户'}
        footer={
          <>
            <Button variant="secondary" onClick={() => setFormOpen(false)}>
              取消
            </Button>
            <Button loading={saving} onClick={() => void save()}>
              保存
            </Button>
          </>
        }
      >
        <Input
          label="租户名称"
          value={form.displayName}
          autoFocus
          onChange={(event) => setForm((current) => ({ ...current, displayName: event.target.value }))}
        />
        {editing ? (
          <ToggleSwitch
            checked={form.enabled}
            onChange={(enabled) => setForm((current) => ({ ...current, enabled }))}
            label="启用此租户"
          />
        ) : null}
      </Modal>

      <Modal
        open={passwordDisclosure !== null}
        onClose={() => setPasswordDisclosure(null)}
        title={passwordDisclosure?.title}
        footer={
          <>
            <Button variant="secondary" onClick={() => void copyPassword()}>
              复制密码
            </Button>
            <Button onClick={() => setPasswordDisclosure(null)}>关闭</Button>
          </>
        }
      >
        <p>此密码只会显示一次。请在关闭前将其交给租户并安全保存。</p>
        <div className={styles.secretBox}>{passwordDisclosure?.password}</div>
      </Modal>

      <Modal
        open={providerTenant !== null}
        onClose={() => setProviderTenant(null)}
        title={providerTenant ? `${providerTenant.display_name} 的提供商` : '提供商'}
        width={1080}
        footer={<Button onClick={() => setProviderTenant(null)}>关闭</Button>}
      >
        {providerLoading ? <div className={styles.loading}>正在加载...</div> : null}
        {!providerLoading && providerError ? <div className={styles.error}>{providerError}</div> : null}
        {!providerLoading && !providerError && providers.length === 0 ? (
          <div className={styles.empty}>该租户尚未配置提供商。</div>
        ) : null}
        {!providerLoading && !providerError && providers.length > 0 ? (
          <div className={styles.tableWrap}>
            <table className={styles.table}>
              <thead>
                <tr>
                  <th>名称</th>
                  <th>通道</th>
                  <th>Base URL</th>
                  <th>API Key</th>
                  <th>优先级</th>
                  <th>模型数</th>
                  <th>状态</th>
                </tr>
              </thead>
              <tbody>
                {providers.map((provider) => (
                  <tr key={provider.id}>
                    <td>
                      <div className={styles.name}>{provider.name}</div>
                      <div className={styles.muted}>{provider.auth_index || '-'}</div>
                    </td>
                    <td>{provider.channel}</td>
                    <td>{provider.base_url || '默认地址'}</td>
                    <td>
                      <code>{provider.api_key}</code>
                    </td>
                    <td>{provider.priority}</td>
                    <td>{provider.model_count}</td>
                    <td>
                      <span
                        className={`${styles.badge} ${
                          provider.disabled ? styles.badgeDisabled : styles.badgeActive
                        }`}
                      >
                        {provider.disabled ? '已停用' : '可用'}
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
      </Modal>
    </div>
  );
}
