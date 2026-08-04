import { useCallback, useEffect, useMemo, useState } from 'react';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import {
  tenantClientAccessApi,
  type ClientGroup,
  type ClientKey,
  type CreatedClientKey,
} from '@/services/api';
import { useNotificationStore } from '@/stores';
import { copyToClipboard } from '@/utils/clipboard';
import styles from './TenantPanel.module.scss';

type KeyForm = {
  name: string;
  secret: string;
  enabled: boolean;
  allowAllGroups: boolean;
  allowUngrouped: boolean;
  groupIDs: number[];
  concurrencyLimit: string;
};

const emptyForm = (): KeyForm => ({
  name: '',
  secret: '',
  enabled: true,
  allowAllGroups: false,
  allowUngrouped: false,
  groupIDs: [],
  concurrencyLimit: '0',
});

const numberValue = (value: string) => Math.max(0, Number.parseInt(value || '0', 10) || 0);

export function TenantKeysPage() {
  const showNotification = useNotificationStore((state) => state.showNotification);
  const showConfirmation = useNotificationStore((state) => state.showConfirmation);
  const [keys, setKeys] = useState<ClientKey[]>([]);
  const [groups, setGroups] = useState<ClientGroup[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [editing, setEditing] = useState<ClientKey | null>(null);
  const [form, setForm] = useState<KeyForm>(emptyForm);
  const [modalOpen, setModalOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [createdSecret, setCreatedSecret] = useState('');

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const [keyPage, groupPage] = await Promise.all([
        tenantClientAccessApi.listKeys(1, 200),
        tenantClientAccessApi.listGroups(1, 200),
      ]);
      setKeys(keyPage.items ?? []);
      setGroups(groupPage.items ?? []);
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : '加载 API Key 失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const groupByID = useMemo(() => new Map(groups.map((group) => [group.id, group])), [groups]);

  const openCreate = () => {
    setEditing(null);
    setForm(emptyForm());
    setModalOpen(true);
  };

  const openEdit = (key: ClientKey) => {
    setEditing(key);
    setForm({
      name: key.name,
      secret: '',
      enabled: key.enabled,
      allowAllGroups: key.allow_all_groups,
      allowUngrouped: key.allow_ungrouped,
      groupIDs: key.group_ids ?? [],
      concurrencyLimit: String(key.concurrency_limit ?? 0),
    });
    setModalOpen(true);
  };

  const toggleGroup = (id: number) => {
    setForm((current) => ({
      ...current,
      groupIDs: current.groupIDs.includes(id)
        ? current.groupIDs.filter((groupID) => groupID !== id)
        : [...current.groupIDs, id],
    }));
  };

  const save = async () => {
    if (!form.name.trim()) return;
    setSaving(true);
    const input = {
      name: form.name.trim(),
      enabled: form.enabled,
      allow_all_groups: form.allowAllGroups,
      allow_ungrouped: form.allowUngrouped,
      group_ids: form.groupIDs,
      concurrency_limit: numberValue(form.concurrencyLimit),
      ...(!editing && form.secret.trim() ? { secret: form.secret.trim() } : {}),
    };
    try {
      if (editing) {
        await tenantClientAccessApi.updateKey(editing.id, input);
      } else {
        const created: CreatedClientKey = await tenantClientAccessApi.createKey(input);
        setCreatedSecret(created.secret);
      }
      setModalOpen(false);
      showNotification('API Key 已保存', 'success');
      await load();
    } catch (saveError) {
      showNotification(saveError instanceof Error ? saveError.message : '保存失败', 'error');
    } finally {
      setSaving(false);
    }
  };

  const remove = (key: ClientKey) => {
    showConfirmation({
      title: '删除 API Key',
      message: `确认删除 ${key.name}？`,
      variant: 'danger',
      onConfirm: async () => {
        await tenantClientAccessApi.deleteKey(key.id);
        showNotification('API Key 已删除', 'success');
        await load();
      },
    });
  };

  const copySecret = async () => {
    const copied = await copyToClipboard(createdSecret);
    showNotification(copied ? '已复制 API Key' : '复制失败', copied ? 'success' : 'error');
  };

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <div>
          <h1>API Key</h1>
          <p>可为每个 Key 单独设置并发限制和可访问的分组。</p>
        </div>
        <Button onClick={openCreate}>创建 API Key</Button>
      </header>
      <div className={styles.tableWrap}>
        {loading ? <div className={styles.loading}>正在加载...</div> : null}
        {error ? <div className={styles.error}>{error}</div> : null}
        {!loading && !error && keys.length === 0 ? (
          <div className={styles.empty}>尚未创建 API Key。</div>
        ) : null}
        {!loading && !error && keys.length > 0 ? (
          <table className={styles.table}>
            <thead>
              <tr>
                <th>名称</th>
                <th>Key</th>
                <th>范围</th>
                <th>并发</th>
                <th>状态</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {keys.map((key) => (
                <tr key={key.id}>
                  <td>{key.name}</td>
                  <td>
                    <code>{key.key_mask}</code>
                  </td>
                  <td>
                    {key.allow_all_groups
                      ? '全部分组'
                      : (key.group_ids ?? [])
                          .map((id) => groupByID.get(id)?.name ?? `#${id}`)
                          .join(', ') || (key.allow_ungrouped ? '未分组提供商' : '未授权分组')}
                  </td>
                  <td>
                    {key.current_concurrency} / {key.concurrency_limit || '无限制'}
                  </td>
                  <td>
                    <span
                      className={`${styles.badge} ${key.enabled ? styles.enabled : styles.disabled}`}
                    >
                      {key.enabled ? '启用' : '停用'}
                    </span>
                  </td>
                  <td>
                    <div className={styles.actions}>
                      <Button size="xs" variant="secondary" onClick={() => openEdit(key)}>
                        编辑
                      </Button>
                      <Button size="xs" variant="danger" onClick={() => remove(key)}>
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
        open={modalOpen}
        onClose={() => setModalOpen(false)}
        title={editing ? '编辑 API Key' : '创建 API Key'}
        width={680}
        footer={
          <>
            <Button variant="secondary" onClick={() => setModalOpen(false)}>
              取消
            </Button>
            <Button loading={saving} onClick={() => void save()}>
              保存
            </Button>
          </>
        }
      >
        <div className={styles.modalGrid}>
          <Input
            label="名称"
            value={form.name}
            onChange={(event) => setForm((current) => ({ ...current, name: event.target.value }))}
          />
          {!editing ? (
            <Input
              label="自定义 Key（可选）"
              hint="留空后由服务端生成。"
              value={form.secret}
              autoComplete="off"
              onChange={(event) =>
                setForm((current) => ({ ...current, secret: event.target.value }))
              }
            />
          ) : null}
          <Input
            label="并发限制"
            hint="0 表示不限制。"
            type="number"
            min={0}
            value={form.concurrencyLimit}
            onChange={(event) =>
              setForm((current) => ({ ...current, concurrencyLimit: event.target.value }))
            }
          />
          <div className={styles.fullWidth}>
            <ToggleSwitch
              checked={form.enabled}
              onChange={(enabled) => setForm((current) => ({ ...current, enabled }))}
              label="启用此 API Key"
            />
          </div>
          <div className={styles.fullWidth}>
            <ToggleSwitch
              checked={form.allowAllGroups}
              onChange={(allowAllGroups) => setForm((current) => ({ ...current, allowAllGroups }))}
              label="允许全部分组"
            />
          </div>
          <div className={styles.fullWidth}>
            <ToggleSwitch
              checked={form.allowUngrouped}
              onChange={(allowUngrouped) => setForm((current) => ({ ...current, allowUngrouped }))}
              label="允许未分组提供商"
            />
          </div>
          {!form.allowAllGroups ? (
            <div className={`${styles.fullWidth} ${styles.checkList}`}>
              {groups.map((group) => (
                <label key={group.id}>
                  <input
                    type="checkbox"
                    checked={form.groupIDs.includes(group.id)}
                    onChange={() => toggleGroup(group.id)}
                  />
                  {group.name}
                </label>
              ))}
              {groups.length === 0 ? <span className={styles.muted}>尚未创建分组。</span> : null}
            </div>
          ) : null}
        </div>
      </Modal>
      <Modal
        open={Boolean(createdSecret)}
        onClose={() => setCreatedSecret('')}
        title="请保存新的 API Key"
        footer={
          <>
            <Button variant="secondary" onClick={() => void copySecret()}>
              复制
            </Button>
            <Button onClick={() => setCreatedSecret('')}>已保存</Button>
          </>
        }
      >
        <p>该 Key 只会显示一次。</p>
        <div className={styles.secret}>{createdSecret}</div>
      </Modal>
    </div>
  );
}
