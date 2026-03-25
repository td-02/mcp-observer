import { Fragment, useEffect, useMemo, useState } from 'react'
import {
  BarElement,
  CategoryScale,
  Chart as ChartJS,
  Legend,
  LinearScale,
  Tooltip,
} from 'chart.js'
import { Bar } from 'react-chartjs-2'
import './App.css'

ChartJS.register(CategoryScale, LinearScale, BarElement, Tooltip, Legend)

type TabKey = 'traces' | 'latency' | 'errors'
type WindowKey = '5m' | '30m' | '1h'

type TraceRecord = {
  id: string
  trace_id: string
  server_name: string
  method: string
  params?: unknown
  response?: unknown
  latency_ms: number
  is_error: boolean
  error_message?: string
  created_at: string
}

type LatencyStatRecord = {
  server_name: string
  method: string
  count: number
  p50_ms: number
  p95_ms: number
  p99_ms: number
}

type ErrorStatRecord = {
  method: string
  count: number
  error_count: number
  error_rate_pct: number
  recent_error_message?: string
  recent_error_at?: string
}

const windows: { value: WindowKey; label: string }[] = [
  { value: '5m', label: 'Last 5m' },
  { value: '30m', label: 'Last 30m' },
  { value: '1h', label: 'Last 1h' },
]

const tabs: { key: TabKey; label: string }[] = [
  { key: 'traces', label: 'Traces' },
  { key: 'latency', label: 'Latency' },
  { key: 'errors', label: 'Errors' },
]

