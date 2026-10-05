import { useState, useCallback, useEffect, useRef } from 'react';
import type { ChatError, Message, ModuleType } from '../types';
import { sendChatMessage, createSession, getSession } from '../services/api';

interface UseChatOptions {
  module: ModuleType;
  sessionId: string | null;
  /** Model for new requests; omit to let the server use its default. */
  model?: string;
  onSessionCreated?: (sessionId: string) => void;
  /** Called for every failed request (e.g. to remember which models ran out of quota). */
  onError?: (err: ChatError) => void;
}

interface SendOptions {
  /** Overrides the hook's model for this request only. */
  model?: string;
  /** Resend the previous message: no new bubble is added to the conversation. */
  retry?: boolean;
}

export function useChat({ module, sessionId, model, onSessionCreated, onError }: UseChatOptions) {
  const [messages, setMessages] = useState<Message[]>([]);
  const [streamingContent, setStreamingContent] = useState('');
  const [isStreaming, setIsStreaming] = useState(false);
  const [error, setError] = useState<ChatError | null>(null);
  const abortRef = useRef<AbortController | null>(null);
  const currentSessionRef = useRef<string | null>(sessionId);
  const doneHandledRef = useRef(false);
  const streamingContentRef = useRef('');
  /** 防止重复提交：同一时间只允许一个请求在途（不依赖 state，避免 Strict Mode 下双调导致发两次请求） */
  const requestInFlightRef = useRef(false);
  /** The last message the user sent, so a failed request can be retried (possibly with another model). */
  const lastRequestRef = useRef<{ content: string; fileIds?: string[] } | null>(null);
  /** A session this hook created itself: its messages are already on screen, so don't reload (and wipe) them. */
  const ownSessionRef = useRef<string | null>(null);
  const onErrorRef = useRef(onError);

  // Keep refs in sync (refs must not be written during render)
  useEffect(() => {
    currentSessionRef.current = sessionId;
  }, [sessionId]);
  useEffect(() => {
    onErrorRef.current = onError;
  }, [onError]);

  const loadSession = useCallback(async (id: string) => {
    // The server may not have persisted the in-flight message yet (it is saved
    // only once the model accepts the request), so reloading now would erase it.
    if (ownSessionRef.current === id) return;
    ownSessionRef.current = null;
    lastRequestRef.current = null;
    try {
      const session = await getSession(id);
      setMessages(session.messages || []);
      setError(null);
    } catch {
      setError({ message: 'Failed to load session' });
    }
  }, []);

  const sendMessage = useCallback(async (content: string, fileIds?: string[], opts?: SendOptions) => {
    if (!content.trim()) return;
    if (requestInFlightRef.current) return;
    requestInFlightRef.current = true;

    const requestModel = opts?.model ?? model;
    const isRetry = opts?.retry === true;
    if (!isRetry) lastRequestRef.current = { content, fileIds };

    setError(null);
    setIsStreaming(true);
    setStreamingContent('');
    streamingContentRef.current = '';
    doneHandledRef.current = false;

    // Add user message optimistically (a retry reuses the bubble already shown)
    if (!isRetry) {
      const userMsg: Message = {
        id: `temp-${Date.now()}`,
        session_id: currentSessionRef.current || '',
        role: 'user',
        content,
        file_ids: fileIds,
        created_at: new Date().toISOString(),
      };
      setMessages(prev => [...prev, userMsg]);
    }

    // If no session, create one
    let sid = currentSessionRef.current;
    if (!sid) {
      try {
        const session = await createSession(module);
        sid = session.id;
        currentSessionRef.current = sid;
        ownSessionRef.current = sid;
        onSessionCreated?.(sid);
      } catch {
        setError({ message: 'Failed to create session' });
        setIsStreaming(false);
        requestInFlightRef.current = false;
        return;
      }
    }

    const controller = sendChatMessage(
      {
        session_id: sid,
        module,
        message: content,
        file_ids: fileIds,
        model: requestModel || undefined,
      },
      // onToken
      (token) => {
        setStreamingContent(prev => {
          const next = prev + token;
          streamingContentRef.current = next;
          return next;
        });
      },
      // onDone - 只处理一次，避免 Strict Mode 或重复 SSE 导致回答出现两遍
      (messageId) => {
        if (doneHandledRef.current) return;
        doneHandledRef.current = true;
        requestInFlightRef.current = false;

        const finalContent = streamingContentRef.current;
        if (finalContent) {
          const assistantMsg: Message = {
            id: messageId || `msg-${Date.now()}`,
            session_id: sid!,
            role: 'assistant',
            content: finalContent,
            created_at: new Date().toISOString(),
          };
          setMessages(msgs => [...msgs, assistantMsg]);
        }
        streamingContentRef.current = '';
        setStreamingContent('');
        setIsStreaming(false);
      },
      // onError
      (err) => {
        requestInFlightRef.current = false;
        setError(err);
        onErrorRef.current?.(err);
        setIsStreaming(false);
        streamingContentRef.current = '';
        setStreamingContent('');
      },
      // onSessionId
      (newSid) => {
        if (!currentSessionRef.current) {
          currentSessionRef.current = newSid;
          ownSessionRef.current = newSid;
          onSessionCreated?.(newSid);
        }
      },
    );

    abortRef.current = controller;
  }, [module, model, onSessionCreated]);

  const retryLast = useCallback((retryModel?: string) => {
    const last = lastRequestRef.current;
    if (!last) return;
    void sendMessage(last.content, last.fileIds, { model: retryModel, retry: true });
  }, [sendMessage]);

  const dismissError = useCallback(() => setError(null), []);

  const stopStreaming = useCallback(() => {
    abortRef.current?.abort();
    // An aborted fetch never fires onDone/onError, so release the in-flight
    // lock here — otherwise no further message could be sent after stopping.
    doneHandledRef.current = true;
    requestInFlightRef.current = false;
    setIsStreaming(false);

    const partial = streamingContentRef.current;
    if (partial) {
      const assistantMsg: Message = {
        id: `msg-${Date.now()}`,
        session_id: currentSessionRef.current || '',
        role: 'assistant',
        content: partial + '\n\n[已停止生成]',
        created_at: new Date().toISOString(),
      };
      setMessages(prev => [...prev, assistantMsg]);
    }
    streamingContentRef.current = '';
    setStreamingContent('');
  }, []);

  const clearMessages = useCallback(() => {
    ownSessionRef.current = null;
    lastRequestRef.current = null;
    setMessages([]);
    setStreamingContent('');
    setError(null);
  }, []);

  return {
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
  };
}
