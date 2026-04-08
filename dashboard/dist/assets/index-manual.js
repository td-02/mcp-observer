const state = {
  workspace: localStorage.getItem('mcpscope.workspace') || 'default',
  environment: localStorage.getItem('mcpscope.environment') || 'default',
  token: localStorage.getItem('mcpscope.authToken') || '',
  server: '',
  method: '',
  status: '',
  window: '5m',
  tab: 'traces',
  traces: [],
  latency: [],
  errors: [],
  rules: [],
  events: [],
  evaluations: [],
  stream: 'connecting',
  error: '',
}

let eventSource

function qs(path, params = {}) {
  const search = new URLSearchParams()
  Object.entries(params).forEach(([key, value]) => {
    if (value) search.set(key, value)
  })
  if (state.token) search.set('token', state.token)
  const query = search.toString()
  return query ? `${path}?${query}` : path
}

async function api(path, params = {}) {
  const headers = {}
  if (state.token) headers.Authorization = `Bearer ${state.token}`
  const response = await fetch(qs(path, params), { headers })
  if (!response.ok) {
    throw new Error(response.status === 401 ? 'dashboard authentication failed' : `request failed: ${response.status}`)
  }
  return response.json()
}

function pill(status) {
  if (status === 'error' || status === 'firing' || status === 'failed') return 'pill bad'
  if (status === 'success' || status === 'ok' || status === 'sent' || status === 'live') return 'pill good'
  return 'pill neutral'
}

function fmtTime(value) {
  return new Date(value).toLocaleString()
}

function stats() {
  const total = state.traces.length
  const errors = state.traces.filter((trace) => trace.is_error).length
  const avg = total ? Math.round(state.traces.reduce((sum, trace) => sum + trace.latency_ms, 0) / total) : 0
  const firing = state.evaluations.filter((item) => item.status === 'firing').length
  return { total, errors, avg, firing }
}

function render() {
  localStorage.setItem('mcpscope.workspace', state.workspace)
  localStorage.setItem('mcpscope.environment', state.environment)
  localStorage.setItem('mcpscope.authToken', state.token)

  const summary = stats()
  const app = document.getElementById('app')
  app.innerHTML = `
    <section class="topbar">
      <div>
        <h1 class="title">mcpscope</h1>
        <p class="subtitle">Observe MCP traffic, alerts, and schema checks from one proxy.</p>
      </div>
      <div class="controls">
        ${field('Workspace', 'workspace', state.workspace)}
        ${field('Environment', 'environment', state.environment)}
        ${field('API Token', 'token', state.token, 'password')}
        ${field('Server', 'server', state.server)}
        ${field('Method', 'method', state.method)}
        ${selectField('Status', 'status', state.status, [
          ['', 'All'],
          ['success', 'Success'],
          ['error', 'Error'],
        ])}
        ${selectField('Window', 'window', state.window, [
          ['5m', 'Last 5m'],
          ['30m', 'Last 30m'],
          ['1h', 'Last 1h'],
        ])}
        <div class="field"><label>&nbsp;</label><button id="refresh">Refresh</button></div>
      </div>
    </section>
    ${state.error ? `<div class="banner">${escapeHtml(state.error)}</div>` : ''}
    <section class="tabs">
      ${tab('traces', 'Traces')}
      ${tab('latency', 'Latency')}
      ${tab('errors', 'Errors')}
      ${tab('alerts', 'Alerts')}
    </section>
    <section class="hero">
      ${statCard('Workspace', state.workspace)}
      ${statCard('Environment', state.environment)}
      ${statCard('Stream', state.stream, pill(state.stream))}
      ${statCard('Visible traces', String(summary.total))}
      ${statCard('Errors', String(summary.errors))}
      ${statCard('Avg latency', `${summary.avg} ms`)}
      ${statCard('Firing alerts', String(summary.firing), pill(summary.firing ? 'failed' : 'sent'))}
    </section>
    ${renderPanel()}
  `

  document.querySelectorAll('[data-tab]').forEach((button) => {
    button.addEventListener('click', () => {
      state.tab = button.dataset.tab
      render()
    })
  })
  document.querySelectorAll('[data-field]').forEach((input) => {
    input.addEventListener('change', () => {
      state[input.dataset.field] = input.value
      connectEvents()
      loadAll()
    })
  })
  document.getElementById('refresh').addEventListener('click', () => loadAll())
}

function field(label, key, value, type = 'text') {
  return `<div class="field"><label>${label}</label><input data-field="${key}" type="${type}" value="${escapeAttr(value)}" /></div>`
}

function selectField(label, key, value, options) {
  return `<div class="field"><label>${label}</label><select data-field="${key}">${options
    .map(([optionValue, optionLabel]) => `<option value="${optionValue}" ${optionValue === value ? 'selected' : ''}>${optionLabel}</option>`)
    .join('')}</select></div>`
}

function tab(key, label) {
  return `<button class="tab ${state.tab === key ? 'active' : ''}" data-tab="${key}">${label}</button>`
}

function statCard(label, value, extraClass = '') {
  return `<div class="stat"><span>${label}</span><strong class="${extraClass}">${escapeHtml(value)}</strong></div>`
}

function renderPanel() {
  if (state.tab === 'latency') {
    return panel('Latency', 'SQL-backed percentile stats for the active window.', renderLatency())
  }
  if (state.tab === 'errors') {
    return panel('Errors', 'Recent method error rates in the active workspace/environment.', renderErrors())
  }
  if (state.tab === 'alerts') {
    return panel('Alerts', 'Current evaluations and recent delivery events.', renderAlerts())
  }
  return panel('Traces', 'Latest MCP calls captured through the proxy.', renderTraces())
}

