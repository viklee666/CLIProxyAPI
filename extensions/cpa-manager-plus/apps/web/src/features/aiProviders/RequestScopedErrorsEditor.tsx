import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Select } from '@/components/ui/Select';
import { formatRequestScopedErrors, parseRequestScopedErrors } from './requestScopedErrors';
import type { RequestScopedErrorRule } from './requestScopedErrors';
import styles from './AiProvidersPage.module.scss';

type RequestScopedErrorsEditorProps = {
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
};

const actions = ['stop', 'stop-and-cooldown', 'continue', 'continue-and-cooldown'];

export function RequestScopedErrorsEditor({
  value,
  onChange,
  disabled = false,
}: RequestScopedErrorsEditorProps) {
  const { t } = useTranslation();
  const parsed = useMemo(() => parseRequestScopedErrors(value, false), [value]);
  const validationError = parseRequestScopedErrors(value).error;
  const rules = parsed.rules ?? [];
  const update = (index: number, patch: Partial<RequestScopedErrorRule>) =>
    onChange(
      formatRequestScopedErrors(
        rules.map((rule, i) => (i === index ? { ...rule, ...patch } : rule))
      )
    );

  return (
    <div className="form-group">
      <label>{t('ai_providers.request_scoped_errors_label')}</label>
      <div className={styles.sectionHint}>{t('ai_providers.request_scoped_errors_hint')}</div>
      {rules.map((rule, index) => (
        <fieldset key={index} disabled={disabled} className="form-group">
          <legend>{t('ai_providers.error_rule_number', { number: index + 1 })}</legend>
          <Input
            label={t('ai_providers.error_rule_status')}
            hint={t('ai_providers.error_rule_status_hint')}
            type="number"
            min={100}
            max={599}
            step={1}
            value={rule.status ?? 429}
            onChange={(event) =>
              update(index, { status: event.target.value === '' ? 0 : Number(event.target.value) })
            }
          />
          <div className="form-group">
            <label>
              {t('ai_providers.error_rule_match')}
              <textarea
                className="input"
                rows={3}
                value={(rule.match ?? []).join('\n')}
                onChange={(event) => update(index, { match: event.target.value.split('\n') })}
              />
            </label>
            <div className={styles.sectionHint}>{t('ai_providers.error_rule_match_hint')}</div>
          </div>
          <div className="form-group">
            <label>
              {t('ai_providers.error_rule_regex')}
              <textarea
                className="input"
                rows={2}
                value={(rule.matchRegexr ?? []).join('\n')}
                onChange={(event) => update(index, { matchRegexr: event.target.value.split('\n') })}
                spellCheck={false}
              />
            </label>
          </div>
          <div className="form-group">
            <label>{t('ai_providers.error_rule_action')}</label>
            <Select
              value={rule.action ?? 'stop'}
              options={actions.map((action) => ({
                value: action,
                label: t(`ai_providers.error_rule_action_${action}`),
              }))}
              onChange={(action) => update(index, { action })}
              disabled={disabled}
              ariaLabel={t('ai_providers.error_rule_action')}
            />
          </div>
          <Button
            variant="danger"
            size="sm"
            onClick={() => onChange(formatRequestScopedErrors(rules.filter((_, i) => i !== index)))}
            disabled={disabled}
          >
            {t('ai_providers.error_rule_remove')}
          </Button>
        </fieldset>
      ))}
      {validationError ? (
        <div className="error-box" role="alert">
          {t(`ai_providers.request_scoped_errors_${validationError}`)}
        </div>
      ) : null}
      <Button
        variant="secondary"
        size="sm"
        disabled={disabled || Boolean(parsed.error)}
        onClick={() =>
          onChange(
            formatRequestScopedErrors([...rules, { status: 429, action: 'continue-and-cooldown' }])
          )
        }
      >
        {t('ai_providers.error_rule_add')}
      </Button>
    </div>
  );
}
