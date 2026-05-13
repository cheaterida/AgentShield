const severityColors: Record<string, string> = {
  low: '#3b82f6',
  medium: '#eab308',
  high: '#f97316',
  critical: '#ef4444',
};

export function SeverityBadge({ severity }: { severity: string }) {
  const color = severityColors[severity] ?? severityColors.low;
  return (
    <span
      style={{
        display: 'inline-block',
        padding: '1px 8px',
        borderRadius: 9999,
        fontSize: 12,
        fontWeight: 600,
        background: `${color}18`,
        color,
        border: `1px solid ${color}40`,
      }}
    >
      {severity}
    </span>
  );
}
