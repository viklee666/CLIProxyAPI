import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import { useDebounce } from '@/hooks/useDebounce';
import { useHeaderRefresh } from '@/hooks/useHeaderRefresh';
import {
  authFilesApi,
  clientAccessApi,
  type ClientGroup,
  type CredentialBinding,
} from '@/services/api';
import type { AuthFileItem } from '@/types';
import { useNotificationStore } from '@/stores';
import { resolveCodexPlanType } from '@/utils/quota';
import styles from './ClientAccessPage.module.scss';
import {
  buildCredentialBindingSelection,
  isQueryCredentialSelectionActive,
} from './credentialSelection';

type GroupForm = { name: string; description: string; enabled: boolean };
const emptyForm: GroupForm = { name: '', description: '', enabled: true };
const authIndexOf = (file: AuthFileItem) => String(file.authIndex ?? file.auth_index ?? '').trim();
const planTypeOf = (file: AuthFileItem) => resolveCodexPlanType(file) ?? '';

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
  const [bindingGroupFilter, setBindingGroupFilter] = useState('all');
  const [bindingPlanFilter, setBindingPlanFilter] = useState('all');
  const [bindingPlanFacets, setBindingPlanFacets] = useState<Record<string, number>>({});
  const [bindingProviderFilters, setBindingProviderFilters] = useState<string[]>([]);
  const [bindingProviderFacets, setBindingProviderFacets] = useState<Record<string, number>>({});
  const [selectAllCredentials, setSelectAllCredentials] = useState(false);
  const [selectCurrentPlan, setSelectCurrentPlan] = useState(false);
  const [excludedAuthIndices, setExcludedAuthIndices] = useState<string[]>([]);
  const [selectionPreview, setSelectionPreview] = useState<{
    matched: number;
    excluded: number;
  } | null>(null);
  const [selectionPreviewLoading, setSelectionPreviewLoading] = useState(false);
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
    setBindingGroupFilter('all');
    setBindingPlanFilter('all');
    setBindingProviderFilters([]);
    setSelectAllCredentials(false);
    setSelectCurrentPlan(false);
    setExcludedAuthIndices([]);
    setSelectionPreview(null);
    setSelectedAuthIndices([]);
    setSelectedGroupIDs([]);
    setBindingModalOpen(true);
  };

  const loadBindingPage = useCallback(async () => {
    if (!bindingModalOpen) return;
    setBindingLoading(true);
    try {
      const selectedGroupID =
        bindingGroupFilter === 'all' ? 0 : Number.parseInt(bindingGroupFilter, 10) || 0;
      const filesResponse = await authFilesApi.list({
        view: 'summary',
        page: bindingPage,
        page_size: bindingPageSize,
        search: debouncedBindingSearch.trim() || undefined,
        provider: bindingProviderFilters.length > 0 ? bindingProviderFilters.join(',') : undefined,
        plan_type: bindingPlanFilter === 'all' ? undefined : bindingPlanFilter,
        group_id: selectedGroupID > 0 ? selectedGroupID : undefined,
        sort: 'name',
        order: 'asc',
      });
      const authIndices = (filesResponse.files ?? []).map(authIndexOf).filter(Boolean);
      const bindingsResponse =
        authIndices.length > 0 ? await clientAccessApi.listAllCredentialBindings(authIndices) : [];
      const files = filesResponse.files ?? [];
      setAuthFiles(files);
      setBindingTotal(filesResponse.total ?? files.length);
      setBindings(bindingsResponse);
      setBindingPlanFacets((current) => ({
        ...current,
        ...(filesResponse.facets?.plan_types ?? {}),
      }));
      setBindingProviderFacets((current) => ({
        ...current,
        ...(filesResponse.facets?.providers ?? {}),
      }));
    } catch (loadError) {
      showNotification(loadError instanceof Error ? loadError.message : String(loadError), 'error');
    } finally {
      setBindingLoading(false);
    }
  }, [
    bindingGroupFilter,
    bindingModalOpen,
    bindingPage,
    bindingPlanFilter,
    bindingProviderFilters,
    debouncedBindingSearch,
    showNotification,
  ]);

  useEffect(() => {
    if (!bindingModalOpen) return;
    let cancelled = false;
    void clientAccessApi
      .listAllGroups()
      .then((groups) => {
        if (!cancelled) setBindingGroups(groups);
      })
      .catch((loadError) => {
        if (!cancelled)
          showNotification(
            loadError instanceof Error ? loadError.message : String(loadError),
            'error'
          );
      });
    return () => {
      cancelled = true;
    };
  }, [bindingModalOpen, showNotification]);
  useEffect(() => void loadBindingPage(), [loadBindingPage]);
  useEffect(
    () => setBindingPage(1),
    [bindingGroupFilter, bindingPlanFilter, bindingProviderFilters, debouncedBindingSearch]
  );
  useEffect(() => {
    if (bindingPlanFilter === 'all') setSelectCurrentPlan(false);
  }, [bindingPlanFilter]);

  const visibleAuthFiles = authFiles.filter((file) => Boolean(authIndexOf(file)));
  const visibleAuthIndices = useMemo(
    () => visibleAuthFiles.map(authIndexOf).filter(Boolean),
    [visibleAuthFiles]
  );
  const selectedAuthIndexSet = useMemo(() => new Set(selectedAuthIndices), [selectedAuthIndices]);
  const excludedAuthIndexSet = useMemo(() => new Set(excludedAuthIndices), [excludedAuthIndices]);
  const selectedGroupIDSet = useMemo(() => new Set(selectedGroupIDs), [selectedGroupIDs]);
  const bindingGroupByID = useMemo(
    () => new Map(bindingGroups.map((group) => [group.id, group])),
    [bindingGroups]
  );
  const bindingsByAuthIndex = useMemo(() => {
    const result = new Map<string, CredentialBinding[]>();
    for (const binding of bindings) {
      const list = result.get(binding.auth_index) ?? [];
      list.push(binding);
      result.set(binding.auth_index, list);
    }
    return result;
  }, [bindings]);
  const planOptions = useMemo(() => {
    const values = new Set<string>(Object.keys(bindingPlanFacets));
    for (const file of visibleAuthFiles) {
      const planType = planTypeOf(file);
      if (planType) values.add(planType);
    }
    return Array.from(values).sort();
  }, [bindingPlanFacets, visibleAuthFiles]);
  const providerOptions = useMemo(() => {
    const values = new Set<string>(Object.keys(bindingProviderFacets));
    for (const file of visibleAuthFiles) {
      const provider = String(file.provider ?? file.type ?? '')
        .trim()
        .toLowerCase();
      if (provider) values.add(provider);
    }
    return Array.from(values).sort();
  }, [bindingProviderFacets, visibleAuthFiles]);
  const querySelectionState = useMemo(
    () => ({
      all: selectAllCredentials,
      selectCurrentPlan,
      planFilter: bindingPlanFilter,
      providers: bindingProviderFilters,
      excludedAuthIndices,
    }),
    [
      bindingPlanFilter,
      bindingProviderFilters,
      excludedAuthIndices,
      selectAllCredentials,
      selectCurrentPlan,
    ]
  );
  const querySelection = useMemo(
    () => buildCredentialBindingSelection(querySelectionState),
    [querySelectionState]
  );
  const querySelectionActive = isQueryCredentialSelectionActive(querySelectionState);
  const toggleAuthIndex = (authIndex: string) => {
    if (querySelectionActive) {
      setExcludedAuthIndices((current) =>
        current.includes(authIndex)
          ? current.filter((item) => item !== authIndex)
          : [...current, authIndex]
      );
      return;
    }
    setSelectedAuthIndices((current) =>
      current.includes(authIndex)
        ? current.filter((item) => item !== authIndex)
        : [...current, authIndex]
    );
  };
  const toggleBindingGroup = (groupID: number) =>
    setSelectedGroupIDs((current) =>
      current.includes(groupID) ? current.filter((item) => item !== groupID) : [...current, groupID]
    );
  const toggleProviderFilter = (provider: string) => {
    setSelectAllCredentials(false);
    setSelectedAuthIndices([]);
    setExcludedAuthIndices([]);
    setBindingProviderFilters((current) =>
      current.includes(provider)
        ? current.filter((item) => item !== provider)
        : [...current, provider]
    );
  };
  const toggleVisibleAuthIndices = () => {
    if (querySelectionActive) {
      setExcludedAuthIndices((current) => {
        const next = new Set(current);
        const allSelected =
          visibleAuthIndices.length > 0 &&
          visibleAuthIndices.every((authIndex) => !next.has(authIndex));
        for (const authIndex of visibleAuthIndices) {
          if (allSelected) {
            next.add(authIndex);
          } else {
            next.delete(authIndex);
          }
        }
        return Array.from(next);
      });
      return;
    }
    setSelectedAuthIndices((current) => {
      const next = new Set(current);
      const allSelected =
        visibleAuthIndices.length > 0 &&
        visibleAuthIndices.every((authIndex) => next.has(authIndex));
      for (const authIndex of visibleAuthIndices) {
        if (allSelected) {
          next.delete(authIndex);
        } else {
          next.add(authIndex);
        }
      }
      return Array.from(next);
    });
  };
  const bindingLabels = (authIndex: string) =>
    (bindingsByAuthIndex.get(authIndex) ?? [])
      .map((binding) => bindingGroupByID.get(binding.group_id)?.name ?? `#${binding.group_id}`)
      .join(', ');
  const isBoundToSelectedGroups = (authIndex: string) =>
    selectedGroupIDs.length > 0 &&
    (bindingsByAuthIndex.get(authIndex) ?? []).some((binding) =>
      selectedGroupIDSet.has(binding.group_id)
    );
  const isAuthIndexSelected = (authIndex: string) =>
    querySelectionActive
      ? !excludedAuthIndexSet.has(authIndex)
      : selectedAuthIndexSet.has(authIndex);

  useEffect(() => {
    if (!bindingModalOpen || !querySelection) {
      setSelectionPreview(null);
      setSelectionPreviewLoading(false);
      return;
    }
    let cancelled = false;
    setSelectionPreviewLoading(true);
    const timer = window.setTimeout(() => {
      void clientAccessApi
        .bulkReplaceCredentialBindings(querySelection, [], true)
        .then((result) => {
          if (!cancelled)
            setSelectionPreview({ matched: result.matched, excluded: result.excluded });
        })
        .catch(() => {
          if (!cancelled) setSelectionPreview(null);
        })
        .finally(() => {
          if (!cancelled) setSelectionPreviewLoading(false);
        });
    }, 200);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [bindingModalOpen, querySelection]);

  const selectionScope = useMemo(() => {
    if (!querySelection) {
      return `显式选择 ${selectedAuthIndices.length} 个凭证；翻页后仍会保留。`;
    }
    const estimated = selectionPreviewLoading
      ? '正在由服务端计算预计范围…'
      : selectionPreview
        ? `预计命中 ${selectionPreview.matched} 个，已排除 ${selectionPreview.excluded} 个。`
        : '预计范围暂不可用。';
    if (querySelection.all) {
      return `服务端选择全部凭证；provider 与订阅等级条件会被忽略。${estimated}`;
    }
    const parts: string[] = [];
    if (querySelection.providers.length > 0) {
      parts.push(`provider: ${querySelection.providers.join(', ')}`);
    }
    if (querySelection.plan_types.length > 0) {
      parts.push(`订阅等级: ${querySelection.plan_types.join(', ')}`);
    }
    return `服务端跨页选择 ${parts.join(' + ')}；搜索和显示分组只影响当前列表。${estimated}`;
  }, [querySelection, selectedAuthIndices.length, selectionPreview, selectionPreviewLoading]);

  const saveBindings = async () => {
    if (!querySelection && selectedAuthIndices.length === 0) return;
    setBindingSaving(true);
    try {
      const priority = Number.parseInt(bindingPriority || '0', 10) || 0;
      const groups = selectedGroupIDs.map((groupID) => ({ group_id: groupID, priority }));
      if (querySelection) {
        const result = await clientAccessApi.bulkReplaceCredentialBindings(querySelection, groups);
        showNotification(
          `凭证分配完成：命中 ${result.matched}，更新 ${result.updated}，未变化 ${result.unchanged}`,
          'success'
        );
      } else {
        await clientAccessApi.replaceCredentialBindings(selectedAuthIndices, groups);
        showNotification(t('client_access.bindings_saved'), 'success');
      }
      setSelectedAuthIndices([]);
      setExcludedAuthIndices([]);
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
        <div className={styles.actions}>
          <Button variant="secondary" onClick={openBindings}>
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
          <table className={styles.table}>
            <thead>
              <tr>
                <th>{t('common.name')}</th>
                <th>{t('common.status')}</th>
                <th>{t('client_access.keys')}</th>
                <th>{t('client_access.credentials')}</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {items.map((group) => (
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
                  <td>{group.key_count}</td>
                  <td>{group.credential_count}</td>
                  <td>
                    <div className={styles.actions}>
                      <Button size="xs" variant="secondary" onClick={() => openEdit(group)}>
                        {t('common.edit')}
                      </Button>
                      <Button size="xs" variant="danger" onClick={() => remove(group)}>
                        {t('common.delete')}
                      </Button>
                    </div>
                  </td>
                </tr>
              ))}
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
      <Modal
        open={bindingModalOpen}
        onClose={() => setBindingModalOpen(false)}
        width={860}
        title={t('client_access.assign_credentials')}
        footer={
          <>
            <Button variant="secondary" onClick={() => setBindingModalOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button
              loading={bindingSaving}
              disabled={!querySelection && selectedAuthIndices.length === 0}
              onClick={saveBindings}
            >
              {t('common.save')}
            </Button>
          </>
        }
      >
        <div className={styles.modalGrid}>
          <div className={styles.fullWidth}>
            <Input
              label={t('client_access.search_credentials')}
              value={bindingSearch}
              onChange={(event) => setBindingSearch(event.target.value)}
            />
          </div>
          <label className={styles.selectField}>
            <span>显示分组</span>
            <select
              value={bindingGroupFilter}
              onChange={(event) => setBindingGroupFilter(event.target.value)}
            >
              <option value="all">全部凭证</option>
              {bindingGroups.map((group) => (
                <option key={group.id} value={group.id}>
                  {group.name} ({group.credential_count})
                </option>
              ))}
            </select>
          </label>
          <label className={styles.selectField}>
            <span>订阅等级</span>
            <select
              value={bindingPlanFilter}
              onChange={(event) => setBindingPlanFilter(event.target.value)}
            >
              <option value="all">全部等级</option>
              {planOptions.map((planType) => (
                <option key={planType} value={planType}>
                  {planType} ({bindingPlanFacets[planType] ?? 0})
                </option>
              ))}
            </select>
          </label>
          <Input
            label={t('common.priority')}
            type="number"
            value={bindingPriority}
            onChange={(event) => setBindingPriority(event.target.value)}
          />
          <div className={styles.fullWidth}>
            <strong>服务端跨页选择</strong>
            <div className={styles.groupPicker}>
              <label className={styles.groupOption}>
                <input
                  type="checkbox"
                  checked={selectAllCredentials}
                  onChange={(event) => {
                    const checked = event.target.checked;
                    setSelectAllCredentials(checked);
                    setSelectedAuthIndices([]);
                    setExcludedAuthIndices([]);
                    if (checked) {
                      setSelectCurrentPlan(false);
                      setBindingProviderFilters([]);
                    }
                  }}
                />
                全选所有凭证
              </label>
              <label className={styles.groupOption}>
                <input
                  type="checkbox"
                  checked={selectCurrentPlan}
                  disabled={selectAllCredentials || bindingPlanFilter === 'all'}
                  onChange={(event) => {
                    setSelectCurrentPlan(event.target.checked);
                    setSelectedAuthIndices([]);
                    setExcludedAuthIndices([]);
                  }}
                />
                全选当前订阅等级的全部凭证
              </label>
            </div>
            <div className={styles.muted}>{selectionScope}</div>
          </div>
          <div className={styles.fullWidth}>
            <strong>AI provider（多选后跨页选择对应凭证）</strong>
            <div className={styles.groupPicker}>
              {providerOptions.map((provider) => (
                <label className={styles.groupOption} key={provider}>
                  <input
                    type="checkbox"
                    checked={bindingProviderFilters.includes(provider)}
                    disabled={selectAllCredentials}
                    onChange={() => toggleProviderFilter(provider)}
                  />
                  {provider} ({bindingProviderFacets[provider] ?? 0})
                </label>
              ))}
            </div>
          </div>
          <div className={styles.fullWidth}>
            <strong>{t('client_access.groups')}</strong>
            <div className={styles.groupPicker}>
              {bindingGroups.map((group) => (
                <label className={styles.groupOption} key={group.id}>
                  <input
                    type="checkbox"
                    checked={selectedGroupIDs.includes(group.id)}
                    onChange={() => toggleBindingGroup(group.id)}
                  />
                  {group.name}
                </label>
              ))}
            </div>
          </div>
          <div className={styles.fullWidth}>
            <div className={styles.sectionHeader}>
              <strong>
                {t('client_access.credentials')}
                {querySelectionActive ? '（取消勾选会加入排除项）' : ''}
              </strong>
              <Button
                size="xs"
                variant="secondary"
                disabled={visibleAuthIndices.length === 0 || bindingLoading}
                onClick={toggleVisibleAuthIndices}
              >
                {visibleAuthIndices.length > 0 && visibleAuthIndices.every(isAuthIndexSelected)
                  ? '取消全选当前页'
                  : '全选当前页'}
              </Button>
            </div>
            {bindingLoading ? (
              <div className={styles.loading}>{t('common.loading')}</div>
            ) : (
              <div className={styles.credentialPicker}>
                {visibleAuthFiles.map((file) => {
                  const authIndex = authIndexOf(file);
                  const planType = planTypeOf(file);
                  const alreadyBound = isBoundToSelectedGroups(authIndex);
                  return (
                    <label
                      className={`${styles.credentialOption} ${alreadyBound ? styles.credentialBound : ''}`}
                      key={`${file.name}-${authIndex}`}
                    >
                      <input
                        type="checkbox"
                        checked={isAuthIndexSelected(authIndex)}
                        onChange={() => toggleAuthIndex(authIndex)}
                      />
                      <span>
                        <b>{file.name}</b>
                        <small>
                          {file.provider ?? file.type ?? '-'} · {planType || '未知等级'} ·{' '}
                          {authIndex}
                        </small>
                        <small>
                          {bindingLabels(authIndex) || t('client_access.ungrouped')}
                          {alreadyBound ? ' · 已在选中分组' : ''}
                        </small>
                      </span>
                    </label>
                  );
                })}
              </div>
            )}
            <div className={styles.pagination}>
              <Button
                size="xs"
                variant="secondary"
                disabled={bindingPage <= 1 || bindingLoading}
                onClick={() => setBindingPage((value) => value - 1)}
              >
                {t('common.previous')}
              </Button>
              <span>
                {bindingPage} / {Math.max(1, Math.ceil(bindingTotal / bindingPageSize))}
              </span>
              <Button
                size="xs"
                variant="secondary"
                disabled={bindingPage * bindingPageSize >= bindingTotal || bindingLoading}
                onClick={() => setBindingPage((value) => value + 1)}
              >
                {t('common.next')}
              </Button>
            </div>
          </div>
        </div>
      </Modal>
    </div>
  );
}
