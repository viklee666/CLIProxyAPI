import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { RequestLimitsInput } from '@/components/providers/RequestLimitsInput';
import {
  requestLimits,
  requestLimitsChanged,
  validRequestLimits,
} from '@/components/providers/requestLimits';
import { providersApi } from '@/services/api';
import { useNotificationStore } from '@/stores';
import type { ProviderKeyConfig } from '@/types';

// Native providers without a dedicated editor still expose the same admission controls.
export function NativeProviderLimits({ disabled }: { disabled: boolean }) {
  const { t } = useTranslation();
  const { showNotification } = useNotificationStore();
  const [items, setItems] = useState<
    Array<{ kind: 'xai' | 'meta'; original: ProviderKeyConfig; draft: ProviderKeyConfig }>
  >([]);
  const [saving, setSaving] = useState<number | null>(null);
  const [error, setError] = useState('');
  useEffect(() => {
    let cancelled = false;
    void Promise.all(
      (['xai', 'meta'] as const).map(async (kind) => {
        const configs = await providersApi.getNativeProviderLimits(kind);
        return configs.map((original) => ({ kind, original, draft: { ...original } }));
      })
    )
      .then((groups) => {
        if (!cancelled) setItems(groups.flat());
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err));
      });
    return () => {
      cancelled = true;
    };
  }, []);

  if (!items.length && !error) return null;
  return (
    <Card title={t('request_limits.native_title')}>
      {error && <div className="error-box">{error}</div>}
      {items.map((item, index) => (
        <fieldset
          key={`${item.kind}:${item.original.authIndex ?? item.original.apiKey}:${item.original.baseUrl}`}
          disabled={disabled || saving !== null}
        >
          <legend>
            {item.kind === 'xai' ? 'xAI' : 'Meta'} ·{' '}
            {item.original.name || item.original.baseUrl || String(index + 1)}
          </legend>
          <RequestLimitsInput
            value={item.draft}
            onChange={(limits) =>
              setItems((current) =>
                current.map((entry, i) =>
                  i === index ? { ...entry, draft: { ...entry.draft, ...limits } } : entry
                )
              )
            }
            disabled={disabled || saving !== null}
          />
          <Button
            disabled={
              disabled ||
              saving !== null ||
              !validRequestLimits(item.draft) ||
              !requestLimitsChanged(item.original, item.draft)
            }
            loading={saving === index}
            onClick={async () => {
              setSaving(index);
              try {
                await providersApi.updateNativeProviderLimits(
                  item.kind,
                  item.original,
                  requestLimits(item.draft)
                );
                setItems((current) =>
                  current.map((entry, i) =>
                    i === index ? { ...entry, original: { ...entry.draft } } : entry
                  )
                );
                showNotification(t('client_access.saved'), 'success');
              } catch (err: unknown) {
                showNotification(
                  `${t('notification.update_failed')}: ${err instanceof Error ? err.message : String(err)}`,
                  'error'
                );
              } finally {
                setSaving(null);
              }
            }}
          >
            {t('common.save')}
          </Button>
        </fieldset>
      ))}
    </Card>
  );
}
