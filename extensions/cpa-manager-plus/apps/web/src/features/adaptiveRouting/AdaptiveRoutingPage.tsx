import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import { useHeaderRefresh } from '@/hooks/useHeaderRefresh';
import {
  adaptiveRoutingApi,
  configApi,
  type AdaptiveRoutingConfig,
  type AdaptiveRoutingScore,
} from '@/services/api';
import { useNotificationStore } from '@/stores';
import styles from './AdaptiveRoutingPage.module.scss';

const defaultConfig: AdaptiveRoutingConfig = {
  'top-k': 3,
  'ewma-alpha': 0.2,
  'ttft-target-ms': 30000,
  weights: { priority: 4, load: 2, 'success-rate': 2, ttft: 1 },
  'sticky-escape': {
    enabled: true,
    'min-samples': 3,
    'error-rate-threshold': 0.5,
    'ttft-threshold-ms': 30000,
    'active-request-threshold': 0,
  },
};

const finiteNumber = (value: string, fallback = 0) => {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : fallback;
};

export function AdaptiveRoutingPage() {
  const { t } = useTranslation();
  const showNotification = useNotificationStore((state) => state.showNotification);
  const [config, setConfig] = useState<AdaptiveRoutingConfig>(defaultConfig);
  const [enabled, setEnabled] = useState(false);
  const [scores, setScores] = useState<AdaptiveRoutingScore[]>([]);
  const [provider, setProvider] = useState('');
  const [model, setModel] = useState('');
  const [appliedProvider, setAppliedProvider] = useState('');
  const [appliedModel, setAppliedModel] = useState('');
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const [loadedConfig, strategy, snapshot] = await Promise.all([
        adaptiveRoutingApi.getConfig(),
        configApi.getRoutingStrategy(),
        adaptiveRoutingApi.getScores(appliedProvider, appliedModel),
      ]);
      setConfig({
        ...defaultConfig,
        ...loadedConfig,
        weights: { ...defaultConfig.weights, ...(loadedConfig.weights ?? {}) },
        'sticky-escape': {
          ...defaultConfig['sticky-escape'],
          ...(loadedConfig['sticky-escape'] ?? {}),
        },
      });
      setEnabled(strategy === 'adaptive' && snapshot.enabled);
      setScores(snapshot.items ?? []);
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : String(loadError));
    } finally {
      setLoading(false);
    }
  }, [appliedModel, appliedProvider]);

  useEffect(() => void load(), [load]);
  useHeaderRefresh(load);

  const save = async () => {
    setSaving(true);
    try {
      await adaptiveRoutingApi.updateConfig(config);
      showNotification(t('adaptive_routing.saved'), 'success');
      await load();
    } catch (saveError) {
      showNotification(saveError instanceof Error ? saveError.message : String(saveError), 'error');
    } finally {
      setSaving(false);
    }
  };

  const toggleEnabled = async () => {
    setSaving(true);
    try {
      await configApi.updateRoutingStrategy(enabled ? 'round-robin' : 'adaptive');
      showNotification(t(enabled ? 'adaptive_routing.disabled' : 'adaptive_routing.enabled'), 'success');
      await load();
    } catch (toggleError) {
      showNotification(toggleError instanceof Error ? toggleError.message : String(toggleError), 'error');
    } finally {
      setSaving(false);
    }
  };

  const setRootNumber = (key: 'top-k' | 'ewma-alpha' | 'ttft-target-ms', value: string) =>
    setConfig((current) => ({ ...current, [key]: finiteNumber(value) }));
  const setWeight = (key: keyof AdaptiveRoutingConfig['weights'], value: string) =>
    setConfig((current) => ({
      ...current,
      weights: { ...current.weights, [key]: finiteNumber(value) },
    }));
  const setStickyNumber = (
    key: Exclude<keyof AdaptiveRoutingConfig['sticky-escape'], 'enabled'>,
    value: string
  ) =>
    setConfig((current) => ({
      ...current,
      'sticky-escape': {
        ...current['sticky-escape'],
        [key]: finiteNumber(value),
      },
    }));

  const applyScoreFilters = () => {
    setAppliedProvider(provider.trim());
    setAppliedModel(model.trim());
    if (provider.trim() === appliedProvider && model.trim() === appliedModel) {
      void load();
    }
  };

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <div>
          <h1>{t('adaptive_routing.title')}</h1>
          <p>{t('adaptive_routing.description')}</p>
        </div>
        <div className={styles.actions}>
          <span className={`${styles.badge} ${enabled ? '' : styles.badgeOff}`}>
            {enabled ? t('common.enabled') : t('common.disabled')}
          </span>
          <Button variant="secondary" loading={saving} onClick={toggleEnabled}>
            {t(enabled ? 'adaptive_routing.switch_off' : 'adaptive_routing.switch_on')}
          </Button>
        </div>
      </header>

      <section className={styles.panel}>
        <div className={styles.panelTitle}>
          <h2>{t('adaptive_routing.configuration')}</h2>
          <Button loading={saving} onClick={save}>{t('common.save')}</Button>
        </div>
        <div className={styles.grid}>
          <Input label={t('adaptive_routing.top_k')} type="number" min={1} max={64} value={config['top-k']} onChange={(event) => setRootNumber('top-k', event.target.value)} />
          <Input label={t('adaptive_routing.ewma_alpha')} type="number" min={0.01} max={1} step={0.05} value={config['ewma-alpha']} onChange={(event) => setRootNumber('ewma-alpha', event.target.value)} />
          <Input label={t('adaptive_routing.ttft_target')} type="number" min={1} value={config['ttft-target-ms']} onChange={(event) => setRootNumber('ttft-target-ms', event.target.value)} />
          <Input label={t('adaptive_routing.weight_priority')} type="number" min={0} step={0.1} value={config.weights.priority} onChange={(event) => setWeight('priority', event.target.value)} />
          <Input label={t('adaptive_routing.weight_load')} type="number" min={0} step={0.1} value={config.weights.load} onChange={(event) => setWeight('load', event.target.value)} />
          <Input label={t('adaptive_routing.weight_success')} type="number" min={0} step={0.1} value={config.weights['success-rate']} onChange={(event) => setWeight('success-rate', event.target.value)} />
          <Input label={t('adaptive_routing.weight_ttft')} type="number" min={0} step={0.1} value={config.weights.ttft} onChange={(event) => setWeight('ttft', event.target.value)} />
          <div className={styles.toggle}><ToggleSwitch checked={config['sticky-escape'].enabled} onChange={(checked) => setConfig((current) => ({ ...current, 'sticky-escape': { ...current['sticky-escape'], enabled: checked } }))} label={t('adaptive_routing.sticky_escape')} /></div>
          <Input label={t('adaptive_routing.min_samples')} type="number" min={1} value={config['sticky-escape']['min-samples']} onChange={(event) => setStickyNumber('min-samples', event.target.value)} />
          <Input label={t('adaptive_routing.error_threshold')} type="number" min={0} max={1} step={0.05} value={config['sticky-escape']['error-rate-threshold']} onChange={(event) => setStickyNumber('error-rate-threshold', event.target.value)} />
          <Input label={t('adaptive_routing.ttft_threshold')} type="number" min={0} value={config['sticky-escape']['ttft-threshold-ms']} onChange={(event) => setStickyNumber('ttft-threshold-ms', event.target.value)} />
          <Input label={t('adaptive_routing.active_threshold')} type="number" min={0} value={config['sticky-escape']['active-request-threshold']} onChange={(event) => setStickyNumber('active-request-threshold', event.target.value)} />
        </div>
      </section>

      <section className={styles.panel}>
        <div className={styles.panelTitle}><h2>{t('adaptive_routing.live_scores')}</h2></div>
        <div className={styles.toolbar}>
          <Input placeholder={t('adaptive_routing.provider_filter')} value={provider} onChange={(event) => setProvider(event.target.value)} />
          <Input placeholder={t('adaptive_routing.model_filter')} value={model} onChange={(event) => setModel(event.target.value)} />
          <Button variant="secondary" onClick={applyScoreFilters}>{t('common.refresh')}</Button>
        </div>
        {loading ? <div className={styles.loading}>{t('common.loading')}</div> : null}
        {!loading && error ? <div className={styles.error}>{error}</div> : null}
        {!loading && !error && scores.length === 0 ? <div className={styles.empty}>{t('adaptive_routing.no_scores')}</div> : null}
        {!loading && !error && scores.length > 0 ? <div className={styles.tableWrap}><table className={styles.table}>
          <thead><tr><th>{t('adaptive_routing.rank')}</th><th>auth_index</th><th>{t('common.provider')}</th><th>{t('common.priority')}</th><th>{t('adaptive_routing.score')}</th><th>{t('adaptive_routing.active')}</th><th>{t('adaptive_routing.success_rate')}</th><th>TTFT</th><th>{t('adaptive_routing.samples')}</th><th>{t('common.status')}</th></tr></thead>
          <tbody>{scores.map((item) => <tr key={item.auth_id}><td>{item.rank || '-'}</td><td><b>{item.auth_index || item.auth_id}</b><div className={styles.muted}>{item.auth_id}</div></td><td>{item.provider}</td><td>{item.priority}</td><td>{item.score.toFixed(3)}</td><td>{item.active_requests}</td><td>{(item.success_rate * 100).toFixed(1)}%</td><td>{item.ttft_ms > 0 ? `${Math.round(item.ttft_ms)} ms` : '-'}</td><td>{item.outcome_samples} / {item.ttft_samples}</td><td className={item.escape_sticky ? styles.warning : ''}>{item.escape_sticky ? t('adaptive_routing.escape') : item.eligible ? t('adaptive_routing.eligible') : t('adaptive_routing.ineligible')}</td></tr>)}</tbody>
        </table></div> : null}
      </section>
    </div>
  );
}
