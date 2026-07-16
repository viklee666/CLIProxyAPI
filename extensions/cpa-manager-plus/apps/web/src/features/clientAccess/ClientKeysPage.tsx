import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import { useHeaderRefresh } from '@/hooks/useHeaderRefresh';
import { clientAccessApi, type ClientGroup, type ClientKey } from '@/services/api';
import { useNotificationStore } from '@/stores';
import { copyToClipboard } from '@/utils/clipboard';
import styles from './ClientAccessPage.module.scss';

type KeyForm = {
  name: string;
  secret: string;
  enabled: boolean;
  allowAllGroups: boolean;
  allowUngrouped: boolean;
  groupIDs: number[];
  expiresAt: string;
  rpmLimit: string;
  concurrencyLimit: string;
  requestLimitTotal: string;
  requestLimit5h: string;
  requestLimit1d: string;
  requestLimit7d: string;
  tokenLimitTotal: string;
  tokenLimit5h: string;
  tokenLimit1d: string;
  tokenLimit7d: string;
};

const emptyForm: KeyForm = {
  name: '',
  secret: '',
  enabled: true,
  allowAllGroups: true,
  allowUngrouped: false,
  groupIDs: [],
  expiresAt: '',
  rpmLimit: '0',
  concurrencyLimit: '0',
  requestLimitTotal: '0',
  requestLimit5h: '0',
  requestLimit1d: '0',
  requestLimit7d: '0',
  tokenLimitTotal: '0',
  tokenLimit5h: '0',
  tokenLimit1d: '0',
  tokenLimit7d: '0',
};

const numberValue = (value: string) => Math.max(0, Number.parseInt(value || '0', 10) || 0);
const localDateTime = (value?: string) => (value ? new Date(value).toISOString().slice(0, 16) : '');
const resetAt = (startedAt: string | undefined, hours: number) => {
  if (!startedAt) return '';
  const timestamp = new Date(startedAt).getTime();
  if (!Number.isFinite(timestamp)) return '';
  return new Date(timestamp + hours * 60 * 60 * 1000).toLocaleString();
};

type QuotaLineProps = {
  label: string;
  used: number;
  limit: number;
  reserved?: number;
  reset?: string;
};

