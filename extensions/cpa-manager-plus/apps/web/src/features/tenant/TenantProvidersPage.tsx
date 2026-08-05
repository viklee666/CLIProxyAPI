import { useCallback, useEffect, useMemo, useState } from 'react';
import { Button } from '@/components/ui/Button';
import { HeaderInputList } from '@/components/ui/HeaderInputList';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { ModelInputList } from '@/components/ui/ModelInputList';
import {
  createDiscoveredModelEntry,
  entriesToModels,
  modelsToEntries,
  type ModelEntry,
} from '@/components/ui/modelInputListUtils';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import {
  tenantProvidersApi,
  type TenantDiscoveredModel,
  type TenantProvider,
  type TenantProviderChannel,
  type TenantProviderInput,
} from '@/services/api';
import { useNotificationStore } from '@/stores';
import type { ModelAlias } from '@/types';
import { buildHeaderObject, headersToEntries, type HeaderEntry } from '@/utils/headers';
import styles from './TenantPanel.module.scss';

type ProviderExtra = Record<string, unknown>;

type ProviderForm = {
  channel: TenantProviderChannel;
  name: string;
  baseURL: string;
  apiKey: string;
  proxyURL: string;
  priority: string;
  prefix: string;
  disabled: boolean;
  headers: HeaderEntry[];
  modelEntries: ModelEntry[];
  excludedModels: string;
  disableCooling: boolean;
  websockets: boolean;
  rebuildMidSystemMessage: boolean;
  experimentalCchSigning: boolean;
  cloakEnabled: boolean;
  cloakMode: string;
  cloakStrictMode: boolean;
  cloakSensitiveWords: string;
  cloakCacheUserID: boolean;
  weight: string;
  extra: ProviderExtra;
};

const emptyForm = (): ProviderForm => ({
  channel: 'openai-compat',
  name: '',
  baseURL: '',
  apiKey: '',
  proxyURL: '',
  priority: '0',
  prefix: '',
  disabled: false,
  headers: [],
  modelEntries: [{ name: '', alias: '' }],
  excludedModels: '',
  disableCooling: false,
  websockets: false,
  rebuildMidSystemMessage: false,
  experimentalCchSigning: false,
  cloakEnabled: false,
  cloakMode: 'auto',
  cloakStrictMode: false,
  cloakSensitiveWords: '',
  cloakCacheUserID: false,
  weight: '',
  extra: {},
});

const asRecord = (value: unknown): ProviderExtra =>
  value && typeof value === 'object' && !Array.isArray(value)
    ? { ...(value as Record<string, unknown>) }
    : {};

const readExtra = (extra: ProviderExtra, ...keys: string[]) => {
  for (const key of keys) {
    if (key in extra) return extra[key];
  }
  return undefined;
};

const stringList = (value: unknown) =>
  Array.isArray(value)
    ? value.map((item) => String(item ?? '').trim()).filter(Boolean)
    : [];

const textList = (value: string) =>
  value
    .split(/[\n,]+/)
    .map((item) => item.trim())
    .filter(Boolean);

const booleanExtra = (extra: ProviderExtra, ...keys: string[]) => {
  const value = readExtra(extra, ...keys);
  return value === true || value === 'true';
};

const numberText = (value: unknown) =>
  typeof value === 'number' && Number.isFinite(value) ? String(Math.trunc(value)) : '';

const deserializeModels = (value: unknown): ModelEntry[] => {
  if (!Array.isArray(value)) return modelsToEntries([]);
  const models: ModelAlias[] = value.flatMap((item) => {
    const raw = asRecord(item);
    const name = String(raw.name ?? '').trim();
    if (!name) return [];
    const thinking = asRecord(raw.thinking);
    const inputModalities = raw['input-modalities'] ?? raw.inputModalities;
    const outputModalities = raw['output-modalities'] ?? raw.outputModalities;
    return [
      {
        name,
        alias: String(raw.alias ?? '').trim() || undefined,
        image: typeof raw.image === 'boolean' ? raw.image : undefined,
        forceMapping:
          typeof raw['force-mapping'] === 'boolean'
            ? raw['force-mapping']
            : typeof raw.forceMapping === 'boolean'
            ? raw.forceMapping
              : undefined,
        ...(inputModalities !== undefined ? { inputModalities: stringList(inputModalities) } : {}),
        ...(outputModalities !== undefined ? { outputModalities: stringList(outputModalities) } : {}),
        thinking: Object.keys(thinking).length ? thinking : undefined,
      },
    ];
  });
  return modelsToEntries(models);
};

