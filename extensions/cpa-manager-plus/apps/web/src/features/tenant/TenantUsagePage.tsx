import { useCallback, useEffect, useState } from 'react';
import { Button } from '@/components/ui/Button';
import { tenantObservabilityApi, type TenantUsagePage } from '@/services/api';
import styles from './TenantPanel.module.scss';

const value = (item: Record<string, unknown>, key: string) => {
  const raw = item[key];
  if (raw === undefined || raw === null || raw === '') return '-';
  return String(raw);
};

export function TenantUsagePage() {
  const [page, setPage] = useState(1);
  const [data, setData] = useState<TenantUsagePage | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      setData(await tenantObservabilityApi.usage(page, 50));
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : '加载用量数据失败');
    } finally {
      setLoading(false);
    }
  }, [page]);

  useEffect(() => {
    void load();
  }, [load]);

  const totalPages = Math.max(
    1,
    data?.total_pages || Math.ceil((data?.total || 0) / (data?.page_size || 50)) || 1
  );

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <div>
          <h1>用量分析</h1>
          <p>按 API Key 隔离后的原始请求用量记录。</p>
        </div>
        <Button variant="secondary" onClick={() => void load()} loading={loading}>
          刷新
        </Button>
      </header>
      <div className={styles.tableWrap}>
        {loading ? <div className={styles.loading}>正在加载...</div> : null}
        {error ? <div className={styles.error}>{error}</div> : null}
        {!loading && !error && (data?.items ?? []).length === 0 ? (
          <div className={styles.empty}>暂无用量记录。</div>
        ) : null}
        {!loading && !error && (data?.items ?? []).length > 0 ? (
          <table className={styles.table}>
            <thead>
              <tr>
                <th>时间</th>
                <th>模型</th>
                <th>提供商</th>
                <th>输入</th>
                <th>输出</th>
                <th>总 Token</th>
                <th>结果</th>
              </tr>
            </thead>
            <tbody>
              {(data?.items ?? []).map((item, index) => (
                <tr
                  key={
                    value(item, 'request_id') !== '-'
                      ? value(item, 'request_id')
                      : `${page}-${index}`
                  }
                >
                  <td>{value(item, 'timestamp')}</td>
                  <td>{value(item, 'model')}</td>
                  <td>{value(item, 'provider')}</td>
                  <td>{value(item, 'input_tokens')}</td>
                  <td>{value(item, 'output_tokens')}</td>
                  <td>{value(item, 'total_tokens')}</td>
                  <td>
                    <span
                      className={`${styles.badge} ${item.failed ? styles.failed : styles.enabled}`}
                    >
                      {item.failed ? '失败' : '成功'}
                    </span>
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
          disabled={page <= 1 || loading}
          onClick={() => setPage((current) => current - 1)}
        >
          上一页
        </Button>
        <span>
          {page} / {totalPages}
        </span>
        <Button
          size="xs"
          variant="secondary"
          disabled={page >= totalPages || loading}
          onClick={() => setPage((current) => current + 1)}
        >
          下一页
        </Button>
      </div>
    </div>
  );
}
