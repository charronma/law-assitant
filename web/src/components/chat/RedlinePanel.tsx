import { useEffect, useState } from 'react';
import { FileDiff, Loader2, ChevronDown, ChevronUp, X } from 'lucide-react';
import { getFileInfo } from '../../services/api';
import type { RedlineView } from '../../hooks/useRedlineJobs';
import type { FileInfo, RedlineResult } from '../../types';

interface RedlinePanelProps {
  /** Files attached anywhere in the conversation, oldest first. */
  fileIds: string[];
  /** The user's original request, offered as the starting instruction. */
  suggestedInstruction: string;
  /** This conversation's job, if any. The panel is keyed by conversation, so it never shows another's. */
  job: RedlineView | undefined;
  onStart: (fileId: string, instruction: string) => void;
  onCancel: () => void;
  onDownload: () => void;
  onDismissError: () => void;
}

/**
 * Offers a Word version of an uploaded contract with the AI's changes written in as
 * tracked changes and comments, so the original formatting and revision history are kept.
 */
export default function RedlinePanel({
  fileIds,
  suggestedInstruction,
  job,
  onStart,
  onCancel,
  onDownload,
  onDismissError,
}: RedlinePanelProps) {
  const [files, setFiles] = useState<FileInfo[]>([]);
  const [open, setOpen] = useState(false);
  const [fileId, setFileId] = useState('');
  const [instruction, setInstruction] = useState('');
  const [edited, setEdited] = useState(false);
  const [now, setNow] = useState(() => Date.now());

  // Tick once a second while a job runs, to show the elapsed time.
  const running = job?.phase === 'running';
  useEffect(() => {
    if (!running) return;
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, [running]);

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

  // A running or finished job for this conversation keeps the panel visible even before
  // the file names have loaded.
  if (files.length === 0 && !job) return null;
  const current = files.find(f => f.id === fileId) ?? files[files.length - 1];
  const download = job?.phase === 'running' ? job.download : null;
  const elapsed = job?.phase === 'running' ? Math.max(0, Math.floor((now - job.startedAt) / 1000)) : 0;

  return (
    <div data-testid="redline-panel" className="mx-4 mb-2 rounded-xl border border-indigo-200 bg-indigo-50/60 text-sm">
      <button
        type="button"
        onClick={() => setOpen(o => !o)}
        className="flex w-full items-center gap-2 px-3 py-2 text-left font-medium text-indigo-800"
        aria-expanded={open}
      >
        <FileDiff size={16} className="shrink-0" />
        <span className="flex-1">
          生成修订版 Word：在原文件上标出修改并附批注
          {running && <span className="ml-2 text-xs font-normal text-indigo-600">（进行中）</span>}
        </span>
        {open ? <ChevronUp size={16} /> : <ChevronDown size={16} />}
      </button>

      {open && (
        <div className="space-y-2 border-t border-indigo-200 px-3 py-3">
          {files.length > 1 && (
            <label className="block text-xs text-gray-600">
              文件
              <select
                value={current?.id ?? ''}
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
          {files.length === 1 && current && <p className="text-xs text-gray-600">文件：{current.filename}</p>}

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
              onClick={() => current && onStart(current.id, effectiveInstruction)}
              disabled={running || !current}
              className="inline-flex items-center gap-1.5 rounded-lg bg-indigo-600 px-3 py-1.5 font-medium text-white hover:bg-indigo-700 disabled:opacity-60"
            >
              {running && <Loader2 size={14} className="animate-spin" />}
              {running ? (download === null ? '正在审阅合同…' : `正在下载 ${Math.round(download * 100)}%`) : '生成并下载'}
            </button>
            {job?.phase === 'running' && (
              <button type="button" onClick={onCancel} className="text-xs text-gray-500 underline hover:text-gray-700">
                取消
              </button>
            )}
            {job?.phase === 'running' && download === null && (
              <span data-testid="redline-elapsed" className="text-xs text-gray-500">
                {job.applying
                  ? '正在把修改写入文档'
                  : job.edits > 0
                    ? `AI 已提出 ${job.edits} 处修改，仍在审阅`
                    : 'AI 正在阅读合同（通常需要 30–90 秒）'}
                {' · '}已用时 {elapsed} 秒
              </span>
            )}
          </div>
          {job?.phase === 'running' && download !== null && (
            <div
              role="progressbar"
              aria-label="修订版下载进度"
              aria-valuemin={0}
              aria-valuemax={100}
              aria-valuenow={Math.round(download * 100)}
              className="h-1.5 w-full overflow-hidden rounded-full bg-gray-200"
            >
              <div className="h-full rounded-full bg-indigo-500 transition-[width] duration-150" style={{ width: `${Math.round(download * 100)}%` }} />
            </div>
          )}

          {job?.phase === 'error' && (
            <div role="alert" data-testid="redline-error" className="flex items-start justify-between gap-2 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700">
              <span>{job.message}</span>
              <button type="button" onClick={onDismissError} aria-label="关闭" className="shrink-0">
                <X size={12} />
              </button>
            </div>
          )}

          {job?.phase === 'done' && <Result result={job.result} downloaded={job.downloaded} onDownload={onDownload} />}
        </div>
      )}
    </div>
  );
}

function Result({ result, downloaded, onDownload }: { result: RedlineResult; downloaded: boolean; onDownload: () => void }) {
  const [showSkipped, setShowSkipped] = useState(false);
  const none = !result.docx_base64;
  return (
    <div data-testid="redline-result" className="space-y-2 rounded-lg border border-gray-200 bg-white px-3 py-2 text-xs text-gray-700">
      {none ? (
        <p className="font-medium text-gray-800">
          没有生成文件：{result.applied.length === 0 && result.skipped.length > 0 ? '模型提出的修改都无法写入文档。' : '模型认为无需修改。'}
        </p>
      ) : (
        <>
          <p className="font-medium text-green-700">
            {downloaded ? '已生成并下载' : '已生成（你离开时完成的）'}《{result.filename}》：写入 {result.applied.length} 处修订/批注
            {result.skipped.length > 0 && `，${result.skipped.length} 处未能写入`}。请在 Word 的“审阅”中逐条接受或拒绝。
          </p>
          <button type="button" onClick={onDownload} className="rounded-md border border-indigo-300 px-2 py-1 font-medium text-indigo-700 hover:bg-indigo-50">
            {downloaded ? '再次下载修订版' : '下载修订版'}
          </button>
        </>
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
