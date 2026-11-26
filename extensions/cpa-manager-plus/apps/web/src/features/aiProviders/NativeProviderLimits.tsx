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
import { buildNativePelicanTestTargets, type PelicanTestTarget } from '@/components/providers/PelicanTestDrawer/pelicanTest';

// Native providers without a dedicated editor still expose the same admission controls.
export function NativeProviderLimits({ disabled, onPelicanTest, onPelicanTargetsChange }: {
  disabled: boolean;
  onPelicanTest: (targets: PelicanTestTarget[]) => void;
  onPelicanTargetsChange: (targets: PelicanTestTarget[]) => void;
}) {
  const { t } = useTranslation();
  const { showNotification } = useNotificationStore();
  const [items, setItems] = useState<
    Array<{ kind: 'xai' | 'meta'; original: ProviderKeyConfig; draft: ProviderKeyConfig; target: PelicanTestTarget }>
  >([]);
  const [saving, setSaving] = useState<number | null>(null);
  const [error, setError] = useState('');
  useEffect(() => {
    let cancelled = false;
    void Promise.all(
      (['xai', 'meta'] as const).map(async (kind) => {
        const configs = await providersApi.getNativeProviderLimits(kind);
        const targets = buildNativePelicanTestTargets(kind, configs);
        return configs.map((original, index) => ({ kind, original, draft: { ...original }, target: targets[index] }));
      })
    )
      .then((groups) => {
        if (!cancelled) {
          const entries = groups.flat();
          setItems(entries);
          onPelicanTargetsChange(entries.map((entry) => entry.target));
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err));
      });
    return () => {
      cancelled = true;
    };
  }, [onPelicanTargetsChange]);

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
          <Button variant="secondary" disabled={disabled || saving !== null} onClick={() => onPelicanTest([item.target])}>
            {t('ai_providers.pelican_button')}
          </Button>
        </fieldset>
      ))}
    </Card>
  );
}
