import type { ModuleType } from '../../types';
import { useChat } from '../../hooks/useChat';
import MessageList from './MessageList';
import ChatInput from './ChatInput';
import { useEffect } from 'react';

const MODULE_NAMES: Record<ModuleType, string> = {
  consult: '法律咨询',
  pleading: '诉状撰写',
  contract: '合同优化',
  evidence_org: '证据整理',
  evidence: '取证指导',
  communication: '沟通话术',
};

interface ChatWindowProps {
  module: ModuleType;
  sessionId: string | null;
  onSessionCreated: (sessionId: string) => void;
  onNewChat: () => void;
}

export default function ChatWindow({ module, sessionId, onSessionCreated, onNewChat }: ChatWindowProps) {
  const {
    messages,
    streamingContent,
    isStreaming,
    error,
    sendMessage,
    stopStreaming,
    clearMessages,
    loadSession,
  } = useChat({ module, sessionId, onSessionCreated });

  // Load session when sessionId changes
  useEffect(() => {
    if (sessionId) {
      loadSession(sessionId);
    } else {
      clearMessages();
    }
  }, [sessionId, loadSession, clearMessages]);

  return (
    <div className="flex-1 flex flex-col h-full bg-white">
      {/* Header */}
      <div className="flex items-center justify-between px-6 py-3 border-b border-gray-200 bg-white">
        <h2 className="text-base font-semibold text-gray-800">{MODULE_NAMES[module]}</h2>
        <button
          onClick={onNewChat}
          className="text-sm text-indigo-600 hover:text-indigo-700 font-medium"
        >
          + 新建对话
        </button>
      </div>

      {/* Error banner */}
      {error && (
        <div className="mx-4 mt-2 px-4 py-2 bg-red-50 text-red-600 text-sm rounded-lg">
          {error}
        </div>
      )}

      {/* Messages */}
      <MessageList
        messages={messages}
        streamingContent={streamingContent}
        isStreaming={isStreaming}
      />

      {/* Input */}
      <ChatInput
        onSend={sendMessage}
        onStop={stopStreaming}
        isStreaming={isStreaming}
        module={module}
        sessionId={sessionId}
      />
    </div>
  );
}
