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

export interface UploadedFile {
  file_id: string;
  filename: string;
  size: number;
  content_type: string;
  /** Characters the server extracted from the document. */
  chars: number;
  /** First characters of the extracted text. */
  preview: string;
  /** The text was cut at the server's limit. */
  truncated: boolean;
}
