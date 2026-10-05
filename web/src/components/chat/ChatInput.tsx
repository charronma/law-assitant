import { useState, useRef, useCallback } from 'react';
import { Send, Paperclip, Square, X } from 'lucide-react';
import { uploadFile } from '../../services/api';
import type { ModuleType } from '../../types';

interface ChatInputProps {
  onSend: (message: string, fileIds?: string[]) => void;
  onStop: () => void;
  isStreaming: boolean;
  module: ModuleType;
  sessionId: string | null;
}

const FILE_MODULES: ModuleType[] = ['contract', 'evidence_org'];

export default function ChatInput({ onSend, onStop, isStreaming, module, sessionId }: ChatInputProps) {
  const [input, setInput] = useState('');
  const [uploadedFiles, setUploadedFiles] = useState<{ id: string; name: string; chars: number; truncated: boolean }[]>([]);
  const [isUploading, setIsUploading] = useState(false);
  const [uploadError, setUploadError] = useState<string | null>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const showUpload = FILE_MODULES.includes(module);

  const handleSubmit = useCallback(() => {
    const text = input.trim();
    if (!text || isStreaming) return;
    const fileIds = uploadedFiles.map(f => f.id);
    onSend(text, fileIds.length > 0 ? fileIds : undefined);
    setInput('');
    setUploadedFiles([]);
  }, [input, isStreaming, uploadedFiles, onSend]);

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSubmit();
    }
  };

  const handleFileUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;

    setIsUploading(true);
    setUploadError(null);
    try {
      const result = await uploadFile(file, sessionId || undefined);
      setUploadedFiles(prev => [
        ...prev,
        { id: result.file_id, name: result.filename, chars: result.chars, truncated: result.truncated },
      ]);
    } catch (err) {
      setUploadError(err instanceof Error ? err.message : '文件上传失败，请重试');
    } finally {
      setIsUploading(false);
      if (fileInputRef.current) fileInputRef.current.value = '';
    }
  };

  const removeFile = (fileId: string) => {
    setUploadedFiles(prev => prev.filter(f => f.id !== fileId));
  };

  // Auto-resize textarea
  const handleInput = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    setInput(e.target.value);
    const el = e.target;
    el.style.height = 'auto';
    el.style.height = Math.min(el.scrollHeight, 160) + 'px';
  };

  return (
    <div className="border-t border-gray-200 bg-white p-4">
      {uploadError && (
        <div
          role="alert"
          data-testid="upload-error"
          className="flex items-start justify-between gap-2 mb-2 px-3 py-2 bg-red-50 border border-red-200 text-red-700 text-xs rounded-md"
        >
          <span>{uploadError}</span>
          <button onClick={() => setUploadError(null)} aria-label="关闭" className="shrink-0 hover:text-red-900">
            <X size={12} />
          </button>
        </div>
      )}

      {/* Uploaded files */}
      {uploadedFiles.length > 0 && (
        <div className="flex flex-wrap gap-2 mb-2">
          {uploadedFiles.map(f => (
            <span
              key={f.id}
              data-testid="file-chip"
              className="inline-flex items-center gap-1 px-2 py-1 bg-blue-50 text-blue-700 text-xs rounded-md"
            >
              📎 {f.name}
              <span className={f.truncated ? 'text-amber-600' : 'text-blue-500'}>
                · 已提取 {f.chars.toLocaleString()} 字{f.truncated ? '（内容过长，已截断）' : ''}
              </span>
              <button onClick={() => removeFile(f.id)} className="hover:text-red-500">
                <X size={12} />
              </button>
            </span>
          ))}
        </div>
      )}

      <div className="flex items-end gap-2">
        {/* File upload button */}
        {showUpload && (
          <>
            <button
              onClick={() => fileInputRef.current?.click()}
              disabled={isUploading}
              className="shrink-0 p-2 text-gray-400 hover:text-indigo-500 hover:bg-gray-100 rounded-lg transition-colors disabled:opacity-50"
              title="上传文件 (Word/PDF)"
            >
              <Paperclip size={20} />
            </button>
            <input
              ref={fileInputRef}
              type="file"
              accept=".docx,.pdf,.txt,.md"
              onChange={handleFileUpload}
              className="hidden"
            />
          </>
        )}

        {/* Text input */}
        <textarea
          ref={textareaRef}
          value={input}
          onChange={handleInput}
          onKeyDown={handleKeyDown}
          placeholder="输入您的法律问题..."
          rows={1}
          className="flex-1 resize-none border border-gray-300 rounded-xl px-4 py-2.5 text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
          disabled={isStreaming}
        />

        {/* Send / Stop button */}
        {isStreaming ? (
          <button
            onClick={onStop}
            className="shrink-0 p-2.5 bg-red-500 text-white rounded-xl hover:bg-red-600 transition-colors"
            title="停止生成"
          >
            <Square size={18} />
          </button>
        ) : (
          <button
            onClick={handleSubmit}
            disabled={!input.trim()}
            className="shrink-0 p-2.5 bg-indigo-600 text-white rounded-xl hover:bg-indigo-700 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
            title="发送"
          >
            <Send size={18} />
          </button>
        )}
      </div>

      {isUploading && (
        <p className="text-xs text-gray-400 mt-1">文件上传中...</p>
      )}
    </div>
  );
}
