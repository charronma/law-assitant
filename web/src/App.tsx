import { useState, useCallback } from 'react';
import type { ModuleType, Session } from './types';
import Sidebar from './components/layout/Sidebar';
import ChatWindow from './components/chat/ChatWindow';

function App() {
  const [currentModule, setCurrentModule] = useState<ModuleType>('consult');
  const [currentSessionId, setCurrentSessionId] = useState<string | null>(null);
  const [refreshKey, setRefreshKey] = useState(0);
  // Below the md breakpoint the sidebar is an off-canvas drawer.
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const closeSidebar = useCallback(() => setSidebarOpen(false), []);

  const handleModuleChange = useCallback((module: ModuleType) => {
    setCurrentModule(module);
    setCurrentSessionId(null);
    setSidebarOpen(false);
  }, []);

  const handleSessionSelect = useCallback((session: Session) => {
    setCurrentModule(session.module);
    setCurrentSessionId(session.id);
    setSidebarOpen(false);
  }, []);

  const handleNewChat = useCallback(() => {
    setCurrentSessionId(null);
    setSidebarOpen(false);
  }, []);

  const handleTurnFinished = useCallback(() => setRefreshKey(k => k + 1), []);

  const handleSessionCreated = useCallback((sessionId: string) => {
    setCurrentSessionId(sessionId);
    setRefreshKey(k => k + 1);
  }, []);

  return (
    <div className="flex h-dvh bg-gray-100">
      <Sidebar
        currentModule={currentModule}
        currentSessionId={currentSessionId}
        onModuleChange={handleModuleChange}
        onSessionSelect={handleSessionSelect}
        onNewChat={handleNewChat}
        refreshKey={refreshKey}
        open={sidebarOpen}
        onClose={closeSidebar}
      />
      <ChatWindow
        module={currentModule}
        sessionId={currentSessionId}
        onSessionCreated={handleSessionCreated}
        onNewChat={handleNewChat}
        onTurnFinished={handleTurnFinished}
        onOpenSidebar={() => setSidebarOpen(true)}
      />
    </div>
  );
}

export default App;