function panel(title, subtitle, body) {
  return `<section class="panel"><div class="panel-header"><h2>${title}</h2><p>${subtitle}</p></div><div class="panel-body">${body}</div></section>`
}

function renderTraces() {
  if (!state.traces.length) return '<p class="empty">No traces yet.</p><table><tbody></tbody></table>'
  return `<table>
    <thead><tr><th>Time</th><th>Workspace</th><th>Environment</th><th>Server</th><th>Method</th><th>Latency</th><th>Status</th></tr></thead>
    <tbody>
      ${state.traces.map((trace) => `
        <tr>
          <td>${escapeHtml(fmtTime(trace.created_at))}</td>
          <td>${escapeHtml(trace.workspace || '')}</td>
          <td>${escapeHtml(trace.environment || '')}</td>
          <td>${escapeHtml(trace.server_name || '')}</td>
          <td>${escapeHtml(trace.method || '')}</td>
          <td>${trace.latency_ms} ms</td>
          <td><span class="${pill(trace.is_error ? 'error' : 'success')}">${trace.is_error ? 'error' : 'success'}</span></td>
        </tr>`).join('')}
    </tbody>
  </table>`
}

function renderLatency() {
  if (!state.latency.length) return '<p class="empty">No latency stats available.</p>'
  return `<div class="stack">${state.latency.map((item) => `
    <div class="item">
      <h3>${escapeHtml(item.server_name)} / ${escapeHtml(item.method)}</h3>
      <p>${item.count} calls</p>
      <p>P50 ${item.p50_ms} ms | P95 ${item.p95_ms} ms | P99 ${item.p99_ms} ms</p>
    </div>`).join('')}</div>`
}

function renderErrors() {
  if (!state.errors.length) return '<p class="empty">No error stats available.</p>'
  return `<div class="stack">${state.errors.map((item) => `
    <div class="item">
      <h3>${escapeHtml(item.method)}</h3>
      <p>${item.error_count} errors / ${item.count} calls</p>
      <p>${item.error_rate_pct.toFixed(1)}% error rate${item.recent_error_message ? ` | ${escapeHtml(item.recent_error_message)}` : ''}</p>
    </div>`).join('')}</div>`
}

function renderAlerts() {
  return `<div class="grid">
    <div class="stack">
      ${(state.evaluations.length ? state.evaluations : []).map((item) => `
        <div class="item">
          <h3>${escapeHtml(item.name)}</h3>
          <p>${escapeHtml(item.rule_type)} | ${item.current_value.toFixed(1)} / ${item.threshold.toFixed(1)}</p>
          <p><span class="${pill(item.status)}">${escapeHtml(item.status)}</span></p>
        </div>`).join('') || '<p class="empty">No alert evaluations.</p>'}
    </div>
    <div class="stack">
      ${(state.events.length ? state.events : []).map((item) => `
        <div class="item">
          <h3>${escapeHtml(item.rule_name)}</h3>
          <p>${escapeHtml(item.previous_status || 'new')} to ${escapeHtml(item.status)}</p>
          <p>${escapeHtml(item.notification || 'stored')} | ${escapeHtml(item.delivery_status || 'skipped')}</p>
        </div>`).join('') || '<p class="empty">No alert events.</p>'}
    </div>
  </div>`
}

function escapeHtml(value) {
  return String(value ?? '').replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;')
}

function escapeAttr(value) {
  return escapeHtml(value).replaceAll('"', '&quot;')
}

async function loadAll() {
  try {
    state.error = ''
    const params = {
      workspace: state.workspace,
      environment: state.environment,
      server: state.server,
      method: state.method,
      status: state.status,
    }
    const [traces, latency, errors, rules, evaluations, events] = await Promise.all([
      api('/api/traces', { ...params, limit: '50' }),
      api('/api/stats/latency', { workspace: state.workspace, environment: state.environment, window: state.window, server: state.server, method: state.method }),
      api('/api/stats/errors', { workspace: state.workspace, environment: state.environment, window: state.window, server: state.server, method: state.method }),
      api('/api/alerts/rules', { workspace: state.workspace, environment: state.environment }),
      api('/api/alerts/evaluations', { workspace: state.workspace, environment: state.environment }),
      api('/api/alerts/events', { workspace: state.workspace, environment: state.environment }),
    ])
    state.traces = traces.items || []
    state.latency = latency || []
    state.errors = errors || []
    state.rules = rules || []
    state.evaluations = evaluations || []
    state.events = events || []
  } catch (error) {
    state.error = error instanceof Error ? error.message : 'request failed'
    state.traces = []
    state.latency = []
    state.errors = []
    state.rules = []
    state.evaluations = []
    state.events = []
  }
  render()
}

function connectEvents() {
  if (eventSource) eventSource.close()
  state.stream = 'connecting'
  render()
  eventSource = new EventSource(qs('/events', {
    workspace: state.workspace,
    environment: state.environment,
    server: state.server,
    method: state.method,
    status: state.status,
  }))
  eventSource.onopen = () => {
    state.stream = 'live'
    render()
  }
  eventSource.onmessage = (event) => {
    const trace = JSON.parse(event.data)
    state.traces = [trace, ...state.traces.filter((item) => item.id !== trace.id)].slice(0, 50)
    render()
  }
  eventSource.onerror = () => {
    state.stream = 'closed'
    render()
  }
}

render()
connectEvents()
loadAll()
