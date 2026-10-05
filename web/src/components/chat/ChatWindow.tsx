import { useCallback, useEffect } from 'react';
import type { ChatError, ModuleType } from '../../types';
import { useChat } from '../../hooks/useChat';
import { useModels } from '../../hooks/useModels';
import MessageList from './MessageList';
import ChatInput from './ChatInput';
import ModelSelect from './ModelSelect';
import ErrorBanner from './ErrorBanner';

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
  const { models, selected, select, exhausted, markExhausted } = useModels();

  // Remember models that ran out of quota so the picker can flag them.
  const handleChatError = useCallback(
    (err: ChatError) => {
      if (err.code === 'QUOTA_EXHAUSTED' && err.model) markExhausted(err.model);
    },
    [markExhausted],
  );

  const {
    messages,
    streamingContent,
    isStreaming,
    error,
    sendMessage,
    retryLast,
    dismissError,
    stopStreaming,
    clearMessages,
    loadSession,
  } = useChat({
    module,
    sessionId,
    model: selected || undefined,
    onSessionCreated,
    onError: handleChatError,
  });

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
        <div className="flex items-center gap-4">
          {models.length > 0 && (
            <ModelSelect models={models} value={selected} onChange={select} exhausted={exhausted} />
          )}
          <button
            onClick={onNewChat}
            className="text-sm text-indigo-600 hover:text-indigo-700 font-medium"
          >
            + 新建对话
          </button>
        </div>
      </div>

      {/* Error banner */}
      {error && (
        <ErrorBanner
          error={error}
          models={models}
          selected={selected}
          exhausted={exhausted}
          onDismiss={dismissError}
          onSelectModel={select}
          onRetry={modelId => {
            if (modelId) select(modelId);
            retryLast(modelId);
          }}
        />
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