const serializeModels = (entries: ModelEntry[]) => {
  const normalized = entries.map((entry) => {
    const thinkingDraft = String(entry.thinkingDraft ?? '').trim();
    if (!thinkingDraft) return { ...entry, thinking: undefined };
    let thinking: unknown;
    try {
      thinking = JSON.parse(thinkingDraft);
    } catch {
      throw new Error(`模型 ${entry.name || '配置'} 的 Thinking 必须是 JSON 对象`);
    }
    if (!thinking || typeof thinking !== 'object' || Array.isArray(thinking)) {
      throw new Error(`模型 ${entry.name || '配置'} 的 Thinking 必须是 JSON 对象`);
    }
    return { ...entry, thinking: thinking as Record<string, unknown> };
  });
  return entriesToModels(normalized).map((model) => {
    const payload: Record<string, unknown> = { name: model.name };
    if (model.alias && model.alias !== model.name) payload.alias = model.alias;
    if (model.image !== undefined) payload.image = model.image;
    if (model.forceMapping !== undefined) payload['force-mapping'] = model.forceMapping;
    if (model.inputModalities !== undefined) payload['input-modalities'] = model.inputModalities;
    if (model.outputModalities !== undefined) payload['output-modalities'] = model.outputModalities;
    if (model.thinking) payload.thinking = model.thinking;
    return payload;
  });
};

const withoutKnownExtra = (extra: ProviderExtra) => {
  const next = { ...extra };
  for (const key of [
    'excluded_models',
    'excluded-models',
    'disable_cooling',
    'disable-cooling',
    'websockets',
    'rebuild_mid_system_message',
    'rebuild-mid-system-message',
    'cloak',
    'experimental_cch_signing',
    'experimental-cch-signing',
    'weight',
  ]) {
    delete next[key];
  }
  return next;
};

const serializeExtra = (form: ProviderForm): ProviderExtra => {
  const extra = withoutKnownExtra(form.extra);
  const excludedModels = textList(form.excludedModels);
  if (excludedModels.length) extra.excluded_models = excludedModels;
  if (form.channel !== 'vertex' && form.disableCooling) extra.disable_cooling = true;
  if ((form.channel === 'codex' || form.channel === 'xai') && form.websockets) extra.websockets = true;
  if (form.channel === 'claude') {
    if (form.rebuildMidSystemMessage) extra.rebuild_mid_system_message = true;
    if (form.experimentalCchSigning) extra.experimental_cch_signing = true;
    if (form.cloakEnabled) {
      const sensitiveWords = textList(form.cloakSensitiveWords);
      extra.cloak = {
        mode: form.cloakMode.trim() || 'auto',
        'strict-mode': form.cloakStrictMode,
        ...(sensitiveWords.length ? { 'sensitive-words': sensitiveWords } : {}),
        'cache-user-id': form.cloakCacheUserID,
      };
    }
  }
  if (form.channel === 'vertex' && form.weight.trim()) {
    const weight = Number.parseInt(form.weight, 10);
    if (!Number.isFinite(weight)) throw new Error('权重必须是整数');
    extra.weight = weight;
  }
  return extra;
};

const providerInput = (form: ProviderForm): Omit<TenantProviderInput, 'channel'> => ({
  name: form.name.trim(),
  base_url: form.baseURL.trim(),
  ...(form.apiKey.trim() ? { api_key: form.apiKey.trim() } : {}),
  proxy_url: form.proxyURL.trim(),
  priority: Number.parseInt(form.priority || '0', 10) || 0,
  prefix: form.prefix.trim(),
  disabled: form.disabled,
  headers: buildHeaderObject(form.headers),
  models: serializeModels(form.modelEntries),
  extra: serializeExtra(form),
});

