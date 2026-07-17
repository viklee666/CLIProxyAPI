import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { authFilesApi, clientAccessApi, providersApi, type ClientGroup } from '@/services/api';
import { useNotificationStore } from '@/stores';
import type { AuthFileItem } from '@/types';
import { resolveCodexPlanType } from '@/utils/quota';
import styles from './ClientAccessPage.module.scss';
import {
  buildProviderResources,
  type ProviderLabels,
  type ProviderResource,
} from './providerResources';

type GroupResourceAssignmentsModalProps = {
  group: ClientGroup | null;
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
  open,
  onClose,
  onSaved,
}: GroupResourceAssignmentsModalProps) {
  const { t } = useTranslation();
  const showNotification = useNotificationStore((state) => state.showNotification);
  const [authFiles, setAuthFiles] = useState<AuthFileItem[]>([]);
  const [providerResources, setProviderResources] = useState<ProviderResource[]>([]);
  const [selectedAuthIndices, setSelectedAuthIndices] = useState<string[]>([]);
  const [bindingPriority, setBindingPriority] = useState('0');
  const [resourceSearch, setResourceSearch] = useState('');
  const [resourceLoading, setResourceLoading] = useState(false);
  const [resourceSaving, setResourceSaving] = useState(false);

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

  const loadResources = useCallback(async () => {
    if (!open || !group) return;
    setResourceLoading(true);
    try {
      const [filesResponse, bindings, gemini, interactions, codex, claude, vertex, openAI] =
        await Promise.all([
          authFilesApi.listSummary(),
          clientAccessApi.listAllCredentialBindings([], [group.id]),
          providersApi.getGeminiKeys(),
          providersApi.getInteractionsKeys(),
          providersApi.getCodexConfigs(),
          providersApi.getClaudeConfigs(),
          providersApi.getVertexConfigs(),
          providersApi.getOpenAIProviders(),
        ]);
      setAuthFiles((filesResponse.files ?? []).filter((file) => Boolean(authIndexOf(file))));
      setSelectedAuthIndices(uniqueAuthIndices(bindings.map((binding) => binding.auth_index)));
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
      const priority = bindings[0]?.priority;
      setBindingPriority(
        typeof priority === 'number' && Number.isFinite(priority) ? String(priority) : '0'
      );
    } catch (loadError) {
      showNotification(loadError instanceof Error ? loadError.message : String(loadError), 'error');
    } finally {
      setResourceLoading(false);
    }
  }, [group, open, providerLabels, showNotification]);

  useEffect(() => {
    if (!open) return;
    setResourceSearch('');
    setAuthFiles([]);
    setProviderResources([]);
    setSelectedAuthIndices([]);
    setBindingPriority('0');
    void loadResources();
  }, [loadResources, open]);

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

  const toggleAuthIndex = (authIndex: string, checked: boolean) => {
    setSelectedAuthIndices((current) => {
      const next = new Set(current);
      if (checked) {
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

  const save = async () => {
    if (!group) return;
    setResourceSaving(true);
    try {
      const priority = Number.parseInt(bindingPriority || '0', 10) || 0;
      await clientAccessApi.replaceGroupCredentialBindings(group.id, selectedAuthIndices, priority);
      showNotification(t('client_access.bindings_saved'), 'success');
      onClose();
      await onSaved();
    } catch (saveError) {
      showNotification(saveError instanceof Error ? saveError.message : String(saveError), 'error');
    } finally {
      setResourceSaving(false);
    }
  };

  return (
    <Modal
      open={open}
      onClose={onClose}
      width={860}
      title={t('client_access.configure_group_resources', { name: group?.name ?? '' })}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button loading={resourceSaving} onClick={save}>
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
  );
}
