import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import { useDebounce } from '@/hooks/useDebounce';
import { useHeaderRefresh } from '@/hooks/useHeaderRefresh';
import { authFilesApi, clientAccessApi, type ClientGroup, type CredentialBinding } from '@/services/api';
import type { AuthFileItem } from '@/types';
import { useNotificationStore } from '@/stores';
import styles from './ClientAccessPage.module.scss';

type GroupForm = { name: string; description: string; enabled: boolean };
const emptyForm: GroupForm = { name: '', description: '', enabled: true };
const authIndexOf = (file: AuthFileItem) => String(file.authIndex ?? file.auth_index ?? '').trim();

export function ClientGroupsPage() {
  const { t } = useTranslation();
  const showNotification = useNotificationStore((state) => state.showNotification);
  const showConfirmation = useNotificationStore((state) => state.showConfirmation);
  const [items, setItems] = useState<ClientGroup[]>([]);
  const [search, setSearch] = useState('');
  const [page, setPage] = useState(1);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [editing, setEditing] = useState<ClientGroup | null>(null);
  const [modalOpen, setModalOpen] = useState(false);
  const [form, setForm] = useState<GroupForm>(emptyForm);
  const [saving, setSaving] = useState(false);
  const [bindingModalOpen, setBindingModalOpen] = useState(false);
  const [authFiles, setAuthFiles] = useState<AuthFileItem[]>([]);
  const [bindings, setBindings] = useState<CredentialBinding[]>([]);
  const [bindingGroups, setBindingGroups] = useState<ClientGroup[]>([]);
  const [selectedAuthIndices, setSelectedAuthIndices] = useState<string[]>([]);
  const [selectedGroupIDs, setSelectedGroupIDs] = useState<number[]>([]);
  const [bindingPriority, setBindingPriority] = useState('0');
  const [bindingSearch, setBindingSearch] = useState('');
  const [bindingPage, setBindingPage] = useState(1);
  const [bindingTotal, setBindingTotal] = useState(0);
  const [bindingLoading, setBindingLoading] = useState(false);
  const [bindingSaving, setBindingSaving] = useState(false);
  const pageSize = 20;
  const bindingPageSize = 50;
  const debouncedBindingSearch = useDebounce(bindingSearch, 300);

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const response = await clientAccessApi.listGroups(page, pageSize, search);
      setItems(response.items ?? []);
      setTotal(response.total ?? 0);
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : String(loadError));
    } finally {
      setLoading(false);
    }
  }, [page, search]);

  useEffect(() => void load(), [load]);
  useHeaderRefresh(load);

  const openCreate = () => {
    setEditing(null);
    setForm(emptyForm);
    setModalOpen(true);
  };

  const openEdit = (group: ClientGroup) => {
    setEditing(group);
    setForm({ name: group.name, description: group.description ?? '', enabled: group.enabled });
    setModalOpen(true);
  };

  const save = async () => {
    if (!form.name.trim()) return;
    setSaving(true);
    try {
      if (editing) {
        await clientAccessApi.updateGroup(editing.id, form);
      } else {
        await clientAccessApi.createGroup(form);
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

  const remove = (group: ClientGroup) => {
    showConfirmation({
      title: t('client_access.delete_group'),
      message: t('client_access.delete_group_confirm', { name: group.name }),
      variant: 'danger',
      onConfirm: async () => {
        await clientAccessApi.deleteGroup(group.id);
        showNotification(t('client_access.deleted'), 'success');
        await load();
      },
    });
  };

  const openBindings = () => {
    setBindingPage(1);
    setBindingSearch('');
    setSelectedAuthIndices([]);
    setBindingModalOpen(true);
  };

  const loadBindingPage = useCallback(async () => {
    if (!bindingModalOpen) return;
    setBindingLoading(true);
    try {
      const [filesResponse, groupsResponse] = await Promise.all([
        authFilesApi.list({
          view: 'summary',
          page: bindingPage,
          page_size: bindingPageSize,
          search: debouncedBindingSearch.trim() || undefined,
          sort: 'name',
          order: 'asc',
        }),
        clientAccessApi.listGroups(1, 200),
      ]);
      const files = filesResponse.files ?? [];
      const authIndices = files.map(authIndexOf).filter(Boolean);
      const bindingsResponse = authIndices.length > 0
        ? await clientAccessApi.listCredentialBindings(1, 200, '', authIndices)
        : { items: [], total: 0, page: 1, page_size: 200 };
      setAuthFiles(files);
      setBindingTotal(filesResponse.total ?? files.length);
      setBindingGroups(groupsResponse.items ?? []);
      setBindings(bindingsResponse.items ?? []);
    } catch (loadError) {
      showNotification(loadError instanceof Error ? loadError.message : String(loadError), 'error');
    } finally {
      setBindingLoading(false);
    }
  }, [bindingModalOpen, bindingPage, debouncedBindingSearch, showNotification]);

  useEffect(() => void loadBindingPage(), [loadBindingPage]);
  useEffect(() => setBindingPage(1), [debouncedBindingSearch]);

  const visibleAuthFiles = authFiles.filter((file) => Boolean(authIndexOf(file)));
  const toggleAuthIndex = (authIndex: string) => setSelectedAuthIndices((current) => current.includes(authIndex) ? current.filter((item) => item !== authIndex) : [...current, authIndex]);
  const toggleBindingGroup = (groupID: number) => setSelectedGroupIDs((current) => current.includes(groupID) ? current.filter((item) => item !== groupID) : [...current, groupID]);
  const bindingLabels = (authIndex: string) => bindings.filter((binding) => binding.auth_index === authIndex).map((binding) => bindingGroups.find((group) => group.id === binding.group_id)?.name ?? `#${binding.group_id}`).join(', ');

  const saveBindings = async () => {
    if (selectedAuthIndices.length === 0) return;
    setBindingSaving(true);
    try {
      const priority = Number.parseInt(bindingPriority || '0', 10) || 0;
      await clientAccessApi.replaceCredentialBindings(
        selectedAuthIndices,
        selectedGroupIDs.map((groupID) => ({ group_id: groupID, priority }))
      );
      showNotification(t('client_access.bindings_saved'), 'success');
      setSelectedAuthIndices([]);
      await loadBindingPage();
      await load();
    } catch (saveError) {
      showNotification(saveError instanceof Error ? saveError.message : String(saveError), 'error');
    } finally {
      setBindingSaving(false);
    }
  };

  const pages = Math.max(1, Math.ceil(total / pageSize));

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <div>
          <h1>{t('client_access.groups_title')}</h1>
          <p>{t('client_access.groups_description')}</p>
        </div>
        <div className={styles.actions}><Button variant="secondary" onClick={openBindings}>{t('client_access.assign_credentials')}</Button><Button onClick={openCreate}>{t('client_access.add_group')}</Button></div>
      </header>
      <div className={styles.toolbar}>
        <Input
          value={search}
          placeholder={t('client_access.search_groups')}
          onChange={(event) => { setSearch(event.target.value); setPage(1); }}
        />
      </div>
      <div className={styles.tableWrap}>
        {loading ? <div className={styles.loading}>{t('common.loading')}</div> : null}
        {!loading && error ? <div className={styles.error}>{error}</div> : null}
        {!loading && !error && items.length === 0 ? <div className={styles.empty}>{t('client_access.no_groups')}</div> : null}
        {!loading && !error && items.length > 0 ? (
          <table className={styles.table}>
            <thead><tr><th>{t('common.name')}</th><th>{t('common.status')}</th><th>{t('client_access.keys')}</th><th>{t('client_access.credentials')}</th><th /></tr></thead>
            <tbody>
              {items.map((group) => (
                <tr key={group.id}>
                  <td><div className={styles.name}>{group.name}</div><div className={styles.muted}>{group.description || '-'}</div></td>
                  <td><span className={`${styles.badge} ${group.enabled ? styles.badgeActive : styles.badgeDisabled}`}>{group.enabled ? t('common.enabled') : t('common.disabled')}</span></td>
                  <td>{group.key_count}</td>
                  <td>{group.credential_count}</td>
                  <td><div className={styles.actions}><Button size="xs" variant="secondary" onClick={() => openEdit(group)}>{t('common.edit')}</Button><Button size="xs" variant="danger" onClick={() => remove(group)}>{t('common.delete')}</Button></div></td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : null}
      </div>
      <div className={styles.pagination}><Button size="xs" variant="secondary" disabled={page <= 1} onClick={() => setPage((value) => value - 1)}>{t('common.previous')}</Button><span>{page} / {pages}</span><Button size="xs" variant="secondary" disabled={page >= pages} onClick={() => setPage((value) => value + 1)}>{t('common.next')}</Button></div>
      <Modal open={modalOpen} onClose={() => setModalOpen(false)} title={editing ? t('client_access.edit_group') : t('client_access.add_group')} footer={<><Button variant="secondary" onClick={() => setModalOpen(false)}>{t('common.cancel')}</Button><Button loading={saving} onClick={save}>{t('common.save')}</Button></>}>
        <Input label={t('common.name')} value={form.name} onChange={(event) => setForm((value) => ({ ...value, name: event.target.value }))} />
        <Input label={t('client_access.description')} value={form.description} onChange={(event) => setForm((value) => ({ ...value, description: event.target.value }))} />
        <ToggleSwitch checked={form.enabled} onChange={(enabled) => setForm((value) => ({ ...value, enabled }))} label={t('common.enabled')} />
      </Modal>
      <Modal open={bindingModalOpen} onClose={() => setBindingModalOpen(false)} width={860} title={t('client_access.assign_credentials')} footer={<><Button variant="secondary" onClick={() => setBindingModalOpen(false)}>{t('common.cancel')}</Button><Button loading={bindingSaving} disabled={selectedAuthIndices.length === 0} onClick={saveBindings}>{t('common.save')}</Button></>}>
        <div className={styles.modalGrid}>
          <div className={styles.fullWidth}><Input label={t('client_access.search_credentials')} value={bindingSearch} onChange={(event) => setBindingSearch(event.target.value)} /></div>
          <Input label={t('common.priority')} type="number" value={bindingPriority} onChange={(event) => setBindingPriority(event.target.value)} />
          <div className={styles.fullWidth}><strong>{t('client_access.groups')}</strong><div className={styles.groupPicker}>{bindingGroups.map((group) => <label className={styles.groupOption} key={group.id}><input type="checkbox" checked={selectedGroupIDs.includes(group.id)} onChange={() => toggleBindingGroup(group.id)} />{group.name}</label>)}</div></div>
          <div className={styles.fullWidth}><strong>{t('client_access.credentials')}</strong>{bindingLoading ? <div className={styles.loading}>{t('common.loading')}</div> : <div className={styles.credentialPicker}>{visibleAuthFiles.map((file) => { const authIndex = authIndexOf(file); return <label className={styles.credentialOption} key={`${file.name}-${authIndex}`}><input type="checkbox" checked={selectedAuthIndices.includes(authIndex)} onChange={() => toggleAuthIndex(authIndex)} /><span><b>{file.name}</b><small>{file.provider ?? file.type ?? '-'} · {authIndex} · {bindingLabels(authIndex) || t('client_access.ungrouped')}</small></span></label>; })}</div>}<div className={styles.pagination}><Button size="xs" variant="secondary" disabled={bindingPage <= 1 || bindingLoading} onClick={() => setBindingPage((value) => value - 1)}>{t('common.previous')}</Button><span>{bindingPage} / {Math.max(1, Math.ceil(bindingTotal / bindingPageSize))}</span><Button size="xs" variant="secondary" disabled={bindingPage * bindingPageSize >= bindingTotal || bindingLoading} onClick={() => setBindingPage((value) => value + 1)}>{t('common.next')}</Button></div></div>
        </div>
      </Modal>
    </div>
  );
}
