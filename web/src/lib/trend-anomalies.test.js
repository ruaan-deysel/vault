import { describe, expect, it } from 'vitest'

import { anomalyMarkers, severityColor } from './trend-anomalies.js'

describe('anomalyMarkers', () => {
  const from = new Date('2026-06-10T00:00:00Z')
  const to = new Date('2026-06-20T00:00:00Z')

  it('returns markers only for matching metric within the window', () => {
    const anomalies = [
      { id: 1, metric: 'size_bytes', severity: 'warning', first_seen_at: '2026-06-15T00:00:00Z', summary: 'grew' },
      { id: 2, metric: 'duration_seconds', severity: 'critical', first_seen_at: '2026-06-18T00:00:00Z', summary: 'slower' },
      { id: 3, metric: 'size_bytes', severity: 'info', first_seen_at: '2026-05-01T00:00:00Z', summary: 'old' },
    ]

    const markers = anomalyMarkers(anomalies, 'size_bytes', from, to)

    expect(markers).toHaveLength(1)
    expect(markers[0].id).toBe(1)
    expect(markers[0].severity).toBe('warning')
    expect(markers[0].x).toBeCloseTo(0.5, 5) // 5 of 10 days in
  })

  it('returns an empty array for non-array or empty input', () => {
    expect(anomalyMarkers(null, 'size_bytes', from, to)).toEqual([])
    expect(anomalyMarkers(undefined, 'size_bytes', from, to)).toEqual([])
    expect(anomalyMarkers([], 'size_bytes', from, to)).toEqual([])
  })

  it('returns an empty array when the window is empty or reversed', () => {
    const anomalies = [{ id: 1, metric: 'size_bytes', first_seen_at: '2026-06-15T00:00:00Z' }]
    expect(anomalyMarkers(anomalies, 'size_bytes', to, from)).toEqual([])
  })
})

describe('severityColor', () => {
  it('maps known severities to colors and falls back to info', () => {
    expect(severityColor('critical')).toContain('--color-danger')
    expect(severityColor('warning')).toContain('--color-warning')
    expect(severityColor('bogus')).toContain('--color-text-muted')
  })
})
