package handler

import (
	"net/http"
)

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1.0"/>
<title>Wallet Transfer Service — Dashboard</title>
<style>
  :root {
    --bg: #0f1117; --surface: #1a1d27; --border: #2a2d3a;
    --primary: #6366f1; --success: #22c55e; --danger: #ef4444;
    --warn: #f59e0b; --text: #e2e8f0; --muted: #64748b;
  }
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { background: var(--bg); color: var(--text); font-family: 'Inter', system-ui, sans-serif; min-height: 100vh; padding: 24px; }
  h1 { font-size: 1.5rem; font-weight: 700; color: var(--primary); margin-bottom: 4px; }
  .subtitle { color: var(--muted); font-size: 0.85rem; margin-bottom: 24px; }
  .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(340px, 1fr)); gap: 16px; }
  .card { background: var(--surface); border: 1px solid var(--border); border-radius: 12px; padding: 20px; }
  .card h2 { font-size: 0.95rem; font-weight: 600; margin-bottom: 16px; display: flex; align-items: center; gap: 8px; }
  .badge { font-size: 0.65rem; font-weight: 700; padding: 2px 6px; border-radius: 4px; }
  .badge-post { background: #1d4ed8; color: #93c5fd; }
  .badge-get  { background: #166534; color: #86efac; }
  input, textarea {
    width: 100%; background: var(--bg); border: 1px solid var(--border);
    color: var(--text); border-radius: 8px; padding: 8px 12px;
    font-size: 0.85rem; margin-bottom: 10px; outline: none;
    transition: border-color .2s;
  }
  input:focus, textarea:focus { border-color: var(--primary); }
  label { font-size: 0.75rem; color: var(--muted); display: block; margin-bottom: 4px; }
  button {
    width: 100%; padding: 10px; background: var(--primary); color: #fff;
    border: none; border-radius: 8px; font-size: 0.85rem; font-weight: 600;
    cursor: pointer; transition: opacity .2s;
  }
  button:hover { opacity: 0.85; }
  .result { margin-top: 12px; background: var(--bg); border: 1px solid var(--border); border-radius: 8px; padding: 12px; display: none; }
  .result.show { display: block; }
  .result pre { font-size: 0.76rem; white-space: pre-wrap; word-break: break-all; color: var(--text); line-height: 1.6; }
  .result.ok  { border-color: var(--success); }
  .result.err { border-color: var(--danger); }
  .result.warn { border-color: var(--warn); }
  .status-bar { font-size: 0.72rem; margin-bottom: 6px; font-weight: 600; }
  .status-bar.ok   { color: var(--success); }
  .status-bar.err  { color: var(--danger); }
  .status-bar.warn { color: var(--warn); }
  .divider { height: 1px; background: var(--border); margin: 20px 0; }
  .load-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; margin-bottom: 10px; }
  .stat { background: var(--bg); border: 1px solid var(--border); border-radius: 8px; padding: 10px; text-align: center; }
  .stat .val { font-size: 1.4rem; font-weight: 700; color: var(--primary); }
  .stat .lbl { font-size: 0.7rem; color: var(--muted); margin-top: 2px; }
  .stat.ok .val { color: var(--success); }
  .stat.err .val { color: var(--danger); }
  .log { max-height: 160px; overflow-y: auto; background: var(--bg); border: 1px solid var(--border); border-radius: 8px; padding: 8px; font-size: 0.72rem; color: var(--muted); font-family: monospace; display: none; }
  .log.show { display: block; }
  .log p { margin-bottom: 2px; }
  .log .ok  { color: var(--success); }
  .log .err { color: var(--danger); }
  #health-dot { width: 8px; height: 8px; border-radius: 50%; background: var(--muted); display: inline-block; margin-right: 6px; }
  #health-dot.up { background: var(--success); }
</style>
</head>
<body>
<h1>🏦 Wallet Transfer Service</h1>
<p class="subtitle">Live API Dashboard &nbsp;·&nbsp; <span id="health-dot"></span><span id="health-status">Checking...</span></p>

<div class="grid">

  <!-- Create Wallet -->
  <div class="card">
    <h2><span class="badge badge-post">POST</span> /wallets &nbsp;— Create Wallet</h2>
    <label>Wallet ID</label>
    <input id="cw-id" placeholder="e.g. alice" value="alice"/>
    <label>Initial Balance (cents)</label>
    <input id="cw-bal" type="number" placeholder="10000" value="10000"/>
    <label>Currency</label>
    <input id="cw-cur" placeholder="USD" value="USD"/>
    <button onclick="createWallet()">Create Wallet</button>
    <div id="cw-res" class="result"><div class="status-bar" id="cw-status"></div><pre id="cw-body"></pre></div>
  </div>

  <!-- Get Wallet -->
  <div class="card">
    <h2><span class="badge badge-get">GET</span> /wallets/:id — Balance & Reconcile</h2>
    <label>Wallet ID</label>
    <input id="gw-id" placeholder="alice" value="alice"/>
    <label><input type="checkbox" id="gw-reconcile" checked/> &nbsp;Include ledger reconciliation</label>
    <br/><br/>
    <button onclick="getWallet()">Get Wallet</button>
    <div id="gw-res" class="result"><div class="status-bar" id="gw-status"></div><pre id="gw-body"></pre></div>
  </div>

  <!-- Create Transfer -->
  <div class="card">
    <h2><span class="badge badge-post">POST</span> /transfers — Execute Transfer</h2>
    <label>Idempotency Key</label>
    <input id="tx-key" placeholder="tx-uuid-001" value="tx-001"/>
    <label>From Wallet ID</label>
    <input id="tx-from" placeholder="alice" value="alice"/>
    <label>To Wallet ID</label>
    <input id="tx-to" placeholder="bob" value="bob"/>
    <label>Amount (cents)</label>
    <input id="tx-amt" type="number" placeholder="1000" value="1000"/>
    <button onclick="createTransfer()">Send Transfer</button>
    <button style="margin-top:8px; background:#1e293b;" onclick="randomKey()">🎲 Randomise Key</button>
    <div id="tx-res" class="result"><div class="status-bar" id="tx-status"></div><pre id="tx-body"></pre></div>
  </div>

  <!-- Get Transfer -->
  <div class="card">
    <h2><span class="badge badge-get">GET</span> /transfers/:id — Transfer Details</h2>
    <label>Transfer ID</label>
    <input id="gt-id" placeholder="tr_abc123" id="gt-id"/>
    <button onclick="getTransfer()">Get Transfer</button>
    <div id="gt-res" class="result"><div class="status-bar" id="gt-status"></div><pre id="gt-body"></pre></div>
  </div>

  <!-- Idempotency Tester -->
  <div class="card">
    <h2>🔁 Idempotency Replay Test</h2>
    <p style="font-size:0.78rem;color:var(--muted);margin-bottom:12px;">Sends the exact same transfer N times. Money must only move once.</p>
    <label>Idempotency Key</label>
    <input id="idem-key" value="idem-replay-test"/>
    <label>From Wallet</label>
    <input id="idem-from" value="alice"/>
    <label>To Wallet</label>
    <input id="idem-to" value="bob"/>
    <label>Amount (cents)</label>
    <input id="idem-amt" type="number" value="500"/>
    <label>Repeat N times</label>
    <input id="idem-n" type="number" value="5" min="2" max="20"/>
    <button onclick="testIdempotency()">Run Replay Test</button>
    <div id="idem-res" class="result"><div class="status-bar" id="idem-status"></div><pre id="idem-body"></pre></div>
  </div>

  <!-- Load Tester -->
  <div class="card">
    <h2>⚡ Load Test — Concurrent Transfers</h2>
    <p style="font-size:0.78rem;color:var(--muted);margin-bottom:12px;">Fires N concurrent transfers simultaneously from the browser.</p>
    <div class="load-grid">
      <div><label>From Wallet</label><input id="lt-from" value="alice"/></div>
      <div><label>To Wallet</label><input id="lt-to" value="bob"/></div>
    </div>
    <label>Amount per transfer (cents)</label>
    <input id="lt-amt" type="number" value="10"/>
    <label>Total concurrent requests</label>
    <input id="lt-n" type="number" value="20" min="1" max="200"/>
    <button onclick="runLoadTest()">🚀 Fire Load Test</button>
    <div class="divider"></div>
    <div class="load-grid" id="lt-stats" style="display:none">
      <div class="stat ok"><div class="val" id="lt-ok">-</div><div class="lbl">Success (201)</div></div>
      <div class="stat err"><div class="val" id="lt-err">-</div><div class="lbl">Failed / Errors</div></div>
      <div class="stat"><div class="val" id="lt-rps">-</div><div class="lbl">Req/s</div></div>
      <div class="stat"><div class="val" id="lt-avg">-</div><div class="lbl">Avg Latency (ms)</div></div>
    </div>
    <div id="lt-log" class="log"></div>
  </div>

</div>

<script>
const API = 'http://localhost:8080';

async function req(method, path, body) {
  const start = Date.now();
  try {
    const res = await fetch(API + path, {
      method,
      headers: {'Content-Type':'application/json'},
      body: body ? JSON.stringify(body) : undefined
    });
    const data = await res.json().catch(() => ({}));
    return { status: res.status, data, ms: Date.now() - start };
  } catch(e) {
    return { status: 0, data: { error: e.message }, ms: Date.now() - start };
  }
}

function show(resEl, statusEl, bodyEl, r) {
  const el = document.getElementById(resEl);
  const st = document.getElementById(statusEl);
  const bd = document.getElementById(bodyEl);
  el.className = 'result show ' + (r.status >= 200 && r.status < 300 ? 'ok' : r.status === 409 ? 'warn' : 'err');
  st.className = 'status-bar ' + (r.status >= 200 && r.status < 300 ? 'ok' : r.status === 409 ? 'warn' : 'err');
  st.textContent = 'HTTP ' + (r.status || 'ERROR') + '  ·  ' + r.ms + 'ms';
  bd.textContent = JSON.stringify(r.data, null, 2);
}

async function createWallet() {
  const r = await req('POST', '/wallets', {
    id: document.getElementById('cw-id').value,
    initialBalance: +document.getElementById('cw-bal').value,
    currency: document.getElementById('cw-cur').value
  });
  show('cw-res','cw-status','cw-body', r);
}

async function getWallet() {
  const id = document.getElementById('gw-id').value;
  const recon = document.getElementById('gw-reconcile').checked;
  const r = await req('GET', '/wallets/' + id + (recon ? '?reconcile=true' : ''));
  show('gw-res','gw-status','gw-body', r);
}

async function createTransfer() {
  const r = await req('POST', '/transfers', {
    idempotencyKey: document.getElementById('tx-key').value,
    fromWalletId: document.getElementById('tx-from').value,
    toWalletId: document.getElementById('tx-to').value,
    amount: +document.getElementById('tx-amt').value
  });
  show('tx-res','tx-status','tx-body', r);
  if (r.data && r.data.transferId) {
    document.getElementById('gt-id').value = r.data.transferId;
  }
}

async function getTransfer() {
  const id = document.getElementById('gt-id').value;
  const r = await req('GET', '/transfers/' + id);
  show('gt-res','gt-status','gt-body', r);
}

function randomKey() {
  document.getElementById('tx-key').value = 'tx-' + Math.random().toString(36).slice(2,10);
}

async function testIdempotency() {
  const key = document.getElementById('idem-key').value;
  const body = {
    idempotencyKey: key,
    fromWalletId: document.getElementById('idem-from').value,
    toWalletId: document.getElementById('idem-to').value,
    amount: +document.getElementById('idem-amt').value,
  };
  const n = +document.getElementById('idem-n').value;
  const results = [];
  for (let i = 0; i < n; i++) {
    results.push(await req('POST', '/transfers', body));
  }
  const ids = [...new Set(results.filter(r=>r.data?.transferId).map(r=>r.data.transferId))];
  const statuses = results.map(r=>r.status);
  const pass = ids.length === 1;
  const summary = {
    test: pass ? '✅ PASSED — Money moved exactly once' : '❌ FAILED — Multiple unique transfer IDs detected!',
    uniqueTransferIDs: ids,
    responseCodes: statuses,
    totalRequests: n,
    firstTransferId: ids[0] || null,
  };
  const el = document.getElementById('idem-res');
  const st = document.getElementById('idem-status');
  const bd = document.getElementById('idem-body');
  el.className = 'result show ' + (pass ? 'ok' : 'err');
  st.className = 'status-bar ' + (pass ? 'ok' : 'err');
  st.textContent = pass ? '✅ IDEMPOTENCY PASSED' : '❌ IDEMPOTENCY FAILED';
  bd.textContent = JSON.stringify(summary, null, 2);
}

async function runLoadTest() {
  const from = document.getElementById('lt-from').value;
  const to = document.getElementById('lt-to').value;
  const amt = +document.getElementById('lt-amt').value;
  const n = +document.getElementById('lt-n').value;
  const log = document.getElementById('lt-log');
  const stats = document.getElementById('lt-stats');

  log.className = 'log show';
  log.innerHTML = '<p>🚀 Firing ' + n + ' concurrent requests...</p>';
  stats.style.display = 'none';

  const start = Date.now();
  const promises = Array.from({length: n}, (_, i) =>
    req('POST', '/transfers', {
      idempotencyKey: 'load-' + Date.now() + '-' + i,
      fromWalletId: from,
      toWalletId: to,
      amount: amt
    })
  );
  const results = await Promise.all(promises);
  const elapsed = (Date.now() - start) / 1000;

  const ok = results.filter(r => r.status === 201).length;
  const err = results.filter(r => r.status !== 201).length;
  const avgMs = Math.round(results.reduce((s, r) => s + r.ms, 0) / results.length);
  const rps = Math.round(n / elapsed);

  document.getElementById('lt-ok').textContent = ok;
  document.getElementById('lt-err').textContent = err;
  document.getElementById('lt-rps').textContent = rps;
  document.getElementById('lt-avg').textContent = avgMs + 'ms';
  stats.style.display = 'grid';

  log.innerHTML = '';
  results.forEach((r, i) => {
    const cls = r.status === 201 ? 'ok' : 'err';
    const icon = r.status === 201 ? '✅' : '❌';
    log.innerHTML += '<p class="'+cls+'">'+icon+' Request '+(i+1)+' — HTTP '+r.status+' ('+r.ms+'ms)</p>';
  });
  log.innerHTML += '<p style="margin-top:6px;color:var(--text)">Done in '+(elapsed*1000|0)+'ms · '+rps+' req/s · Avg '+avgMs+'ms</p>';
}

// Health check on load
async function checkHealth() {
  const r = await req('GET', '/health');
  const dot = document.getElementById('health-dot');
  const status = document.getElementById('health-status');
  if (r.status === 200) {
    dot.className = 'up';
    status.textContent = 'Service UP — ' + (r.data.database || 'DB OK');
  } else {
    status.textContent = 'Service Unreachable — start server first';
  }
}
checkHealth();
setInterval(checkHealth, 10000);
</script>
</body>
</html>`

// UIHandler serves the interactive API dashboard.
func UIHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(dashboardHTML))
}
