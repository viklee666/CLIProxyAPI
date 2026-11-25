import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Drawer } from '@/components/ui/Drawer';
import { IconBinary, IconCheck, IconRefreshCw, IconX } from '@/components/ui/icons';
import type { ProviderRow } from '../ProviderTable/rowData';
import {
  buildPelicanTestTargets,
  pelicanPrompt,
  pelicanStrengths,
  runPelicanTest,
  type PelicanStrength,
  type PelicanTestResult,
  type PelicanTestStatus,
  type PelicanTestTarget,
} from './pelicanTest';
import styles from './PelicanTestDrawer.module.scss';

interface PelicanTestDrawerProps {
  open: boolean;
  rows: ProviderRow[];
  actionsDisabled: boolean;
  onClose: () => void;
}

const statusClassName = (status: PelicanTestStatus) =>
  `${styles.status} ${status === 'success' ? styles.success : status === 'error' ? styles.error : ''}`;

export function PelicanTestDrawer({ open, rows, actionsDisabled, onClose }: PelicanTestDrawerProps) {
  const { t } = useTranslation();
  const [targets, setTargets] = useState<PelicanTestTarget[]>([]);
  const [model, setModel] = useState('');
  const [strength, setStrength] = useState<PelicanStrength>('medium');
  const [results, setResults] = useState<Map<string, PelicanTestResult>>(new Map());
  const [running, setRunning] = useState(false);

  useEffect(() => {
    if (!open) {
      setRunning(false);
      return;
    }
    const nextTargets = buildPelicanTestTargets(rows);
    setTargets(nextTargets);
    setModel(nextTargets.flatMap((target) => target.modelNames)[0] ?? '');
    setResults(new Map());
  }, [open, rows]);

  const modelOptions = useMemo(
    () => Array.from(new Set(targets.flatMap((target) => target.modelNames))).filter(Boolean),
    [targets]
  );

  const run = async () => {
    const selectedModel = model.trim();
    if (!selectedModel || running || actionsDisabled || targets.length === 0) return;
    setRunning(true);
    const initial = new Map<string, PelicanTestResult>();
    targets.forEach((target) => initial.set(target.id, { id: target.id, status: 'pending', model: selectedModel }));
    setResults(initial);
    for (const target of targets) {
      setResults((current) => new Map(current).set(target.id, { id: target.id, status: 'running', model: selectedModel }));
      try {
        const result = await runPelicanTest(target, selectedModel, strength);
        setResults((current) => new Map(current).set(target.id, result));
      } catch (error) {
        setResults((current) => new Map(current).set(target.id, {
          id: target.id,
          status: 'error',
          model: selectedModel,
          durationMs: undefined,
          message: error instanceof Error ? error.message : String(error),
        }));
      }
    }
    setRunning(false);
  };

  const footer = (
    <>
      <Button variant="secondary" size="sm" onClick={onClose} disabled={running}>
        {t('ai_providers.pelican_close')}
      </Button>
      <Button size="sm" onClick={() => void run()} loading={running} disabled={actionsDisabled || running || !model.trim() || targets.length === 0}>
        <IconRefreshCw size={14} />
        {t('ai_providers.pelican_run')}
      </Button>
    </>
  );

  return (
    <Drawer open={open} onClose={onClose} width={680} footer={footer} title={<><IconBinary size={18} />{t('ai_providers.pelican_title')}</>}>
      <div className={styles.root}>
        <p className={styles.description}>{t('ai_providers.pelican_description')}</p>
        <div className={styles.controls}>
          <label>
            <span>{t('ai_providers.pelican_model')}</span>
            <input list="pelican-model-options" value={model} onChange={(event) => setModel(event.target.value)} placeholder={t('ai_providers.pelican_model_placeholder')} disabled={running} />
            <datalist id="pelican-model-options">{modelOptions.map((option) => <option key={option} value={option} />)}</datalist>
          </label>
          <label>
            <span>{t('ai_providers.pelican_strength')}</span>
            <select value={strength} onChange={(event) => setStrength(event.target.value as PelicanStrength)} disabled={running}>
              {pelicanStrengths.map((value) => <option key={value} value={value}>{t(`ai_providers.pelican_strength_${value}`)}</option>)}
            </select>
          </label>
        </div>
        <div className={styles.prompt}><strong>{t('ai_providers.pelican_prompt')}</strong><code>{pelicanPrompt}</code></div>
        <div className={styles.list}>
          {targets.length === 0 ? <div className={styles.empty}>{t('ai_providers.pelican_empty')}</div> : targets.map((target) => {
            const result = results.get(target.id);
            return <div key={target.id} className={styles.row}>
              <div className={styles.icon}>{result?.status === 'success' && result.passed ? <IconCheck size={15} /> : result?.status === 'error' || (result?.status === 'success' && !result.passed) ? <IconX size={15} /> : <span />}</div>
              <div className={styles.main}><strong>{target.label}</strong><small>{target.subtitle || target.providerKind}</small></div>
              {result && <div className={statusClassName(result.status)}>{result.status === 'success' ? (result.passed ? t('ai_providers.pelican_passed') : t('ai_providers.pelican_wrong_answer')) : result.status === 'error' ? result.message : t('ai_providers.pelican_running')}</div>}
              {result?.durationMs !== undefined && <small className={styles.duration}>{result.durationMs} ms</small>}
            </div>;
          })}
        </div>
      </div>
    </Drawer>
  );
}
