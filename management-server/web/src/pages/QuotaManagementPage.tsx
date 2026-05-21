import { useCallback, useEffect, useState } from 'react';
import { Plus, Pencil, Trash2, ToggleLeft, ToggleRight, RefreshCw } from 'lucide-react';
import { api } from '../api/client';
import type { TokenQuota } from '../api/types';

const cardStyle: React.CSSProperties = {
  background: '#fff',
  borderRadius: 12,
  padding: '20px 24px',
  boxShadow: '0 1px 3px rgba(0,0,0,0.06)',
};

const inputStyle: React.CSSProperties = {
  width: '100%',
  padding: '8px 12px',
  border: '1px solid #e2e8f0',
  borderRadius: 8,
  fontSize: 13,
  outline: 'none',
  boxSizing: 'border-box',
};

const btnStyle: React.CSSProperties = {
  padding: '8px 16px',
  borderRadius: 8,
  border: 'none',
  fontSize: 13,
  fontWeight: 600,
  cursor: 'pointer',
};

interface QuotaForm {
  target_type: string;
  target_id: string;
  daily_limit: string;
  monthly_limit: string;
  warn_threshold: string;
  block_threshold: string;
  priority: string;
}

const emptyForm: QuotaForm = {
  target_type: 'agent',
  target_id: '',
  daily_limit: '1000000',
  monthly_limit: '20000000',
  warn_threshold: '0.8',
  block_threshold: '1.0',
  priority: '5',
};

