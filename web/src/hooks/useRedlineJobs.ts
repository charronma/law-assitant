import { useCallback, useEffect, useRef, useState } from 'react';
import { ApiError, cancelRedline, getRedlineStatus, startRedline } from '../services/api';
import { base64ToBlob, saveBlob } from '../lib/download';
import type { RedlineResult, RedlineStatus } from '../types';

const POLL_MS = 1500;
const DOCX_MIME = 'application/vnd.openxmlformats-officedocument.wordprocessingml.document';

/** What the panel shows for one conversation's revised-Word job. */
export type RedlineView =
  | { phase: 'running'; startedAt: number; edits: number; applying: boolean; download: number | null }
  | { phase: 'done'; result: RedlineResult; downloaded: boolean }
  | { phase: 'error'; message: string };

interface Options {
  /** Reports model failures (e.g. quota) so the picker can flag the model. */
  onModelError: (code: string | undefined, model: string | undefined) => void;
}

/**
 * Keeps one revised-Word job per conversation. The state lives above the panel, so a review
 * keeps running (and keeps being polled) while the user looks at another conversation, and
 * each conversation shows its own progress and result.
 */
export function useRedlineJobs({ onModelError }: Options) {
  const [jobs, setJobs] = useState<Record<string, RedlineView>>({});
  /** Conversation currently on screen; results are only downloaded automatically for it. */
  const viewingRef = useRef<string | null>(null);
  const runs = useRef<Record<string, { token: number; jobId?: string }>>({});
  const alive = useRef(true);
  const onModelErrorRef = useRef(onModelError);

  useEffect(() => {
    onModelErrorRef.current = onModelError;
  }, [onModelError]);
  useEffect(() => {
    alive.current = true;
    const current = runs.current;
    return () => {
      alive.current = false;
      for (const r of Object.values(current)) r.token += 1; // stop every poll loop
    };
  }, []);

  const setJob = useCallback((key: string, view: RedlineView | null) => {
    setJobs(prev => {
      const next = { ...prev };
      if (view === null) delete next[key];
      else next[key] = view;
      return next;
    });
  }, []);

  const setViewing = useCallback((key: string | null) => {
    viewingRef.current = key;
  }, []);

  const start = useCallback(
    async (key: string, fileId: string, instruction: string, model: string) => {
      const run = (runs.current[key] = { token: (runs.current[key]?.token ?? 0) + 1 } as { token: number; jobId?: string });
      const myToken = run.token;
      const live = () => alive.current && runs.current[key]?.token === myToken;
      const startedAt = Date.now();
      setJob(key, { phase: 'running', startedAt, edits: 0, applying: false, download: null });
      try {
        run.jobId = await startRedline(fileId, instruction, model);
        let failedPolls = 0;
        for (;;) {
          await new Promise(r => setTimeout(r, POLL_MS));
          if (!live()) return;
          let st: RedlineStatus;
          try {
            st = await getRedlineStatus(run.jobId, f => {
              if (live()) setJob(key, { phase: 'running', startedAt, edits: 0, applying: true, download: f });
            });
            failedPolls = 0;
          } catch (err) {
            // A lost job is final; a dropped connection is worth a few retries.
            if (err instanceof ApiError && err.code === 'JOB_LOST') throw err;
            if (++failedPolls >= 5) throw new Error('网络连接不稳定，无法获取进度，请检查网络后重试', { cause: err });
            continue;
          }
          if (!live()) return;
          if (st.state === 'running') {
            setJob(key, { phase: 'running', startedAt, edits: st.edits_found, applying: st.phase === 'applying', download: null });
          } else if (st.state === 'error') {
            throw new ApiError(st.error ?? { message: '生成失败，请重试' });
          } else if (st.result) {
            // Download straight away only if the user is looking at this conversation;
            // otherwise keep it for them to fetch when they come back.
            const watching = viewingRef.current === key;
            if (watching && st.result.docx_base64 && st.result.filename) {
              saveBlob(base64ToBlob(st.result.docx_base64, DOCX_MIME), st.result.filename);
            }
            setJob(key, { phase: 'done', result: st.result, downloaded: watching });
            return;
          }
        }
      } catch (err) {
        if (!live()) return;
        if (err instanceof ApiError) onModelErrorRef.current(err.code, err.model);
        setJob(key, { phase: 'error', message: err instanceof Error ? err.message : '生成失败，请重试' });
      }
    },
    [setJob],
  );

  const cancel = useCallback(
    (key: string) => {
      const run = runs.current[key];
      if (run) {
        run.token += 1; // abandon the poll loop
        if (run.jobId) void cancelRedline(run.jobId);
        run.jobId = undefined;
      }
      setJob(key, null);
    },
    [setJob],
  );

  const dismiss = useCallback((key: string) => setJob(key, null), [setJob]);

  /** Save the result of a finished job again (or for the first time). */
  const download = useCallback(
    (key: string) => {
      setJobs(prev => {
        const j = prev[key];
        if (j?.phase === 'done' && j.result.docx_base64 && j.result.filename) {
          saveBlob(base64ToBlob(j.result.docx_base64, DOCX_MIME), j.result.filename);
          return { ...prev, [key]: { ...j, downloaded: true } };
        }
        return prev;
      });
    },
    [],
  );

  return { jobs, start, cancel, dismiss, download, setViewing };
}
