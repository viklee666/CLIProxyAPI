import { useCallback, useEffect, useState } from 'react';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import {
  tenantProvidersApi,
  type TenantProvider,
  type TenantProviderChannel,
  type TenantProviderInput,
} from '@/services/api';
import { useNotificationStore } from '@/stores';
import styles from './TenantPanel.module.scss';

type ProviderForm = {
  channel: TenantProviderChannel;
  name: string;
  baseURL: string;
  apiKey: string;
  proxyURL: string;
  priority: string;
  disabled: boolean;
  headers: string;
  models: string;
  extra: string;
};

const emptyForm = (): ProviderForm => ({
  channel: 'openai-compat',
  name: '',
  baseURL: '',
  apiKey: '',
  proxyURL: '',
  priority: '0',
  disabled: false,
  headers: '{}',
  models: '[]',
  extra: '{}',
});

const stringified = (value: unknown, fallback: string) => {
  if (value === undefined || value === null) return fallback;
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return fallback;
  }
};

const parseObject = (value: string, name: string): Record<string, string> => {
  const parsed: unknown = JSON.parse(value || '{}');
  if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object') {
    throw new Error(`${name} 必须是 JSON 对象`);
  }
  return Object.fromEntries(
    Object.entries(parsed as Record<string, unknown>).map(([key, item]) => [key, String(item)])
  );
};

const parseJSON = (value: string, name: string) => {
  try {
    return JSON.parse(value);
  } catch {
    throw new Error(`${name} 必须是有效 JSON`);
  }
};

const providerInput = (form: ProviderForm): Omit<TenantProviderInput, 'channel'> => ({
  name: form.name.trim(),
  ...(form.baseURL.trim() ? { base_url: form.baseURL.trim() } : {}),
  ...(form.apiKey.trim() ? { api_key: form.apiKey.trim() } : {}),
  ...(form.proxyURL.trim() ? { proxy_url: form.proxyURL.trim() } : {}),
  priority: Number.parseInt(form.priority || '0', 10) || 0,
  disabled: form.disabled,
  headers: parseObject(form.headers, 'Headers'),
  models: parseJSON(form.models, 'Models'),
  extra: parseJSON(form.extra, 'Extra'),
});

const providerCreateInput = (form: ProviderForm): TenantProviderInput => ({
  channel: form.channel,
  ...providerInput(form),
});

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

  const openCreate = () => {
    setEditing(null);
    setForm(emptyForm());
    setModalOpen(true);
  };

  const openEdit = (provider: TenantProvider) => {
    setEditing(provider);
    setForm({
      channel: provider.channel,
      name: provider.name,
      baseURL: provider.base_url,
      apiKey: '',
      proxyURL: provider.proxy_url,
      priority: String(provider.priority),
      disabled: provider.disabled,
      headers: stringified(provider.headers, '{}'),
      models: stringified(provider.models, '[]'),
      extra: stringified(provider.extra, '{}'),
    });
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
      setModalOpen(false);
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
        onClose={() => setModalOpen(false)}
        title={editing ? '编辑提供商' : '添加提供商'}
        width={760}
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
          <div className={styles.fullWidth}>
            <ToggleSwitch
              checked={form.disabled}
              onChange={(disabled) => setForm((current) => ({ ...current, disabled }))}
              label="停用此提供商"
            />
          </div>
          <label className={styles.fullWidth}>
            <span>Headers（JSON 对象）</span>
            <textarea
              className="input"
              rows={4}
              value={form.headers}
              onChange={(event) => setForm((current) => ({ ...current, headers: event.target.value }))}
            />
          </label>
          <label className={styles.fullWidth}>
            <span>Models（JSON）</span>
            <textarea
              className="input"
              rows={4}
              value={form.models}
              onChange={(event) => setForm((current) => ({ ...current, models: event.target.value }))}
            />
          </label>
          <label className={styles.fullWidth}>
            <span>额外配置（JSON）</span>
            <textarea
              className="input"
              rows={4}
              value={form.extra}
              onChange={(event) => setForm((current) => ({ ...current, extra: event.target.value }))}
            />
          </label>
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
