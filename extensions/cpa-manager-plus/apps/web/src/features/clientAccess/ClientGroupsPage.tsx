import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import { useHeaderRefresh } from '@/hooks/useHeaderRefresh';
import { authFilesApi, clientAccessApi, type ClientGroup } from '@/services/api';
import { useNotificationStore } from '@/stores';
import type { AuthFileItem } from '@/types';
import { GroupResourceAssignmentsModal } from './GroupResourceAssignmentsModal';
import styles from './ClientAccessPage.module.scss';

type GroupForm = { name: string; description: string; enabled: boolean };
type GroupResourceSummary = {
  credentials: string[];
  providers: Array<{ name: string; count: number }>;
  priorities: number[];
};

const emptyForm: GroupForm = { name: '', description: '', enabled: true };
const authIndexOf = (file: AuthFileItem) => String(file.authIndex ?? file.auth_index ?? '').trim();

export function ClientGroupsPage() {
  const { t } = useTranslation();
  const showNotification = useNotificationStore((state) => state.showNotification);
  const showConfirmation = useNotificationStore((state) => state.showConfirmation);
  const [items, setItems] = useState<ClientGroup[]>([]);
  const [resourceSummaries, setResourceSummaries] = useState<Record<number, GroupResourceSummary>>(
    {}
  );
  const [search, setSearch] = useState('');
  const [page, setPage] = useState(1);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [summaryLoading, setSummaryLoading] = useState(false);
  const [error, setError] = useState('');
  const [editing, setEditing] = useState<ClientGroup | null>(null);
  const [modalOpen, setModalOpen] = useState(false);
  const [form, setForm] = useState<GroupForm>(emptyForm);
  const [saving, setSaving] = useState(false);
  const [resourceGroup, setResourceGroup] = useState<ClientGroup | null>(null);
  const [bindingModalOpen, setBindingModalOpen] = useState(false);
  const pageSize = 20;

  const loadResourceSummaries = useCallback(async (groups: ClientGroup[]) => {
    const groupIDs = groups.map((group) => group.id);
    if (groupIDs.length === 0) {
      setResourceSummaries({});
      return;
    }
    setSummaryLoading(true);
    try {
      const [filesResponse, bindings] = await Promise.all([
        authFilesApi.listSummary(),
        clientAccessApi.listAllCredentialBindings([], groupIDs),
      ]);
      const fileByAuthIndex = new Map<string, AuthFileItem>();
      for (const file of filesResponse.files ?? []) {
        const authIndex = authIndexOf(file);
        if (authIndex) fileByAuthIndex.set(authIndex, file);
      }
      const credentialsByGroup = new Map<number, string[]>();
      const providersByGroup = new Map<number, Map<string, number>>();
      const prioritiesByGroup = new Map<number, Set<number>>();
      for (const binding of bindings) {
        const file = fileByAuthIndex.get(binding.auth_index);
        const credentialName = file?.name || binding.auth_index;
        const providerName = String(file?.provider ?? file?.type ?? 'Unknown').trim() || 'Unknown';
        credentialsByGroup.set(binding.group_id, [
          ...(credentialsByGroup.get(binding.group_id) ?? []),
          credentialName,
        ]);
        const providerCounts = providersByGroup.get(binding.group_id) ?? new Map<string, number>();
        providerCounts.set(providerName, (providerCounts.get(providerName) ?? 0) + 1);
        providersByGroup.set(binding.group_id, providerCounts);
        const priorities = prioritiesByGroup.get(binding.group_id) ?? new Set<number>();
        priorities.add(binding.priority);
        prioritiesByGroup.set(binding.group_id, priorities);
      }
      const next: Record<number, GroupResourceSummary> = {};
      for (const group of groups) {
        next[group.id] = {
          credentials: Array.from(new Set(credentialsByGroup.get(group.id) ?? [])).sort(),
          providers: Array.from(providersByGroup.get(group.id)?.entries() ?? [])
            .map(([name, count]) => ({ name, count }))
            .sort((left, right) => right.count - left.count || left.name.localeCompare(right.name)),
          priorities: Array.from(prioritiesByGroup.get(group.id) ?? []).sort((a, b) => b - a),
        };
      }
      setResourceSummaries(next);
    } catch {
      setResourceSummaries({});
    } finally {
      setSummaryLoading(false);
    }
  }, []);

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const response = await clientAccessApi.listGroups(page, pageSize, search);
      const groups = response.items ?? [];
      setItems(groups);
      setTotal(response.total ?? 0);
      await loadResourceSummaries(groups);
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : String(loadError));
    } finally {
      setLoading(false);
    }
  }, [loadResourceSummaries, page, search]);

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
      if (editing) await clientAccessApi.updateGroup(editing.id, form);
      else await clientAccessApi.createGroup(form);
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

  const pages = Math.max(1, Math.ceil(total / pageSize));

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <div>
          <h1>{t('client_access.groups_title')}</h1>
          <p>{t('client_access.groups_description')}</p>
        </div>
        <div className={styles.actions}>
          <Button variant="secondary" onClick={() => setBindingModalOpen(true)}>
            {t('client_access.assign_credentials')}
          </Button>
          <Button onClick={openCreate}>{t('client_access.add_group')}</Button>
        </div>
      </header>
      <div className={styles.toolbar}>
        <Input
          value={search}
          placeholder={t('client_access.search_groups')}
          onChange={(event) => {
            setSearch(event.target.value);
            setPage(1);
          }}
        />
      </div>
      <div className={styles.tableWrap}>
        {loading ? <div className={styles.loading}>{t('common.loading')}</div> : null}
        {!loading && error ? <div className={styles.error}>{error}</div> : null}
        {!loading && !error && items.length === 0 ? (
          <div className={styles.empty}>{t('client_access.no_groups')}</div>
        ) : null}
        {!loading && !error && items.length > 0 ? (
          <table className={`${styles.table} ${styles.groupsTable}`}>
            <thead>
              <tr>
                <th>{t('common.name')}</th>
                <th>{t('common.status')}</th>
                <th>{t('client_access.keys')}</th>
                <th>
                  {t('client_access.assigned_resources', { defaultValue: 'Assigned resources' })}
                </th>
                <th />
              </tr>
            </thead>
            <tbody>
              {items.map((group) => {
                const summary = resourceSummaries[group.id];
                const visibleCredentials = summary?.credentials.slice(0, 3) ?? [];
                const remainingCredentials = Math.max(
                  0,
                  (summary?.credentials.length ?? group.credential_count) -
                    visibleCredentials.length
                );
                return (
                  <tr key={group.id}>
                    <td>
                      <div className={styles.name}>{group.name}</div>
                      <div className={styles.muted}>{group.description || '-'}</div>
                    </td>
                    <td>
                      <span
                        className={`${styles.badge} ${group.enabled ? styles.badgeActive : styles.badgeDisabled}`}
                      >
                        {group.enabled ? t('common.enabled') : t('common.disabled')}
                      </span>
                    </td>
                    <td>
                      <span className={styles.metricValue}>{group.key_count}</span>
                    </td>
                    <td>
                      {summaryLoading && !summary ? (
                        <span className={styles.muted}>{t('common.loading')}</span>
                      ) : group.credential_count === 0 ? (
                        <span className={styles.resourceEmpty}>
                          {t('client_access.no_assigned_resources', {
                            defaultValue: 'No resources assigned',
                          })}
                        </span>
                      ) : (
                        <div className={styles.resourceSummary}>
                          <div className={styles.resourceSummaryTop}>
                            <b>
                              {t('client_access.credential_count', {
                                count: group.credential_count,
                                defaultValue: `${group.credential_count} credentials`,
                              })}
                            </b>
                            {summary?.priorities.length ? (
                              <span>
                                P {summary.priorities.slice(0, 2).join(' / ')}
                                {summary.priorities.length > 2 ? '…' : ''}
                              </span>
                            ) : null}
                          </div>
                          <div className={styles.resourceChips}>
                            {(summary?.providers ?? []).slice(0, 4).map((provider) => (
                              <span key={provider.name}>
                                {provider.name} · {provider.count}
                              </span>
                            ))}
                          </div>
                          <div
                            className={styles.resourceNames}
                            title={summary?.credentials.join(', ') || undefined}
                          >
                            {visibleCredentials.join(' · ')}
                            {remainingCredentials > 0 ? ` · +${remainingCredentials}` : ''}
                          </div>
                        </div>
                      )}
                    </td>
                    <td>
                      <div className={styles.actions}>
                        <Button
                          size="xs"
                          variant="secondary"
                          onClick={() => setResourceGroup(group)}
                        >
                          {t('client_access.assign_resources')}
                        </Button>
                        <Button size="xs" variant="secondary" onClick={() => openEdit(group)}>
                          {t('common.edit')}
                        </Button>
                        <Button size="xs" variant="danger" onClick={() => remove(group)}>
                          {t('common.delete')}
                        </Button>
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        ) : null}
      </div>
      <div className={styles.pagination}>
        <Button
          size="xs"
          variant="secondary"
          disabled={page <= 1}
          onClick={() => setPage((value) => value - 1)}
        >
          {t('common.previous')}
        </Button>
        <span>
          {page} / {pages}
        </span>
        <Button
          size="xs"
          variant="secondary"
          disabled={page >= pages}
          onClick={() => setPage((value) => value + 1)}
        >
          {t('common.next')}
        </Button>
      </div>
      <Modal
        open={modalOpen}
        onClose={() => setModalOpen(false)}
        title={editing ? t('client_access.edit_group') : t('client_access.add_group')}
        footer={
          <>
            <Button variant="secondary" onClick={() => setModalOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button loading={saving} onClick={save}>
              {t('common.save')}
            </Button>
          </>
        }
      >
        <Input
          label={t('common.name')}
          value={form.name}
          onChange={(event) => setForm((value) => ({ ...value, name: event.target.value }))}
        />
        <Input
          label={t('client_access.description')}
          value={form.description}
          onChange={(event) => setForm((value) => ({ ...value, description: event.target.value }))}
        />
        <ToggleSwitch
          checked={form.enabled}
          onChange={(enabled) => setForm((value) => ({ ...value, enabled }))}
          label={t('common.enabled')}
        />
      </Modal>
      <GroupResourceAssignmentsModal
        group={null}
        batch
        open={bindingModalOpen}
        onClose={() => setBindingModalOpen(false)}
        onSaved={load}
      />
      <GroupResourceAssignmentsModal
        group={resourceGroup}
        open={resourceGroup !== null}
        onClose={() => setResourceGroup(null)}
        onSaved={load}
      />
    </div>
  );
}