export function QuotaManagementPage() {
  const [quotas, setQuotas] = useState<TokenQuota[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showForm, setShowForm] = useState(false);
  const [editId, setEditId] = useState<string | null>(null);
  const [form, setForm] = useState<QuotaForm>(emptyForm);
  const [saving, setSaving] = useState(false);

  const fetchQuotas = useCallback(async () => {
    try {
      setError(null);
      const res = await api.listQuotas();
      setQuotas(res.quotas);
    } catch (e) {
      setError(e instanceof Error ? e.message : '加载失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchQuotas();
  }, [fetchQuotas]);

  const handleSave = async () => {
    if (!form.target_id.trim()) return;
    setSaving(true);
    try {
      const body: Parameters<typeof api.createQuota>[0] = {
        quota_id: editId || `q_${Date.now()}`,
        target_type: form.target_type as 'agent' | 'family_group',
        target_id: form.target_id,
        quota_name: 'default',
        daily_limit: parseInt(form.daily_limit) || -1,
        monthly_limit: parseInt(form.monthly_limit) || -1,
        warn_threshold: parseFloat(form.warn_threshold) || 0.8,
        block_threshold: parseFloat(form.block_threshold) || 1.0,
        priority: parseInt(form.priority) || 5,
        active: true,
      };
      if (editId) {
        await api.updateQuota(editId, body);
      } else {
        await api.createQuota(body);
      }
      setShowForm(false);
      setEditId(null);
      setForm(emptyForm);
      await fetchQuotas();
    } catch (e) {
      setError(e instanceof Error ? e.message : '保存失败');
    } finally {
      setSaving(false);
    }
  };

  const handleEdit = (q: TokenQuota) => {
    setEditId(q.quota_id);
    setForm({
      target_type: q.target_type,
      target_id: q.target_id,
      daily_limit: String(q.daily_limit),
      monthly_limit: String(q.monthly_limit),
      warn_threshold: String(q.warn_threshold),
      block_threshold: String(q.block_threshold),
      priority: String(q.priority),
    });
    setShowForm(true);
  };

  const handleDelete = async (id: string) => {
    if (!confirm('确定删除此配额规则？')) return;
    try {
      await api.deleteQuota(id);
      await fetchQuotas();
    } catch (e) {
      setError(e instanceof Error ? e.message : '删除失败');
    }
  };

  // Error display
  if (error) {
    return (
      <div>
        <h1 style={{ fontSize: 24, fontWeight: 700, marginBottom: 24 }}>配额管理</h1>
        <div style={{ padding: 40, textAlign: 'center', background: '#fef2f2', borderRadius: 8, color: '#dc2626' }}>
          <p style={{ fontWeight: 600, marginBottom: 8 }}>加载失败</p>
          <p style={{ fontSize: 13 }}>{error}</p>
          <button onClick={fetchQuotas} style={{ marginTop: 12, padding: '8px 16px', borderRadius: 8, border: '1px solid #fecaca', background: '#fff', color: '#dc2626', cursor: 'pointer', fontSize: 13 }}>重试</button>
        </div>
      </div>
    );
  }

  // Loading state
  if (loading) {
    return (
      <div>
        <h1 style={{ fontSize: 24, fontWeight: 700, marginBottom: 24 }}>配额管理</h1>
        <div style={{ padding: 40, textAlign: 'center', color: '#94a3b8' }}>加载中...</div>
      </div>
    );
  }

  // Empty state
  if (quotas.length === 0 && !showForm) {
    return (
      <div>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 24 }}>
          <h1 style={{ fontSize: 24, fontWeight: 700 }}>配额管理</h1>
          <button onClick={() => setShowForm(true)} style={{ ...btnStyle, background: '#6366f1', color: '#fff' }}>
            <Plus size={16} style={{ verticalAlign: 'middle', marginRight: 4 }} /> 新建配额
          </button>
        </div>
        <div style={cardStyle}>
          <div style={{ color: '#94a3b8', fontSize: 14, padding: 40, textAlign: 'center' }}>
            暂无配额规则。点击"新建配额"为 Agent 或家庭组设定 Token 用量限制。
          </div>
        </div>
      </div>
    );
  }

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 24 }}>
        <h1 style={{ fontSize: 24, fontWeight: 700 }}>配额管理</h1>
        <div style={{ display: 'flex', gap: 8 }}>
          <button onClick={fetchQuotas} style={{ ...btnStyle, background: '#f1f5f9', color: '#475569' }}>
            <RefreshCw size={14} style={{ verticalAlign: 'middle', marginRight: 4 }} /> 刷新
          </button>
          <button onClick={() => { setShowForm(true); setEditId(null); setForm(emptyForm); }} style={{ ...btnStyle, background: '#6366f1', color: '#fff' }}>
            <Plus size={16} style={{ verticalAlign: 'middle', marginRight: 4 }} /> 新建配额
          </button>
        </div>
      </div>

      {/* New/Edit form */}
      {showForm && (
        <div style={{ ...cardStyle, marginBottom: 24 }}>
          <h3 style={{ margin: '0 0 16px', color: '#334155', fontSize: 15, fontWeight: 600 }}>
            {editId ? '编辑配额规则' : '新建配额规则'}
          </h3>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: 12, marginBottom: 16 }}>
            <div>
              <label style={{ fontSize: 12, color: '#64748b', display: 'block', marginBottom: 4 }}>目标类型</label>
              <select value={form.target_type} onChange={e => setForm({ ...form, target_type: e.target.value })} style={inputStyle}>
                <option value="agent">Agent</option>
                <option value="family_group">家庭组</option>
              </select>
            </div>
            <div>
              <label style={{ fontSize: 12, color: '#64748b', display: 'block', marginBottom: 4 }}>目标 ID</label>
              <input value={form.target_id} onChange={e => setForm({ ...form, target_id: e.target.value })} placeholder="agent-001" style={inputStyle} />
            </div>
            <div>
              <label style={{ fontSize: 12, color: '#64748b', display: 'block', marginBottom: 4 }}>优先级 (1-10)</label>
              <input type="number" min={1} max={10} value={form.priority} onChange={e => setForm({ ...form, priority: e.target.value })} style={inputStyle} />
            </div>
            <div>
              <label style={{ fontSize: 12, color: '#64748b', display: 'block', marginBottom: 4 }}>日限额</label>
              <input type="number" value={form.daily_limit} onChange={e => setForm({ ...form, daily_limit: e.target.value })} style={inputStyle} />
            </div>
            <div>
              <label style={{ fontSize: 12, color: '#64748b', display: 'block', marginBottom: 4 }}>月限额</label>
              <input type="number" value={form.monthly_limit} onChange={e => setForm({ ...form, monthly_limit: e.target.value })} style={inputStyle} />
            </div>
            <div>
              <label style={{ fontSize: 12, color: '#64748b', display: 'block', marginBottom: 4 }}>告警阈值</label>
              <input type="number" step={0.05} min={0} max={1} value={form.warn_threshold} onChange={e => setForm({ ...form, warn_threshold: e.target.value })} style={inputStyle} />
            </div>
            <div>
              <label style={{ fontSize: 12, color: '#64748b', display: 'block', marginBottom: 4 }}>阻断阈值</label>
              <input type="number" step={0.05} min={0} max={1} value={form.block_threshold} onChange={e => setForm({ ...form, block_threshold: e.target.value })} style={inputStyle} />
            </div>
          </div>
          <div style={{ display: 'flex', gap: 8 }}>
            <button onClick={handleSave} disabled={saving} style={{ ...btnStyle, background: '#6366f1', color: '#fff', opacity: saving ? 0.6 : 1 }}>
              {saving ? '保存中...' : (editId ? '更新' : '创建')}
            </button>
            <button onClick={() => { setShowForm(false); setEditId(null); setForm(emptyForm); }} style={{ ...btnStyle, background: '#f1f5f9', color: '#475569' }}>
              取消
            </button>
          </div>
        </div>
      )}

      {/* Quota rules table */}
      <div style={cardStyle}>
        {quotas.length === 0 ? (
          <div style={{ color: '#94a3b8', fontSize: 14, padding: 40, textAlign: 'center' }}>暂无配额规则</div>
        ) : (
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
            <thead>
              <tr style={{ borderBottom: '1px solid #e2e8f0' }}>
                <th style={{ textAlign: 'left', padding: '8px 12px', color: '#64748b', fontWeight: 600 }}>目标</th>
                <th style={{ textAlign: 'left', padding: '8px 12px', color: '#64748b', fontWeight: 600 }}>类型</th>
                <th style={{ textAlign: 'right', padding: '8px 12px', color: '#64748b', fontWeight: 600 }}>日限额</th>
                <th style={{ textAlign: 'right', padding: '8px 12px', color: '#64748b', fontWeight: 600 }}>月限额</th>
                <th style={{ textAlign: 'right', padding: '8px 12px', color: '#64748b', fontWeight: 600 }}>告警</th>
                <th style={{ textAlign: 'right', padding: '8px 12px', color: '#64748b', fontWeight: 600 }}>阻断</th>
                <th style={{ textAlign: 'center', padding: '8px 12px', color: '#64748b', fontWeight: 600 }}>状态</th>
                <th style={{ textAlign: 'center', padding: '8px 12px', color: '#64748b', fontWeight: 600 }}>操作</th>
              </tr>
            </thead>
            <tbody>
              {quotas.map((q) => (
                <tr key={q.quota_id} style={{ borderBottom: '1px solid #f1f5f9' }}>
                  <td style={{ padding: '10px 12px', fontWeight: 500 }}>
                    <span style={{ fontSize: 11, color: '#94a3b8', marginRight: 4 }}>
                      {q.target_type === 'agent' ? 'Agent' : 'FG'}
                    </span>
                    {q.target_id}
                  </td>
                  <td style={{ padding: '10px 12px', color: '#64748b' }}>{q.quota_name}</td>
                  <td style={{ padding: '10px 12px', textAlign: 'right' }}>{q.daily_limit === -1 ? '∞' : q.daily_limit.toLocaleString()}</td>
                  <td style={{ padding: '10px 12px', textAlign: 'right' }}>{q.monthly_limit === -1 ? '∞' : q.monthly_limit.toLocaleString()}</td>
                  <td style={{ padding: '10px 12px', textAlign: 'right' }}>{(q.warn_threshold * 100).toFixed(0)}%</td>
                  <td style={{ padding: '10px 12px', textAlign: 'right' }}>{(q.block_threshold * 100).toFixed(0)}%</td>
                  <td style={{ padding: '10px 12px', textAlign: 'center' }}>
                    {q.active ? (
                      <span style={{ color: '#16a34a', fontSize: 12 }}>启用</span>
                    ) : (
                      <span style={{ color: '#94a3b8', fontSize: 12 }}>停用</span>
                    )}
                  </td>
                  <td style={{ padding: '10px 12px', textAlign: 'center' }}>
                    <button onClick={() => handleEdit(q)} style={{ background: 'none', border: 'none', cursor: 'pointer', color: '#6366f1', padding: '4px 8px' }} title="编辑">
                      <Pencil size={14} />
                    </button>
                    <button onClick={() => handleDelete(q.quota_id)} style={{ background: 'none', border: 'none', cursor: 'pointer', color: '#ef4444', padding: '4px 8px' }} title="删除">
                      <Trash2 size={14} />
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
