import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { parseRequestScopedErrors } from './requestScopedErrors';
import styles from './AiProvidersPage.module.scss';

type RequestScopedErrorsEditorProps = {
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
};

export function RequestScopedErrorsEditor({
  value,
  onChange,
  disabled = false,
}: RequestScopedErrorsEditorProps) {
  const { t } = useTranslation();
  const parsed = useMemo(() => parseRequestScopedErrors(value), [value]);

  return (
    <div className="form-group">
      <label htmlFor="request-scoped-errors">{t('ai_providers.request_scoped_errors_label')}</label>
      <div className={styles.sectionHint}>{t('ai_providers.request_scoped_errors_hint')}</div>
      <textarea
        id="request-scoped-errors"
        className="input"
        rows={8}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        disabled={disabled}
        spellCheck={false}
        aria-invalid={Boolean(parsed.error)}
      />
      {parsed.error ? (
        <div className="error-box" role="alert">
          {t(`ai_providers.request_scoped_errors_${parsed.error}`)}
        </div>
      ) : null}
    </div>
  );
}
