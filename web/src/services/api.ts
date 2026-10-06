import type { ChatError, ChatRequest, ModelList, Session, SSEEvent, UploadedFile } from '../types';

import { supabase } from '../lib/supabase';

// Empty in development (Vite proxies /api). In production set VITE_API_BASE_URL to
// the backend origin (no trailing slash) so the browser talks to it directly —
// this avoids routing long-lived SSE streams through a static host's proxy.
const API_ORIGIN = ((import.meta.env.VITE_API_BASE_URL as string | undefined) ?? '').replace(/\/+$/, '');
const API_BASE = `${API_ORIGIN}/api`;

async function authHeaders(): Promise<Record<string, string>> {
  if (!supabase) return {};
  // getSession() transparently refreshes an expired access token.
  const { data } = await supabase.auth.getSession();
  return data.session ? { Authorization: `Bearer ${data.session.access_token}` } : {};
}

/** fetch() against the API with the user's bearer token attached. */
async function apiFetch(path: string, init: RequestInit = {}): Promise<Response> {
  const headers = new Headers(init.headers);
  for (const [k, v] of Object.entries(await authHeaders())) headers.set(k, v);
  const res = await fetch(`${API_BASE}${path}`, { ...init, headers });
  // The server rejected our token: drop the session so the login page shows.
  if (res.status === 401 && supabase) void supabase.auth.signOut();
  return res;
}

// Create a new session
export async function createSession(module: string, title?: string): Promise<Session> {
  const res = await apiFetch('/sessions', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ module, title: title || '' }),
  });
  if (!res.ok) throw new Error('Failed to create session');
  return res.json();
}

// List all sessions
export async function listSessions(): Promise<Session[]> {
  const res = await apiFetch('/sessions');
  if (!res.ok) throw new Error('Failed to list sessions');
  const data = await res.json();
  return data.sessions || [];
}

// Get a session with messages
export async function getSession(id: string): Promise<Session> {
  const res = await apiFetch(`/sessions/${id}`);
  if (!res.ok) throw new Error('Failed to get session');
  return res.json();
}

// Delete a session
export async function deleteSession(id: string): Promise<void> {
  const res = await apiFetch(`/sessions/${id}`, { method: 'DELETE' });
  if (!res.ok) throw new Error('Failed to delete session');
}

// Upload a file
export async function uploadFile(file: File, sessionId?: string): Promise<UploadedFile> {
  const formData = new FormData();
  formData.append('file', file);
  if (sessionId) formData.append('session_id', sessionId);

  const res = await apiFetch('/upload', {
    method: 'POST',
    body: formData,
  });
  if (!res.ok) {
    // The server explains why it rejected the file (unreadable, scanned, .doc, too large...).
    const body: unknown = await res.json().catch(() => null);
    throw new Error(toChatError(body, `文件上传失败（HTTP ${res.status}）`).message);
  }
  return res.json();
}

/** Render Markdown as a Word document; returns the file and the server-suggested name. */
export async function exportDocx(content: string, title?: string): Promise<{ blob: Blob; filename: string }> {
  const res = await apiFetch('/export/docx', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ content, title }),
  });
  if (!res.ok) {
    const body: unknown = await res.json().catch(() => null);
    throw new Error(toChatError(body, `导出失败（HTTP ${res.status}）`).message);
  }
  const cd = res.headers.get('Content-Disposition') ?? '';
  const m = /filename\*=UTF-8''([^;]+)/i.exec(cd);
  let filename = '法律文书.docx';
  if (m) {
    try {
      filename = decodeURIComponent(m[1]);
    } catch {
      /* keep the default */
    }
  }
  return { blob: await res.blob(), filename };
}

// List the selectable models and the server's default
export async function getModels(): Promise<ModelList> {
  const res = await apiFetch('/models');
  if (!res.ok) throw new Error('Failed to list models');
  return res.json();
}

/** Normalise a server error body ({code, model, message} or the legacy {error}). */
function toChatError(raw: unknown, fallback: string): ChatError {
  if (raw && typeof raw === 'object') {
    const o = raw as Record<string, unknown>;
    const message =
      typeof o.message === 'string' ? o.message : typeof o.error === 'string' ? o.error : fallback;
    return {
      code: typeof o.code === 'string' ? o.code : undefined,
      model: typeof o.model === 'string' ? o.model : undefined,
      message,
    };
  }
  return { message: fallback };
}

// Send a chat message with SSE streaming
export function sendChatMessage(
  req: ChatRequest,
  onToken: (content: string) => void,
  onDone: (messageId: string) => void,
  onError: (error: ChatError) => void,
  onSessionId?: (sessionId: string) => void,
): AbortController {
  const controller = new AbortController();

  apiFetch('/chat', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
    signal: controller.signal,
  })
    .then(async (res) => {
      if (res.status === 401) {
        onError({ code: 'UNAUTHORIZED', message: '登录已过期，请重新登录' });
        return;
      }
      if (!res.ok) {
        // The server answers a rejected request with {code, model, message}.
        const body = await res.json().catch(() => null);
        onError(toChatError(body, 'Request failed'));
        return;
      }

      // Read X-Session-ID header
      const sessionId = res.headers.get('X-Session-ID');
      if (sessionId && onSessionId) {
        onSessionId(sessionId);
      }

      const reader = res.body?.getReader();
      if (!reader) {
        onError({ message: 'No response body' });
        return;
      }

      const decoder = new TextDecoder();
      let buffer = '';
      let eventName = '';

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split('\n');
        buffer = lines.pop() || '';

        for (const rawLine of lines) {
          const line = rawLine.replace(/\r$/, '');
          if (line === '') {
            eventName = ''; // a blank line ends the event
            continue;
          }
          if (line.startsWith('event:')) {
            eventName = line.slice(6).trim();
            continue;
          }
          if (!line.startsWith('data:')) continue;

          try {
            const payload = JSON.parse(line.slice(5).trim());
            if (eventName === 'error') {
              // Failure after output began: same JSON as a plain HTTP error.
              onError(toChatError(payload, 'Unknown error'));
              continue;
            }
            const event = payload as SSEEvent;
            switch (event.type) {
              case 'token':
                if (event.content) onToken(event.content);
                break;
              case 'done':
                onDone(event.message_id || '');
                break;
            }
          } catch {
            // Skip malformed events
          }
        }
      }
    })
    .catch((err) => {
      if (err.name !== 'AbortError') {
        onError({ message: err.message || 'Network error' });
      }
    });

  return controller;
}
