import { useCallback, useEffect, useState } from 'react';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import {
  tenantClientAccessApi,
  tenantProvidersApi,
  type ClientGroup,
  type TenantProvider,
} from '@/services/api';
import { useNotificationStore } from '@/stores';
import styles from './TenantPanel.module.scss';

type GroupForm = { name: string; description: string; enabled: boolean };
const emptyForm: GroupForm = { name: '', description: '', enabled: true };

export function TenantGroupsPage() {
  const showNotification = useNotificationStore((state) => state.showNotification);
  const showConfirmation = useNotificationStore((state) => state.showConfirmation);
  const [groups, setGroups] = useState<ClientGroup[]>([]);
  const [providers, setProviders] = useState<TenantProvider[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [editing, setEditing] = useState<ClientGroup | null>(null);
  const [form, setForm] = useState<GroupForm>(emptyForm);
  const [groupModal, setGroupModal] = useState(false);
  const [bindingGroup, setBindingGroup] = useState<ClientGroup | null>(null);
  const [selectedIndices, setSelectedIndices] = useState<string[]>([]);
  const [priority, setPriority] = useState('0');
  const [saving, setSaving] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const [groupPage, providerRows] = await Promise.all([
        tenantClientAccessApi.listGroups(1, 200),
        tenantProvidersApi.list(),
      ]);
      setGroups(groupPage.items ?? []);
      setProviders(providerRows);
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : '加载分组失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const openCreate = () => {
    setEditing(null);
    setForm(emptyForm);
    setGroupModal(true);
  };

  const openEdit = (group: ClientGroup) => {
    setEditing(group);
    setForm({ name: group.name, description: group.description || '', enabled: group.enabled });
    setGroupModal(true);
  };

  const saveGroup = async () => {
    if (!form.name.trim()) return;
    setSaving(true);
    try {
      if (editing) {
        await tenantClientAccessApi.updateGroup(editing.id, form);
      } else {
        await tenantClientAccessApi.createGroup(form);
      }
      setGroupModal(false);
      showNotification('分组已保存', 'success');
      await load();
    } catch (saveError) {
      showNotification(saveError instanceof Error ? saveError.message : '保存失败', 'error');
    } finally {
      setSaving(false);
    }
  };

  const openBindings = async (group: ClientGroup) => {
    setBindingGroup(group);
    setSelectedIndices([]);
    setPriority('0');
    try {
      const response = await tenantClientAccessApi.listBindings(1, 500, [group.id]);
      const bindings = response.items ?? [];
      setSelectedIndices(bindings.map((item) => item.auth_index));
      if (bindings[0]) setPriority(String(bindings[0].priority));
    } catch (bindingError) {
      showNotification(
        bindingError instanceof Error ? bindingError.message : '加载绑定失败',
        'error'
      );
    }
  };

  const saveBindings = async () => {
    if (!bindingGroup) return;
    setSaving(true);
    try {
      await tenantClientAccessApi.replaceGroupBindings(
        bindingGroup.id,
        selectedIndices,
        Number.parseInt(priority || '0', 10) || 0
      );
      setBindingGroup(null);
      showNotification('提供商绑定已保存', 'success');
      await load();
    } catch (saveError) {
      showNotification(saveError instanceof Error ? saveError.message : '保存失败', 'error');
    } finally {
      setSaving(false);
    }
  };

  const remove = (group: ClientGroup) => {
    showConfirmation({
      title: '删除分组',
      message: `确认删除 ${group.name}？关联的 Key 将不再使用此分组。`,
      variant: 'danger',
      onConfirm: async () => {
        await tenantClientAccessApi.deleteGroup(group.id);
        showNotification('分组已删除', 'success');
        await load();
      },
    });
  };

  const toggleProvider = (authIndex: string) => {
    setSelectedIndices((current) =>
      current.includes(authIndex)
        ? current.filter((item) => item !== authIndex)
        : [...current, authIndex]
    );
  };

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <div>
          <h1>客户端分组</h1>
          <p>将自己的提供商分配给分组，再将分组绑定到 API Key。</p>
        </div>
        <Button onClick={openCreate}>添加分组</Button>
      </header>
      <div className={styles.tableWrap}>
        {loading ? <div className={styles.loading}>正在加载...</div> : null}
        {error ? <div className={styles.error}>{error}</div> : null}
        {!loading && !error && groups.length === 0 ? (
          <div className={styles.empty}>尚未创建分组。</div>
        ) : null}
        {!loading && !error && groups.length > 0 ? (
          <table className={styles.table}>
            <thead>
              <tr>
                <th>名称</th>
                <th>状态</th>
                <th>API Key</th>
                <th>提供商</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {groups.map((group) => (
                <tr key={group.id}>
                  <td>
                    <strong>{group.name}</strong>
                    <div className={styles.muted}>{group.description || '-'}</div>
                  </td>
                  <td>
                    <span
                      className={`${styles.badge} ${group.enabled ? styles.enabled : styles.disabled}`}
                    >
                      {group.enabled ? '启用' : '停用'}
                    </span>
                  </td>
                  <td>{group.key_count}</td>
                  <td>{group.credential_count}</td>
                  <td>
                    <div className={styles.actions}>
                      <Button
                        size="xs"
                        variant="secondary"
                        onClick={() => void openBindings(group)}
                      >
                        绑定提供商
                      </Button>
                      <Button size="xs" variant="secondary" onClick={() => openEdit(group)}>
                        编辑
                      </Button>
                      <Button size="xs" variant="danger" onClick={() => remove(group)}>
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
        open={groupModal}
        onClose={() => setGroupModal(false)}
        title={editing ? '编辑分组' : '添加分组'}
        footer={
          <>
            <Button variant="secondary" onClick={() => setGroupModal(false)}>
              取消
            </Button>
            <Button loading={saving} onClick={() => void saveGroup()}>
              保存
            </Button>
          </>
        }
      >
        <div className={styles.inlineForm}>
          <Input
            label="名称"
            value={form.name}
            onChange={(event) => setForm((current) => ({ ...current, name: event.target.value }))}
          />
          <Input
            label="说明"
            value={form.description}
            onChange={(event) =>
              setForm((current) => ({ ...current, description: event.target.value }))
            }
          />
          <ToggleSwitch
            checked={form.enabled}
            onChange={(enabled) => setForm((current) => ({ ...current, enabled }))}
            label="启用此分组"
          />
        </div>
      </Modal>
      <Modal
        open={bindingGroup !== null}
        onClose={() => setBindingGroup(null)}
        title={bindingGroup ? `绑定提供商：${bindingGroup.name}` : '绑定提供商'}
        footer={
          <>
            <Button variant="secondary" onClick={() => setBindingGroup(null)}>
              取消
            </Button>
            <Button loading={saving} onClick={() => void saveBindings()}>
              保存
            </Button>
          </>
        }
      >
        <div className={styles.inlineForm}>
          <Input
            label="绑定优先级"
            type="number"
            value={priority}
            onChange={(event) => setPriority(event.target.value)}
          />
          <div className={styles.checkList}>
            {providers.length === 0 ? (
              <span className={styles.muted}>尚无可绑定的提供商。</span>
            ) : null}
            {providers.map((provider) => (
              <label key={provider.id}>
                <input
                  type="checkbox"
                  checked={selectedIndices.includes(provider.auth_index)}
                  onChange={() => toggleProvider(provider.auth_index)}
                />
                <span>
                  {provider.name} ({provider.channel})
                </span>
              </label>
            ))}
          </div>
        </div>
      </Modal>
    </div>
  );
}
