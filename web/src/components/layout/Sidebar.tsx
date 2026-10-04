import { useState, useEffect } from 'react';
import {
  MessageSquare, FileText, FileCheck, FolderOpen, Search, Users,
  Plus, Trash2, Scale, LogOut,
} from 'lucide-react';
import type { ModuleType, Session } from '../../types';
import { listSessions, deleteSession } from '../../services/api';
import { useAuth } from '../../auth/authContext';

const MODULE_LIST: { type: ModuleType; name: string; icon: React.ReactNode }[] = [
  { type: 'consult', name: '法律咨询', icon: <MessageSquare size={18} /> },
  { type: 'pleading', name: '诉状撰写', icon: <FileText size={18} /> },
  { type: 'contract', name: '合同优化', icon: <FileCheck size={18} /> },
  { type: 'evidence_org', name: '证据整理', icon: <FolderOpen size={18} /> },
  { type: 'evidence', name: '取证指导', icon: <Search size={18} /> },
  { type: 'communication', name: '沟通话术', icon: <Users size={18} /> },
];

interface SidebarProps {
  currentModule: ModuleType;
  currentSessionId: string | null;
  onModuleChange: (module: ModuleType) => void;
  onSessionSelect: (session: Session) => void;
  onNewChat: () => void;
  refreshKey: number;
}

export default function Sidebar({
  currentModule,
  currentSessionId,
  onModuleChange,
  onSessionSelect,
  onNewChat,
  refreshKey,
}: SidebarProps) {
  const [sessions, setSessions] = useState<Session[]>([]);
  const { user, signOut } = useAuth();

  useEffect(() => {
    listSessions().then(setSessions).catch(() => {});
  }, [refreshKey]);

  const handleDelete = async (e: React.MouseEvent, id: string) => {
    e.stopPropagation();
    try {
      await deleteSession(id);
      setSessions(prev => prev.filter(s => s.id !== id));
    } catch { /* ignore */ }
  };

  const moduleSessions = sessions.filter(s => s.module === currentModule);

  return (
    <div className="w-64 bg-gray-900 text-white flex flex-col h-full">
      {/* Logo */}
      <div className="p-4 border-b border-gray-700">
        <div className="flex items-center gap-2">
          <Scale size={24} className="text-indigo-400" />
          <h1 className="text-lg font-bold">AI 法律助手</h1>
        </div>
      </div>

      {/* Module Navigation */}
      <div className="p-3">
        <div className="text-xs text-gray-400 uppercase tracking-wider mb-2 px-2">功能模块</div>
        <nav className="space-y-1">
          {MODULE_LIST.map(({ type, name, icon }) => (
            <button
              key={type}
              onClick={() => onModuleChange(type)}
              className={`w-full flex items-center gap-2.5 px-3 py-2 rounded-lg text-sm transition-colors ${
                currentModule === type
                  ? 'bg-indigo-600 text-white'
                  : 'text-gray-300 hover:bg-gray-800 hover:text-white'
              }`}
            >
              {icon}
              <span>{name}</span>
            </button>
          ))}
        </nav>
      </div>

      {/* New Chat Button */}
      <div className="px-3 py-2">
        <button
          onClick={onNewChat}
          className="w-full flex items-center justify-center gap-2 px-3 py-2 bg-indigo-600 hover:bg-indigo-500 rounded-lg text-sm font-medium transition-colors"
        >
          <Plus size={16} />
          <span>新建对话</span>
        </button>
      </div>

      {/* Session History */}
      <div className="flex-1 overflow-y-auto px-3 py-2">
        <div className="text-xs text-gray-400 uppercase tracking-wider mb-2 px-2">历史会话</div>
        {moduleSessions.length === 0 ? (
          <p className="text-xs text-gray-500 px-2">暂无会话记录</p>
        ) : (
          <div className="space-y-1">
            {moduleSessions.map(session => (
              <button
                key={session.id}
                onClick={() => onSessionSelect(session)}
                className={`w-full flex items-center justify-between px-3 py-2 rounded-lg text-sm transition-colors group ${
                  currentSessionId === session.id
                    ? 'bg-gray-700 text-white'
                    : 'text-gray-400 hover:bg-gray-800 hover:text-gray-200'
                }`}
              >
                <span className="truncate flex-1 text-left">{session.title}</span>
                <Trash2
                  size={14}
                  className="opacity-0 group-hover:opacity-100 text-gray-500 hover:text-red-400 transition-opacity shrink-0 ml-2"
                  onClick={(e) => handleDelete(e, session.id)}
                />
              </button>
            ))}
          </div>
        )}
      </div>

      {/* Account */}
      <div className="p-3 border-t border-gray-700 flex items-center justify-between gap-2">
        <span className="text-xs text-gray-400 truncate" title={user?.email}>{user?.email}</span>
        <button
          onClick={() => void signOut()}
          className="flex items-center gap-1 text-xs text-gray-400 hover:text-white transition-colors shrink-0"
          title="退出登录"
        >
          <LogOut size={14} />
          <span>退出</span>
        </button>
      </div>
    </div>
  );
}
