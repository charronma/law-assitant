import { useEffect, useState } from 'react';
import { FileDiff, Loader2, ChevronDown, ChevronUp, X } from 'lucide-react';
import { ApiError, createRedline, getFileInfo } from '../../services/api';
import { base64ToBlob, saveBlob } from '../../lib/download';
import type { FileInfo, RedlineResult } from '../../types';

const DOCX_MIME = 'application/vnd.openxmlformats-officedocument.wordprocessingml.document';

interface RedlinePanelProps {
  /** Files attached anywhere in the conversation, oldest first. */
  fileIds: string[];
  /** The user's original request, offered as the starting instruction. */
  suggestedInstruction: string;
  /** Model picked in the header. */
  model: string;
  /** Reports model failures (e.g. quota) so the picker can flag the model. */
  onModelError: (code: string | undefined, model: string | undefined) => void;
}

type State =
  | { phase: 'idle' }
  | { phase: 'running' }
  | { phase: 'done'; result: RedlineResult }
  | { phase: 'error'; message: string };

/**
 * Offers a Word version of an uploaded contract with the AI's changes written in as
 * tracked changes and comments, so the original formatting and revision history are kept.
 */
export default function RedlinePanel({ fileIds, suggestedInstruction, model, onModelError }: RedlinePanelProps) {
  const [files, setFiles] = useState<FileInfo[]>([]);
  const [open, setOpen] = useState(false);
  const [fileId, setFileId] = useState('');
  const [instruction, setInstruction] = useState('');
  const [edited, setEdited] = useState(false);
  const [state, setState] = useState<State>({ phase: 'idle' });

  // Look up the names of the conversation's files; only .docx files qualify.
  const idsKey = fileIds.join(',');
  useEffect(() => {
    let alive = true;
    const ids = idsKey ? idsKey.split(',') : [];
    void Promise.all(ids.map(id => getFileInfo(id).catch(() => null))).then(infos => {
      if (!alive) return;
      const docx = infos.filter((f): f is FileInfo => f !== null && f.filename.toLowerCase().endsWith('.docx'));
      setFiles(docx);
      setFileId(prev => (docx.some(f => f.id === prev) ? prev : (docx[docx.length - 1]?.id ?? '')));
    });
    return () => {
      alive = false;
    };
  }, [idsKey]);

  // Start from the user's own request until they type their own.
  const effectiveInstruction = edited ? instruction : suggestedInstruction;

  if (files.length === 0) return null;
  const current = files.find(f => f.id === fileId) ?? files[files.length - 1];
  const running = state.phase === 'running';

  const run = async () => {
    setState({ phase: 'running' });
    try {
      const result = await createRedline(current.id, effectiveInstruction, model);
      if (result.docx_base64 && result.filename) {
        saveBlob(base64ToBlob(result.docx_base64, DOCX_MIME), result.filename);
      }
      setState({ phase: 'done', result });
    } catch (err) {
      if (err instanceof ApiError) onModelError(err.code, err.model);
      setState({ phase: 'error', message: err instanceof Error ? err.message : '生成失败，请重试' });
    }
  };

  return (
    <div data-testid="redline-panel" className="mx-4 mb-2 rounded-xl border border-indigo-200 bg-indigo-50/60 text-sm">
      <button
        type="button"
        onClick={() => setOpen(o => !o)}
        className="flex w-full items-center gap-2 px-3 py-2 text-left font-medium text-indigo-800"
        aria-expanded={open}
      >
        <FileDiff size={16} className="shrink-0" />
        <span className="flex-1">生成修订版 Word：在原文件上标出修改并附批注</span>
        {open ? <ChevronUp size={16} /> : <ChevronDown size={16} />}
      </button>

      {open && (
        <div className="space-y-2 border-t border-indigo-200 px-3 py-3">
          {files.length > 1 && (
            <label className="block text-xs text-gray-600">
              文件
              <select
                value={current.id}
                onChange={e => setFileId(e.target.value)}
                disabled={running}
                className="mt-1 block w-full rounded-lg border border-gray-300 bg-white px-2 py-1.5 text-sm"
              >
                {files.map(f => (
                  <option key={f.id} value={f.id}>
                    {f.filename}
                  </option>
                ))}
              </select>
            </label>
          )}
          {files.length === 1 && <p className="text-xs text-gray-600">文件：{current.filename}</p>}

          <label className="block text-xs text-gray-600">
            修改要求（说明你的立场）
            <textarea
              value={effectiveInstruction}
              onChange={e => {
                setEdited(true);
                setInstruction(e.target.value);
              }}
              disabled={running}
              rows={3}
              maxLength={2000}
              placeholder="例如：我是劳务派遣公司，站在公司立场，把不利于公司的条款标出来并修改"
              className="mt-1 block w-full resize-y rounded-lg border border-gray-300 bg-white px-2 py-1.5 text-sm"
            />
          </label>

          <div className="flex items-center gap-3">
            <button
              type="button"
              onClick={() => void run()}
              disabled={running}
              className="inline-flex items-center gap-1.5 rounded-lg bg-indigo-600 px-3 py-1.5 font-medium text-white hover:bg-indigo-700 disabled:opacity-60"
            >
              {running && <Loader2 size={14} className="animate-spin" />}
              {running ? '正在审阅合同…' : '生成并下载'}
            </button>
            {running && <span className="text-xs text-gray-500">通常需要 30–90 秒，请不要关闭页面</span>}
          </div>

          {state.phase === 'error' && (
            <div role="alert" data-testid="redline-error" className="flex items-start justify-between gap-2 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700">
              <span>{state.message}</span>
              <button type="button" onClick={() => setState({ phase: 'idle' })} aria-label="关闭" className="shrink-0">
                <X size={12} />
              </button>
            </div>
          )}

          {state.phase === 'done' && <Result result={state.result} />}
        </div>
      )}
    </div>
  );
}

function Result({ result }: { result: RedlineResult }) {
  const [showSkipped, setShowSkipped] = useState(false);
  const none = !result.docx_base64;
  return (
    <div data-testid="redline-result" className="space-y-2 rounded-lg border border-gray-200 bg-white px-3 py-2 text-xs text-gray-700">
      {none ? (
        <p className="font-medium text-gray-800">没有生成文件：{result.applied.length === 0 && result.skipped.length > 0 ? '模型提出的修改都无法写入文档。' : '模型认为无需修改。'}</p>
      ) : (
        <p className="font-medium text-green-700">
          已生成并下载《{result.filename}》：写入 {result.applied.length} 处修订/批注
          {result.skipped.length > 0 && `，${result.skipped.length} 处未能写入`}。请在 Word 的“审阅”中逐条接受或拒绝。
        </p>
      )}
      {result.summary && <p>{result.summary}</p>}
      {result.truncated && <p className="text-amber-700">文档较长，只审阅了前面的部分段落。</p>}
      {result.skipped.length > 0 && (
        <div>
          <button type="button" onClick={() => setShowSkipped(s => !s)} className="text-indigo-700 underline">
            {showSkipped ? '收起' : '查看'}未写入的 {result.skipped.length} 处
          </button>
          {showSkipped && (
            <ul className="mt-1 list-disc space-y-1 pl-5">
              {result.skipped.map((s, i) => (
                <li key={i}>
                  第 {s.para} 段{s.find ? `「${s.find}」` : ''}：{s.reason}
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  );
}
