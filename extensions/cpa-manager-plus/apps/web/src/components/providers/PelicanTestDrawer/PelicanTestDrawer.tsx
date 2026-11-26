import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Drawer } from '@/components/ui/Drawer';
import { IconBinary, IconRefreshCw } from '@/components/ui/icons';
import {
  candyPrompt,
  pelicanBenchmark,
  pelicanBenchmarks,
  pelicanPrompt,
  pelicanStrengths,
  resolvePelicanTestTarget,
  runPelicanTest,
  type PelicanBenchmark,
  type ResolvedPelicanTestTarget,
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

function PelicanArtifact({ answer, textOnly }: { answer: string; textOnly: boolean }) {
  const { t } = useTranslation();
  const preview = useMemo(() => textOnly ? null : buildPelicanPreview(answer), [answer, textOnly]);
  return <div className={styles.artifact}>
    {preview && <details open>
      <summary>{t('ai_providers.pelican_preview')}</summary>
      <iframe title={t('ai_providers.pelican_preview')} sandbox="allow-scripts" referrerPolicy="no-referrer" srcDoc={preview} className={styles.preview} />
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
  const [resolvedTargets, setResolvedTargets] = useState<Record<string, ResolvedPelicanTestTarget>>({});
  const [modelErrors, setModelErrors] = useState<Record<string, string>>({});
  const [search, setSearch] = useState('');
  const [strength, setStrength] = useState<PelicanStrength>('medium');
  const [prompt, setPrompt] = useState(pelicanPrompt);
  const [results, setResults] = useState<Map<string, PelicanTestResult>>(new Map());
  const [resultPrompt, setResultPrompt] = useState('');
  const [closing, setClosing] = useState(false);
  const [running, setRunning] = useState(false);
  const controllerRef = useRef<AbortController | null>(null);
  const discoveryRef = useRef<AbortController | null>(null);
  const onCloseRef = useRef(onClose);
  const runIdRef = useRef(0);
  useEffect(() => { onCloseRef.current = onClose; }, [onClose]);

  const cancel = useCallback(() => {
    runIdRef.current += 1;
    controllerRef.current?.abort();
    controllerRef.current = null;
    setRunning(false);
    setResults((current) => new Map(Array.from(current, ([id, result]) => [id,
      result.status === 'pending' || result.status === 'running' ? { ...result, status: 'cancelled' as const } : result,
    ])));
  }, []);
  const closeStart = useCallback(() => {
    setClosing(true);
    discoveryRef.current?.abort();
    cancel();
  }, [cancel]);
  const close = useCallback(() => { closeStart(); onCloseRef.current(); }, [closeStart]);

  useEffect(() => {
    if (!open) return;
    const discovery = new AbortController();
    discoveryRef.current = discovery;
    setClosing(false);
    setSelected(new Set(targets.map((target) => target.id)));
    setModels({});
    setResolvedTargets({});
    setModelErrors({});
    setSearch('');
    setPrompt(pelicanPrompt);
    setResults(new Map());
    setRunning(false);
    // Resolve identity and models together; configuration indices alone can collide.
    void (async () => {
      for (const target of targets) {
        if (discovery.signal.aborted) return;
        try {
          const resolved = await resolvePelicanTestTarget(target, discovery.signal);
          if (discovery.signal.aborted) return;
          setResolvedTargets((current) => ({ ...current, [target.id]: resolved }));
          setModels((current) => ({ ...current, [target.id]: current[target.id] || resolved.modelNames[0] || '' }));
        } catch (cause) {
          if (discovery.signal.aborted) return;
          setModelErrors((current) => ({ ...current, [target.id]: cause instanceof Error ? cause.message : String(cause) }));
        }
      }
    })();
    return () => {
      discovery.abort();
      discoveryRef.current = null;
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
  const canRun = open && !closing && !actionsDisabled && !running && !!prompt.trim() && selectedTargets.length > 0 &&
    selectedTargets.every((target) => resolvedTargets[target.id]?.modelNames.includes(models[target.id]));

  const run = async () => {
    if (!canRun || controllerRef.current) return;
    const runID = ++runIdRef.current;
    const controller = new AbortController();
    controllerRef.current = controller;
    setRunning(true);
    const jobs = selectedTargets.map((target) => ({ target: resolvedTargets[target.id], model: models[target.id] }));
    setResultPrompt(prompt);
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

  return <Drawer open={open} onCloseStart={closeStart} onClose={close} width={860}
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
        <span>{t('ai_providers.pelican_benchmark')}</span>
        <select aria-label={t('ai_providers.pelican_benchmark')} value={pelicanBenchmark(prompt)} disabled={running}
          onChange={(event) => setPrompt(pelicanBenchmarks[event.target.value as PelicanBenchmark])}>
          {Object.keys(pelicanBenchmarks).map((mode) => <option key={mode} value={mode}>{t(`ai_providers.pelican_benchmark_${mode}`)}</option>)}
        </select>
      </label>
      <label className={styles.prompt}>
        <span>{t('ai_providers.pelican_prompt')}</span>
        <textarea aria-label={t('ai_providers.pelican_prompt')} value={prompt} placeholder={t('ai_providers.pelican_custom_placeholder')} maxLength={100000} rows={pelicanBenchmark(prompt) === 'candy' ? 14 : 4} disabled={running} onChange={(event) => setPrompt(event.target.value)} />
      </label>
      {prompt === candyPrompt && <details open className={styles.reference}>
        <summary>{t('ai_providers.pelican_reference')}</summary>
        <p>{t('ai_providers.pelican_candy_reference')}</p>
        <small>{t('ai_providers.pelican_reference_ui_only')}</small>
      </details>}
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
        {visibleTargets.map((target) => {
          const result = results.get(target.id);
          const resolved = resolvedTargets[target.id];
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
              <select aria-label={`${target.label} · ${t('ai_providers.pelican_model')}`} value={models[target.id] ?? ''} disabled={running || !resolved}
                onChange={(event) => setModels((current) => ({ ...current, [target.id]: event.target.value }))}>
                <option value="" disabled>{t(resolved ? 'ai_providers.pelican_model_placeholder' : 'ai_providers.pelican_resolving')}</option>
                {(resolved?.modelNames ?? []).map((model) => <option key={model} value={model}>{model}</option>)}
              </select>
            </label>
            {resolved && !resolved.modelNames.length && <small className={styles.error}>{t('ai_providers.pelican_no_models')}</small>}
            {modelErrors[target.id] && <small className={styles.error}>{modelErrors[target.id]}</small>}
            {result && <div className={result.status === 'error' ? styles.error : styles.status} role="status">
              {t(`ai_providers.pelican_status_${result.status}`)} · {result.model}
              {result.durationMs !== undefined && <> · {result.durationMs} ms</>}
              {result.message && <p>{result.message}</p>}
            </div>}
            {result?.answer && <PelicanArtifact answer={result.answer} textOnly={resultPrompt === candyPrompt} />}
          </section>;
        })}
      </div>
    </div>
  </Drawer>;
}
