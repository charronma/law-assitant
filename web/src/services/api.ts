import type { ChatError, ChatRequest, FileInfo, ModelList, RedlineStatus, Session, SSEEvent, UploadedFile } from '../types';

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
/**
 * Upload a file. onProgress receives the fraction (0..1) of the request body sent so far;
 * it reaches 1 before the server has finished parsing the document.
 */
export async function uploadFile(
  file: File,
  sessionId?: string,
  onProgress?: (fraction: number) => void,
): Promise<UploadedFile> {
  const formData = new FormData();
  formData.append('file', file);
  if (sessionId) formData.append('session_id', sessionId);
  const headers = await authHeaders();

  // fetch() cannot report upload progress, XMLHttpRequest can.
  return new Promise<UploadedFile>((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open('POST', `${API_BASE}/upload`);
    for (const [k, v] of Object.entries(headers)) xhr.setRequestHeader(k, v);
    xhr.responseType = 'json';
    xhr.upload.onprogress = e => {
      if (e.lengthComputable && e.total > 0) onProgress?.(e.loaded / e.total);
    };
    xhr.onerror = () => reject(new Error('网络错误，文件上传失败'));
    xhr.onabort = () => reject(new Error('上传已取消'));
    xhr.ontimeout = () => reject(new Error('上传超时，请重试'));
    xhr.onload = () => {
      if (xhr.status === 401 && supabase) void supabase.auth.signOut();
      if (xhr.status >= 200 && xhr.status < 300) {
        resolve(xhr.response as UploadedFile);
        return;
      }
      // The server explains why it rejected the file (unreadable, scanned, .doc, too large...).
      reject(new Error(toChatError(xhr.response, `文件上传失败（HTTP ${xhr.status}）`).message));
    };
    xhr.send(formData);
  });
}

/** Read a JSON response body, reporting progress when the server announced its size. */
async function readJSONWithProgress<T>(res: Response, onProgress?: (fraction: number) => void): Promise<T> {
  const total = Number(res.headers.get('Content-Length')) || 0;
  // Only worth a progress bar for a payload big enough to take a while.
  if (!onProgress || total < 100_000 || !res.body) return res.json();
  const reader = res.body.getReader();
  const chunks: Uint8Array[] = [];
  let loaded = 0;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    chunks.push(value);
    loaded += value.length;
    onProgress(Math.min(1, loaded / total));
  }
  const all = new Uint8Array(loaded);
  let off = 0;
  for (const c of chunks) {
    all.set(c, off);
    off += c.length;
  }
  return JSON.parse(new TextDecoder().decode(all)) as T;
}

/** An API failure carrying the server's {code, model, message}. */
export class ApiError extends Error {
  code?: string;
  model?: string;
  constructor(err: ChatError) {
    super(err.message);
    this.code = err.code;
    this.model = err.model;
  }
}

/** Metadata of one of the user's uploads. */
export async function getFileInfo(id: string): Promise<FileInfo> {
  const res = await apiFetch(`/files/${encodeURIComponent(id)}`);
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return res.json();
}

/**
 * Start a background review of an uploaded .docx: the model proposes changes and the server
 * writes them into the original as tracked changes and comments. A review can take minutes,
 * so this returns a job id at once; follow it with getRedlineStatus.
 */
export async function startRedline(fileId: string, instruction: string, model?: string): Promise<string> {
  const res = await apiFetch('/redline', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ file_id: fileId, instruction, model: model || undefined }),
  });
  if (!res.ok) {
    const body: unknown = await res.json().catch(() => null);
    throw new ApiError(toChatError(body, `生成失败（HTTP ${res.status}）`));
  }
  const body = (await res.json()) as { job_id: string };
  return body.job_id;
}

/** Progress of a review; once finished it carries the result (which includes the file). */
export async function getRedlineStatus(jobId: string, onDownloadProgress?: (fraction: number) => void): Promise<RedlineStatus> {
  const res = await apiFetch(`/redline/${encodeURIComponent(jobId)}`);
  if (res.status === 404) {
    throw new ApiError({ code: 'JOB_LOST', message: '任务已丢失（服务刚重启或已过期），请重新生成' });
  }
  if (!res.ok) throw new ApiError({ message: `查询进度失败（HTTP ${res.status}）` });
  return readJSONWithProgress<RedlineStatus>(res, onDownloadProgress);
}

/** Stop a review that is still running. */
export async function cancelRedline(jobId: string): Promise<void> {
  await apiFetch(`/redline/${encodeURIComponent(jobId)}`, { method: 'DELETE' }).catch(() => undefined);
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
