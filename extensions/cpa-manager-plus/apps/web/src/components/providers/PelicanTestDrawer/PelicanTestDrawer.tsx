import { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Drawer } from '@/components/ui/Drawer';
import { IconBinary, IconRefreshCw } from '@/components/ui/icons';
import { authFilesApi } from '@/services/api/authFiles';
import {
  pelicanPrompt,
  pelicanStrengths,
  runPelicanTest,
  type PelicanStrength,
  type PelicanTestResult,
  type PelicanTestTarget,
} from './pelicanTest';
import { buildPelicanPreview } from './pelicanPreview';
import styles from './PelicanTestDrawer.module.scss';

interface PelicanTestDrawerProps {
  open: boolean;
  targets: PelicanTestTarget[];
  actionsDisabled: boolean;
  onClose: () => void;
}

function PelicanArtifact({ answer }: { answer: string }) {
  const { t } = useTranslation();
  const preview = useMemo(() => buildPelicanPreview(answer), [answer]);
  return <div className={styles.artifact}>
    {preview && <details>
      <summary>{t('ai_providers.pelican_preview')}</summary>
      <iframe title={t('ai_providers.pelican_preview')} sandbox="" referrerPolicy="no-referrer" srcDoc={preview} className={styles.preview} />
    </details>}
    <details open={!preview}>
      <summary>{t('ai_providers.pelican_source')}</summary>
      <pre className={styles.answer}>{answer}</pre>
    </details>
  </div>;
}

