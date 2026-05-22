import { Fragment, useEffect, useMemo, useRef, useState } from 'react'
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

type TabKey = 'traces' | 'latency' | 'errors' | 'alerts' | 'budgets'
type WindowKey = '5m' | '30m' | '1h'
type StatusFilter = '' | 'success' | 'error' | 'blocked'
type AlertRuleType = 'error_rate' | 'latency_p95'

type TraceRecord = {
  id: string
  trace_id: string
  workspace: string
  environment: string
  team_id: string
  server_id: string
  server_name: string
  method: string
  status: string
  params?: unknown
  response?: unknown
  latency_ms: number
  is_error: boolean
  error_message?: string
  sdk_reported: boolean
  created_at: string
}

type ReplayResult = {
  trace_id: string
  server: string
  match: boolean
  latency_ms: number
  diff: string
  status: string
}

type TraceListResponse = {
  items: TraceRecord[]
  offset: number
  limit: number
  has_more: boolean
  next_offset: number
}

type LatencyStatRecord = {
  server_id: string
  server_name: string
  method: string
  count: number
  p50_ms: number
  p95_ms: number
  p99_ms: number
}

type ErrorStatRecord = {
  server_id: string
  workspace: string
  environment: string
  method: string
  count: number
  error_count: number
  error_rate_pct: number
  recent_error_message?: string
  recent_error_at?: string
}

type AlertRule = {
  id: string
  workspace: string
  environment: string
  name: string
  rule_type: AlertRuleType
  threshold: number
  window_minutes: number
  server_name?: string
  method?: string
  enabled: boolean
}

type AlertRuleDraft = {
  id: string
  name: string
  rule_type: AlertRuleType
  threshold: string
  window_minutes: string
  server_name: string
  method: string
  enabled: boolean
}

type AlertEvaluation = {
  rule_id: string
  name: string
  rule_type: AlertRuleType
  status: 'firing' | 'ok' | 'no_data' | 'disabled'
  threshold: number
  current_value: number
  window_minutes: number
  server_name?: string
  method?: string
  sample_count: number
  last_evaluated_at: string
}

type AlertEvent = {
  id: string
  rule_id: string
  workspace: string
  environment: string
  rule_name: string
  status: string
  previous_status: string
  current_value: number
  threshold: number
  sample_count: number
  notification?: string
  delivery_status?: string
  delivery_error?: string
  created_at: string
}

type ConfiguredAlertRule = {
  name: string
  condition: string
  channels: string[]
  last_fired_at?: string
}

type BudgetRecord = {
  team_id: string
  header: string
  window_type: 'hour' | 'day' | string
  window_start: string
  usage: {
    team_id: string
    window_type: string
    window_start: string
    call_count: number
    token_count: number
  }
  limits: {
    calls_per_hour: number
    calls_per_day: number
    tokens_per_day: number
  }
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
  { key: 'alerts', label: 'Alerts' },
  { key: 'budgets', label: 'Budgets' },
]

