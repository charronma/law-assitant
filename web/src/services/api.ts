import type { ChatRequest, Session, SSEEvent, UploadedFile } from '../types';

const API_BASE = '/api';

// Create a new session
export async function createSession(module: string, title?: string): Promise<Session> {
  const res = await fetch(`${API_BASE}/sessions`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ module, title: title || '' }),
  });
  if (!res.ok) throw new Error('Failed to create session');
  return res.json();
}

// List all sessions
export async function listSessions(): Promise<Session[]> {
  const res = await fetch(`${API_BASE}/sessions`);
  if (!res.ok) throw new Error('Failed to list sessions');
  const data = await res.json();
  return data.sessions || [];
}

// Get a session with messages
export async function getSession(id: string): Promise<Session> {
  const res = await fetch(`${API_BASE}/sessions/${id}`);
  if (!res.ok) throw new Error('Failed to get session');
  return res.json();
}

// Delete a session
export async function deleteSession(id: string): Promise<void> {
  const res = await fetch(`${API_BASE}/sessions/${id}`, { method: 'DELETE' });
  if (!res.ok) throw new Error('Failed to delete session');
}

// Upload a file
export async function uploadFile(file: File, sessionId?: string): Promise<UploadedFile> {
  const formData = new FormData();
  formData.append('file', file);
  if (sessionId) formData.append('session_id', sessionId);

  const res = await fetch(`${API_BASE}/upload`, {
    method: 'POST',
    body: formData,
  });
  if (!res.ok) throw new Error('Failed to upload file');
  return res.json();
}

// Send a chat message with SSE streaming
export function sendChatMessage(
  req: ChatRequest,
  onToken: (content: string) => void,
  onDone: (messageId: string) => void,
  onError: (error: string) => void,
  onSessionId?: (sessionId: string) => void,
): AbortController {
  const controller = new AbortController();

  fetch(`${API_BASE}/chat`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
    signal: controller.signal,
  })
    .then(async (res) => {
      if (!res.ok) {
        const err = await res.json().catch(() => ({ error: 'Request failed' }));
        onError(err.error || 'Request failed');
        return;
      }

      // Read X-Session-ID header
      const sessionId = res.headers.get('X-Session-ID');
      if (sessionId && onSessionId) {
        onSessionId(sessionId);
      }

      const reader = res.body?.getReader();
      if (!reader) {
        onError('No response body');
        return;
      }

      const decoder = new TextDecoder();
      let buffer = '';

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split('\n');
        buffer = lines.pop() || '';

        for (const line of lines) {
          if (line.startsWith('data: ')) {
            try {
              const event: SSEEvent = JSON.parse(line.slice(6));
              switch (event.type) {
                case 'token':
                  if (event.content) onToken(event.content);
                  break;
                case 'done':
                  onDone(event.message_id || '');
                  break;
                case 'error':
                  onError(event.error || 'Unknown error');
                  break;
              }
            } catch {
              // Skip malformed events
            }
          }
        }
      }
    })
    .catch((err) => {
      if (err.name !== 'AbortError') {
        onError(err.message || 'Network error');
      }
    });

  return controller;
}
