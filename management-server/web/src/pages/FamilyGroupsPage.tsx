import { useCallback, useEffect, useState } from 'react';
import { Pencil, Trash2 } from 'lucide-react';
import { api } from '../api/client';
import type { FamilyGroup } from '../api/types';

export function FamilyGroupsPage() {
  const [groups, setGroups] = useState<FamilyGroup[]>([]);
  const [loading, setLoading] = useState(true);
  const [editing, setEditing] = useState<FamilyGroup | null>(null);
  const [creating, setCreating] = useState(false);
  const [form, setForm] = useState({ id: '', display_name: '', labels: '' });

  const fetchGroups = useCallback(async () => {
    try {
      const data = await api.listFamilyGroups();
      setGroups(data.groups);
    } catch (e) {
      console.error(e);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { fetchGroups(); }, [fetchGroups]);

  const handleSave = async () => {
    try {
      const labels: Record<string, string> = {};
      if (form.labels) form.labels.split(',').forEach((kv) => { const [k, v] = kv.split('='); if (k) labels[k.trim()] = (v || '').trim(); });

      if (editing) {
        await api.updateFamilyGroup(editing.id, { display_name: form.display_name, labels });
      } else {
        await api.createFamilyGroup({ id: form.id, display_name: form.display_name, labels });
      }
      setEditing(null);
      setCreating(false);
      setForm({ id: '', display_name: '', labels: '' });
      fetchGroups();
    } catch (e) {
      console.error(e);
    }
  };

  const handleDelete = async (id: string) => {
    if (!confirm('确认删除此家庭组？')) return;
    try {
      await api.deleteFamilyGroup(id);
      fetchGroups();
    } catch (e) {
      console.error(e);
    }
  };

  const openEdit = (g: FamilyGroup) => {
    setEditing(g);
    setForm({
      id: g.id,
      display_name: g.display_name,
      labels: Object.entries(g.labels || {}).map(([k, v]) => `${k}=${v}`).join(', '),
    });
  };

  const formComp = (
    <div style={{ background: '#fff', borderRadius: 12, padding: 24, marginBottom: 16, boxShadow: '0 1px 3px rgba(0,0,0,0.06)' }}>
      <h3 style={{ marginBottom: 16, fontSize: 16, fontWeight: 600 }}>{creating ? '新建家庭组' : '编辑家庭组'}</h3>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
        {creating && (
          <input placeholder="ID (唯一标识)" value={form.id} onChange={(e) => setForm({ ...form, id: e.target.value })} style={{ padding: '8px 12px', borderRadius: 8, border: '1px solid #e2e8f0', fontSize: 13 }} />
        )}
        <input placeholder="显示名称" value={form.display_name} onChange={(e) => setForm({ ...form, display_name: e.target.value })} style={{ padding: '8px 12px', borderRadius: 8, border: '1px solid #e2e8f0', fontSize: 13 }} />
        <input placeholder="标签 (key=value, 逗号分隔)" value={form.labels} onChange={(e) => setForm({ ...form, labels: e.target.value })} style={{ padding: '8px 12px', borderRadius: 8, border: '1px solid #e2e8f0', fontSize: 13 }} />
        <div style={{ display: 'flex', gap: 8 }}>
          <button onClick={handleSave} style={{ padding: '8px 20px', borderRadius: 8, border: 'none', background: '#16a34a', color: '#fff', fontSize: 13, fontWeight: 500, cursor: 'pointer' }}>保存</button>
          <button onClick={() => { setEditing(null); setCreating(false); }} style={{ padding: '8px 20px', borderRadius: 8, border: '1px solid #e2e8f0', background: '#fff', fontSize: 13, cursor: 'pointer' }}>取消</button>
        </div>
      </div>
    </div>
  );

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 24 }}>
        <h1 style={{ fontSize: 24, fontWeight: 700 }}>家庭组管理</h1>
        <button onClick={() => setCreating(true)} style={{ padding: '8px 20px', borderRadius: 8, border: 'none', background: '#6366f1', color: '#fff', fontSize: 13, fontWeight: 500, cursor: 'pointer' }}>
          新建家庭组
        </button>
      </div>

      {(editing || creating) && formComp}

      {loading ? (
        <div style={{ padding: 40, textAlign: 'center', color: '#94a3b8' }}>加载中...</div>
      ) : (
        <div style={{ background: '#fff', borderRadius: 12, boxShadow: '0 1px 3px rgba(0,0,0,0.06)', overflow: 'hidden' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
            <thead>
              <tr style={{ borderBottom: '1px solid #e2e8f0', background: '#f8fafc' }}>
                <th style={{ textAlign: 'left', padding: '12px 16px', color: '#64748b', fontWeight: 600 }}>ID</th>
                <th style={{ textAlign: 'left', padding: '12px 16px', color: '#64748b', fontWeight: 600 }}>名称</th>
                <th style={{ textAlign: 'left', padding: '12px 16px', color: '#64748b', fontWeight: 600 }}>标签</th>
                <th style={{ textAlign: 'left', padding: '12px 16px', color: '#64748b', fontWeight: 600 }}>创建时间</th>
                <th style={{ textAlign: 'left', padding: '12px 16px', color: '#64748b', fontWeight: 600 }}>操作</th>
              </tr>
            </thead>
            <tbody>
              {groups.length === 0 ? (
                <tr><td colSpan={5} style={{ padding: 40, textAlign: 'center', color: '#94a3b8' }}>暂无家庭组</td></tr>
              ) : (
                groups.map((g) => (
                  <tr key={g.id} style={{ borderBottom: '1px solid #f1f5f9' }}>
                    <td style={{ padding: '12px 16px', fontWeight: 600 }}>{g.id}</td>
                    <td style={{ padding: '12px 16px' }}>{g.display_name}</td>
                    <td style={{ padding: '12px 16px', color: '#64748b' }}>{Object.entries(g.labels || {}).map(([k, v]) => `${k}=${v}`).join(', ') || '-'}</td>
                    <td style={{ padding: '12px 16px', fontSize: 12, color: '#94a3b8' }}>{new Date(g.created_at).toLocaleString()}</td>
                    <td style={{ padding: '12px 16px' }}>
                      <div style={{ display: 'flex', gap: 8 }}>
                        <button onClick={() => openEdit(g)} style={{ border: 'none', background: 'none', cursor: 'pointer', color: '#6366f1' }}><Pencil size={16} /></button>
                        <button onClick={() => handleDelete(g.id)} style={{ border: 'none', background: 'none', cursor: 'pointer', color: '#ef4444' }}><Trash2 size={16} /></button>
                      </div>
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