function App() {
  const [activeTab, setActiveTab] = useState<TabKey>('traces')
  const [windowKey, setWindowKey] = useState<WindowKey>('5m')
  const [selectedServer, setSelectedServer] = useState<string>('')
  const selectedServerRef = useRef('')
  const [methodFilter, setMethodFilter] = useState<string>('')
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('')
  const [traceSearch, setTraceSearch] = useState('')
  const [traceAfter, setTraceAfter] = useState('')
  const [traceBefore, setTraceBefore] = useState('')
  const [workspace, setWorkspace] = useState(() => readStoredValue('mcpscope.workspace', 'default'))
  const [environment, setEnvironment] = useState(() => readStoredValue('mcpscope.environment', 'default'))
  const [authToken, setAuthToken] = useState(() => readStoredValue('mcpscope.authToken', ''))

  const [traces, setTraces] = useState<TraceRecord[]>([])
  const [traceOffset, setTraceOffset] = useState(0)
  const [hasMoreTraces, setHasMoreTraces] = useState(false)
  const [loadingMore, setLoadingMore] = useState(false)
  const [latencyStats, setLatencyStats] = useState<LatencyStatRecord[]>([])
  const [errorStats, setErrorStats] = useState<ErrorStatRecord[]>([])
  const [configuredAlertRules, setConfiguredAlertRules] = useState<ConfiguredAlertRule[]>([])
  const [budgetRecords, setBudgetRecords] = useState<BudgetRecord[]>([])
  const [alertRules, setAlertRules] = useState<AlertRule[]>([])
  const [alertEvaluations, setAlertEvaluations] = useState<AlertEvaluation[]>([])
  const [alertEvents, setAlertEvents] = useState<AlertEvent[]>([])
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [replayResult, setReplayResult] = useState<ReplayResult | null>(null)
  const [replayBusy, setReplayBusy] = useState(false)
  const [streamState, setStreamState] = useState<'connecting' | 'live' | 'closed'>('connecting')
  const [errorMessage, setErrorMessage] = useState('')
  const [alertDraft, setAlertDraft] = useState<AlertRuleDraft>(() => emptyAlertDraft())

  useEffect(() => {
    window.localStorage.setItem('mcpscope.workspace', workspace)
  }, [workspace])

  useEffect(() => {
    window.localStorage.setItem('mcpscope.environment', environment)
  }, [environment])

  useEffect(() => {
    window.localStorage.setItem('mcpscope.authToken', authToken)
  }, [authToken])

  useEffect(() => {
    selectedServerRef.current = selectedServer
  }, [selectedServer])

  useEffect(() => {
    let active = true

    const loadTraces = async (offset = 0, append = false) => {
      const response = await apiFetch(
        apiURL(
          '/api/traces',
          authToken,
          buildTraceParams({
            workspace,
            environment,
            serverId: '',
            method: methodFilter,
            status: statusFilter,
            search: traceSearch,
            createdAfter: traceAfter,
            createdBefore: traceBefore,
            limit: '50',
            offset: String(offset),
          }),
        ),
        authToken,
      )
      const data = (await response.json()) as TraceListResponse
      if (!active) {
        return
      }

      setErrorMessage('')
      setTraceOffset(data.next_offset)
      setHasMoreTraces(data.has_more)
      setTraces((current) => (append ? [...current, ...data.items] : data.items))
    }

    loadTraces().catch((error: unknown) => {
      if (!active) {
        return
      }
      setErrorMessage(asErrorMessage(error))
      setTraces([])
      setTraceOffset(0)
      setHasMoreTraces(false)
    })

    const source = new EventSource(
      apiURL(
        '/events',
        authToken,
        buildTraceParams({
          workspace,
          environment,
          serverId: '',
          method: methodFilter,
          status: statusFilter,
          search: traceSearch,
          createdAfter: traceAfter,
          createdBefore: traceBefore,
          limit: '',
          offset: '',
        }),
      ),
    )
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
      if (selectedServerRef.current && next.server_id !== selectedServerRef.current) {
        return
      }
      setTraces((current) => {
        const deduped = current.filter((trace) => trace.id !== next.id)
        return [next, ...deduped].slice(0, Math.max(deduped.length, 50))
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
  }, [authToken, workspace, environment, methodFilter, statusFilter, traceSearch, traceAfter, traceBefore])

  useEffect(() => {
    let active = true

    const loadPanels = async () => {
      const [
        latencyResponse,
        errorResponse,
        configuredRuleResponse,
        budgetResponse,
        ruleResponse,
        evaluationResponse,
        eventResponse,
      ] =
        await Promise.all([
          apiFetch(
            apiURL('/api/stats/latency', authToken, {
              environment,
              workspace,
              window: windowKey,
              server_id: selectedServer,
              method: methodFilter,
            }),
            authToken,
          ),
          apiFetch(
            apiURL('/api/stats/errors', authToken, {
              environment,
              workspace,
              window: windowKey,
              server_id: selectedServer,
              method: methodFilter,
            }),
            authToken,
          ),
          apiFetch(apiURL('/api/alert-rules', authToken, { workspace, environment }), authToken),
          apiFetch(apiURL('/api/budgets', authToken, {}), authToken),
          apiFetch(apiURL('/api/alerts/rules', authToken, { workspace, environment }), authToken),
          apiFetch(apiURL('/api/alerts/evaluations', authToken, { workspace, environment }), authToken),
          apiFetch(apiURL('/api/alerts/events', authToken, { workspace, environment }), authToken),
        ])

      const [latencyData, errorData, configuredRules, budgets, rules, evaluations, events] = (await Promise.all([
        latencyResponse.json(),
        errorResponse.json(),
        configuredRuleResponse.json(),
        budgetResponse.json(),
        ruleResponse.json(),
        evaluationResponse.json(),
        eventResponse.json(),
      ])) as [
        LatencyStatRecord[],
        ErrorStatRecord[],
        ConfiguredAlertRule[],
        BudgetRecord[],
        AlertRule[],
        AlertEvaluation[],
        AlertEvent[],
      ]

      if (!active) {
        return
      }

      setErrorMessage('')
      setLatencyStats(latencyData)
      setErrorStats(errorData)
      setConfiguredAlertRules(configuredRules)
      setBudgetRecords(budgets)
      setAlertRules(rules)
      setAlertEvaluations(evaluations)
      setAlertEvents(events)
    }

    loadPanels().catch((error: unknown) => {
      if (!active) {
        return
      }
      setErrorMessage(asErrorMessage(error))
      setLatencyStats([])
      setErrorStats([])
      setConfiguredAlertRules([])
      setBudgetRecords([])
      setAlertRules([])
      setAlertEvaluations([])
      setAlertEvents([])
    })

    const interval = window.setInterval(() => {
      loadPanels().catch((error: unknown) => {
        if (!active) {
          return
        }
        setErrorMessage(asErrorMessage(error))
        setConfiguredAlertRules([])
      })
    }, 10_000)

    return () => {
      active = false
      window.clearInterval(interval)
    }
  }, [authToken, workspace, environment, selectedServer, windowKey, methodFilter])

  const stats = useMemo(() => {
    const total = traces.length
    const errors = traces.filter((trace) => trace.is_error).length
    const avgLatency =
      total === 0
        ? 0
        : Math.round(traces.reduce((sum, trace) => sum + trace.latency_ms, 0) / total)
    const firingAlerts = alertEvaluations.filter((alert) => alert.status === 'firing').length

    return { total, errors, avgLatency, firingAlerts }
  }, [traces, alertEvaluations])

  const serverOptions = useMemo(() => {
    const labels = new Map<string, string>()

    const addServer = (serverId: string, serverName?: string) => {
      const value = serverId.trim()
      if (!value) {
        return
      }
      if (labels.has(value)) {
        return
      }
      const name = serverName?.trim() || value
      labels.set(value, name === value ? name : `${name} (${value})`)
    }

    traces.forEach((trace) => addServer(trace.server_id, trace.server_name))
    latencyStats.forEach((record) => addServer(record.server_id, record.server_name))
    errorStats.forEach((record) => addServer(record.server_id, record.server_id))

    return [
      { value: '', label: 'All servers' },
      ...Array.from(labels.entries())
        .sort((a, b) => a[1].localeCompare(b[1]))
        .map(([value, label]) => ({ value, label })),
    ]
  }, [traces, latencyStats, errorStats])

  const latencyChartData = useMemo(() => {
    const labels = latencyStats.map((record) => `${record.server_name || record.server_id} :: ${record.method}`)
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

  const loadMoreTraces = async () => {
    setLoadingMore(true)
    try {
      const response = await apiFetch(
        apiURL(
          '/api/traces',
          authToken,
          buildTraceParams({
            workspace,
            environment,
            serverId: selectedServer,
            method: methodFilter,
            status: statusFilter,
            search: traceSearch,
            createdAfter: traceAfter,
            createdBefore: traceBefore,
            limit: '50',
            offset: String(traceOffset),
          }),
        ),
        authToken,
      )
      const data = (await response.json()) as TraceListResponse
      setErrorMessage('')
      setTraceOffset(data.next_offset)
      setHasMoreTraces(data.has_more)
      setTraces((current) => [...current, ...data.items])
    } catch (error) {
      setErrorMessage(asErrorMessage(error))
    } finally {
      setLoadingMore(false)
    }
  }

  const refreshAlerts = async () => {
    const [rulesResponse, evaluationsResponse, eventsResponse] = await Promise.all([
      apiFetch(apiURL('/api/alerts/rules', authToken, { workspace, environment }), authToken),
      apiFetch(apiURL('/api/alerts/evaluations', authToken, { workspace, environment }), authToken),
      apiFetch(apiURL('/api/alerts/events', authToken, { workspace, environment }), authToken),
    ])

    setAlertRules((await rulesResponse.json()) as AlertRule[])
    setAlertEvaluations((await evaluationsResponse.json()) as AlertEvaluation[])
    setAlertEvents((await eventsResponse.json()) as AlertEvent[])
  }

  const startEditingRule = (rule: AlertRule) => {
    setAlertDraft({
      id: rule.id,
      name: rule.name,
      rule_type: rule.rule_type,
      threshold: String(rule.threshold),
      window_minutes: String(rule.window_minutes),
      server_name: rule.server_name ?? '',
      method: rule.method ?? '',
      enabled: rule.enabled,
    })
  }

  const clearAlertDraft = () => {
    setAlertDraft(emptyAlertDraft())
  }

  const upsertAlertRule = async (draft: AlertRuleDraft) => {
    await apiFetch(apiURL('/api/alerts/rules', authToken, { workspace, environment }), authToken, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        id: draft.id || undefined,
        workspace,
        environment,
        name: draft.name,
        rule_type: draft.rule_type,
        threshold: Number(draft.threshold),
        window_minutes: Number(draft.window_minutes),
        server_name: draft.server_name,
        method: draft.method,
        enabled: draft.enabled,
      }),
    })
  }

  const saveAlertRule = async () => {
    try {
      await upsertAlertRule(alertDraft)
      clearAlertDraft()
      setErrorMessage('')
      await refreshAlerts()
    } catch (error) {
      setErrorMessage(asErrorMessage(error))
    }
  }

  const deleteAlertRule = async (id: string) => {
    try {
      const response = await apiFetch(
        apiURL('/api/alerts/rules', authToken, { workspace, environment, id }),
        authToken,
        { method: 'DELETE' },
      )
      if (!response.ok) {
        throw new Error(`failed to delete alert rule: ${response.status}`)
      }

      setErrorMessage('')
      await refreshAlerts()
    } catch (error) {
      setErrorMessage(asErrorMessage(error))
    }
  }

  const toggleAlertRule = async (rule: AlertRule) => {
    try {
      await upsertAlertRule({
        id: rule.id,
        name: rule.name,
        rule_type: rule.rule_type,
        threshold: String(rule.threshold),
        window_minutes: String(rule.window_minutes),
        server_name: rule.server_name ?? '',
        method: rule.method ?? '',
        enabled: !rule.enabled,
      })
      setErrorMessage('')
      await refreshAlerts()
    } catch (error) {
      setErrorMessage(asErrorMessage(error))
    }
  }

  const replayTrace = async (trace: TraceRecord) => {
    const defaultServer = trace.server_name.startsWith('http') ? trace.server_name : 'http://localhost:5555'
    const server = window.prompt('Replay against server URL', defaultServer)
    if (!server || server.trim() === '') {
      return
    }

    setReplayBusy(true)
    setReplayResult(null)
    try {
      const response = await apiFetch(
        apiURL('/api/replay', authToken, {}),
        authToken,
        {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
          },
          body: JSON.stringify({ trace_id: trace.trace_id, server: server.trim() }),
        },
      )
      const result = await readSsePayload(response)
      const payload = JSON.parse(result) as ReplayResult
      setReplayResult(payload)
      setErrorMessage('')
    } catch (error) {
      setErrorMessage(asErrorMessage(error))
    } finally {
      setReplayBusy(false)
    }
  }

  return (
    <main className="shell">
      <nav className="topbar">
        <div>
          <p className="eyebrow">mcpscope dashboard</p>
          <h1>Observe MCP traffic live.</h1>
        </div>

        <div className="controls">
          <label>
            <span>Workspace</span>
            <input value={workspace} onChange={(event) => setWorkspace(event.target.value || 'default')} />
          </label>

          <label>
            <span>Environment</span>
            <input value={environment} onChange={(event) => setEnvironment(event.target.value || 'default')} />
          </label>

          <label>
            <span>API Token</span>
            <input
              type="password"
              value={authToken}
              onChange={(event) => setAuthToken(event.target.value)}
              placeholder="optional"
            />
          </label>

          <label>
            <span>Server</span>
            <select value={selectedServer} onChange={(event) => setSelectedServer(event.target.value)}>
              {serverOptions.map((option) => (
                <option key={option.value || 'all'} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
          </label>

          <label>
            <span>Method</span>
            <input
              value={methodFilter}
              onChange={(event) => setMethodFilter(event.target.value)}
              placeholder="tools/call"
            />
          </label>

          <label className="trace-search">
            <span>Search</span>
            <input
              value={traceSearch}
              onChange={(event) => setTraceSearch(event.target.value)}
              placeholder="trace id, method, payload..."
            />
          </label>

          <label className="date-control">
            <span>From</span>
            <input type="datetime-local" value={traceAfter} onChange={(event) => setTraceAfter(event.target.value)} />
          </label>

          <label className="date-control">
            <span>To</span>
            <input type="datetime-local" value={traceBefore} onChange={(event) => setTraceBefore(event.target.value)} />
          </label>

          <label>
            <span>Status</span>
            <select value={statusFilter} onChange={(event) => setStatusFilter(event.target.value as StatusFilter)}>
              <option value="">All statuses</option>
              <option value="success">Success</option>
              <option value="error">Error</option>
              <option value="blocked">Blocked</option>
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

      {errorMessage ? <section className="error-banner">{errorMessage}</section> : null}

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
            <span>Workspace</span>
            <strong>{workspace || 'default'}</strong>
          </div>
          <div>
            <span>Environment</span>
            <strong>{environment || 'default'}</strong>
          </div>
          <div>
            <span>Server</span>
            <strong>{selectedServer ? (serverOptions.find((option) => option.value === selectedServer)?.label ?? selectedServer) : 'All servers'}</strong>
          </div>
          <div>
            <span>Stream</span>
            <strong data-state={streamState}>{streamState}</strong>
          </div>
          <div>
            <span>Visible traces</span>
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
          <div>
            <span>Firing alerts</span>
            <strong data-state={stats.firingAlerts > 0 ? 'closed' : 'live'}>{stats.firingAlerts}</strong>
          </div>
        </div>
      </section>

      {activeTab === 'traces' ? (
        <section className="table-card">
          <div className="table-header table-header-actions">
            <div>
              <h2>Recent tool calls</h2>
              <p>Traces are paginated, retained by policy, and already redacted before they reach the UI.</p>
            </div>
            <a
              className="load-more export-link"
              href={apiURL('/api/export/traces', authToken, {
                environment,
            workspace,
            server_id: selectedServer,
            method: methodFilter,
            status: statusFilter,
            search: traceSearch,
                created_after: toQueryTimestamp(traceAfter),
                created_before: toQueryTimestamp(traceBefore),
                limit: '200',
              })}
            >
              Export JSON
            </a>
          </div>
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Timestamp</th>
                  <th>Trace ID</th>
                  <th>Workspace</th>
                  <th>Environment</th>
                  <th>Server</th>
                  <th>Method</th>
                  <th>Latency</th>
                  <th>Status</th>
                </tr>
              </thead>
              <tbody>
                {traces.length === 0 ? (
                  <tr>
                    <td colSpan={8} className="empty">
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
                          <td className="mono">{trace.trace_id}</td>
                          <td>{trace.workspace}</td>
                          <td>{trace.environment}</td>
                          <td>
                            <div>{trace.server_name || trace.server_id}</div>
                            {trace.server_name && trace.server_name !== trace.server_id ? (
                              <div className="muted mono">{trace.server_id}</div>
                            ) : null}
                          </td>
                      <td>{trace.method || '(response)'}</td>
                      <td>{trace.latency_ms} ms</td>
                      <td>
                            <span className={traceStatusClass(trace.status || (trace.is_error ? 'error' : 'success'))}>
                              {trace.status || (trace.is_error ? 'error' : 'success')}
                            </span>
                      </td>
                    </tr>
                        {expanded ? (
                          <tr className="detail-row">
                            <td colSpan={8}>
                              {trace.sdk_reported ? (
                                <div className="detail-badges">
                                  <span className="pill neutral detail-pill">SDK reported</span>
                                </div>
                              ) : null}
                              <div className="detail-actions">
                                <button
                                  type="button"
                                  className="inline-action"
                                  onClick={() => replayTrace(trace)}
                                  disabled={replayBusy}
                                >
                                  {replayBusy ? 'Replaying...' : 'Replay'}
                                </button>
                              </div>
                              {replayResult && replayResult.trace_id === trace.trace_id ? (
                                <div className="replay-result">
                                  <p className={replayResult.match ? 'pill success' : 'pill error'}>
                                    {replayResult.match ? 'match' : 'mismatch'}
                                  </p>
                                  <pre>{replayResult.diff || 'Responses match'}</pre>
                                </div>
                              ) : null}
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
          {hasMoreTraces ? (
            <div className="footer-actions">
              <button type="button" className="load-more" onClick={loadMoreTraces} disabled={loadingMore}>
                {loadingMore ? 'Loading...' : 'Load More'}
              </button>
            </div>
          ) : null}
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
                <article key={`${item.environment}:${item.method}`} className="timeline-item">
                  <div className="timeline-top">
                    <div>
                      <h3>{item.method}</h3>
                      <p>
                        {item.server_id} · {item.error_count} errors out of {item.count} calls
                      </p>
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

      {activeTab === 'budgets' ? (
        <section className="panel-card">
          <div className="panel-header">
            <div>
              <h2>Budget usage</h2>
              <p>Current call and token usage tracked per team and window.</p>
            </div>
          </div>
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Team</th>
                  <th>Header</th>
                  <th>Window</th>
                  <th>Calls</th>
                  <th>Tokens</th>
                </tr>
              </thead>
              <tbody>
                {budgetRecords.length === 0 ? (
                  <tr>
                    <td colSpan={5} className="empty">
                      No budget rows yet. Add a budgets config and route traffic through the proxy.
                    </td>
                  </tr>
                ) : (
                  budgetRecords.map((record) => (
                    <tr key={`${record.team_id}:${record.window_type}:${record.window_start}`}>
                      <td>{record.team_id}</td>
                      <td>{record.header || 'default'}</td>
                      <td>
                        {record.window_type} · {formatTimestamp(record.window_start)}
                      </td>
                      <td>
                        <div className="budget-metric">
                          <strong>{record.usage.call_count}</strong>
                          <span>/ {budgetLimitForWindow(record, 'calls')}</span>
                        </div>
                      </td>
                      <td>
                        <div className="budget-metric">
                          <strong>{record.usage.token_count}</strong>
                          <span>/ {budgetLimitForWindow(record, 'tokens')}</span>
                        </div>
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        </section>
      ) : null}

      {activeTab === 'alerts' ? (
        <section className="alerts-layout">
          <section className="panel-card alert-config-card">
            <div className="panel-header">
              <div>
                <h2>Configured Alerting</h2>
                <p>YAML-backed Slack and PagerDuty rules loaded at startup.</p>
              </div>
            </div>
            <div className="timeline timeline-compact">
              {configuredAlertRules.length === 0 ? (
                <p className="empty-block">No configured alert rules loaded.</p>
              ) : (
                configuredAlertRules.map((rule) => (
                  <article key={rule.name} className="timeline-item">
                    <div className="timeline-top">
                      <div>
                        <h3>{rule.name}</h3>
                        <p className="mono">{rule.condition}</p>
                      </div>
                      <span className="pill neutral">{rule.channels.join(', ')}</span>
                    </div>
                    <p className="timeline-meta">
                      {rule.last_fired_at ? `Last fired ${formatTimestamp(rule.last_fired_at)}` : 'Never fired'}
                    </p>
                  </article>
                ))
              )}
            </div>
          </section>

          <section className="panel-card">
            <div className="panel-header">
              <div>
                <h2>{alertDraft.id ? 'Edit Alert Rule' : 'Alert Rules'}</h2>
                <p>Thresholds are scoped to the active environment and can notify configured webhooks.</p>
              </div>
              {alertDraft.id ? (
                <button type="button" className="inline-action" onClick={clearAlertDraft}>
                  Cancel edit
                </button>
              ) : null}
            </div>
            <div className="alert-form">
              <label>
                <span>Name</span>
                <input
                  value={alertDraft.name}
                  onChange={(event) => setAlertDraft((current) => ({ ...current, name: event.target.value }))}
                />
              </label>
              <label>
                <span>Type</span>
                <select
                  value={alertDraft.rule_type}
                  onChange={(event) =>
                    setAlertDraft((current) => ({
                      ...current,
                      rule_type: event.target.value as AlertRuleType,
                    }))
                  }
                >
                  <option value="error_rate">Error Rate %</option>
                  <option value="latency_p95">Latency P95 ms</option>
                </select>
              </label>
              <label>
                <span>Threshold</span>
                <input
                  value={alertDraft.threshold}
                  onChange={(event) => setAlertDraft((current) => ({ ...current, threshold: event.target.value }))}
                />
              </label>
              <label>
                <span>Window Minutes</span>
                <input
                  value={alertDraft.window_minutes}
                  onChange={(event) =>
                    setAlertDraft((current) => ({ ...current, window_minutes: event.target.value }))
                  }
                />
              </label>
              <label>
                <span>Server</span>
                <input
                  value={alertDraft.server_name}
                  onChange={(event) =>
                    setAlertDraft((current) => ({ ...current, server_name: event.target.value }))
                  }
                  placeholder="optional"
                />
              </label>
              <label>
                <span>Method</span>
                <input
                  value={alertDraft.method}
                  onChange={(event) => setAlertDraft((current) => ({ ...current, method: event.target.value }))}
                  placeholder="optional"
                />
              </label>
              <label className="alert-enabled">
                <span>Enabled</span>
                <input
                  type="checkbox"
                  checked={alertDraft.enabled}
                  onChange={(event) =>
                    setAlertDraft((current) => ({ ...current, enabled: event.target.checked }))
                  }
                />
              </label>
              <button type="button" className="load-more" onClick={saveAlertRule}>
                {alertDraft.id ? 'Update Rule' : 'Save Rule'}
              </button>
            </div>

            <div className="timeline">
              {alertRules.length === 0 ? (
                <p className="empty-block">No alert rules yet.</p>
              ) : (
                alertRules.map((rule) => (
                  <article key={rule.id} className="timeline-item">
                    <div className="timeline-top">
                      <div>
                        <h3>{rule.name}</h3>
                        <p>
                          {rule.rule_type} threshold {rule.threshold}
                        </p>
                      </div>
                      <div className="rule-actions">
                        <span className={rule.enabled ? 'pill success' : 'pill neutral'}>
                          {rule.enabled ? 'enabled' : 'disabled'}
                        </span>
                        <button type="button" className="inline-action" onClick={() => startEditingRule(rule)}>
                          Edit
                        </button>
                        <button type="button" className="inline-action" onClick={() => toggleAlertRule(rule)}>
                          {rule.enabled ? 'Disable' : 'Enable'}
                        </button>
                        <button type="button" className="inline-action" onClick={() => deleteAlertRule(rule.id)}>
                          Delete
                        </button>
                      </div>
                    </div>
                    <p className="timeline-meta">
                      {rule.window_minutes}m {rule.server_name ? `| ${rule.server_name}` : ''}{' '}
                      {rule.method ? `| ${rule.method}` : ''}
                    </p>
                  </article>
                ))
              )}
            </div>
          </section>

          <section className="panel-card">
            <div className="panel-header">
              <div>
                <h2>Alert Activity</h2>
                <p>Live evaluations and persisted state changes for notifications and incident review.</p>
              </div>
            </div>
            <div className="timeline">
              {alertEvaluations.length === 0 ? (
                <p className="empty-block">No alert evaluations available.</p>
              ) : (
                alertEvaluations.map((item) => (
                  <article key={item.rule_id} className="timeline-item">
                    <div className="timeline-top">
                      <div>
                        <h3>{item.name}</h3>
                        <p>{item.rule_type} over {item.window_minutes}m window</p>
                      </div>
                      <span className={statusPillClass(item.status)}>
                        {item.status.replace('_', ' ')}
                      </span>
                    </div>
                    <p className="timeline-message">
                      Current {item.current_value.toFixed(1)} vs threshold {item.threshold.toFixed(1)}
                    </p>
                    <p className="timeline-meta">
                      {item.sample_count} traces evaluated
                      {item.last_evaluated_at ? ` | ${formatTimestamp(item.last_evaluated_at)}` : ''}
                    </p>
                  </article>
                ))
              )}
            </div>

            <div className="timeline timeline-secondary">
              <h3 className="subsection-title">Recent transitions</h3>
              {alertEvents.length === 0 ? (
                <p className="empty-block">No alert events recorded yet.</p>
              ) : (
                alertEvents.map((item) => (
                  <article key={item.id} className="timeline-item">
                    <div className="timeline-top">
                      <div>
                        <h3>{item.rule_name}</h3>
                        <p>
                          {item.previous_status || 'new'} to {item.status}
                        </p>
                      </div>
                      <span className={statusPillClass(item.status as AlertEvaluation['status'])}>
                        {item.delivery_status || 'stored'}
                      </span>
                    </div>
                    <p className="timeline-message">
                      Value {item.current_value.toFixed(1)} vs threshold {item.threshold.toFixed(1)}
                    </p>
                    <p className="timeline-meta">
                      {item.sample_count} traces | {formatTimestamp(item.created_at)}
                      {item.notification ? ` | ${item.notification}` : ''}
                    </p>
                    {item.delivery_error ? <p className="error-text">Delivery: {item.delivery_error}</p> : null}
                  </article>
                ))
              )}
            </div>
          </section>
        </section>
      ) : null}
    </main>
  )
}

function readStoredValue(key: string, fallback: string) {
  const value = window.localStorage.getItem(key)
  return value && value.trim() !== '' ? value : fallback
}

function emptyAlertDraft(): AlertRuleDraft {
  return {
    id: '',
    name: '',
    rule_type: 'error_rate',
    threshold: '5',
    window_minutes: '15',
    server_name: '',
    method: '',
    enabled: true,
  }
}

function buildTraceParams(input: {
  workspace: string
  environment: string
  serverId: string
  method: string
  status: StatusFilter
  search: string
  createdAfter: string
  createdBefore: string
  limit: string
  offset: string
}) {
  return {
    workspace: input.workspace,
    environment: input.environment,
    server_id: input.serverId,
    method: input.method,
    status: input.status,
    search: input.search,
    created_after: toQueryTimestamp(input.createdAfter),
    created_before: toQueryTimestamp(input.createdBefore),
    limit: input.limit,
    offset: input.offset,
  }
}

function toQueryTimestamp(value: string) {
  if (value.trim() === '') {
    return ''
  }

  const parsed = new Date(value)
  if (Number.isNaN(parsed.valueOf())) {
    return value
  }

  return parsed.toISOString()
}

async function apiFetch(input: string, authToken: string, init?: RequestInit) {
  const headers = new Headers(init?.headers)
  if (authToken.trim() !== '') {
    headers.set('Authorization', `Bearer ${authToken.trim()}`)
  }

  const response = await fetch(input, {
    ...init,
    headers,
  })
  if (!response.ok) {
    if (response.status === 401) {
      throw new Error('dashboard authentication failed; update the API token')
    }
    throw new Error(`request failed with status ${response.status}`)
  }
  return response
}

function apiURL(path: string, authToken: string, params: Record<string, string>) {
  const query = withQuery({
    ...params,
    token: authToken.trim() !== '' ? authToken.trim() : '',
  })
  return `${path}${query}`
}

async function readSsePayload(response: Response) {
  if (!response.body) {
    return response.text()
  }

  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  while (true) {
    const { value, done } = await reader.read()
    if (done) {
      break
    }
    buffer += decoder.decode(value, { stream: true })
    const events = buffer.split('\n\n')
    buffer = events.pop() ?? ''
    for (const event of events) {
      const lines = event.split('\n').filter((line) => line.startsWith('data:'))
      if (lines.length === 0) {
        continue
      }
      return lines
        .map((line) => line.slice(5).trimStart())
        .join('\n')
        .trim()
    }
  }

  const lines = buffer.split('\n').filter((line) => line.startsWith('data:'))
  if (lines.length === 0) {
    return ''
  }
  return lines
    .map((line) => line.slice(5).trimStart())
    .join('\n')
    .trim()
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

function traceStatusClass(status: string) {
  if (status === 'blocked') {
    return 'pill warning'
  }
  if (status === 'error') {
    return 'pill error'
  }
  return 'pill success'
}

function budgetLimitForWindow(record: BudgetRecord, kind: 'calls' | 'tokens') {
  if (kind === 'calls') {
    if (record.window_type === 'hour') {
      return record.limits.calls_per_hour > 0 ? String(record.limits.calls_per_hour) : 'unlimited'
    }
    return record.limits.calls_per_day > 0 ? String(record.limits.calls_per_day) : 'unlimited'
  }

  if (record.window_type === 'day' && record.limits.tokens_per_day > 0) {
    return String(record.limits.tokens_per_day)
  }
  return 'n/a'
}

function statusPillClass(status: AlertEvaluation['status']) {
  if (status === 'firing') {
    return 'pill error'
  }
  if (status === 'ok') {
    return 'pill success'
  }
  return 'pill neutral'
}

function asErrorMessage(error: unknown) {
  if (error instanceof Error) {
    return error.message
  }
  return 'request failed'
}

export default App