const providerCreateInput = (form: ProviderForm): TenantProviderInput => ({
  channel: form.channel,
  ...providerInput(form),
});

const formForProvider = (provider: TenantProvider): ProviderForm => {
  const extra = asRecord(provider.extra);
  const cloak = asRecord(readExtra(extra, 'cloak'));
  return {
    channel: provider.channel,
    name: provider.name,
    baseURL: provider.base_url,
    apiKey: '',
    proxyURL: provider.proxy_url,
    priority: String(provider.priority),
    prefix: provider.prefix ?? '',
    disabled: provider.disabled,
    headers: headersToEntries(provider.headers),
    modelEntries: deserializeModels(provider.models),
    excludedModels: stringList(readExtra(extra, 'excluded_models', 'excluded-models')).join('\n'),
    disableCooling: booleanExtra(extra, 'disable_cooling', 'disable-cooling'),
    websockets: booleanExtra(extra, 'websockets'),
    rebuildMidSystemMessage: booleanExtra(
      extra,
      'rebuild_mid_system_message',
      'rebuild-mid-system-message'
    ),
    experimentalCchSigning: booleanExtra(
      extra,
      'experimental_cch_signing',
      'experimental-cch-signing'
    ),
    cloakEnabled: Object.keys(cloak).length > 0,
    cloakMode: String(cloak.mode ?? 'auto'),
    cloakStrictMode: cloak['strict-mode'] === true || cloak.strictMode === true,
    cloakSensitiveWords: stringList(cloak['sensitive-words'] ?? cloak.sensitiveWords).join('\n'),
    cloakCacheUserID: cloak['cache-user-id'] === true || cloak.cacheUserId === true,
    weight: numberText(readExtra(extra, 'weight')),
    extra,
  };
};

const modelKey = (name: string) => name.trim().toLowerCase();

