import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import { useHeaderRefresh } from '@/hooks/useHeaderRefresh';
import { clientAccessApi, type ClientGroup, type ClientKey } from '@/services/api';
import { useNotificationStore } from '@/stores';
import { copyToClipboard } from '@/utils/clipboard';
import styles from './ClientAccessPage.module.scss';

type KeyForm = {
  name: string;
  enabled: boolean;
  allowAllGroups: boolean;
  allowUngrouped: boolean;
  groupIDs: number[];
  expiresAt: string;
  rpmLimit: string;
  concurrencyLimit: string;
};

const emptyForm: KeyForm = { name: '', enabled: true, allowAllGroups: true, allowUngrouped: false, groupIDs: [], expiresAt: '', rpmLimit: '0', concurrencyLimit: '0' };
const numberValue = (value: string) => Math.max(0, Number.parseInt(value || '0', 10) || 0);
const localDateTime = (value?: string) => value ? new Date(value).toISOString().slice(0, 16) : '';

export function ClientKeysPage() {
  const { t } = useTranslation();
  const showNotification = useNotificationStore((state) => state.showNotification);
  const showConfirmation = useNotificationStore((state) => state.showConfirmation);
  const [items, setItems] = useState<ClientKey[]>([]);
  const [groups, setGroups] = useState<ClientGroup[]>([]);
  const [search, setSearch] = useState('');
  const [page, setPage] = useState(1);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [editing, setEditing] = useState<ClientKey | null>(null);
  const [modalOpen, setModalOpen] = useState(false);
  const [secret, setSecret] = useState('');
  const [form, setForm] = useState<KeyForm>(emptyForm);
  const [saving, setSaving] = useState(false);
  const pageSize = 20;

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const [keyResponse, groupResponse] = await Promise.all([
        clientAccessApi.listKeys(page, pageSize, search),
        clientAccessApi.listGroups(1, 200),
      ]);
      setItems(keyResponse.items ?? []);
      setTotal(keyResponse.total ?? 0);
      setGroups(groupResponse.items ?? []);
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : String(loadError));
    } finally {
      setLoading(false);
    }
  }, [page, search]);

  useEffect(() => void load(), [load]);
  useHeaderRefresh(load);
  const groupByID = useMemo(() => new Map(groups.map((group) => [group.id, group])), [groups]);

  const openCreate = () => { setEditing(null); setForm(emptyForm); setModalOpen(true); };
  const openEdit = (key: ClientKey) => {
    setEditing(key);
    setForm({ name: key.name, enabled: key.enabled, allowAllGroups: key.allow_all_groups, allowUngrouped: key.allow_ungrouped, groupIDs: key.group_ids ?? [], expiresAt: localDateTime(key.expires_at), rpmLimit: String(key.rpm_limit ?? 0), concurrencyLimit: String(key.concurrency_limit ?? 0) });
    setModalOpen(true);
  };

  const toggleGroup = (id: number) => setForm((value) => ({ ...value, groupIDs: value.groupIDs.includes(id) ? value.groupIDs.filter((groupID) => groupID !== id) : [...value.groupIDs, id] }));

  const save = async () => {
    if (!form.name.trim()) return;
    setSaving(true);
    const payload = {
      name: form.name.trim(), enabled: form.enabled, allow_all_groups: form.allowAllGroups,
      allow_ungrouped: form.allowUngrouped, group_ids: form.groupIDs,
      rpm_limit: numberValue(form.rpmLimit), concurrency_limit: numberValue(form.concurrencyLimit),
      ...(form.expiresAt ? { expires_at: new Date(form.expiresAt).toISOString() } : editing ? { clear_expires_at: true } : {}),
    };
    try {
      if (editing) {
        await clientAccessApi.updateKey(editing.id, payload);
      } else {
        const created = await clientAccessApi.createKey(payload);
        setSecret(created.secret);
      }
      setModalOpen(false);
      showNotification(t('client_access.saved'), 'success');
      await load();
    } catch (saveError) {
      showNotification(saveError instanceof Error ? saveError.message : String(saveError), 'error');
    } finally {
      setSaving(false);
    }
  };

  const remove = (key: ClientKey) => showConfirmation({ title: t('client_access.delete_key'), message: t('client_access.delete_key_confirm', { name: key.name }), variant: 'danger', onConfirm: async () => { await clientAccessApi.deleteKey(key.id); showNotification(t('client_access.deleted'), 'success'); await load(); } });
  const pages = Math.max(1, Math.ceil(total / pageSize));

  return (
    <div className={styles.page}>
      <header className={styles.header}><div><h1>{t('client_access.keys_title')}</h1><p>{t('client_access.keys_description')}</p></div><Button onClick={openCreate}>{t('client_access.add_key')}</Button></header>
      <div className={styles.toolbar}><Input value={search} placeholder={t('client_access.search_keys')} onChange={(event) => { setSearch(event.target.value); setPage(1); }} /></div>
      <div className={styles.tableWrap}>
        {loading ? <div className={styles.loading}>{t('common.loading')}</div> : null}
        {!loading && error ? <div className={styles.error}>{error}</div> : null}
        {!loading && !error && items.length === 0 ? <div className={styles.empty}>{t('client_access.no_keys')}</div> : null}
        {!loading && !error && items.length > 0 ? <table className={styles.table}><thead><tr><th>{t('common.name')}</th><th>{t('common.status')}</th><th>{t('client_access.groups')}</th><th>RPM</th><th>{t('client_access.concurrency')}</th><th>{t('client_access.expires')}</th><th /></tr></thead><tbody>{items.map((key) => <tr key={key.id}><td><div className={styles.name}>{key.name}</div><div className={styles.muted}>{key.key_mask}</div></td><td><span className={`${styles.badge} ${key.enabled ? styles.badgeActive : styles.badgeDisabled}`}>{key.enabled ? t('common.enabled') : t('common.disabled')}</span></td><td>{key.allow_all_groups ? t('client_access.all_groups') : (key.group_ids ?? []).map((id) => groupByID.get(id)?.name ?? `#${id}`).join(', ') || '-'}</td><td>{key.rpm_limit || '∞'}</td><td>{key.current_concurrency} / {key.concurrency_limit || '∞'}</td><td>{key.expires_at ? new Date(key.expires_at).toLocaleString() : t('client_access.never')}</td><td><div className={styles.actions}><Button size="xs" variant="secondary" onClick={() => openEdit(key)}>{t('common.edit')}</Button><Button size="xs" variant="danger" onClick={() => remove(key)}>{t('common.delete')}</Button></div></td></tr>)}</tbody></table> : null}
      </div>
      <div className={styles.pagination}><Button size="xs" variant="secondary" disabled={page <= 1} onClick={() => setPage((value) => value - 1)}>{t('common.previous')}</Button><span>{page} / {pages}</span><Button size="xs" variant="secondary" disabled={page >= pages} onClick={() => setPage((value) => value + 1)}>{t('common.next')}</Button></div>
      <Modal open={modalOpen} onClose={() => setModalOpen(false)} width={720} title={editing ? t('client_access.edit_key') : t('client_access.add_key')} footer={<><Button variant="secondary" onClick={() => setModalOpen(false)}>{t('common.cancel')}</Button><Button loading={saving} onClick={save}>{t('common.save')}</Button></>}><div className={styles.modalGrid}><Input label={t('common.name')} value={form.name} onChange={(event) => setForm((value) => ({ ...value, name: event.target.value }))} /><Input label={t('client_access.expires')} type="datetime-local" value={form.expiresAt} onChange={(event) => setForm((value) => ({ ...value, expiresAt: event.target.value }))} /><Input label="RPM" type="number" min={0} value={form.rpmLimit} onChange={(event) => setForm((value) => ({ ...value, rpmLimit: event.target.value }))} /><Input label={t('client_access.concurrency_limit')} type="number" min={0} value={form.concurrencyLimit} onChange={(event) => setForm((value) => ({ ...value, concurrencyLimit: event.target.value }))} /><div className={styles.toggleRow}><ToggleSwitch checked={form.enabled} onChange={(enabled) => setForm((value) => ({ ...value, enabled }))} label={t('common.enabled')} /></div><div className={styles.toggleRow}><ToggleSwitch checked={form.allowAllGroups} onChange={(allowAllGroups) => setForm((value) => ({ ...value, allowAllGroups }))} label={t('client_access.all_groups')} /></div><div className={styles.toggleRow}><ToggleSwitch checked={form.allowUngrouped} onChange={(allowUngrouped) => setForm((value) => ({ ...value, allowUngrouped }))} label={t('client_access.allow_ungrouped')} /></div>{!form.allowAllGroups ? <div className={`${styles.fullWidth} ${styles.groupPicker}`}>{groups.map((group) => <label className={styles.groupOption} key={group.id}><input type="checkbox" checked={form.groupIDs.includes(group.id)} onChange={() => toggleGroup(group.id)} />{group.name}</label>)}</div> : null}</div></Modal>
      <Modal open={Boolean(secret)} onClose={() => setSecret('')} title={t('client_access.secret_title')} footer={<><Button variant="secondary" onClick={async () => { const copied = await copyToClipboard(secret); showNotification(t(copied ? 'notification.link_copied' : 'notification.copy_failed'), copied ? 'success' : 'error'); }}>{t('common.copy')}</Button><Button onClick={() => setSecret('')}>{t('common.confirm')}</Button></>}><p>{t('client_access.secret_hint')}</p><div className={styles.secretBox}>{secret}</div></Modal>
    </div>
  );
}
