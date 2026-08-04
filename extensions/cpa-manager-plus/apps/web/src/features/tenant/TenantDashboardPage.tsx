import { useCallback, useEffect, useMemo, useState } from 'react';
import { Button } from '@/components/ui/Button';
import { tenantObservabilityApi } from '@/services/api';
import styles from './TenantPanel.module.scss';

const startOfToday = () => {
  const now = new Date();
  now.setHours(0, 0, 0, 0);
  return now.getTime();
};

const asRecord = (value: unknown): Record<string, unknown> =>
  value && typeof value === 'object' && !Array.isArray(value) ? (value as Record<string, unknown>) : {};

const printable = (value: unknown) => {
  if (value === null || value === undefined) return '-';
  if (typeof value === 'number') return new Intl.NumberFormat().format(value);
  if (typeof value === 'boolean') return value ? '是' : '否';
  if (typeof value === 'string') return value;
  return JSON.stringify(value);
};

export function TenantDashboardPage() {
  const [data, setData] = useState<Record<string, unknown>>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      setData(await tenantObservabilityApi.dashboard(startOfToday()));
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : '加载仪表盘失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const summary = useMemo(() => {
    const candidate = asRecord(data.summary);
    return Object.keys(candidate).length > 0 ? candidate : data;
  }, [data]);
  const metrics = Object.entries(summary).filter(([, value]) =>
    ['string', 'number', 'boolean'].includes(typeof value)
  );

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <div>
          <h1>仪表盘</h1>
          <p>当前租户 API Key 的今日请求概览。</p>
        </div>
        <Button variant="secondary" onClick={() => void load()} loading={loading}>
          刷新
        </Button>
      </header>
      {error ? <div className={styles.error}>{error}</div> : null}
      {!error && loading ? <div className={styles.loading}>正在加载...</div> : null}
      {!error && !loading ? (
        <>
          <section className={styles.metrics}>
            {metrics.length > 0 ? (
              metrics.map(([key, value]) => (
                <div className={styles.metric} key={key}>
                  <span>{key.replace(/_/g, ' ')}</span>
                  <b title={printable(value)}>{printable(value)}</b>
                </div>
              ))
            ) : (
              <div className={styles.empty}>今天尚无可展示的请求数据。</div>
            )}
          </section>
          {data.timeline || data.model_share ? (
            <section className={styles.summaryBlock}>
              <h2>趋势与模型分布</h2>
              <pre className={styles.codeBlock}>
                {JSON.stringify({ timeline: data.timeline, model_share: data.model_share }, null, 2)}
              </pre>
            </section>
          ) : null}
        </>
      ) : null}
    </div>
  );
}
