import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import { useHeaderRefresh } from '@/hooks/useHeaderRefresh';
import { authFilesApi, clientAccessApi, providersApi, type ClientGroup } from '@/services/api';
import type { AuthFileItem } from '@/types';
import { useNotificationStore } from '@/stores';
import { resolveCodexPlanType } from '@/utils/quota';
import styles from './ClientAccessPage.module.scss';

type GroupForm = { name: string; description: string; enabled: boolean };
type ProviderResource = { key: string; label: string; authIndices: string[] };
type ProviderLabels = {
  defaultEndpoint: string;
  gemini: string;
  interactions: string;
  codex: string;
  claude: string;
  vertex: string;
  openAICompatible: string;
};
type ProviderConfigWithAuthIndex = { baseUrl?: string; authIndex?: string };

const emptyForm: GroupForm = { name: '', description: '', enabled: true };

const authIndexOf = (file: AuthFileItem) => String(file.authIndex ?? file.auth_index ?? '').trim();
const planTypeOf = (file: AuthFileItem) => resolveCodexPlanType(file) ?? '';

const uniqueAuthIndices = (values: Array<string | null | undefined>) =>
  Array.from(new Set(values.map((value) => String(value ?? '').trim()).filter(Boolean)));

const providerDomain = (baseUrl: string | undefined, defaultEndpoint: string) => {
  const value = String(baseUrl ?? '').trim();
  if (!value) return defaultEndpoint;
  try {
    return new URL(value).host || value;
  } catch {
    return value.replace(/^https?:\/\//i, '').split('/')[0] || value;
  }
};

const buildProviderResources = (
  fixedProviders: Array<{
    type: keyof Omit<ProviderLabels, 'defaultEndpoint' | 'openAICompatible'>;
    configs: ProviderConfigWithAuthIndex[];
  }>,
  openAIProviders: Array<{
    name: string;
    authIndex?: string;
    apiKeyEntries?: Array<{ authIndex?: string }>;
  }>,
  labels: ProviderLabels
) => {
  const resources = new Map<string, ProviderResource>();
  const add = (key: string, label: string, authIndices: Array<string | null | undefined>) => {
    const indices = uniqueAuthIndices(authIndices);
    if (indices.length === 0) return;
    const current = resources.get(key);
    if (current) {
      current.authIndices = uniqueAuthIndices([...current.authIndices, ...indices]);
      return;
    }
    resources.set(key, { key, label, authIndices: indices });
  };

  for (const provider of fixedProviders) {
    for (const config of provider.configs) {
      const domain = providerDomain(config.baseUrl, labels.defaultEndpoint);
      add(`${provider.type}\u0000${domain.toLowerCase()}`, `${labels[provider.type]} · ${domain}`, [
        config.authIndex,
      ]);
    }
  }
  for (const provider of openAIProviders) {
    const name = String(provider.name ?? '').trim();
    if (!name) continue;
    add(`openai-compatibility\u0000${name.toLowerCase()}`, `${labels.openAICompatible} · ${name}`, [
      provider.authIndex,
      ...(provider.apiKeyEntries ?? []).map((entry) => entry.authIndex),
    ]);
  }
  return Array.from(resources.values()).sort((left, right) =>
    left.label.localeCompare(right.label)
  );
};

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
  const [resourceGroup, setResourceGroup] = useState<ClientGroup | null>(null);
  const [resourceModalOpen, setResourceModalOpen] = useState(false);
  const [authFiles, setAuthFiles] = useState<AuthFileItem[]>([]);
  const [providerResources, setProviderResources] = useState<ProviderResource[]>([]);
  const [selectedAuthIndices, setSelectedAuthIndices] = useState<string[]>([]);
  const [bindingPriority, setBindingPriority] = useState('0');
  const [resourceSearch, setResourceSearch] = useState('');
  const [resourceLoading, setResourceLoading] = useState(false);
  const [resourceSaving, setResourceSaving] = useState(false);
  const pageSize = 20;

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

  const openResourceBindings = (group: ClientGroup) => {
    setResourceGroup(group);
    setResourceSearch('');
    setBindingPriority('0');
    setSelectedAuthIndices([]);
    setAuthFiles([]);
    setProviderResources([]);
    setResourceModalOpen(true);
  };

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

  const loadResourceBindings = useCallback(async () => {
    if (!resourceModalOpen || !resourceGroup) return;
    setResourceLoading(true);
    try {
      const [filesResponse, bindings, gemini, interactions, codex, claude, vertex, openAI] =
        await Promise.all([
          authFilesApi.listSummary(),
          clientAccessApi.listAllCredentialBindings([], [resourceGroup.id]),
          providersApi.getGeminiKeys(),
          providersApi.getInteractionsKeys(),
          providersApi.getCodexConfigs(),
          providersApi.getClaudeConfigs(),
          providersApi.getVertexConfigs(),
          providersApi.getOpenAIProviders(),
        ]);
      setAuthFiles((filesResponse.files ?? []).filter((file) => Boolean(authIndexOf(file))));
      setSelectedAuthIndices(uniqueAuthIndices(bindings.map((binding) => binding.auth_index)));
      const resources = buildProviderResources(
        [
          { type: 'gemini', configs: gemini },
          { type: 'interactions', configs: interactions },
          { type: 'codex', configs: codex },
          { type: 'claude', configs: claude },
          { type: 'vertex', configs: vertex },
        ],
        openAI,
        providerLabels
      );
      setProviderResources(resources);
      const priority = bindings[0]?.priority;
      setBindingPriority(
        typeof priority === 'number' && Number.isFinite(priority) ? String(priority) : '0'
      );
    } catch (loadError) {
      showNotification(loadError instanceof Error ? loadError.message : String(loadError), 'error');
    } finally {
      setResourceLoading(false);
    }
  }, [providerLabels, resourceGroup, resourceModalOpen, showNotification]);

  useEffect(() => void loadResourceBindings(), [loadResourceBindings]);

  const selectedAuthIndexSet = useMemo(() => new Set(selectedAuthIndices), [selectedAuthIndices]);
  const filteredAuthFiles = useMemo(() => {
    const query = resourceSearch.trim().toLowerCase();
    if (!query) return authFiles;
    return authFiles.filter((file) =>
      [file.name, file.provider, file.type, authIndexOf(file), planTypeOf(file)]
        .map((value) => String(value ?? '').toLowerCase())
        .some((value) => value.includes(query))
    );
  }, [authFiles, resourceSearch]);

  const toggleAuthIndex = (authIndex: string, checked?: boolean) => {
    setSelectedAuthIndices((current) => {
      const next = new Set(current);
      if (checked ?? !next.has(authIndex)) {
        next.add(authIndex);
      } else {
        next.delete(authIndex);
      }
      return Array.from(next);
    });
  };

  const toggleProviderResource = (resource: ProviderResource) => {
    const selected = resource.authIndices.every((authIndex) => selectedAuthIndexSet.has(authIndex));
    setSelectedAuthIndices((current) => {
      const next = new Set(current);
      for (const authIndex of resource.authIndices) {
        if (selected) {
          next.delete(authIndex);
        } else {
          next.add(authIndex);
        }
      }
      return Array.from(next);
    });
  };

  const saveResourceBindings = async () => {
    if (!resourceGroup) return;
    setResourceSaving(true);
    try {
      const priority = Number.parseInt(bindingPriority || '0', 10) || 0;
      await clientAccessApi.replaceGroupCredentialBindings(
        resourceGroup.id,
        selectedAuthIndices,
        priority
      );
      showNotification(t('client_access.bindings_saved'), 'success');
      setResourceModalOpen(false);
      await load();
    } catch (saveError) {
      showNotification(saveError instanceof Error ? saveError.message : String(saveError), 'error');
    } finally {
      setResourceSaving(false);
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
                      <Button
                        size="xs"
                        variant="secondary"
                        onClick={() => openResourceBindings(group)}
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
        open={resourceModalOpen}
        onClose={() => setResourceModalOpen(false)}
        width={860}
        title={t('client_access.configure_group_resources', { name: resourceGroup?.name ?? '' })}
        footer={
          <>
            <Button variant="secondary" onClick={() => setResourceModalOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button loading={resourceSaving} onClick={saveResourceBindings}>
              {t('common.save')}
            </Button>
          </>
        }
      >
        <div className={styles.modalGrid}>
          <div className={styles.fullWidth}>
            <div className={styles.muted}>{t('client_access.group_resources_description')}</div>
          </div>
          <Input
            label={t('client_access.search_credentials')}
            value={resourceSearch}
            onChange={(event) => setResourceSearch(event.target.value)}
          />
          <Input
            label={t('common.priority')}
            type="number"
            value={bindingPriority}
            onChange={(event) => setBindingPriority(event.target.value)}
          />
          <div className={styles.fullWidth}>
            <strong>{t('client_access.ai_providers')}</strong>
            {resourceLoading ? (
              <div className={styles.loading}>{t('common.loading')}</div>
            ) : providerResources.length === 0 ? (
              <div className={styles.muted}>{t('client_access.no_available_providers')}</div>
            ) : (
              <div className={styles.groupPicker}>
                {providerResources.map((resource) => {
                  const selected = resource.authIndices.every((authIndex) =>
                    selectedAuthIndexSet.has(authIndex)
                  );
                  return (
                    <label className={styles.groupOption} key={resource.key}>
                      <input
                        type="checkbox"
                        checked={selected}
                        onChange={() => toggleProviderResource(resource)}
                      />
                      {resource.label} ({resource.authIndices.length})
                    </label>
                  );
                })}
              </div>
            )}
          </div>
          <div className={styles.fullWidth}>
            <div className={styles.sectionHeader}>
              <strong>{t('client_access.credentials')}</strong>
              <span className={styles.muted}>
                {t('client_access.selected_credentials', { count: selectedAuthIndices.length })}
              </span>
            </div>
            {resourceLoading ? (
              <div className={styles.loading}>{t('common.loading')}</div>
            ) : filteredAuthFiles.length === 0 ? (
              <div className={styles.empty}>{t('client_access.no_credentials')}</div>
            ) : (
              <div className={styles.credentialPicker}>
                {filteredAuthFiles.map((file) => {
                  const authIndex = authIndexOf(file);
                  const planType = planTypeOf(file);
                  return (
                    <label
                      className={`${styles.credentialOption} ${selectedAuthIndexSet.has(authIndex) ? styles.credentialBound : ''}`}
                      key={`${file.name}-${authIndex}`}
                    >
                      <input
                        type="checkbox"
                        checked={selectedAuthIndexSet.has(authIndex)}
                        onChange={(event) => toggleAuthIndex(authIndex, event.target.checked)}
                      />
                      <span>
                        <b>{file.name}</b>
                        <small>
                          {file.provider ?? file.type ?? '-'} · {planType || '-'} · {authIndex}
                        </small>
                      </span>
                    </label>
                  );
                })}
              </div>
            )}
          </div>
        </div>
      </Modal>
    </div>
  );
}
