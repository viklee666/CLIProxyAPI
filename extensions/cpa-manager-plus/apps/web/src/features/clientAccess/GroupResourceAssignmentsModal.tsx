import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { useDebounce } from '@/hooks/useDebounce';
import {
  authFilesApi,
  clientAccessApi,
  providersApi,
  type ClientGroup,
  type CredentialBinding,
} from '@/services/api';
import { useNotificationStore } from '@/stores';
import type { AuthFileItem } from '@/types';
import { resolveCodexPlanType } from '@/utils/quota';
import {
  buildCredentialBindingSelection,
  isQueryCredentialSelectionActive,
} from './credentialSelection';
import styles from './ClientAccessPage.module.scss';
import {
  buildProviderResources,
  type ProviderLabels,
  type ProviderResource,
} from './providerResources';

type GroupResourceAssignmentsModalProps = {
  group: ClientGroup | null;
  batch?: boolean;
  open: boolean;
  onClose: () => void;
  onSaved: () => Promise<void> | void;
};

const authIndexOf = (file: AuthFileItem) => String(file.authIndex ?? file.auth_index ?? '').trim();
const planTypeOf = (file: AuthFileItem) => resolveCodexPlanType(file) ?? '';
const uniqueAuthIndices = (values: Array<string | null | undefined>) =>
  Array.from(new Set(values.map((value) => String(value ?? '').trim()).filter(Boolean)));