export function TenantProvidersPage() {
  const showNotification = useNotificationStore((state) => state.showNotification);
  const showConfirmation = useNotificationStore((state) => state.showConfirmation);
  const [providers, setProviders] = useState<TenantProvider[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [editing, setEditing] = useState<TenantProvider | null>(null);
  const [form, setForm] = useState<ProviderForm>(emptyForm);
  const [modalOpen, setModalOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [testResult, setTestResult] = useState<{ title: string; body: string } | null>(null);
  const [discovering, setDiscovering] = useState(false);
  const [discoveredModels, setDiscoveredModels] = useState<TenantDiscoveredModel[]>([]);
  const [discoverySearch, setDiscoverySearch] = useState('');
  const [selectedDiscoveredModels, setSelectedDiscoveredModels] = useState<Set<string>>(new Set());

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      setProviders(await tenantProvidersApi.list());
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : '加载提供商失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const resetDiscovery = () => {
    setDiscoveredModels([]);
    setDiscoverySearch('');
    setSelectedDiscoveredModels(new Set());
  };

  const closeEditor = () => {
    setModalOpen(false);
    resetDiscovery();
  };

  const openCreate = () => {
    setEditing(null);
    setForm(emptyForm());
    resetDiscovery();
    setModalOpen(true);
  };

  const openEdit = (provider: TenantProvider) => {
    setEditing(provider);
    setForm(formForProvider(provider));
    resetDiscovery();
    setModalOpen(true);
  };

  const save = async () => {
    if (!form.name.trim()) {
      showNotification('请填写提供商名称', 'error');
      return;
    }
    if (!editing && !form.apiKey.trim()) {
      showNotification('请填写 API Key', 'error');
      return;
    }
    setSaving(true);
    try {
      if (editing) {
        await tenantProvidersApi.update(editing.id, providerInput(form));
      } else {
        await tenantProvidersApi.create(providerCreateInput(form));
      }
      closeEditor();
      showNotification('提供商已保存', 'success');
      await load();
    } catch (saveError) {
      showNotification(saveError instanceof Error ? saveError.message : '保存失败', 'error');
    } finally {
      setSaving(false);
    }
  };

  const remove = (provider: TenantProvider) => {
    showConfirmation({
      title: '删除提供商',
      message: `确认删除 ${provider.name}？`,
      variant: 'danger',
      onConfirm: async () => {
        await tenantProvidersApi.delete(provider.id);
        showNotification('提供商已删除', 'success');
        await load();
      },
    });
  };

  const test = async (provider: TenantProvider) => {
    try {
      const result = await tenantProvidersApi.test(provider.id);
      setTestResult({
        title: `${provider.name} 测试结果 (${result.status_code})`,
        body: result.body || '请求完成且无响应正文',
      });
    } catch (testError) {
      showNotification(testError instanceof Error ? testError.message : '测试失败', 'error');
    }
  };

  const discover = async () => {
    if (!editing) {
      showNotification('请先保存提供商，再获取上游模型', 'error');
      return;
    }
    setDiscovering(true);
    try {
      const result = await tenantProvidersApi.models(editing.id);
      const existing = new Set(form.modelEntries.map((entry) => modelKey(entry.name)));
      setDiscoveredModels(result.models);
      setSelectedDiscoveredModels(
        new Set(result.models.filter((item) => existing.has(modelKey(item.name))).map((item) => modelKey(item.name)))
      );
    } catch (discoverError) {
      showNotification(discoverError instanceof Error ? discoverError.message : '获取模型失败', 'error');
    } finally {
      setDiscovering(false);
    }
  };

  const filteredDiscoveredModels = useMemo(() => {
    const query = discoverySearch.trim().toLowerCase();
    if (!query) return discoveredModels;
    return discoveredModels.filter((model) =>
      [model.name, model.alias, model.description].some((value) =>
        String(value ?? '').toLowerCase().includes(query)
      )
    );
  }, [discoveredModels, discoverySearch]);

  const addDiscoveredModels = () => {
    const existing = new Set(form.modelEntries.map((entry) => modelKey(entry.name)));
    const additions = discoveredModels
      .filter((model) => selectedDiscoveredModels.has(modelKey(model.name)))
      .filter((model) => !existing.has(modelKey(model.name)))
      .map((model) => ({ ...createDiscoveredModelEntry(model.name), alias: model.alias ?? '' }));
    if (!additions.length) return;
    setForm((current) => ({
      ...current,
      modelEntries: [...current.modelEntries.filter((entry) => entry.name.trim()), ...additions],
    }));
    showNotification(`已加入 ${additions.length} 个模型`, 'success');
  };

  const showModalities = form.channel === 'openai-compat';
  const showImage = form.channel === 'openai-compat';
  const showThinking = form.channel === 'openai-compat' || form.channel === 'vertex';
  const supportsExcludedModels = form.channel !== 'openai-compat';
  const supportsDisableCooling = form.channel !== 'vertex';
  const supportsWebsockets = form.channel === 'codex' || form.channel === 'xai';
  const isClaude = form.channel === 'claude';

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <div>
          <h1>AI 提供商</h1>
          <p>这些上游仅可被当前租户的 API Key 使用。</p>
        </div>
        <Button onClick={openCreate}>添加提供商</Button>
      </header>
      <div className={styles.tableWrap}>
        {loading ? <div className={styles.loading}>正在加载...</div> : null}
        {error ? <div className={styles.error}>{error}</div> : null}
        {!loading && !error && providers.length === 0 ? (
          <div className={styles.empty}>尚未添加提供商。</div>
        ) : null}
        {!loading && !error && providers.length > 0 ? (
          <table className={styles.table}>
            <thead>
              <tr>
                <th>名称</th>
                <th>通道</th>
                <th>地址</th>
                <th>优先级</th>
                <th>状态</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {providers.map((provider) => (
                <tr key={provider.id}>
                  <td>
                    <strong>{provider.name}</strong>
                    <div className={styles.muted}>{provider.auth_index}</div>
                  </td>
                  <td>{provider.channel}</td>
                  <td>{provider.base_url || '默认地址'}</td>
                  <td>{provider.priority}</td>
                  <td>
                    <span className={`${styles.badge} ${provider.disabled ? styles.disabled : styles.enabled}`}>
                      {provider.disabled ? '已停用' : '可用'}
                    </span>
                  </td>
                  <td>
                    <div className={styles.actions}>
                      <Button size="xs" variant="secondary" onClick={() => void test(provider)}>
                        测试
                      </Button>
                      <Button size="xs" variant="secondary" onClick={() => openEdit(provider)}>
                        编辑
                      </Button>
                      <Button size="xs" variant="danger" onClick={() => remove(provider)}>
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
        onClose={closeEditor}
        title={editing ? '编辑提供商' : '添加提供商'}
        width={900}
        footer={
          <>
            <Button variant="secondary" onClick={closeEditor}>
              取消
            </Button>
            <Button loading={saving} onClick={() => void save()}>
              保存
            </Button>
          </>
        }
      >
        <div className={styles.modalGrid}>
          <label className={styles.toggleRow}>
            <span>通道</span>
            <select
              className="input"
              value={form.channel}
              disabled={Boolean(editing)}
              onChange={(event) =>
                setForm((current) => ({ ...current, channel: event.target.value as TenantProviderChannel }))
              }
            >
              <option value="openai-compat">OpenAI 兼容</option>
              <option value="claude">Claude</option>
              <option value="codex">Codex</option>
              <option value="gemini">Gemini</option>
              <option value="xai">xAI</option>
              <option value="vertex">Vertex</option>
            </select>
          </label>
          <Input
            label="名称"
            value={form.name}
            onChange={(event) => setForm((current) => ({ ...current, name: event.target.value }))}
          />
          <Input
            label="Base URL"
            value={form.baseURL}
            placeholder="留空使用通道默认地址"
            onChange={(event) => setForm((current) => ({ ...current, baseURL: event.target.value }))}
          />
          <Input
            label={editing ? '替换 API Key（留空不修改）' : 'API Key'}
            type="password"
            value={form.apiKey}
            autoComplete="off"
            onChange={(event) => setForm((current) => ({ ...current, apiKey: event.target.value }))}
          />
          <Input
            label="代理地址"
            value={form.proxyURL}
            onChange={(event) => setForm((current) => ({ ...current, proxyURL: event.target.value }))}
          />
          <Input
            label="优先级"
            type="number"
            value={form.priority}
            onChange={(event) => setForm((current) => ({ ...current, priority: event.target.value }))}
          />
          <Input
            label="模型前缀"
            value={form.prefix}
            placeholder="可选单段名称"
            onChange={(event) => setForm((current) => ({ ...current, prefix: event.target.value }))}
          />
          <div className={styles.fullWidth}>
            <ToggleSwitch
              checked={form.disabled}
              onChange={(disabled) => setForm((current) => ({ ...current, disabled }))}
              label="停用此提供商"
            />
          </div>
          <div className={styles.fullWidth}>
            <div className={styles.toggleRow}>
              <span>Headers</span>
              <HeaderInputList
                entries={form.headers}
                onChange={(headers) => setForm((current) => ({ ...current, headers }))}
                addLabel="添加 Header"
              />
            </div>
          </div>
          <div className={styles.fullWidth}>
            <div className={styles.toolbar}>
              <strong>模型</strong>
              <Button
                size="xs"
                variant="secondary"
                loading={discovering}
                disabled={!editing}
                onClick={() => void discover()}
              >
                获取上游模型
              </Button>
            </div>
            <ModelInputList
              entries={form.modelEntries}
              onChange={(modelEntries) => setForm((current) => ({ ...current, modelEntries }))}
              addLabel="添加模型"
              showForceMapping
              showModalities={showModalities}
              showImage={showImage}
              showThinking={showThinking}
              forceMappingLabel="改写响应模型"
              imageLabel="图像模型"
              thinkingPlaceholder="Thinking JSON"
            />
          </div>
          {discoveredModels.length > 0 ? (
            <div className={styles.fullWidth}>
              <div className={styles.toolbar}>
                <Input
                  label=""
                  value={discoverySearch}
                  placeholder="筛选上游模型"
                  onChange={(event) => setDiscoverySearch(event.target.value)}
                />
                <Button size="xs" variant="secondary" onClick={addDiscoveredModels}>
                  加入选中模型
                </Button>
              </div>
              <div className={styles.checkList}>
                {filteredDiscoveredModels.map((model) => {
                  const key = modelKey(model.name);
                  return (
                    <label key={key}>
                      <input
                        type="checkbox"
                        checked={selectedDiscoveredModels.has(key)}
                        onChange={() =>
                          setSelectedDiscoveredModels((current) => {
                            const next = new Set(current);
                            if (next.has(key)) next.delete(key);
                            else next.add(key);
                            return next;
                          })
                        }
                      />
                      <span>{model.name}</span>
                      {model.description ? <small>{model.description}</small> : null}
                    </label>
                  );
                })}
              </div>
            </div>
          ) : null}
          {supportsExcludedModels ? (
            <label className={styles.fullWidth}>
              <span>排除模型</span>
              <textarea
                className="input"
                rows={3}
                value={form.excludedModels}
                onChange={(event) => setForm((current) => ({ ...current, excludedModels: event.target.value }))}
              />
            </label>
          ) : null}
          {supportsDisableCooling ? (
            <div className={styles.fullWidth}>
              <ToggleSwitch
                checked={form.disableCooling}
                onChange={(disableCooling) => setForm((current) => ({ ...current, disableCooling }))}
                label="禁用冷却"
              />
            </div>
          ) : null}
          {supportsWebsockets ? (
            <div className={styles.fullWidth}>
              <ToggleSwitch
                checked={form.websockets}
                onChange={(websockets) => setForm((current) => ({ ...current, websockets }))}
                label="WebSocket"
              />
            </div>
          ) : null}
          {isClaude ? (
            <>
              <div className={styles.fullWidth}>
                <ToggleSwitch
                  checked={form.rebuildMidSystemMessage}
                  onChange={(rebuildMidSystemMessage) =>
                    setForm((current) => ({ ...current, rebuildMidSystemMessage }))
                  }
                  label="重建中间 System 消息"
                />
              </div>
              <div className={styles.fullWidth}>
                <ToggleSwitch
                  checked={form.experimentalCchSigning}
                  onChange={(experimentalCchSigning) =>
                    setForm((current) => ({ ...current, experimentalCchSigning }))
                  }
                  label="实验性 CCH 签名"
                />
              </div>
              <div className={styles.fullWidth}>
                <ToggleSwitch
                  checked={form.cloakEnabled}
                  onChange={(cloakEnabled) => setForm((current) => ({ ...current, cloakEnabled }))}
                  label="Cloak"
                />
              </div>
              {form.cloakEnabled ? (
                <>
                  <Input
                    label="Cloak 模式"
                    value={form.cloakMode}
                    onChange={(event) => setForm((current) => ({ ...current, cloakMode: event.target.value }))}
                  />
                  <div className={`${styles.fullWidth} ${styles.toggleStack}`}>
                    <ToggleSwitch
                      checked={form.cloakStrictMode}
                      onChange={(cloakStrictMode) => setForm((current) => ({ ...current, cloakStrictMode }))}
                      label="Cloak 严格模式"
                    />
                    <ToggleSwitch
                      checked={form.cloakCacheUserID}
                      onChange={(cloakCacheUserID) => setForm((current) => ({ ...current, cloakCacheUserID }))}
                      label="缓存 Cloak User ID"
                    />
                  </div>
                  <label className={styles.fullWidth}>
                    <span>Cloak 敏感词</span>
                    <textarea
                      className="input"
                      rows={3}
                      value={form.cloakSensitiveWords}
                      onChange={(event) =>
                        setForm((current) => ({ ...current, cloakSensitiveWords: event.target.value }))
                      }
                    />
                  </label>
                </>
              ) : null}
            </>
          ) : null}
          {form.channel === 'vertex' ? (
            <Input
              label="权重"
              type="number"
              value={form.weight}
              onChange={(event) => setForm((current) => ({ ...current, weight: event.target.value }))}
            />
          ) : null}
        </div>
      </Modal>
      <Modal
        open={testResult !== null}
        onClose={() => setTestResult(null)}
        title={testResult?.title}
        footer={<Button onClick={() => setTestResult(null)}>关闭</Button>}
      >
        <pre className={styles.codeBlock}>{testResult?.body}</pre>
      </Modal>
    </div>
  );
}
