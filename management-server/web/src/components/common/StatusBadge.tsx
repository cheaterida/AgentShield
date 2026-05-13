const statusColors: Record<string, string> = {
  online: '#16a34a',
  offline: '#9ca3af',
  suspicious: '#eab308',
  degraded: '#ef4444',
  unknown: '#6b7280',
};

export function StatusBadge({ status }: { status: string }) {
  const color = statusColors[status] ?? statusColors.unknown;
  return (
    <span
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 4,
        padding: '2px 8px',
        borderRadius: 9999,
        fontSize: 12,
        fontWeight: 600,
        background: `${color}18`,
        color,
        border: `1px solid ${color}40`,
      }}
    >
      <span style={{ width: 6, height: 6, borderRadius: '50%', background: color }} />
      {status}
    </span>
  );
}
