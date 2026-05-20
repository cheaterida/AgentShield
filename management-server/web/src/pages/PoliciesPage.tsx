import { useCallback, useEffect, useState } from 'react';
import { api } from '../api/client';
import type { PolicyBundle } from '../api/types';

export function PoliciesPage() {
  const [bundles, setBundles] = useState<PolicyBundle[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showForm, setShowForm] = useState(false);
  const [form, setForm] = useState({ version: '', payload: '', digest: '' });

  const fetchBundles = useCallback(async () => {
    try {
      const data = await api.listPolicyBundles();
      setBundles(data.bundles);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : '加载策略包列表失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { fetchBundles(); }, [fetchBundles]);

  const handleCreate = async () => {
    try {
      await api.createPolicyBundle({
        version: form.version,
        payload: btoa(form.payload),
        digest: form.digest,
      });
      setShowForm(false);
      setForm({ version: '', payload: '', digest: '' });
      fetchBundles();
    } catch (e) {
      console.error(e);
    }
  };

  const handleActivate = async (version: string) => {
    try {
      await api.activatePolicyBundle(version);
      fetchBundles();
    } catch (e) {
      console.error(e);
    }
  };

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 24 }}>
        <h1 style={{ fontSize: 24, fontWeight: 700 }}>策略管理</h1>
        <button onClick={() => setShowForm(true)} style={{ padding: '8px 20px', borderRadius: 8, border: 'none', background: '#6366f1', color: '#fff', fontSize: 13, fontWeight: 500, cursor: 'pointer' }}>
          上传策略包
        </button>
      </div>

      {showForm && (
        <div style={{ background: '#fff', borderRadius: 12, padding: 24, marginBottom: 16, boxShadow: '0 1px 3px rgba(0,0,0,0.06)' }}>
          <h3 style={{ marginBottom: 16, fontSize: 16, fontWeight: 600 }}>新建策略包</h3>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
            <input placeholder="版本号 (如 v1.0.0)" value={form.version} onChange={(e) => setForm({ ...form, version: e.target.value })} style={{ padding: '8px 12px', borderRadius: 8, border: '1px solid #e2e8f0', fontSize: 13 }} />
            <textarea placeholder="策略内容 (Rego/JSON)" rows={5} value={form.payload} onChange={(e) => setForm({ ...form, payload: e.target.value })} style={{ padding: '8px 12px', borderRadius: 8, border: '1px solid #e2e8f0', fontSize: 13 }} />
            <input placeholder="内容摘要 (SHA256)" value={form.digest} onChange={(e) => setForm({ ...form, digest: e.target.value })} style={{ padding: '8px 12px', borderRadius: 8, border: '1px solid #e2e8f0', fontSize: 13 }} />
            <div style={{ display: 'flex', gap: 8 }}>
              <button onClick={handleCreate} style={{ padding: '8px 20px', borderRadius: 8, border: 'none', background: '#16a34a', color: '#fff', fontSize: 13, fontWeight: 500, cursor: 'pointer' }}>创建</button>
              <button onClick={() => setShowForm(false)} style={{ padding: '8px 20px', borderRadius: 8, border: '1px solid #e2e8f0', background: '#fff', fontSize: 13, cursor: 'pointer' }}>取消</button>
            </div>
          </div>
        </div>
      )}

      {error ? (
        <div style={{ padding: 40, textAlign: 'center', background: '#fef2f2', borderRadius: 8, color: '#dc2626' }}>
          <p style={{ fontWeight: 600, marginBottom: 8 }}>加载失败</p>
          <p style={{ fontSize: 13 }}>{error}</p>
          <button onClick={fetchBundles} style={{ marginTop: 12, padding: '8px 16px', borderRadius: 8, border: '1px solid #fecaca', background: '#fff', color: '#dc2626', cursor: 'pointer', fontSize: 13 }}>重试</button>
        </div>
      ) : loading ? (
        <div style={{ padding: 40, textAlign: 'center', color: '#94a3b8' }}>加载中...</div>
      ) : (
        <div style={{ background: '#fff', borderRadius: 12, boxShadow: '0 1px 3px rgba(0,0,0,0.06)', overflow: 'hidden' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
            <thead>
              <tr style={{ borderBottom: '1px solid #e2e8f0', background: '#f8fafc' }}>
                <th style={{ textAlign: 'left', padding: '12px 16px', color: '#64748b', fontWeight: 600 }}>版本</th>
                <th style={{ textAlign: 'left', padding: '12px 16px', color: '#64748b', fontWeight: 600 }}>摘要</th>
                <th style={{ textAlign: 'left', padding: '12px 16px', color: '#64748b', fontWeight: 600 }}>状态</th>
                <th style={{ textAlign: 'left', padding: '12px 16px', color: '#64748b', fontWeight: 600 }}>创建时间</th>
                <th style={{ textAlign: 'left', padding: '12px 16px', color: '#64748b', fontWeight: 600 }}>操作</th>
              </tr>
            </thead>
            <tbody>
              {bundles.length === 0 ? (
                <tr><td colSpan={5} style={{ padding: 40, textAlign: 'center', color: '#94a3b8' }}>暂无策略包</td></tr>
              ) : (
                bundles.map((b) => (
                  <tr key={b.version} style={{ borderBottom: '1px solid #f1f5f9' }}>
                    <td style={{ padding: '12px 16px', fontWeight: 600 }}>{b.version}</td>
                    <td style={{ padding: '12px 16px', color: '#64748b', fontSize: 12, fontFamily: 'monospace' }}>{b.digest?.substring(0, 16)}...</td>
                    <td style={{ padding: '12px 16px' }}>
                      <span style={{ display: 'inline-block', padding: '2px 10px', borderRadius: 9999, fontSize: 12, fontWeight: 600, background: b.active ? '#16a34a18' : '#f1f5f9', color: b.active ? '#16a34a' : '#94a3b8' }}>
                        {b.active ? '活跃' : '非活跃'}
                      </span>
                    </td>
                    <td style={{ padding: '12px 16px', fontSize: 12, color: '#94a3b8' }}>{new Date(b.created_at).toLocaleString()}</td>
                    <td style={{ padding: '12px 16px' }}>
                      {!b.active && (
                        <button onClick={() => handleActivate(b.version)} style={{ padding: '4px 12px', borderRadius: 6, border: '1px solid #6366f1', background: '#fff', color: '#6366f1', fontSize: 12, fontWeight: 500, cursor: 'pointer' }}>
                          激活
                        </button>
                      )}
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
