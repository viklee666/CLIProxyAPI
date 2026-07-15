import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { IconCheck, IconEye, IconRefreshCw, IconSearch, IconTrash2 } from '@/components/ui/icons';
import { usePanelFeatureAvailability } from '@/hooks/usePanelFeatureAvailability';
import {
  usageServiceApi,
  type AccountActionCandidate,
  type AccountActionStatus,
} from '@/services/api/usageService';
import { useAuthStore, useNotificationStore } from '@/stores';
import { formatDateTime, maskSensitiveText } from '@/utils/format';
import styles from './AccountActionCandidatesPage.module.scss';

type StatusFilter = 'pending' | 'all' | 'ignored' | 'resolved' | 'deleted';

type CandidateAction = 'ignore' | 'resolve' | 'enable' | 'delete';

const STATUS_FILTERS: StatusFilter[] = ['pending', 'all', 'ignored', 'resolved', 'deleted'];
const CANDIDATE_PAGE_SIZE = 25;
const SEARCH_DEBOUNCE_MS = 350;

function useDebouncedValue(value: string, delayMs: number) {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(value), delayMs);
    return () => clearTimeout(timer);
  }, [delayMs, value]);
  return debounced;
}

const formatMs = (value?: number) => {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '-';
  return formatDateTime(date);
};

const stringifyEvidence = (candidate: AccountActionCandidate | null) => {
  if (!candidate?.evidence) return '';
  try {
    return maskSensitiveText(JSON.stringify(candidate.evidence, null, 2));
  } catch {
    return maskSensitiveText(String(candidate.evidence));
  }
};

const isRecord = (value: unknown): value is Record<string, unknown> =>
  Boolean(value && typeof value === 'object' && !Array.isArray(value));

const readEvidenceString = (candidate: AccountActionCandidate, key: string) => {
  const evidence = candidate.evidence;
  if (!isRecord(evidence)) return '';
  const value = evidence[key];
  if (typeof value === 'string') return value.trim();
  if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  return '';
};

const getHeaderEvidence = (candidate: AccountActionCandidate) => ({
  errorKind: readEvidenceString(candidate, 'headerErrorKind'),
  errorCode: readEvidenceString(candidate, 'headerErrorCode'),
  traceId: readEvidenceString(candidate, 'headerTraceId'),
});