function QuotaLine({ label, used, limit, reserved = 0, reset }: QuotaLineProps) {
  const consumed = used + reserved;
  const percent = limit > 0 ? Math.min(100, (consumed / limit) * 100) : 0;
  return (
    <div className={styles.quotaLine} title={reset || undefined}>
      <span>{label}</span>
      <b>{used}{reserved > 0 ? ` + ${reserved}` : ''} / {limit || '∞'}</b>
      {limit > 0 ? <i><em style={{ width: `${percent}%` }} /></i> : null}
      {reset ? <small>{reset}</small> : null}
    </div>
  );
}
export function ClientKeysPage() {
  const { t } = useTranslation();
  const showNotification = useNotificationStore((state) => state.showNotification);
  const showConfirmation = useNotificationStore((state) => state.showConfirmation);
  const [items, setItems] = useState<ClientKey[]>([]);
  const [groups, setGroups] = useState<ClientGroup[]>([]);
  const [search, setSearch] = useState('');
  const [page, setPage] = useState(1);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [editing, setEditing] = useState<ClientKey | null>(null);
  const [modalOpen, setModalOpen] = useState(false);
  const [secret, setSecret] = useState('');
  const [form, setForm] = useState<KeyForm>(emptyForm);
  const [saving, setSaving] = useState(false);
  const pageSize = 20;

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const [keyResponse, groupResponse] = await Promise.all([
        clientAccessApi.listKeys(page, pageSize, search),
        clientAccessApi.listAllGroups(),
      ]);
      setItems(keyResponse.items ?? []);
      setTotal(keyResponse.total ?? 0);
      setGroups(groupResponse);
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : String(loadError));
    } finally {
      setLoading(false);
    }
  }, [page, search]);

  useEffect(() => void load(), [load]);
  useHeaderRefresh(load);
  const groupByID = useMemo(() => new Map(groups.map((group) => [group.id, group])), [groups]);

  const openCreate = () => {
    setEditing(null);
    setForm(emptyForm);
    setModalOpen(true);
  };

  const openEdit = (key: ClientKey) => {
    setEditing(key);
    setForm({
      name: key.name,
      secret: key.secret ?? '',
      enabled: key.enabled,
      allowAllGroups: key.allow_all_groups,
      allowUngrouped: key.allow_ungrouped,
      groupIDs: key.group_ids ?? [],
      expiresAt: localDateTime(key.expires_at),
      rpmLimit: String(key.rpm_limit ?? 0),
      concurrencyLimit: String(key.concurrency_limit ?? 0),
      requestLimitTotal: String(key.request_limit_total ?? 0),
      requestLimit5h: String(key.request_limit_5h ?? 0),
      requestLimit1d: String(key.request_limit_1d ?? 0),
      requestLimit7d: String(key.request_limit_7d ?? 0),
      tokenLimitTotal: String(key.token_limit_total ?? 0),
      tokenLimit5h: String(key.token_limit_5h ?? 0),
      tokenLimit1d: String(key.token_limit_1d ?? 0),
      tokenLimit7d: String(key.token_limit_7d ?? 0),
    });
    setModalOpen(true);
  };

  const toggleGroup = (id: number) =>
    setForm((value) => ({
      ...value,
      groupIDs: value.groupIDs.includes(id)
        ? value.groupIDs.filter((groupID) => groupID !== id)
        : [...value.groupIDs, id],
    }));

  const save = async () => {
    if (!form.name.trim()) return;
    setSaving(true);
    const payload = {
      name: form.name.trim(),
      enabled: form.enabled,
      allow_all_groups: form.allowAllGroups,
      allow_ungrouped: form.allowUngrouped,
      group_ids: form.groupIDs,
      rpm_limit: numberValue(form.rpmLimit),
      concurrency_limit: numberValue(form.concurrencyLimit),
      request_limit_total: numberValue(form.requestLimitTotal),
      request_limit_5h: numberValue(form.requestLimit5h),
      request_limit_1d: numberValue(form.requestLimit1d),
      request_limit_7d: numberValue(form.requestLimit7d),
      token_limit_total: numberValue(form.tokenLimitTotal),
      token_limit_5h: numberValue(form.tokenLimit5h),
      token_limit_1d: numberValue(form.tokenLimit1d),
      token_limit_7d: numberValue(form.tokenLimit7d),
      ...(form.expiresAt
        ? { expires_at: new Date(form.expiresAt).toISOString() }
        : editing
          ? { clear_expires_at: true }
          : {}),
      ...(!editing && form.secret.trim() ? { secret: form.secret.trim() } : {}),
    };
    try {
      if (editing) {
        await clientAccessApi.updateKey(editing.id, payload);
      } else {
        const created = await clientAccessApi.createKey(payload);
        setSecret(created.secret);
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

  const remove = (key: ClientKey) =>
    showConfirmation({
      title: t('client_access.delete_key'),
      message: t('client_access.delete_key_confirm', { name: key.name }),
      variant: 'danger',
      onConfirm: async () => {
        await clientAccessApi.deleteKey(key.id);
        showNotification(t('client_access.deleted'), 'success');
        await load();
      },
    });

  const resetUsage = (key: ClientKey, kind: 'request' | 'token') =>
    showConfirmation({
      title: t(kind === 'request' ? 'client_access.reset_requests' : 'client_access.reset_tokens'),
      message: t('client_access.reset_usage_confirm', { name: key.name }),
      variant: 'danger',
      onConfirm: async () => {
        await clientAccessApi.updateKey(key.id, kind === 'request' ? { reset_request_usage: true } : { reset_token_usage: true });
        showNotification(t('client_access.usage_reset'), 'success');
        await load();
      },
    });

  const pages = Math.max(1, Math.ceil(total / pageSize));
  const quotaInput = (label: string, key: keyof KeyForm) => (
    <Input
      label={label}
      type="number"
      min={0}
      value={form[key] as string}
      onChange={(event) => setForm((value) => ({ ...value, [key]: event.target.value }))}
    />
  );

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <div><h1>{t('client_access.keys_title')}</h1><p>{t('client_access.keys_description')}</p></div>
        <Button onClick={openCreate}>{t('client_access.add_key')}</Button>
      </header>
      <div className={styles.toolbar}>
        <Input value={search} placeholder={t('client_access.search_keys')} onChange={(event) => { setSearch(event.target.value); setPage(1); }} />
      </div>
      <div className={styles.tableWrap}>
        {loading ? <div className={styles.loading}>{t('common.loading')}</div> : null}
        {!loading && error ? <div className={styles.error}>{error}</div> : null}
        {!loading && !error && items.length === 0 ? <div className={styles.empty}>{t('client_access.no_keys')}</div> : null}
        {!loading && !error && items.length > 0 ? (
          <table className={styles.table}>
            <thead><tr><th>{t('common.name')}</th><th>{t('client_access.key_value')}</th><th>{t('common.status')}</th><th>{t('client_access.groups')}</th><th>RPM</th><th>{t('client_access.concurrency')}</th><th>{t('client_access.request_quota')}</th><th>{t('client_access.token_quota')}</th><th>{t('client_access.expires')}</th><th /></tr></thead>
            <tbody>{items.map((key) => (
              <tr key={key.id}>
                <td><div className={styles.name}>{key.name}</div></td>
                <td><div className={styles.keyValue}><code>{key.secret || key.key_mask}</code><Button size="xs" variant="secondary" disabled={!key.secret} onClick={async () => { const copied = await copyToClipboard(key.secret ?? ''); showNotification(t(copied ? 'client_access.key_copied' : 'notification.copy_failed'), copied ? 'success' : 'error'); }}>{t('common.copy')}</Button></div></td>
                <td><span className={`${styles.badge} ${key.enabled ? styles.badgeActive : styles.badgeDisabled}`}>{key.enabled ? t('common.enabled') : t('common.disabled')}</span></td>
                <td>{key.allow_all_groups ? t('client_access.all_groups') : (key.group_ids ?? []).map((id) => groupByID.get(id)?.name ?? `#${id}`).join(', ') || '-'}</td>
                <td>{key.rpm_limit || '∞'}</td>
                <td>{key.current_concurrency} / {key.concurrency_limit || '∞'}</td>
                <td><div className={styles.quotaStack}>
                  <QuotaLine label={t('client_access.total')} used={key.request_used_total} limit={key.request_limit_total} />
                  <QuotaLine label="5h" used={key.request_used_5h} limit={key.request_limit_5h} reset={resetAt(key.request_window_5h_at, 5)} />
                  <QuotaLine label="1d" used={key.request_used_1d} limit={key.request_limit_1d} reset={resetAt(key.request_window_1d_at, 24)} />
                  <QuotaLine label="7d" used={key.request_used_7d} limit={key.request_limit_7d} reset={resetAt(key.request_window_7d_at, 168)} />
                </div></td>
                <td><div className={styles.quotaStack}>
                  <QuotaLine label={t('client_access.total')} used={key.token_used_total} limit={key.token_limit_total} reserved={key.token_reserved} />
                  <QuotaLine label="5h" used={key.token_used_5h} limit={key.token_limit_5h} reserved={key.token_reserved} reset={resetAt(key.token_window_5h_at, 5)} />
                  <QuotaLine label="1d" used={key.token_used_1d} limit={key.token_limit_1d} reserved={key.token_reserved} reset={resetAt(key.token_window_1d_at, 24)} />
                  <QuotaLine label="7d" used={key.token_used_7d} limit={key.token_limit_7d} reserved={key.token_reserved} reset={resetAt(key.token_window_7d_at, 168)} />
                </div></td>
                <td>{key.expires_at ? new Date(key.expires_at).toLocaleString() : t('client_access.never')}</td>
                <td><div className={styles.actionStack}>
                  <div className={styles.actions}><Button size="xs" variant="secondary" onClick={() => openEdit(key)}>{t('common.edit')}</Button><Button size="xs" variant="danger" onClick={() => remove(key)}>{t('common.delete')}</Button></div>
                  <div className={styles.actions}><Button size="xs" variant="secondary" onClick={() => resetUsage(key, 'request')}>{t('client_access.reset_requests_short')}</Button><Button size="xs" variant="secondary" onClick={() => resetUsage(key, 'token')}>{t('client_access.reset_tokens_short')}</Button></div>
                </div></td>
              </tr>
            ))}</tbody>
          </table>
        ) : null}
      </div>
      <div className={styles.pagination}><Button size="xs" variant="secondary" disabled={page <= 1} onClick={() => setPage((value) => value - 1)}>{t('common.previous')}</Button><span>{page} / {pages}</span><Button size="xs" variant="secondary" disabled={page >= pages} onClick={() => setPage((value) => value + 1)}>{t('common.next')}</Button></div>
      <Modal open={modalOpen} onClose={() => setModalOpen(false)} width={920} title={editing ? t('client_access.edit_key') : t('client_access.add_key')} footer={<><Button variant="secondary" onClick={() => setModalOpen(false)}>{t('common.cancel')}</Button><Button loading={saving} onClick={save}>{t('common.save')}</Button></>}>
        <div className={styles.modalGrid}>
          <Input label={t('common.name')} value={form.name} onChange={(event) => setForm((value) => ({ ...value, name: event.target.value }))} />
          {!editing ? <Input label={t('client_access.key_value')} hint={t('client_access.key_value_hint')} value={form.secret} onChange={(event) => setForm((value) => ({ ...value, secret: event.target.value }))} autoComplete="off" /> : null}
          <Input label={t('client_access.expires')} type="datetime-local" value={form.expiresAt} onChange={(event) => setForm((value) => ({ ...value, expiresAt: event.target.value }))} />
          <Input label="RPM" type="number" min={0} value={form.rpmLimit} onChange={(event) => setForm((value) => ({ ...value, rpmLimit: event.target.value }))} />
          <Input label={t('client_access.concurrency_limit')} type="number" min={0} value={form.concurrencyLimit} onChange={(event) => setForm((value) => ({ ...value, concurrencyLimit: event.target.value }))} />
          <div className={styles.fullWidth}><h3 className={styles.sectionTitle}>{t('client_access.request_limits')}</h3></div>
          {quotaInput(t('client_access.total'), 'requestLimitTotal')}
          {quotaInput('5h', 'requestLimit5h')}
          {quotaInput('1d', 'requestLimit1d')}
          {quotaInput('7d', 'requestLimit7d')}
          <div className={styles.fullWidth}><h3 className={styles.sectionTitle}>{t('client_access.token_limits')}</h3></div>
          {quotaInput(t('client_access.total'), 'tokenLimitTotal')}
          {quotaInput('5h', 'tokenLimit5h')}
          {quotaInput('1d', 'tokenLimit1d')}
          {quotaInput('7d', 'tokenLimit7d')}
          <div className={styles.toggleRow}><ToggleSwitch checked={form.enabled} onChange={(enabled) => setForm((value) => ({ ...value, enabled }))} label={t('common.enabled')} /></div>
          <div className={styles.toggleRow}><ToggleSwitch checked={form.allowAllGroups} onChange={(allowAllGroups) => setForm((value) => ({ ...value, allowAllGroups }))} label={t('client_access.all_groups')} /></div>
          <div className={styles.toggleRow}><ToggleSwitch checked={form.allowUngrouped} onChange={(allowUngrouped) => setForm((value) => ({ ...value, allowUngrouped }))} label={t('client_access.allow_ungrouped')} /></div>
          {!form.allowAllGroups ? <div className={`${styles.fullWidth} ${styles.groupPicker}`}>{groups.map((group) => <label className={styles.groupOption} key={group.id}><input type="checkbox" checked={form.groupIDs.includes(group.id)} onChange={() => toggleGroup(group.id)} />{group.name}</label>)}</div> : null}
        </div>
      </Modal>
      <Modal open={Boolean(secret)} onClose={() => setSecret('')} title={t('client_access.secret_title')} footer={<><Button variant="secondary" onClick={async () => { const copied = await copyToClipboard(secret); showNotification(t(copied ? 'client_access.key_copied' : 'notification.copy_failed'), copied ? 'success' : 'error'); }}>{t('common.copy')}</Button><Button onClick={() => setSecret('')}>{t('common.confirm')}</Button></>}>
        <p>{t('client_access.secret_hint')}</p><div className={styles.secretBox}>{secret}</div>
      </Modal>
    </div>
  );
}
