import { useCallback, useEffect, useMemo, useState } from 'react';
import { Button } from '@/components/ui/Button';
import { tenantObservabilityApi } from '@/services/api';
import styles from './TenantPanel.module.scss';

const asRecord = (value: unknown): Record<string, unknown> =>
  value && typeof value === 'object' && !Array.isArray(value) ? (value as Record<string, unknown>) : {};

export function TenantMonitoringPage() {
  const [hours, setHours] = useState(24);
  const [data, setData] = useState<Record<string, unknown>>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    const toMS = Date.now();
    try {
      setData(await tenantObservabilityApi.analytics(toMS - hours * 60 * 60 * 1000, toMS));
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : '加载监控数据失败');
    } finally {
      setLoading(false);
    }
  }, [hours]);

  useEffect(() => {
    void load();
  }, [load]);

  const summary = useMemo(() => asRecord(data.summary), [data]);
  const summaryEntries = Object.entries(summary).filter(([, value]) =>
    ['string', 'number', 'boolean'].includes(typeof value)
  );

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <div>
          <h1>请求监控</h1>
          <p>只显示当前租户 API Key 产生的请求。</p>
        </div>
        <div className={styles.actions}>
          {[24, 168, 720].map((value) => (
            <Button
              key={value}
              size="sm"
              variant={hours === value ? 'primary' : 'secondary'}
              onClick={() => setHours(value)}
            >
              {value === 24 ? '24 小时' : value === 168 ? '7 天' : '30 天'}
            </Button>
          ))}
          <Button variant="secondary" onClick={() => void load()} loading={loading}>
            刷新
          </Button>
        </div>
      </header>
      {error ? <div className={styles.error}>{error}</div> : null}
      {loading && !error ? <div className={styles.loading}>正在加载...</div> : null}
      {!loading && !error ? (
        <>
          <section className={styles.metrics}>
            {summaryEntries.map(([key, value]) => (
              <div className={styles.metric} key={key}>
                <span>{key.replace(/_/g, ' ')}</span>
                <b>{String(value)}</b>
              </div>
            ))}
            {summaryEntries.length === 0 ? <div className={styles.empty}>该时间段内尚无请求。</div> : null}
          </section>
          <section className={styles.summaryBlock}>
            <h2>监控详情</h2>
            <pre className={styles.codeBlock}>{JSON.stringify(data, null, 2)}</pre>
          </section>
        </>
      ) : null}
    </div>
  );
}
