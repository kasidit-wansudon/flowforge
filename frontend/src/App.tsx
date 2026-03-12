import React, { useState } from 'react';
import { Routes, Route, Navigate } from 'react-router-dom';
import Sidebar from './components/common/Sidebar';
import Header from './components/common/Header';
import Dashboard from './pages/monitor/Dashboard';
import WorkflowList from './pages/workflows/WorkflowList';
import WorkflowDetail from './pages/workflows/WorkflowDetail';
import WorkflowEditor from './pages/workflows/WorkflowEditor';
import RunList from './pages/runs/RunList';
import RunDetail from './pages/runs/RunDetail';
import Settings from './pages/settings/Settings';

const App: React.FC = () => {
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);

  return (
    <div className="flex h-screen overflow-hidden bg-surface-secondary">
      <Sidebar
        collapsed={sidebarCollapsed}
        onToggle={() => setSidebarCollapsed(!sidebarCollapsed)}
      />
      <div className="flex flex-1 flex-col overflow-hidden">
        <Header />
        <main className="flex-1 overflow-y-auto p-6">
          <Routes>
            <Route path="/" element={<Navigate to="/dashboard" replace />} />
            <Route path="/dashboard" element={<Dashboard />} />
            <Route path="/workflows" element={<WorkflowList />} />
            <Route path="/workflows/new" element={<WorkflowEditor />} />
            <Route path="/workflows/:id" element={<WorkflowDetail />} />
            <Route path="/workflows/:id/edit" element={<WorkflowEditor />} />
            <Route path="/runs" element={<RunList />} />
            <Route path="/runs/:id" element={<RunDetail />} />
            <Route path="/settings" element={<Settings />} />
          </Routes>
        </main>
      </div>
    </div>
  );
};

export default App;
