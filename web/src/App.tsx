import { useState, useCallback } from 'react';
import type { ModuleType, Session } from './types';
import Sidebar from './components/layout/Sidebar';
import ChatWindow from './components/chat/ChatWindow';

function App() {
  const [currentModule, setCurrentModule] = useState<ModuleType>('consult');
  const [currentSessionId, setCurrentSessionId] = useState<string | null>(null);
  const [refreshKey, setRefreshKey] = useState(0);

  const handleModuleChange = useCallback((module: ModuleType) => {
    setCurrentModule(module);
    setCurrentSessionId(null);
  }, []);

  const handleSessionSelect = useCallback((session: Session) => {
    setCurrentModule(session.module);
    setCurrentSessionId(session.id);
  }, []);

  const handleNewChat = useCallback(() => {
    setCurrentSessionId(null);
  }, []);

  const handleSessionCreated = useCallback((sessionId: string) => {
    setCurrentSessionId(sessionId);
    setRefreshKey(k => k + 1);
  }, []);

  return (
    <div className="flex h-screen bg-gray-100">
      <Sidebar
        currentModule={currentModule}
        currentSessionId={currentSessionId}
        onModuleChange={handleModuleChange}
        onSessionSelect={handleSessionSelect}
        onNewChat={handleNewChat}
        refreshKey={refreshKey}
      />
      <ChatWindow
        module={currentModule}
        sessionId={currentSessionId}
        onSessionCreated={handleSessionCreated}
        onNewChat={handleNewChat}
      />
    </div>
  );
}

export default App;