export function PelicanTestDrawer({ open, targets, actionsDisabled, onClose }: PelicanTestDrawerProps) {
  const { t } = useTranslation();
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [models, setModels] = useState<Record<string, string>>({});
  const [modelOptions, setModelOptions] = useState<Record<string, string[]>>({});
  const [modelErrors, setModelErrors] = useState<Record<string, string>>({});
  const [search, setSearch] = useState('');
  const [strength, setStrength] = useState<PelicanStrength>('medium');
  const [prompt, setPrompt] = useState(pelicanPrompt);
  const [results, setResults] = useState<Map<string, PelicanTestResult>>(new Map());
  const [running, setRunning] = useState(false);
  const controllerRef = useRef<AbortController | null>(null);
  const runIdRef = useRef(0);

  useEffect(() => {
    if (!open) return;
    let disposed = false;
    setSelected(new Set(targets.map((target) => target.id)));
    setModels(Object.fromEntries(targets.map((target) => [target.id, target.modelNames[0] ?? ''])));
    setModelOptions(Object.fromEntries(targets.map((target) => [target.id, target.modelNames])));
    setModelErrors({});
    setSearch('');
    setPrompt(pelicanPrompt);
    setResults(new Map());
    setRunning(false);
    // Model discovery is read-only and sequential; the operator can type a model
    // immediately, and a late discovery result never overwrites their choice.
    void (async () => {
      for (const target of targets) {
        if (disposed) return;
        if (!target.name || target.modelNames.length) continue;
        try {
          const names = (await authFilesApi.getModelsForAuthFile(target.name)).map((model) => model.id);
          if (disposed) return;
          setModelOptions((current) => ({ ...current, [target.id]: names }));
          setModels((current) => ({ ...current, [target.id]: current[target.id] || names[0] || '' }));
        } catch (cause) {
          if (disposed) return;
          setModelErrors((current) => ({ ...current, [target.id]: cause instanceof Error ? cause.message : String(cause) }));
        }
      }
    })();
    return () => {
      disposed = true;
      runIdRef.current += 1;
      controllerRef.current?.abort();
      controllerRef.current = null;
    };
  }, [open, targets]);

  const visibleTargets = useMemo(() => {
    const query = search.trim().toLowerCase();
    return targets.filter((target) => `${target.label} ${target.subtitle}`.toLowerCase().includes(query));
  }, [search, targets]);
  const selectedTargets = targets.filter((target) => selected.has(target.id));
  const allVisibleSelected = visibleTargets.length > 0 && visibleTargets.every((target) => selected.has(target.id));
  const canRun = !actionsDisabled && !running && !!prompt.trim() && selectedTargets.length > 0 &&
    selectedTargets.every((target) => !!models[target.id]?.trim());

  const cancel = () => {
    runIdRef.current += 1;
    controllerRef.current?.abort();
    controllerRef.current = null;
    setRunning(false);
    setResults((current) => new Map(Array.from(current, ([id, result]) => [id,
      result.status === 'pending' || result.status === 'running' ? { ...result, status: 'cancelled' as const } : result,
    ])));
  };
  const close = () => { cancel(); onClose(); };

  const run = async () => {
    if (!canRun || controllerRef.current) return;
    const runID = ++runIdRef.current;
    const controller = new AbortController();
    controllerRef.current = controller;
    setRunning(true);
    const jobs = selectedTargets.map((target) => ({ target, model: models[target.id].trim() }));
    setResults(new Map(jobs.map(({ target, model }) => [target.id, { id: target.id, status: 'pending', model }])));
    for (const { target, model } of jobs) {
      if (controller.signal.aborted || runID !== runIdRef.current) break;
      setResults((current) => new Map(current).set(target.id, { id: target.id, status: 'running', model }));
      const started = Date.now();
      try {
        const result = await runPelicanTest(target, model, strength, prompt, controller.signal);
        if (runID === runIdRef.current) setResults((current) => new Map(current).set(target.id, result));
      } catch (cause) {
        if (controller.signal.aborted || runID !== runIdRef.current) break;
        setResults((current) => new Map(current).set(target.id, {
          id: target.id, status: 'error', model, durationMs: Date.now() - started,
          message: cause instanceof Error ? cause.message : String(cause),
        }));
      }
    }
    if (runID === runIdRef.current) {
      setRunning(false);
      controllerRef.current = null;
    }
  };

  return <Drawer open={open} onClose={close} width={860}
    title={<><IconBinary size={18} />{t('ai_providers.pelican_title')}</>}
    footer={<>
      <Button variant="secondary" size="sm" onClick={close}>{t('ai_providers.pelican_close')}</Button>
      {running && <Button variant="secondary" size="sm" onClick={cancel}>{t('ai_providers.pelican_cancel')}</Button>}
      <Button size="sm" onClick={() => void run()} loading={running} disabled={!canRun}>
        <IconRefreshCw size={14} />{t('ai_providers.pelican_run')} ({selectedTargets.length})
      </Button>
    </>}>
    <div className={styles.root}>
      <p className={styles.description}>{t('ai_providers.pelican_description')}</p>
      <p className={styles.warning}>{t('ai_providers.pelican_warning')}</p>
      <label className={styles.prompt}>
        <span>{t('ai_providers.pelican_prompt')}</span>
        <textarea value={prompt} maxLength={100000} rows={4} disabled={running} onChange={(event) => setPrompt(event.target.value)} />
      </label>
      <div className={styles.controls}>
        <label><span>{t('ai_providers.pelican_search')}</span>
          <input type="search" value={search} onChange={(event) => setSearch(event.target.value)} />
        </label>
        <label><span>{t('ai_providers.pelican_strength')}</span>
          <select value={strength} disabled={running} onChange={(event) => setStrength(event.target.value as PelicanStrength)}>
            {pelicanStrengths.map((value) => <option key={value} value={value}>{t(`ai_providers.pelican_strength_${value}`)}</option>)}
          </select>
        </label>
      </div>
      <label className={styles.selection}>
        <input type="checkbox" checked={allVisibleSelected} disabled={running || !visibleTargets.length}
          onChange={() => setSelected((current) => {
            const next = new Set(current);
            visibleTargets.forEach((target) => allVisibleSelected ? next.delete(target.id) : next.add(target.id));
            return next;
          })} />
        {t('ai_providers.pelican_select_all')} ({selectedTargets.length}/{targets.length})
      </label>
      <div className={styles.list}>
        {!visibleTargets.length && <div className={styles.empty}>{t('ai_providers.pelican_empty')}</div>}
        {visibleTargets.map((target, index) => {
          const result = results.get(target.id);
          const optionsID = `pelican-model-${index}`;
          return <section key={target.id} className={styles.row}>
            <label className={styles.selection}>
              <input type="checkbox" checked={selected.has(target.id)} disabled={running}
                onChange={() => setSelected((current) => {
                  const next = new Set(current);
                  if (next.has(target.id)) next.delete(target.id); else next.add(target.id);
                  return next;
                })} />
              <span className={styles.main}><strong>{target.label}</strong><small>{target.subtitle}</small></span>
            </label>
            <label className={styles.model}>
              <span>{t('ai_providers.pelican_model')}</span>
              <input list={optionsID} value={models[target.id] ?? ''} disabled={running} maxLength={512}
                placeholder={t('ai_providers.pelican_model_placeholder')}
                onChange={(event) => setModels((current) => ({ ...current, [target.id]: event.target.value }))} />
              <datalist id={optionsID}>{(modelOptions[target.id] ?? []).map((model) => <option key={model} value={model} />)}</datalist>
            </label>
            {modelErrors[target.id] && <small className={styles.error}>{modelErrors[target.id]}</small>}
            {result && <div className={result.status === 'error' ? styles.error : styles.status} role="status">
              {t(`ai_providers.pelican_status_${result.status}`)} · {result.model}
              {result.durationMs !== undefined && <> · {result.durationMs} ms</>}
              {result.message && <p>{result.message}</p>}
            </div>}
            {result?.answer && <PelicanArtifact answer={result.answer} />}
          </section>;
        })}
      </div>
    </div>
  </Drawer>;
}
