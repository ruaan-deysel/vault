/**
 * Maps open anomalies onto a chart's time axis so charts can draw markers
 * showing when a drift anomaly first appeared.
 */

const SEVERITY_COLORS = {
  critical: 'var(--color-danger, #ef4444)',
  warning: 'var(--color-warning, #f59e0b)',
  info: 'var(--color-text-muted, #6b7280)',
}

/** Colour for a marker given an anomaly severity string. */
export function severityColor(severity) {
  return SEVERITY_COLORS[severity] || SEVERITY_COLORS.info
}

/**
 * Returns markers for anomalies whose metric matches `metric` and whose
 * first_seen_at falls within the [from, to] window. Each marker carries a
 * normalised x position in [0, 1] for drawing on the chart's time axis.
 */
export function anomalyMarkers(anomalies, metric, from, to) {
  if (!Array.isArray(anomalies)) return []
  if (!from || !to) return []
  const span = to.getTime() - from.getTime()
  if (span <= 0) return []
  const out = []
  for (const a of anomalies) {
    if (!a || a.metric !== metric) continue
    const t = new Date(a.first_seen_at)
    if (isNaN(t.getTime())) continue
    if (t.getTime() < from.getTime() || t.getTime() > to.getTime()) continue
    out.push({
      id: a.id,
      severity: a.severity || 'info',
      summary: a.summary || '',
      x: (t.getTime() - from.getTime()) / span,
      at: t,
    })
  }
  return out
}
