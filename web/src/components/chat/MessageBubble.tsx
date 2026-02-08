import ReactMarkdown from 'react-markdown';
import { Bot, User, Copy, Check } from 'lucide-react';
import { useState } from 'react';
import type { Message } from '../../types';

interface MessageBubbleProps {
  message: Message;
}

export default function MessageBubble({ message }: MessageBubbleProps) {
  const [copied, setCopied] = useState(false);
  const isUser = message.role === 'user';

  const handleCopy = async () => {
    await navigator.clipboard.writeText(message.content);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div className={`flex gap-3 py-4 px-4 ${isUser ? '' : 'bg-gray-50'}`}>
      {/* Avatar */}
      <div className={`shrink-0 w-8 h-8 rounded-full flex items-center justify-center ${
        isUser ? 'bg-indigo-100 text-indigo-600' : 'bg-emerald-100 text-emerald-600'
      }`}>
        {isUser ? <User size={16} /> : <Bot size={16} />}
      </div>

      {/* Content */}
      <div className="flex-1 min-w-0">
        <div className="text-xs text-gray-400 mb-1">
          {isUser ? '你' : 'AI 法律助手'}
        </div>

        {isUser ? (
          <div className="text-gray-800 whitespace-pre-wrap leading-relaxed">
            {message.content}
          </div>
        ) : (
          <div className="markdown-content text-gray-800">
            <ReactMarkdown>{message.content}</ReactMarkdown>
          </div>
        )}

        {/* File attachments */}
        {message.file_ids && message.file_ids.length > 0 && (
          <div className="mt-2 flex flex-wrap gap-2">
            {message.file_ids.map(fid => (
              <span key={fid} className="inline-flex items-center gap-1 px-2 py-1 bg-blue-50 text-blue-600 text-xs rounded-md">
                📎 附件
              </span>
            ))}
          </div>
        )}

        {/* Actions for assistant messages */}
        {!isUser && (
          <div className="mt-2 flex gap-2">
            <button
              onClick={handleCopy}
              className="inline-flex items-center gap-1 px-2 py-1 text-xs text-gray-400 hover:text-gray-600 hover:bg-gray-100 rounded transition-colors"
            >
              {copied ? <Check size={12} /> : <Copy size={12} />}
              {copied ? '已复制' : '复制'}
            </button>
          </div>
        )}
      </div>
    </div>
  );
}
