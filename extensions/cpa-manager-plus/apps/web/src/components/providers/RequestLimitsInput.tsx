import { useTranslation } from 'react-i18next';
import { Input } from '@/components/ui/Input';

import type { RequestLimits } from '@/types';
import { requestLimits } from './requestLimits';

export function RequestLimitsInput({
  value,
  onChange,
  disabled = false,
}: {
  value: RequestLimits;
  onChange: (limits: Required<RequestLimits>) => void;
  disabled?: boolean;
}) {
  const { t } = useTranslation();
  return (
    <div>
      {(['concurrencyLimit', 'rpmLimit'] as const).map((key) => (
        <Input
          key={key}
          label={t(`request_limits.${key}`)}
          hint={t('request_limits.hint')}
          type="number"
          min={0}
          step={1}
          value={Number.isNaN(value[key]) ? '' : (value[key] ?? 0)}
          onChange={(event) =>
            onChange({
              ...requestLimits(value),
              [key]: event.target.value === '' ? 0 : Number(event.target.value),
            })
          }
          disabled={disabled}
          error={
            Number.isSafeInteger(value[key] ?? 0) && (value[key] ?? 0) >= 0
              ? undefined
              : t('request_limits.invalid')
          }
        />
      ))}
    </div>
  );
}
