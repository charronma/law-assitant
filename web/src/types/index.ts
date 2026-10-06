export type ModuleType = 
  | 'consult' 
  | 'pleading' 
  | 'contract' 
  | 'evidence_org' 
  | 'evidence' 
  | 'communication';

export interface ModuleInfo {
  type: ModuleType;
  name: string;
  description: string;
  icon: string;
}

export interface Message {
  id: string;
  session_id: string;
  role: 'user' | 'assistant';
  content: string;
  file_ids?: string[];
  /** Model that produced this turn (absent on older messages). */
  model?: string;
  created_at: string;
}

export interface Session {
  id: string;
  module: ModuleType;
  title: string;
  messages?: Message[];
  created_at: string;
  updated_at: string;
  message_count: number;
}

export interface ChatRequest {
  session_id?: string;
  module: ModuleType;
  message: string;
  /** Omit to let the server use its default model. */
  model?: string;
  file_ids?: string[];
  metadata?: Record<string, string>;
}

export interface SSEEvent {
  type: 'token' | 'done';
  content?: string;
  message_id?: string;
}

/** Tiers the backend groups models into; unknown values are shown as "standard". */
export type ModelTier = 'flagship' | 'standard' | 'fast';

export interface ModelOption {
  id: string;
  label: string;
  tier: string;
}

export interface ModelList {
  models: ModelOption[];
  default: string;
}

/**
 * A failed chat request. `code` is one of QUOTA_EXHAUSTED, INVALID_API_KEY,
 * RATE_LIMITED, UPSTREAM_ERROR, INVALID_MODEL (from the server) or
 * UNAUTHORIZED (session expired); it is absent for unclassified failures.
 */
export interface ChatError {
  code?: string;
  model?: string;
  message: string;
}

export interface FileInfo {
  id: string;
  filename: string;
  size: number;
  content_type: string;
  chars: number;
}

export interface RedlineChange {
  kind: 'edit' | 'insertion';
  para: number;
  find?: string;
  replace?: string;
  comment?: string;
}

export interface RedlineSkip {
  kind: 'edit' | 'insertion';
  para: number;
  find?: string;
  reason: string;
}

export interface RedlineResult {
  filename?: string;
  docx_base64?: string;
  summary: string;
  applied: RedlineChange[];
  skipped: RedlineSkip[];
  truncated?: boolean;
}

export interface RedlineStatus {
  job_id: string;
  state: 'running' | 'done' | 'error';
  phase?: 'reviewing' | 'applying';
  /** Characters of the model's answer so far. */
  chars: number;
  /** Changes the model has proposed so far. */
  edits_found: number;
  elapsed_ms: number;
  result?: RedlineResult;
  error?: ChatError;
}

export interface UploadedFile {
  file_id: string;
  filename: string;
  size: number;
  content_type: string;
  /** Characters the server extracted (absent on servers older than the parsing fix). */
  chars?: number;
  /** First characters of the extracted text. */
  preview?: string;
  /** The text was cut at the server's limit. */
  truncated?: boolean;
}
