import { useLayoutEffect, useRef } from 'react';
import ReactMarkdown from 'react-markdown';
import { Bot } from 'lucide-react';
import type { Message } from '../../types';
import MessageBubble from './MessageBubble';

/** Within this distance of the bottom the list follows new content; further up the user is reading. */
const STICK_THRESHOLD_PX = 160;

interface MessageListProps {
  /** Identifies the conversation on screen; changing it re-pins the list to the bottom without animation. */
  conversationKey: string;
  messages: Message[];
  streamingContent: string;
  isStreaming: boolean;
}

export default function MessageList({ conversationKey, messages, streamingContent, isStreaming }: MessageListProps) {
  const scrollerRef = useRef<HTMLDivElement>(null);
  /** Set when the conversation changes; consumed by the first render that has content to show. */
  const pinPendingRef = useRef(true);
  const keyRef = useRef(conversationKey);
  const countRef = useRef(0);

  // Runs before paint, so a switched conversation is already at the bottom on its first frame
  // (a smooth scroll here is what made every switch visibly scroll down from the top).
  useLayoutEffect(() => {
    const el = scrollerRef.current;
    if (!el) return;

    if (keyRef.current !== conversationKey) {
      keyRef.current = conversationKey;
      pinPendingRef.current = true;
      countRef.current = 0;
    }
    if (pinPendingRef.current) {
      if (messages.length === 0) return; // history still loading: wait for it
      el.scrollTop = el.scrollHeight;
      pinPendingRef.current = false;
      countRef.current = messages.length;
      return;
    }

    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight <= STICK_THRESHOLD_PX;
    const appended = messages.length > countRef.current;
    countRef.current = messages.length;
    if (!nearBottom) return;
    // Animate whole new messages; follow streaming tokens instantly so it does not lag or jitter.
    el.scrollTo({ top: el.scrollHeight, behavior: appended && !streamingContent ? 'smooth' : 'auto' });
  }, [conversationKey, messages, streamingContent]);

  return (
    <div ref={scrollerRef} className="flex-1 overflow-y-auto">
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

    </div>
  );
}
