import { useContext, Component, type ReactNode } from 'react';
import { NavLink, Outlet, Route, Routes } from 'react-router-dom';
import {
  LayoutDashboard,
  Bot,
  ScrollText,
  AlertTriangle,
  Shield,
  Users,
  Wifi,
  WifiOff,
  Radio,
  Activity,
} from 'lucide-react';
import { WebSocketContext } from './context/WebSocketContext';
import { DashboardPage } from './pages/DashboardPage';
import { AgentsPage } from './pages/AgentsPage';
import { AgentDetailPage } from './pages/AgentDetailPage';
import { AuditLogPage } from './pages/AuditLogPage';
import { AlertsPage } from './pages/AlertsPage';
import { PoliciesPage } from './pages/PoliciesPage';
import { FamilyGroupsPage } from './pages/FamilyGroupsPage';
import { TracesPage } from './pages/TracesPage';
import { SecurityEventsPage } from './pages/SecurityEventsPage';

// Error boundary to catch render-time crashes and show debug info instead of blank screen.
class ErrorBoundary extends Component<{ children: ReactNode }, { error: Error | null }> {
  constructor(props: { children: ReactNode }) {
    super(props);
    this.state = { error: null };
  }

  static getDerivedStateFromError(error: Error) {
    return { error };
  }

  componentDidCatch(error: Error, info: { componentStack: string }) {
    console.error('[ErrorBoundary] page crash:', error.message);
    console.error('[ErrorBoundary] component stack:', info.componentStack);
  }

  render() {
    if (this.state.error) {
      return (
        <div style={{ padding: 48, margin: 24, background: '#fef2f2', borderRadius: 12, border: '1px solid #fecaca' }}>
          <h2 style={{ color: '#dc2626', marginBottom: 12 }}>页面渲染崩溃</h2>
          <p style={{ fontSize: 14, fontWeight: 600, marginBottom: 8, color: '#1e293b' }}>{this.state.error.message}</p>
          <pre style={{ fontSize: 11, color: '#64748b', whiteSpace: 'pre-wrap', wordBreak: 'break-word', background: '#fff', padding: 12, borderRadius: 6, maxHeight: 300, overflow: 'auto' }}>
            {this.state.error.stack}
          </pre>
        </div>
      );
    }
    return this.props.children;
  }
}

const navItems = [
  { to: '/', icon: LayoutDashboard, label: '仪表盘' },
  { to: '/agents', icon: Bot, label: '智能体' },
  { to: '/traces', icon: Radio, label: '链路追踪' },
  { to: '/security-events', icon: Activity, label: '安全事件' },
  { to: '/audit-log', icon: ScrollText, label: '审计日志' },
  { to: '/alerts', icon: AlertTriangle, label: '安全告警' },
  { to: '/policies', icon: Shield, label: '策略管理' },
  { to: '/family-groups', icon: Users, label: '家庭组' },
];

const navStyle: Record<string, React.CSSProperties> = {
  sidebar: {
    width: 220,
    minHeight: '100vh',
    background: '#0f172a',
    color: '#e2e8f0',
    display: 'flex',
    flexDirection: 'column',
    flexShrink: 0,
  },
  logo: {
    padding: '20px 16px',
    fontSize: 18,
    fontWeight: 700,
    borderBottom: '1px solid #1e293b',
    display: 'flex',
    alignItems: 'center',
    gap: 10,
  },
  nav: { display: 'flex', flexDirection: 'column', gap: 2, padding: '12px 8px' },
  link: {
    display: 'flex',
    alignItems: 'center',
    gap: 10,
    padding: '10px 12px',
    borderRadius: 8,
    color: '#cbd5e1',
    textDecoration: 'none',
    fontSize: 14,
    fontWeight: 500,
    transition: 'all 0.15s',
  },
};

function Layout() {
  const { connected } = useContext(WebSocketContext);

  return (
    <div style={{ display: 'flex', minHeight: '100vh', background: '#f1f5f9' }}>
      <aside style={navStyle.sidebar}>
        <div style={navStyle.logo}>
          <Shield size={24} /> AgentShield
        </div>
        <nav style={navStyle.nav}>
          {navItems.map(({ to, icon: Icon, label }) => (
            <NavLink
              key={to}
              to={to}
              end={to === '/'}
              style={({ isActive }) => ({
                ...navStyle.link,
                background: isActive ? '#1e293b' : 'transparent',
                color: isActive ? '#f8fafc' : '#cbd5e1',
              })}
            >
              <Icon size={18} /> {label}
            </NavLink>
          ))}
        </nav>
        <div style={{ marginTop: 'auto', padding: 16, fontSize: 12, color: '#64748b' }}>
          {connected ? (
            <span style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
              <Wifi size={14} color="#16a34a" /> 实时连接
            </span>
          ) : (
            <span style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
              <WifiOff size={14} color="#ef4444" /> 连接断开
            </span>
          )}
        </div>
      </aside>
      <main style={{ flex: 1, padding: 24, overflow: 'auto' }}>
        <ErrorBoundary>
          <Outlet />
        </ErrorBoundary>
      </main>
    </div>
  );
}

export function App() {
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route path="/" element={<DashboardPage />} />
        <Route path="/agents" element={<AgentsPage />} />
        <Route path="/agents/:id" element={<AgentDetailPage />} />
        <Route path="/traces" element={<TracesPage />} />
        <Route path="/security-events" element={<SecurityEventsPage />} />
        <Route path="/audit-log" element={<AuditLogPage />} />
        <Route path="/alerts" element={<AlertsPage />} />
        <Route path="/policies" element={<PoliciesPage />} />
        <Route path="/family-groups" element={<FamilyGroupsPage />} />
      </Route>
    </Routes>
  );
}