export function AccountActionCandidatesPage() {
  const { t } = useTranslation();
  const managementKey = useAuthStore((state) => state.managementKey);
  const { showNotification, showConfirmation } = useNotificationStore();
  const featureAvailability = usePanelFeatureAvailability();
  const [items, setItems] = useState<AccountActionCandidate[]>([]);
  const [pendingCount, setPendingCount] = useState(0);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [filter, setFilter] = useState<StatusFilter>('pending');
  const [search, setSearch] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [actingId, setActingId] = useState<number | null>(null);
  const [evidenceCandidate, setEvidenceCandidate] = useState<AccountActionCandidate | null>(null);
  const debouncedSearch = useDebouncedValue(search.trim(), SEARCH_DEBOUNCE_MS);

  const managerBase = featureAvailability.managerServiceBase;

  const loadCandidates = useCallback(async () => {
    if (!managerBase || !managementKey) return;
    setLoading(true);
    setError('');
    try {
      const response = await usageServiceApi.listAccountActionCandidates(
        managerBase,
        managementKey,
        {
          status: filter === 'all' ? '' : filter,
          search: debouncedSearch,
          page,
          pageSize: CANDIDATE_PAGE_SIZE,
        }
      );
      setItems(response.items);
      setPendingCount(response.pendingCount);
      setTotal(response.total);
      if (response.items.length === 0 && response.total > 0 && page > 1) {
        setPage((current) => Math.max(1, current - 1));
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err || 'request failed');
      setError(message);
      showNotification(
        t('account_actions.load_failed', { message, defaultValue: `Load failed: ${message}` }),
        'error'
      );
    } finally {
      setLoading(false);
    }
  }, [debouncedSearch, filter, managementKey, managerBase, page, showNotification, t]);

  useEffect(() => {
    if (featureAvailability.checking) return;
    void loadCandidates();
  }, [featureAvailability.checking, loadCandidates]);

  const visibleItems = items;
  const totalPages = Math.max(1, Math.ceil(total / CANDIDATE_PAGE_SIZE));

  const runAction = useCallback(
    async (candidate: AccountActionCandidate, action: CandidateAction) => {
      if (!managerBase || !managementKey) return;
      setActingId(candidate.id);
      try {
        switch (action) {
          case 'ignore':
            await usageServiceApi.ignoreAccountActionCandidate(
              managerBase,
              managementKey,
              candidate.id
            );
            showNotification(t('account_actions.ignore_success'), 'success');
            break;
          case 'resolve':
            await usageServiceApi.resolveAccountActionCandidate(
              managerBase,
              managementKey,
              candidate.id
            );
            showNotification(t('account_actions.resolve_success'), 'success');
            break;
          case 'enable':
            await usageServiceApi.enableAccountActionCandidate(
              managerBase,
              managementKey,
              candidate.id
            );
            showNotification(t('account_actions.enable_success'), 'success');
            break;
          case 'delete':
            await usageServiceApi.deleteAccountActionCandidateAuthFile(
              managerBase,
              managementKey,
              candidate.id
            );
            showNotification(t('account_actions.delete_success'), 'success');
            break;
        }
        await loadCandidates();
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err || 'request failed');
        showNotification(
          t('account_actions.action_failed', {
            message,
            defaultValue: `Action failed: ${message}`,
          }),
          'error'
        );
      } finally {
        setActingId(null);
      }
    },
    [loadCandidates, managementKey, managerBase, showNotification, t]
  );

  const confirmDelete = useCallback(
    (candidate: AccountActionCandidate) => {
      showConfirmation({
        title: t('account_actions.confirm_delete_title'),
        message: (
          <span>
            {t('account_actions.confirm_delete_message', {
              file: candidate.authFileName,
            })}
          </span>
        ),
        confirmText: t('account_actions.confirm_delete_button'),
        cancelText: t('common.cancel'),
        variant: 'danger',
        onConfirm: () => runAction(candidate, 'delete'),
      });
    },
    [runAction, showConfirmation, t]
  );

  const actionLabel = (actionType: string) =>
    t(`account_actions.action_type_${actionType}`, {
      defaultValue: actionType || '-',
    });

  const statusLabel = (status: AccountActionStatus) =>
    t(`account_actions.status_${status}`, {
      defaultValue: status || '-',
    });

  const evidenceText = stringifyEvidence(evidenceCandidate);

  return (
    <div className={styles.page}>
      <section className={styles.hero}>
        <div className={styles.heroMain}>
          {featureAvailability.requestMonitoringAvailable ? (
            <Link to="/monitoring" className={styles.backLink}>
              {t('account_actions.back_to_monitoring')}
            </Link>
          ) : null}
          <p className={styles.kicker}>{t('account_actions.eyebrow')}</p>
          <h1 className={styles.title}>{t('account_actions.title')}</h1>
          <p className={styles.description}>{t('account_actions.description')}</p>
        </div>
        <div className={styles.heroStats}>
          <div className={styles.statCard}>
            <span className={styles.statValue}>{pendingCount}</span>
            <span className={styles.statLabel}>{t('account_actions.pending_count')}</span>
          </div>
          <div className={styles.statCard}>
            <span className={styles.statValue}>{total}</span>
            <span className={styles.statLabel}>{t('account_actions.visible_count')}</span>
          </div>
        </div>
      </section>

      <section className={styles.toolbar}>
        <div className={styles.filterGroup} aria-label={t('account_actions.status_filter')}>
          {STATUS_FILTERS.map((key) => (
            <button
              key={key}
              type="button"
              className={[styles.filterButton, filter === key ? styles.filterButtonActive : '']
                .filter(Boolean)
                .join(' ')}
              onClick={() => {
                setFilter(key);
                setPage(1);
              }}
            >
              {t(`account_actions.filter_${key}`)}
              {key === 'pending' ? ` · ${pendingCount}` : ''}
            </button>
          ))}
        </div>
        <Input
          value={search}
          onChange={(event) => {
            setSearch(event.target.value);
            setPage(1);
          }}
          placeholder={t('account_actions.search_placeholder', {
            defaultValue: 'Search account, file, error or trace',
          })}
          rightElement={<IconSearch size={15} />}
          aria-label={t('account_actions.search_placeholder', {
            defaultValue: 'Search account, file, error or trace',
          })}
        />
        <div className={styles.actions}>
          <Button variant="secondary" size="sm" onClick={loadCandidates} loading={loading}>
            <IconRefreshCw size={15} />
            {t('common.refresh')}
          </Button>
        </div>
      </section>

      <section className={styles.panel}>
        {error ? (
          <div className={styles.errorState}>
            <strong>{t('account_actions.load_failed_title')}</strong>
            <span>{error}</span>
          </div>
        ) : visibleItems.length === 0 && !loading ? (
          <div className={styles.emptyState}>
            <strong>{t('account_actions.empty_title')}</strong>
            <span>{t('account_actions.empty_desc')}</span>
          </div>
        ) : (
          <div className={styles.tableWrap}>
            <table className={styles.table}>
              <thead>
                <tr>
                  <th>{t('account_actions.col_account')}</th>
                  <th>{t('account_actions.col_file')}</th>
                  <th>{t('account_actions.col_action')}</th>
                  <th>{t('account_actions.col_reason')}</th>
                  <th>{t('account_actions.col_seen')}</th>
                  <th>{t('account_actions.col_hits')}</th>
                  <th>{t('account_actions.col_status')}</th>
                  <th>{t('account_actions.col_operations')}</th>
                </tr>
              </thead>
              <tbody>
                {visibleItems.map((candidate) => {
                  const busy = actingId === candidate.id;
                  const header = getHeaderEvidence(candidate);
                  return (
                    <tr key={candidate.id}>
                      <td>
                        <div className={styles.accountCell}>
                          <strong>{candidate.accountSnapshot || candidate.authLabel || '-'}</strong>
                          <span>{candidate.provider || '-'}</span>
                          {candidate.accountIdSnapshot ? (
                            <small>{candidate.accountIdSnapshot}</small>
                          ) : null}
                        </div>
                      </td>
                      <td>
                        <div className={styles.fileCell}>
                          <strong>{candidate.authFileName}</strong>
                          <span>{candidate.authIndex || candidate.authLabel || '-'}</span>
                        </div>
                      </td>
                      <td>
                        <span
                          className={[
                            styles.actionBadge,
                            candidate.actionType === 'delete' ? styles.actionDelete : '',
                            candidate.actionType === 'reauth' ? styles.actionReauth : '',
                          ]
                            .filter(Boolean)
                            .join(' ')}
                        >
                          {actionLabel(candidate.actionType)}
                        </span>
                      </td>
                      <td>
                        <div className={styles.reasonCell}>
                          <strong>{candidate.reason || '-'}</strong>
                          {candidate.lastError ? (
                            <small className={styles.errorText}>{candidate.lastError}</small>
                          ) : null}
                          {header.errorKind || header.errorCode ? (
                            <small>
                              {[header.errorKind, header.errorCode].filter(Boolean).join(' / ')}
                            </small>
                          ) : null}
                          {header.traceId ? <small>{`Trace: ${header.traceId}`}</small> : null}
                          <small>
                            {t('account_actions.last_updated', {
                              time: formatMs(candidate.updatedAtMs),
                            })}
                          </small>
                        </div>
                      </td>
                      <td>
                        <div className={styles.timeCell}>
                          <span>{formatMs(candidate.firstSeenAtMs)}</span>
                          <small>{formatMs(candidate.lastSeenAtMs)}</small>
                        </div>
                      </td>
                      <td>
                        <span className={styles.metaPill}>{candidate.hitCount}</span>
                      </td>
                      <td>
                        <span
                          className={[
                            styles.statusBadge,
                            candidate.status === 'pending' ? styles.statusPending : '',
                            candidate.status === 'ignored' ? styles.statusIgnored : '',
                            candidate.status === 'resolved' ? styles.statusResolved : '',
                            candidate.status === 'deleted' ? styles.statusDeleted : '',
                          ]
                            .filter(Boolean)
                            .join(' ')}
                        >
                          {statusLabel(candidate.status)}
                        </span>
                      </td>
                      <td>
                        <div className={styles.rowActions}>
                          <Button
                            variant="ghost"
                            size="xs"
                            onClick={() => setEvidenceCandidate(candidate)}
                            disabled={busy}
                          >
                            <IconEye size={14} />
                            {t('account_actions.view_evidence')}
                          </Button>
                          {candidate.status === 'pending' && (
                            <>
                              <Button
                                variant="secondary"
                                size="xs"
                                onClick={() => runAction(candidate, 'enable')}
                                loading={busy}
                              >
                                <IconCheck size={14} />
                                {t('account_actions.enable')}
                              </Button>
                              <Button
                                variant="secondary"
                                size="xs"
                                onClick={() => runAction(candidate, 'resolve')}
                                disabled={busy}
                              >
                                {t('account_actions.resolve')}
                              </Button>
                              <Button
                                variant="ghost"
                                size="xs"
                                onClick={() => runAction(candidate, 'ignore')}
                                disabled={busy}
                              >
                                {t('account_actions.ignore')}
                              </Button>
                              <Button
                                variant="danger"
                                size="xs"
                                onClick={() => confirmDelete(candidate)}
                                disabled={busy}
                              >
                                <IconTrash2 size={14} />
                                {t('account_actions.delete_auth_file')}
                              </Button>
                            </>
                          )}
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
        <div className={styles.pagination}>
          <span>
            {page} / {totalPages} · {t('common.total')}: {total}
          </span>
          <div>
            <Button
              variant="secondary"
              size="xs"
              disabled={page <= 1 || loading}
              onClick={() => setPage((current) => Math.max(1, current - 1))}
            >
              {t('common.previous')}
            </Button>
            <Button
              variant="secondary"
              size="xs"
              disabled={page >= totalPages || loading}
              onClick={() => setPage((current) => current + 1)}
            >
              {t('common.next')}
            </Button>
          </div>
        </div>
      </section>

      <Modal
        open={Boolean(evidenceCandidate)}
        title={t('account_actions.evidence_title')}
        width={760}
        onClose={() => setEvidenceCandidate(null)}
        footer={
          <Button variant="secondary" onClick={() => setEvidenceCandidate(null)}>
            {t('common.close')}
          </Button>
        }
      >
        <pre className={styles.evidenceBox}>{evidenceText || t('account_actions.no_evidence')}</pre>
      </Modal>
    </div>
  );
}