export function GroupResourceAssignmentsModal({
  group,
  batch = false,
  open,
  onClose,
  onSaved,
}: GroupResourceAssignmentsModalProps) {
  const { t } = useTranslation();
  const showNotification = useNotificationStore((state) => state.showNotification);
  const [authFiles, setAuthFiles] = useState<AuthFileItem[]>([]);
  const [bindings, setBindings] = useState<CredentialBinding[]>([]);
  const [groups, setGroups] = useState<ClientGroup[]>([]);
  const [providerResources, setProviderResources] = useState<ProviderResource[]>([]);
  const [selectedAuthIndices, setSelectedAuthIndices] = useState<string[]>([]);
  const [selectedGroupIDs, setSelectedGroupIDs] = useState<number[]>([]);
  const [selectedProviderKeys, setSelectedProviderKeys] = useState<string[]>([]);
  const [stagedProviderKeys, setStagedProviderKeys] = useState<string[]>([]);
  const [bindingPriority, setBindingPriority] = useState('0');
  const [resourceSearch, setResourceSearch] = useState('');
  const [groupFilter, setGroupFilter] = useState('all');
  const [planFilter, setPlanFilter] = useState('all');
  const [planFacets, setPlanFacets] = useState<Record<string, number>>({});
  const [selectAllCredentials, setSelectAllCredentials] = useState(false);
  const [selectCurrentPlan, setSelectCurrentPlan] = useState(false);
  const [excludedAuthIndices, setExcludedAuthIndices] = useState<string[]>([]);
  const [selectionPreview, setSelectionPreview] = useState<{
    matched: number;
    excluded: number;
  } | null>(null);
  const [selectionPreviewLoading, setSelectionPreviewLoading] = useState(false);
  const [resourcePage, setResourcePage] = useState(1);
  const [resourceTotal, setResourceTotal] = useState(0);
  const [resourceLoading, setResourceLoading] = useState(false);
  const [catalogLoading, setCatalogLoading] = useState(false);
  const [resourceSaving, setResourceSaving] = useState(false);
  const pageSize = 50;
  const debouncedSearch = useDebounce(resourceSearch, 300);
  const isBatch = batch && group === null;

  const providerLabels = useMemo<ProviderLabels>(
    () => ({
      defaultEndpoint: t('client_access.default_endpoint'),
      gemini: 'Gemini',
      interactions: 'Interactions',
      codex: 'Codex',
      claude: 'Claude',
      vertex: 'Vertex',
      openAICompatible: 'OpenAI Compatible',
    }),
    [t]
  );

  useEffect(() => {
    if (!open || (!isBatch && !group)) return;
    let cancelled = false;
    setCatalogLoading(true);
    setAuthFiles([]);
    setBindings([]);
    setGroups([]);
    setProviderResources([]);
    setSelectedAuthIndices([]);
    setSelectedGroupIDs([]);
    setSelectedProviderKeys([]);
    setStagedProviderKeys([]);
    setBindingPriority('0');
    setResourceSearch('');
    setGroupFilter('all');
    setPlanFilter('all');
    setPlanFacets({});
    setSelectAllCredentials(false);
    setSelectCurrentPlan(false);
    setExcludedAuthIndices([]);
    setSelectionPreview(null);
    setResourcePage(1);

    void Promise.all([
      clientAccessApi.listAllGroups(),
      group ? clientAccessApi.listAllCredentialBindings([], [group.id]) : Promise.resolve([]),
      providersApi.getGeminiKeys(),
      providersApi.getInteractionsKeys(),
      providersApi.getCodexConfigs(),
      providersApi.getClaudeConfigs(),
      providersApi.getVertexConfigs(),
      providersApi.getOpenAIProviders(),
    ])
      .then(
        ([loadedGroups, groupBindings, gemini, interactions, codex, claude, vertex, openAI]) => {
          if (cancelled) return;
          setGroups(loadedGroups);
          setSelectedAuthIndices(
            uniqueAuthIndices(groupBindings.map((binding) => binding.auth_index))
          );
          const priority = groupBindings[0]?.priority;
          setBindingPriority(
            typeof priority === 'number' && Number.isFinite(priority) ? String(priority) : '0'
          );
          setProviderResources(
            buildProviderResources(
              [
                { type: 'gemini', configs: gemini },
                { type: 'interactions', configs: interactions },
                { type: 'codex', configs: codex },
                { type: 'claude', configs: claude },
                { type: 'vertex', configs: vertex },
              ],
              openAI,
              providerLabels
            )
          );
        }
      )
      .catch((loadError) => {
        if (!cancelled) {
          showNotification(
            loadError instanceof Error ? loadError.message : String(loadError),
            'error'
          );
        }
      })
      .finally(() => {
        if (!cancelled) setCatalogLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [group, isBatch, open, providerLabels, showNotification]);

  const loadResourcePage = useCallback(async () => {
    if (!open || (!isBatch && !group)) return;
    setResourceLoading(true);
    try {
      const selectedGroupID =
        isBatch && groupFilter !== 'all' ? Number.parseInt(groupFilter, 10) || 0 : 0;
      const filesResponse = await authFilesApi.list({
        view: 'summary',
        page: resourcePage,
        page_size: pageSize,
        search: debouncedSearch.trim() || undefined,
        plan_type: planFilter === 'all' ? undefined : planFilter,
        group_id: selectedGroupID > 0 ? selectedGroupID : undefined,
        sort: 'name',
        order: 'asc',
      });
      const files = (filesResponse.files ?? []).filter((file) => Boolean(authIndexOf(file)));
      const authIndices = files.map(authIndexOf);
      const loadedBindings =
        authIndices.length > 0 ? await clientAccessApi.listAllCredentialBindings(authIndices) : [];
      setAuthFiles(files);
      setBindings(loadedBindings);
      setResourceTotal(filesResponse.total ?? files.length);
      setPlanFacets((current) => ({
        ...current,
        ...(filesResponse.facets?.plan_types ?? {}),
      }));
    } catch (loadError) {
      showNotification(loadError instanceof Error ? loadError.message : String(loadError), 'error');
    } finally {
      setResourceLoading(false);
    }
  }, [
    debouncedSearch,
    group,
    groupFilter,
    isBatch,
    open,
    planFilter,
    resourcePage,
    showNotification,
  ]);

  useEffect(() => void loadResourcePage(), [loadResourcePage]);
  useEffect(() => setResourcePage(1), [debouncedSearch, groupFilter, planFilter]);
  useEffect(() => {
    if (planFilter === 'all') setSelectCurrentPlan(false);
  }, [planFilter]);

  const visibleAuthIndices = useMemo(() => authFiles.map(authIndexOf).filter(Boolean), [authFiles]);
  const selectedAuthIndexSet = useMemo(() => new Set(selectedAuthIndices), [selectedAuthIndices]);
  const excludedAuthIndexSet = useMemo(() => new Set(excludedAuthIndices), [excludedAuthIndices]);
  const selectedGroupIDSet = useMemo(() => new Set(selectedGroupIDs), [selectedGroupIDs]);
  const selectedProviderKeySet = useMemo(
    () => new Set(selectedProviderKeys),
    [selectedProviderKeys]
  );
  const selectedProviderAuthIndices = useMemo(
    () =>
      uniqueAuthIndices(
        providerResources
          .filter((resource) => selectedProviderKeySet.has(resource.key))
          .flatMap((resource) => resource.authIndices)
      ),
    [providerResources, selectedProviderKeySet]
  );
  const selectedProviderAuthIndexSet = useMemo(
    () => new Set(selectedProviderAuthIndices),
    [selectedProviderAuthIndices]
  );
  const groupByID = useMemo(() => new Map(groups.map((item) => [item.id, item])), [groups]);
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
    const values = new Set(Object.keys(planFacets));
    for (const file of authFiles) {
      const planType = planTypeOf(file);
      if (planType) values.add(planType);
    }
    return Array.from(values).sort();
  }, [authFiles, planFacets]);
  const querySelectionState = useMemo(
    () => ({
      all: selectAllCredentials,
      selectCurrentPlan,
      planFilter,
      providers: [],
      includedAuthIndices: selectedProviderAuthIndices,
      excludedAuthIndices,
    }),
    [
      excludedAuthIndices,
      planFilter,
      selectAllCredentials,
      selectCurrentPlan,
      selectedProviderAuthIndices,
    ]
  );
  const querySelection = useMemo(
    () => buildCredentialBindingSelection(querySelectionState),
    [querySelectionState]
  );
  const querySelectionActive = isQueryCredentialSelectionActive(querySelectionState);
  const isQueryCandidate = (authIndex: string) =>
    selectAllCredentials ||
    selectedProviderAuthIndices.length === 0 ||
    selectedProviderAuthIndexSet.has(authIndex);
  const isAuthIndexSelected = (authIndex: string) =>
    querySelectionActive
      ? isQueryCandidate(authIndex) && !excludedAuthIndexSet.has(authIndex)
      : selectedAuthIndexSet.has(authIndex);

  const toggleAuthIndex = (authIndex: string) => {
    if (querySelectionActive) {
      if (!isQueryCandidate(authIndex)) return;
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

  const toggleVisibleAuthIndices = () => {
    const eligible = visibleAuthIndices.filter(isQueryCandidate);
    if (querySelectionActive) {
      setExcludedAuthIndices((current) => {
        const next = new Set(current);
        const allSelected =
          eligible.length > 0 && eligible.every((authIndex) => !next.has(authIndex));
        for (const authIndex of eligible) {
          if (allSelected) next.add(authIndex);
          else next.delete(authIndex);
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
        if (allSelected) next.delete(authIndex);
        else next.add(authIndex);
      }
      return Array.from(next);
    });
  };

  const toggleProviderResource = (resource: ProviderResource) => {
    if (querySelectionActive) {
      setSelectAllCredentials(false);
      setSelectedProviderKeys((current) =>
        current.includes(resource.key)
          ? current.filter((key) => key !== resource.key)
          : [...current, resource.key]
      );
      setSelectedAuthIndices([]);
      setExcludedAuthIndices([]);
      return;
    }
    const selected = resource.authIndices.every((authIndex) => selectedAuthIndexSet.has(authIndex));
    setStagedProviderKeys((current) =>
      selected
        ? current.filter((key) => key !== resource.key)
        : Array.from(new Set([...current, resource.key]))
    );
    setSelectedAuthIndices((current) => {
      const next = new Set(current);
      for (const authIndex of resource.authIndices) {
        if (selected) next.delete(authIndex);
        else next.add(authIndex);
      }
      return Array.from(next);
    });
  };

  const enableCurrentPlanSelection = (checked: boolean) => {
    if (checked) {
      setSelectedProviderKeys(stagedProviderKeys);
      setSelectedAuthIndices([]);
      setSelectAllCredentials(false);
      setExcludedAuthIndices([]);
    }
    setSelectCurrentPlan(checked);
  };

  const toggleGroup = (groupID: number) =>
    setSelectedGroupIDs((current) =>
      current.includes(groupID) ? current.filter((item) => item !== groupID) : [...current, groupID]
    );

  const bindingLabels = (authIndex: string) =>
    (bindingsByAuthIndex.get(authIndex) ?? [])
      .map((binding) => groupByID.get(binding.group_id)?.name ?? `#${binding.group_id}`)
      .join(', ');
  const isBoundToSelectedGroups = (authIndex: string) =>
    selectedGroupIDs.length > 0 &&
    (bindingsByAuthIndex.get(authIndex) ?? []).some((binding) =>
      selectedGroupIDSet.has(binding.group_id)
    );

  useEffect(() => {
    if (!open || !querySelection) {
      setSelectionPreview(null);
      setSelectionPreviewLoading(false);
      return;
    }
    let cancelled = false;
    setSelectionPreviewLoading(true);
    const timer = window.setTimeout(() => {
      const request = group
        ? clientAccessApi.replaceGroupCredentialBindingsBySelection(
            group.id,
            querySelection,
            Number.parseInt(bindingPriority || '0', 10) || 0,
            true
          )
        : clientAccessApi.bulkReplaceCredentialBindings(querySelection, [], true);
      void request
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
  }, [bindingPriority, group, open, querySelection]);

  const selectionScope = useMemo(() => {
    if (!querySelection) {
      return t('client_access.explicit_selection_scope', {
        count: selectedAuthIndices.length,
        defaultValue: `Explicitly selected ${selectedAuthIndices.length} credentials; selection is preserved across pages.`,
      });
    }
    const estimated = selectionPreviewLoading
      ? t('client_access.selection_preview_loading', { defaultValue: 'Calculating on the server…' })
      : selectionPreview
        ? t('client_access.selection_preview_result', {
            matched: selectionPreview.matched,
            excluded: selectionPreview.excluded,
            defaultValue: `Matches ${selectionPreview.matched}; excluded ${selectionPreview.excluded}.`,
          })
        : t('client_access.selection_preview_unavailable', {
            defaultValue: 'Preview unavailable.',
          });
    if (querySelection.all) {
      return `${t('client_access.all_credentials_scope', { defaultValue: 'All credentials across all pages.' })} ${estimated}`;
    }
    const parts: string[] = [];
    if (querySelection.plan_types.length > 0) parts.push(querySelection.plan_types.join(', '));
    if (selectedProviderKeys.length > 0) {
      parts.push(
        providerResources
          .filter((resource) => selectedProviderKeySet.has(resource.key))
          .map((resource) => resource.label)
          .join(', ')
      );
    }
    return `${t('client_access.query_selection_scope', {
      scope: parts.filter(Boolean).join(' × '),
      defaultValue: `Server-side selection: ${parts.filter(Boolean).join(' × ')}.`,
    })} ${estimated}`;
  }, [
    providerResources,
    querySelection,
    selectedAuthIndices.length,
    selectedProviderKeySet,
    selectedProviderKeys.length,
    selectionPreview,
    selectionPreviewLoading,
    t,
  ]);

  const save = async () => {
    if (!group && !isBatch) return;
    if (isBatch && !querySelection && selectedAuthIndices.length === 0) return;
    setResourceSaving(true);
    try {
      const priority = Number.parseInt(bindingPriority || '0', 10) || 0;
      if (group) {
        if (querySelection) {
          await clientAccessApi.replaceGroupCredentialBindingsBySelection(
            group.id,
            querySelection,
            priority
          );
        } else {
          await clientAccessApi.replaceGroupCredentialBindings(
            group.id,
            selectedAuthIndices,
            priority
          );
        }
      } else {
        const targetGroups = selectedGroupIDs.map((groupID) => ({ group_id: groupID, priority }));
        if (querySelection) {
          await clientAccessApi.bulkReplaceCredentialBindings(querySelection, targetGroups);
        } else {
          await clientAccessApi.replaceCredentialBindings(selectedAuthIndices, targetGroups);
        }
      }
      showNotification(t('client_access.bindings_saved'), 'success');
      onClose();
      await onSaved();
    } catch (saveError) {
      showNotification(saveError instanceof Error ? saveError.message : String(saveError), 'error');
    } finally {
      setResourceSaving(false);
    }
  };

  const totalPages = Math.max(1, Math.ceil(resourceTotal / pageSize));
  const eligibleVisibleAuthIndices = visibleAuthIndices.filter(isQueryCandidate);
  const allVisibleSelected =
    eligibleVisibleAuthIndices.length > 0 && eligibleVisibleAuthIndices.every(isAuthIndexSelected);

  return (
    <Modal
      open={open}
      onClose={onClose}
      width={1040}
      title={
        group
          ? t('client_access.configure_group_resources', { name: group.name })
          : t('client_access.assign_credentials')
      }
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button
            loading={resourceSaving}
            disabled={isBatch && !querySelection && selectedAuthIndices.length === 0}
            onClick={save}
          >
            {t('common.save')}
          </Button>
        </>
      }
    >
      <div className={styles.assignmentLayout}>
        <div className={styles.assignmentHero}>
          <div>
            <strong>
              {group
                ? t('client_access.single_group_assignment', {
                    name: group.name,
                    defaultValue: `Resource pool: ${group.name}`,
                  })
                : t('client_access.batch_assignment', {
                    defaultValue: 'Batch credential assignment',
                  })}
            </strong>
            <span>{t('client_access.group_resources_description')}</span>
          </div>
          <div className={styles.selectionCounter}>
            <b>
              {querySelection ? (selectionPreview?.matched ?? '…') : selectedAuthIndices.length}
            </b>
            <span>{t('client_access.credentials')}</span>
          </div>
        </div>

        <div className={styles.assignmentFilters}>
          <Input
            label={t('client_access.search_credentials')}
            value={resourceSearch}
            onChange={(event) => setResourceSearch(event.target.value)}
          />
          {isBatch ? (
            <label className={styles.selectField}>
              <span>{t('client_access.display_group', { defaultValue: 'Display group' })}</span>
              <select value={groupFilter} onChange={(event) => setGroupFilter(event.target.value)}>
                <option value="all">
                  {t('client_access.all_credentials', { defaultValue: 'All credentials' })}
                </option>
                {groups.map((item) => (
                  <option key={item.id} value={item.id}>
                    {item.name} ({item.credential_count})
                  </option>
                ))}
              </select>
            </label>
          ) : null}
          <label className={styles.selectField}>
            <span>
              {t('client_access.subscription_plan', { defaultValue: 'Subscription plan' })}
            </span>
            <select value={planFilter} onChange={(event) => setPlanFilter(event.target.value)}>
              <option value="all">
                {t('client_access.all_plans', { defaultValue: 'All plans' })}
              </option>
              {planOptions.map((planType) => (
                <option key={planType} value={planType}>
                  {planType} ({planFacets[planType] ?? 0})
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
        </div>

        <section className={styles.assignmentSection}>
          <div className={styles.sectionHeader}>
            <div>
              <strong>
                {t('client_access.cross_page_selection', { defaultValue: 'Cross-page selection' })}
              </strong>
              <span>{selectionScope}</span>
            </div>
          </div>
          <div className={styles.selectionCards}>
            <label
              className={`${styles.selectionCard} ${selectAllCredentials ? styles.selectionCardActive : ''}`}
            >
              <input
                type="checkbox"
                checked={selectAllCredentials}
                onChange={(event) => {
                  const checked = event.target.checked;
                  setSelectAllCredentials(checked);
                  setSelectCurrentPlan(false);
                  setSelectedProviderKeys([]);
                  setStagedProviderKeys([]);
                  setSelectedAuthIndices([]);
                  setExcludedAuthIndices([]);
                }}
              />
              <span>
                <b>
                  {t('client_access.select_all_credentials', {
                    defaultValue: 'Select all credentials',
                  })}
                </b>
                <small>
                  {t('client_access.select_all_credentials_hint', {
                    defaultValue: 'Includes every server page.',
                  })}
                </small>
              </span>
            </label>
            <label
              className={`${styles.selectionCard} ${selectCurrentPlan ? styles.selectionCardActive : ''}`}
            >
              <input
                type="checkbox"
                checked={selectCurrentPlan}
                disabled={selectAllCredentials || planFilter === 'all'}
                onChange={(event) => enableCurrentPlanSelection(event.target.checked)}
              />
              <span>
                <b>
                  {t('client_access.select_current_plan', {
                    defaultValue: 'Select current subscription plan',
                  })}
                </b>
                <small>
                  {planFilter === 'all'
                    ? t('client_access.choose_plan_first', { defaultValue: 'Choose a plan first.' })
                    : planFilter}
                </small>
              </span>
            </label>
          </div>
        </section>

        <section className={styles.assignmentSection}>
          <div className={styles.sectionHeader}>
            <div>
              <strong>{t('client_access.ai_providers')}</strong>
              <span>
                {t('client_access.provider_filter_hint', {
                  defaultValue: 'Can be combined with the current subscription plan.',
                })}
              </span>
            </div>
          </div>
          {catalogLoading ? (
            <div className={styles.loading}>{t('common.loading')}</div>
          ) : providerResources.length === 0 ? (
            <div className={styles.muted}>{t('client_access.no_available_providers')}</div>
          ) : (
            <div className={styles.providerGrid}>
              {providerResources.map((resource) => {
                const explicitlySelected = resource.authIndices.filter((authIndex) =>
                  selectedAuthIndexSet.has(authIndex)
                ).length;
                const selected = querySelectionActive
                  ? selectedProviderKeySet.has(resource.key)
                  : explicitlySelected === resource.authIndices.length;
                return (
                  <label
                    className={`${styles.providerCard} ${selected ? styles.providerCardActive : ''}`}
                    key={resource.key}
                  >
                    <input
                      type="checkbox"
                      checked={selected}
                      onChange={() => toggleProviderResource(resource)}
                    />
                    <span>
                      <b>{resource.label}</b>
                      <small>
                        {querySelectionActive && selected
                          ? t('client_access.provider_filter_active', {
                              defaultValue: 'Active filter',
                            })
                          : `${explicitlySelected}/${resource.authIndices.length} ${t('client_access.credentials')}`}
                      </small>
                    </span>
                  </label>
                );
              })}
            </div>
          )}
        </section>

        {isBatch ? (
          <section className={styles.assignmentSection}>
            <div className={styles.sectionHeader}>
              <div>
                <strong>{t('client_access.groups')}</strong>
                <span>
                  {t('client_access.batch_groups_hint', {
                    defaultValue: 'Selected credentials will use this exact group set.',
                  })}
                </span>
              </div>
            </div>
            <div className={styles.groupPicker}>
              {groups.map((item) => (
                <label className={styles.groupOption} key={item.id}>
                  <input
                    type="checkbox"
                    checked={selectedGroupIDs.includes(item.id)}
                    onChange={() => toggleGroup(item.id)}
                  />
                  <span>{item.name}</span>
                  <small>{item.credential_count}</small>
                </label>
              ))}
            </div>
          </section>
        ) : null}

        <section className={styles.assignmentSection}>
          <div className={styles.sectionHeader}>
            <div>
              <strong>
                {t('client_access.credentials')}
                {querySelectionActive
                  ? ` · ${t('client_access.uncheck_to_exclude', { defaultValue: 'Uncheck to exclude' })}`
                  : ''}
              </strong>
              <span>
                {t('client_access.page_result_count', {
                  count: resourceTotal,
                  defaultValue: `${resourceTotal} results`,
                })}
              </span>
            </div>
            <Button
              size="xs"
              variant="secondary"
              disabled={eligibleVisibleAuthIndices.length === 0 || resourceLoading}
              onClick={toggleVisibleAuthIndices}
            >
              {allVisibleSelected
                ? t('client_access.unselect_current_page', { defaultValue: 'Unselect page' })
                : t('client_access.select_current_page', { defaultValue: 'Select page' })}
            </Button>
          </div>
          {resourceLoading ? (
            <div className={styles.loading}>{t('common.loading')}</div>
          ) : authFiles.length === 0 ? (
            <div className={styles.empty}>{t('client_access.no_credentials')}</div>
          ) : (
            <div className={styles.credentialPicker}>
              {authFiles.map((file) => {
                const authIndex = authIndexOf(file);
                const planType = planTypeOf(file);
                const alreadyBound = isBatch && isBoundToSelectedGroups(authIndex);
                const queryCandidate = isQueryCandidate(authIndex);
                return (
                  <label
                    className={`${styles.credentialOption} ${isAuthIndexSelected(authIndex) ? styles.credentialBound : ''} ${!queryCandidate ? styles.credentialUnavailable : ''}`}
                    key={`${file.name}-${authIndex}`}
                  >
                    <input
                      type="checkbox"
                      checked={isAuthIndexSelected(authIndex)}
                      disabled={querySelectionActive && !queryCandidate}
                      onChange={() => toggleAuthIndex(authIndex)}
                    />
                    <span>
                      <b>{file.name}</b>
                      <small>
                        {file.provider ?? file.type ?? '-'} ·{' '}
                        {planType || t('common.unknown', { defaultValue: 'Unknown' })}
                      </small>
                      <small title={authIndex}>{authIndex}</small>
                      {isBatch ? (
                        <small>
                          {bindingLabels(authIndex) || t('client_access.ungrouped')}
                          {alreadyBound
                            ? ` · ${t('client_access.already_in_selected_group', { defaultValue: 'Already assigned' })}`
                            : ''}
                        </small>
                      ) : null}
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
              disabled={resourcePage <= 1 || resourceLoading}
              onClick={() => setResourcePage((value) => value - 1)}
            >
              {t('common.previous')}
            </Button>
            <span>
              {resourcePage} / {totalPages}
            </span>
            <Button
              size="xs"
              variant="secondary"
              disabled={resourcePage >= totalPages || resourceLoading}
              onClick={() => setResourcePage((value) => value + 1)}
            >
              {t('common.next')}
            </Button>
          </div>
        </section>
      </div>
    </Modal>
  );
}