function App() {
  const [activeTab, setActiveTab] = useState<TabKey>('traces')
  const [windowKey, setWindowKey] = useState<WindowKey>('5m')
  const [selectedServer, setSelectedServer] = useState<string>('')

  const [traces, setTraces] = useState<TraceRecord[]>([])
  const [latencyStats, setLatencyStats] = useState<LatencyStatRecord[]>([])
  const [errorStats, setErrorStats] = useState<ErrorStatRecord[]>([])
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [streamState, setStreamState] = useState<'connecting' | 'live' | 'closed'>('connecting')

  useEffect(() => {
    let active = true

    const loadTraces = async () => {
      const response = await fetch(`/api/traces${withQuery({ server: selectedServer })}`)
      if (!response.ok) {
        throw new Error(`failed to load traces: ${response.status}`)
      }

      const data = (await response.json()) as TraceRecord[]
      if (active) {
        setTraces(data)
      }
    }

    loadTraces().catch(() => {
      if (active) {
        setTraces([])
      }
    })

    const source = new EventSource(`/events${withQuery({ server: selectedServer })}`)
    source.onopen = () => {
      if (active) {
        setStreamState('live')
      }
    }
    source.onmessage = (event) => {
      if (!active) {
        return
      }

      const next = JSON.parse(event.data) as TraceRecord
      setTraces((current) => {
        const deduped = current.filter((trace) => trace.id !== next.id)
        return [next, ...deduped].slice(0, 200)
      })
    }
    source.onerror = () => {
      if (active) {
        setStreamState('closed')
      }
    }

    return () => {
      active = false
      source.close()
    }
  }, [selectedServer])

  useEffect(() => {
    let active = true

    const loadStats = async () => {
      const [latencyResponse, errorResponse] = await Promise.all([
        fetch(`/api/stats/latency${withQuery({ window: windowKey, server: selectedServer })}`),
        fetch(`/api/stats/errors${withQuery({ window: windowKey, server: selectedServer })}`),
      ])

      if (!latencyResponse.ok || !errorResponse.ok) {
        throw new Error('failed to load stats')
      }

      const [latencyData, errorData] = (await Promise.all([
        latencyResponse.json(),
        errorResponse.json(),
      ])) as [LatencyStatRecord[], ErrorStatRecord[]]

      if (!active) {
        return
      }

      setLatencyStats(latencyData)
      setErrorStats(errorData)
    }

    loadStats().catch(() => {
      if (!active) {
        return
      }
      setLatencyStats([])
      setErrorStats([])
    })

    const interval = window.setInterval(() => {
      loadStats().catch(() => {
        if (!active) {
          return
        }
        setLatencyStats([])
        setErrorStats([])
      })
    }, 10_000)

    return () => {
      active = false
      window.clearInterval(interval)
    }
  }, [selectedServer, windowKey])

  const stats = useMemo(() => {
    const total = traces.length
    const errors = traces.filter((trace) => trace.is_error).length
    const avgLatency =
      total === 0
        ? 0
        : Math.round(traces.reduce((sum, trace) => sum + trace.latency_ms, 0) / total)

    return { total, errors, avgLatency }
  }, [traces])

  const serverOptions = useMemo(() => {
    const values = new Set<string>()
    traces.forEach((trace) => values.add(trace.server_name))
    latencyStats.forEach((record) => values.add(record.server_name))
    return ['', ...Array.from(values).sort()]
  }, [traces, latencyStats])

  const latencyChartData = useMemo(() => {
    const labels = latencyStats.map((record) => `${record.server_name} :: ${record.method}`)
    return {
      labels,
      datasets: [
        {
          label: 'P50',
          data: latencyStats.map((record) => record.p50_ms),
          backgroundColor: '#d97706',
        },
        {
          label: 'P95',
          data: latencyStats.map((record) => record.p95_ms),
          backgroundColor: '#2563eb',
        },
        {
          label: 'P99',
          data: latencyStats.map((record) => record.p99_ms),
          backgroundColor: '#8b5cf6',
        },
      ],
    }
  }, [latencyStats])

  return (
    <main className="shell">
      <nav className="topbar">
        <div>
          <p className="eyebrow">mcpscope dashboard</p>
          <h1>Observe MCP traffic live.</h1>
        </div>

        <div className="controls">
          <label>
            <span>Server</span>
            <select value={selectedServer} onChange={(event) => setSelectedServer(event.target.value)}>
              {serverOptions.map((option) => (
                <option key={option || 'all'} value={option}>
                  {option || 'All servers'}
                </option>
              ))}
            </select>
          </label>

          {activeTab !== 'traces' ? (
            <label>
              <span>Window</span>
              <select value={windowKey} onChange={(event) => setWindowKey(event.target.value as WindowKey)}>
                {windows.map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </select>
            </label>
          ) : null}
        </div>
      </nav>

      <section className="tabbar">
        {tabs.map((tab) => (
          <button
            key={tab.key}
            type="button"
            className={activeTab === tab.key ? 'tab active' : 'tab'}
            onClick={() => setActiveTab(tab.key)}
          >
            {tab.label}
          </button>
        ))}
      </section>

      <section className="hero">
        <div className="status-panel">
          <div>
            <span>Stream</span>
            <strong data-state={streamState}>{streamState}</strong>
          </div>
          <div>
            <span>Total traces</span>
            <strong>{stats.total}</strong>
          </div>
          <div>
            <span>Error traces</span>
            <strong>{stats.errors}</strong>
          </div>
          <div>
            <span>Avg latency</span>
            <strong>{stats.avgLatency} ms</strong>
          </div>
        </div>
      </section>

      {activeTab === 'traces' ? (
        <section className="table-card">
          <div className="table-header">
            <div>
              <h2>Recent tool calls</h2>
              <p>Newest traces appear instantly as `mcpscope` intercepts them.</p>
            </div>
          </div>
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Timestamp</th>
                  <th>Server</th>
                  <th>Method</th>
                  <th>Latency</th>
                  <th>Status</th>
                </tr>
              </thead>
              <tbody>
                {traces.length === 0 ? (
                  <tr>
                    <td colSpan={5} className="empty">
                      No traces yet. Start calling tools through the proxy to populate the feed.
                    </td>
                  </tr>
                ) : (
                  traces.map((trace) => {
                    const expanded = expandedId === trace.id
                    return (
                      <Fragment key={trace.id}>
                        <tr
                          className="summary-row"
                          onClick={() =>
                            setExpandedId((current) => (current === trace.id ? null : trace.id))
                          }
                        >
                          <td>{formatTimestamp(trace.created_at)}</td>
                          <td>{trace.server_name}</td>
                          <td>{trace.method || '(response)'}</td>
                          <td>{trace.latency_ms} ms</td>
                          <td>
                            <span className={trace.is_error ? 'pill error' : 'pill success'}>
                              {trace.is_error ? 'error' : 'success'}
                            </span>
                          </td>
                        </tr>
                        {expanded ? (
                          <tr className="detail-row">
                            <td colSpan={5}>
                              <div className="detail-grid">
                                <div>
                                  <h3>Params</h3>
                                  <pre>{formatPayload(trace.params)}</pre>
                                </div>
                                <div>
                                  <h3>Response</h3>
                                  <pre>{formatPayload(trace.response)}</pre>
                                </div>
                              </div>
                              {trace.error_message ? (
                                <p className="error-text">Error: {trace.error_message}</p>
                              ) : null}
                            </td>
                          </tr>
                        ) : null}
                      </Fragment>
                    )
                  })
                )}
              </tbody>
            </table>
          </div>
        </section>
      ) : null}

      {activeTab === 'latency' ? (
        <section className="panel-card">
          <div className="panel-header">
            <div>
              <h2>Latency percentiles</h2>
              <p>P50, P95, and P99 latency grouped by server and method over the selected window.</p>
            </div>
          </div>
          {latencyStats.length === 0 ? (
            <p className="empty-block">No latency stats available for the selected window.</p>
          ) : (
            <div className="chart-wrap">
              <Bar
                data={latencyChartData}
                options={{
                  responsive: true,
                  maintainAspectRatio: false,
                  plugins: {
                    legend: {
                      position: 'top',
                    },
                  },
                }}
              />
            </div>
          )}
        </section>
      ) : null}

      {activeTab === 'errors' ? (
        <section className="panel-card">
          <div className="panel-header">
            <div>
              <h2>Error timeline</h2>
              <p>Methods with their current error rate and most recent error in the selected window.</p>
            </div>
          </div>

          {errorStats.length === 0 ? (
            <p className="empty-block">No error stats available for the selected window.</p>
          ) : (
            <div className="timeline">
              {errorStats.map((item) => (
                <article key={item.method} className="timeline-item">
                  <div className="timeline-top">
                    <div>
                      <h3>{item.method}</h3>
                      <p>{item.error_count} errors out of {item.count} calls</p>
                    </div>
                    <span className={item.error_rate_pct > 0 ? 'pill error' : 'pill success'}>
                      {item.error_rate_pct.toFixed(1)}%
                    </span>
                  </div>
                  <p className="timeline-message">{item.recent_error_message || 'No recent error message'}</p>
                  <p className="timeline-meta">
                    {item.recent_error_at ? formatTimestamp(item.recent_error_at) : 'No recent error'}
                  </p>
                </article>
              ))}
            </div>
          )}
        </section>
      ) : null}
    </main>
  )
}

function withQuery(params: Record<string, string>) {
  const entries = Object.entries(params).filter(([, value]) => value)
  if (entries.length === 0) {
    return ''
  }

  const search = new URLSearchParams(entries)
  return `?${search.toString()}`
}

function formatTimestamp(value: string) {
  const date = new Date(value)
  return `${date.toLocaleDateString()} ${date.toLocaleTimeString()}`
}

function formatPayload(payload: unknown) {
  if (payload === undefined || payload === null || payload === '') {
    return 'No payload'
  }

  return JSON.stringify(payload, null, 2)
}

export default App
