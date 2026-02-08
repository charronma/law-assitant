import { useEffect, useRef } from 'react';
import ReactMarkdown from 'react-markdown';
import { Bot } from 'lucide-react';
import type { Message } from '../../types';
import MessageBubble from './MessageBubble';

interface MessageListProps {
  messages: Message[];
  streamingContent: string;
  isStreaming: boolean;
}

export default function MessageList({ messages, streamingContent, isStreaming }: MessageListProps) {
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages, streamingContent]);

  return (
    <div className="flex-1 overflow-y-auto">
      {messages.length === 0 && !isStreaming && (
        <div className="flex flex-col items-center justify-center h-full text-gray-400">
          <Bot size={48} className="mb-4 text-indigo-300" />
          <p className="text-lg font-medium text-gray-500">您好，我是 AI 法律助手</p>
          <p className="text-sm mt-1">请输入您的法律问题，我将为您提供专业指导</p>
        </div>
      )}

      {messages.map(msg => (
        <MessageBubble key={msg.id} message={msg} />
      ))}

      {/* Streaming message */}
      {isStreaming && streamingContent && (
        <div className="flex gap-3 py-4 px-4 bg-gray-50">
          <div className="shrink-0 w-8 h-8 rounded-full flex items-center justify-center bg-emerald-100 text-emerald-600">
            <Bot size={16} />
          </div>
          <div className="flex-1 min-w-0">
            <div className="text-xs text-gray-400 mb-1">AI 法律助手</div>
            <div className="markdown-content text-gray-800 streaming-cursor">
              <ReactMarkdown>{streamingContent}</ReactMarkdown>
            </div>
          </div>
        </div>
      )}

      {/* Loading indicator */}
      {isStreaming && !streamingContent && (
        <div className="flex gap-3 py-4 px-4 bg-gray-50">
          <div className="shrink-0 w-8 h-8 rounded-full flex items-center justify-center bg-emerald-100 text-emerald-600">
            <Bot size={16} />
          </div>
          <div className="flex-1">
            <div className="text-xs text-gray-400 mb-1">AI 法律助手</div>
            <div className="flex items-center gap-1 text-gray-400">
              <span className="w-2 h-2 bg-gray-400 rounded-full animate-bounce" style={{ animationDelay: '0ms' }} />
              <span className="w-2 h-2 bg-gray-400 rounded-full animate-bounce" style={{ animationDelay: '150ms' }} />
              <span className="w-2 h-2 bg-gray-400 rounded-full animate-bounce" style={{ animationDelay: '300ms' }} />
            </div>
          </div>
        </div>
      )}

      <div ref={bottomRef} />
    </div>
  );
}
