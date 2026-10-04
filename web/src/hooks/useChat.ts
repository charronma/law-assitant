import { useState, useCallback, useEffect, useRef } from 'react';
import type { Message, ModuleType } from '../types';
import { sendChatMessage, createSession, getSession } from '../services/api';

interface UseChatOptions {
  module: ModuleType;
  sessionId: string | null;
  onSessionCreated?: (sessionId: string) => void;
}

export function useChat({ module, sessionId, onSessionCreated }: UseChatOptions) {
  const [messages, setMessages] = useState<Message[]>([]);
  const [streamingContent, setStreamingContent] = useState('');
  const [isStreaming, setIsStreaming] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const abortRef = useRef<AbortController | null>(null);
  const currentSessionRef = useRef<string | null>(sessionId);
  const doneHandledRef = useRef(false);
  const streamingContentRef = useRef('');
  /** 防止重复提交：同一时间只允许一个请求在途（不依赖 state，避免 Strict Mode 下双调导致发两次请求） */
  const requestInFlightRef = useRef(false);

  // Keep ref in sync (refs must not be written during render)
  useEffect(() => {
    currentSessionRef.current = sessionId;
  }, [sessionId]);

  const loadSession = useCallback(async (id: string) => {
    try {
      const session = await getSession(id);
      setMessages(session.messages || []);
      setError(null);
    } catch {
      setError('Failed to load session');
    }
  }, []);

  const sendMessage = useCallback(async (content: string, fileIds?: string[]) => {
    if (!content.trim()) return;
    if (requestInFlightRef.current) return;
    requestInFlightRef.current = true;

    setError(null);
    setIsStreaming(true);
    setStreamingContent('');
    streamingContentRef.current = '';
    doneHandledRef.current = false;

    // Add user message optimistically
    const userMsg: Message = {
      id: `temp-${Date.now()}`,
      session_id: currentSessionRef.current || '',
      role: 'user',
      content,
      file_ids: fileIds,
      created_at: new Date().toISOString(),
    };
    setMessages(prev => [...prev, userMsg]);

    // If no session, create one
    let sid = currentSessionRef.current;
    if (!sid) {
      try {
        const session = await createSession(module);
        sid = session.id;
        currentSessionRef.current = sid;
        onSessionCreated?.(sid);
      } catch {
        setError('Failed to create session');
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
        setIsStreaming(false);
        streamingContentRef.current = '';
        setStreamingContent('');
      },
      // onSessionId
      (newSid) => {
        if (!currentSessionRef.current) {
          currentSessionRef.current = newSid;
          onSessionCreated?.(newSid);
        }
      },
    );

    abortRef.current = controller;
  }, [module, onSessionCreated]);

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
    stopStreaming,
    clearMessages,
    loadSession,
  };
}
