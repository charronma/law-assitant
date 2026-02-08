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
  file_ids?: string[];
  metadata?: Record<string, string>;
}

export interface SSEEvent {
  type: 'token' | 'done' | 'error';
  content?: string;
  message_id?: string;
  error?: string;
}

export interface UploadedFile {
  file_id: string;
  filename: string;
  size: number;
  content_type: string;
}
