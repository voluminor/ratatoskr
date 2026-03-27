const FETCH_TIMEOUT_MS = 3000;
const REFRESH_MS = 10000;
const HISTORY_LEN = 20;

// // // // // // // // // //

const bwHistory = {rx: [], tx: []};
let chartCanvas = null;

// //

function formatBytes(b) {
    if (b < 1024) return b + ' B';
    if (b < 1048576) return (b / 1024).toFixed(1) + ' KB';
    if (b < 1073741824) return (b / 1048576).toFixed(1) + ' MB';
    return (b / 1073741824).toFixed(2) + ' GB';
}

function formatRate(bps) {
    return formatBytes(bps) + '/s';
}

function formatUptime(s) {
    s = Math.floor(s);
    const d = Math.floor(s / 86400);
    const h = Math.floor((s % 86400) / 3600);
    const m = Math.floor((s % 3600) / 60);
    const sc = s % 60;
    if (d > 0) return `${d}d ${h}h ${m}m`;
    if (h > 0) return `${h}h ${m}m ${sc}s`;
    if (m > 0) return `${m}m ${sc}s`;
    return `${sc}s`;
}

// //

function pushHistory(rx, tx) {
    bwHistory.rx.push(rx);
    bwHistory.tx.push(tx);
    while (bwHistory.rx.length > HISTORY_LEN) {
        bwHistory.rx.shift();
        bwHistory.tx.shift();
    }
    while (bwHistory.rx.length < HISTORY_LEN) {
        bwHistory.rx.unshift(0);
        bwHistory.tx.unshift(0);
    }
}

function drawChart() {
    if (!chartCanvas) chartCanvas = document.getElementById('bw-chart');
    const ctx = chartCanvas.getContext('2d');
    chartCanvas.width = chartCanvas.offsetWidth || 600;
    const w = chartCanvas.width;
    const h = 80;
    ctx.clearRect(0, 0, w, h);

    const max = Math.max(...bwHistory.rx, ...bwHistory.tx, 1);
    const step = w / (HISTORY_LEN - 1);

    function drawLine(data, color) {
        ctx.beginPath();
        ctx.strokeStyle = color;
        ctx.lineWidth = 2;
        ctx.lineJoin = 'round';
        data.forEach((v, i) => {
            const x = i * step;
            const y = h - (v / max) * (h - 6) - 3;
            i === 0 ? ctx.moveTo(x, y) : ctx.lineTo(x, y);
        });
        ctx.stroke();
    }

    drawLine(bwHistory.rx, '#6366f1'); // RX — indigo
    drawLine(bwHistory.tx, '#22c55e'); // TX — green
}

// //

function setPeerList(peers) {
    const el = document.getElementById('peers-list');
    if (!peers || peers.length === 0) {
        el.innerHTML = '<p style="color:var(--muted);font-size:0.82rem">No peers connected</p>';
        return;
    }
    const rows = peers.map(p => {
        const errCell = (!p.up && p.last_error)
            ? `<span class="peer-error" title="${p.last_error}">${p.last_error}</span>`
            : '—';
        return `
    <tr>
      <td><span class="dot ${p.up ? 'up' : 'down'}"></span>${p.up ? 'Up' : 'Down'}</td>
      <td class="uri-cell" title="${p.uri}">${p.uri}</td>
      <td>${p.up ? p.latency_ms.toFixed(1) + ' ms' : errCell}</td>
      <td>${formatRate(p.rx_rate)}</td>
      <td>${formatRate(p.tx_rate)}</td>
    </tr>`;
    }).join('');
    el.innerHTML = `
    <table class="peers-table">
      <thead>
        <tr>
          <th>Status</th><th>URI</th><th>Latency / Error</th><th>↓ RX</th><th>↑ TX</th>
        </tr>
      </thead>
      <tbody>${rows}</tbody>
    </table>`;
}

// //

function updateUI(data) {
    // Connection badge.
    const badge = document.getElementById('connection-badge');
    if (data.is_yggdrasil) {
        badge.textContent = 'Connected via Yggdrasil';
        badge.className = 'badge ygg';
    } else {
        badge.textContent = 'Connected via regular web';
        badge.className = 'badge web';
    }

    // Node card.
    const addrEl = document.getElementById('ygg-addr');
    addrEl.textContent = data.ygg_address;

    const keyEl = document.getElementById('pub-key');
    keyEl.textContent = data.public_key;
    keyEl.title = data.public_key;

    document.getElementById('uptime').textContent = formatUptime(data.uptime_seconds);
    document.getElementById('sessions').textContent =
        (data.sessions && data.sessions.length) || 0;

    // Bandwidth.
    const bw = data.bandwidth || {};
    document.getElementById('rx-rate').textContent = formatRate(bw.rx_rate || 0);
    document.getElementById('tx-rate').textContent = formatRate(bw.tx_rate || 0);
    document.getElementById('rx-total').textContent = formatBytes(bw.rx_bytes || 0);
    document.getElementById('tx-total').textContent = formatBytes(bw.tx_bytes || 0);

    pushHistory(bw.rx_rate || 0, bw.tx_rate || 0);
    drawChart();

    // Sessions.
    setSessionList(data.sessions);

    // Peers.
    setPeerList(data.peers);

    // Updated at — when the cached metrics snapshot was taken.
    const ts = new Date(data.cached_at);
    document.getElementById('updated-at').textContent =
        `— data from ${ts.toLocaleTimeString()}`;

    // Show content.
    document.getElementById('main-content').classList.remove('hidden');
    document.getElementById('status-unavailable').classList.add('hidden');
}

function showUnavailable() {
    const badge = document.getElementById('connection-badge');
    badge.textContent = 'Server unavailable';
    badge.className = 'badge loading';
    document.getElementById('status-unavailable').classList.remove('hidden');
    document.getElementById('main-content').classList.add('hidden');
}

// //

async function fetchInfo() {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), FETCH_TIMEOUT_MS);
    try {
        const resp = await fetch('/yggdrasil-server.json', {signal: controller.signal});
        clearTimeout(timer);
        if (!resp.ok) throw new Error('HTTP ' + resp.status);
        const data = await resp.json();
        updateUI(data);
    } catch (_) {
        clearTimeout(timer);
        showUnavailable();
    }
}

// //

function setSessionList(sessions) {
    const el = document.getElementById('sessions-list');
    if (!sessions || sessions.length === 0) {
        el.innerHTML = '<p style="color:var(--muted);font-size:0.82rem">No active sessions</p>';
        return;
    }
    const rows = sessions.map(s => {
        const shortKey = s.key.substring(0, 16) + '…';
        return `
    <tr>
      <td class="key-cell" title="${s.key}">${shortKey}</td>
      <td>${formatBytes(s.rx_bytes)}</td>
      <td>${formatBytes(s.tx_bytes)}</td>
      <td>${formatUptime(s.uptime_sec)}</td>
    </tr>`;
    }).join('');
    el.innerHTML = `
    <table class="peers-table">
      <thead>
        <tr>
          <th>Key</th><th>↓ RX</th><th>↑ TX</th><th>Uptime</th>
        </tr>
      </thead>
      <tbody>${rows}</tbody>
    </table>`;
}

// // // // // // // // // //

// Tree modal — uses a persistent WebSocket for the lifetime of the modal.
// One connection is opened when the modal opens and closed when it closes.
// Each Refresh sends a new scan request over the same connection.

let treeWS = null;

function openTreeModal() {
    document.getElementById('tree-modal').classList.remove('hidden');
    const slider = document.getElementById('tree-depth');
    document.getElementById('tree-depth-val').textContent = slider.value;
    if (!treeWS || treeWS.readyState > WebSocket.OPEN) {
        const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
        treeWS = new WebSocket(proto + '//' + location.host + '/tree-ws');
        treeWS.onmessage = onTreeMessage;
        treeWS.onclose = () => {
            treeWS = null;
        };
        treeWS.onerror = () => {
            document.getElementById('tree-modal-body').textContent = 'WebSocket error';
        };
        treeWS.onopen = () => sendTreeScan();
    } else {
        sendTreeScan();
    }
}

function refreshTree() {
    if (!treeWS || treeWS.readyState !== WebSocket.OPEN) {
        openTreeModal();
        return;
    }
    sendTreeScan();
}

function closeTreeModal(e) {
    if (e && e.target !== e.currentTarget) return;
    document.getElementById('tree-modal').classList.add('hidden');
    if (treeWS) {
        treeWS.close();
        treeWS = null;
    }
}

function sendTreeScan() {
    const depth = parseInt(document.getElementById('tree-depth').value, 10);
    document.getElementById('tree-modal-body').innerHTML = '';
    document.querySelector('.modal-refresh-btn').disabled = true;
    document.getElementById('tree-depth').disabled = true;
    treeWS.send(JSON.stringify({depth}));
}

function onTreeMessage(ev) {
    const msg = JSON.parse(ev.data);
    const body = document.getElementById('tree-modal-body');
    const btn = document.querySelector('.modal-refresh-btn');
    const slider = document.getElementById('tree-depth');

    if (msg.type === 'ack') {
        body.innerHTML =
            '<div id="tree-progress"></div>' +
            '<div id="tree-current" class="tn-stats tn-scanning">Scanning depth 1\u2026</div>';
    } else if (msg.type === 'progress') {
        const prog = document.getElementById('tree-progress');
        const cur = document.getElementById('tree-current');
        if (prog) {
            const line = document.createElement('div');
            line.className = 'tn-stats';
            line.textContent = 'Depth ' + msg.depth + ': +' + msg.found +
                ' nodes (total: ' + msg.total + ')';
            prog.appendChild(line);
        }
        if (cur) {
            cur.textContent = 'Scanning depth ' + (msg.depth + 1) + '\u2026';
        }
    } else if (msg.type === 'result') {
        body.innerHTML = '';
        buildAccordionTree(body, msg.root);
        btn.disabled = false;
        slider.disabled = false;
    } else if (msg.type === 'error') {
        body.textContent = msg.message;
        btn.disabled = false;
        slider.disabled = false;
    }
}

document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') closeTreeModal();
});

// //

// Collapsible accordion tree rendered as DOM nodes.
// Each row shows a color dot, toggle icon, key prefix, child count, and
// an "unreachable" badge when the node did not respond to peer queries.
// All nodes start expanded; clicking a row with children toggles them.

function buildAccordionTree(container, root) {
    const depthColors = ['#6366f1', '#818cf8', '#60a5fa', '#38bdf8', '#22d3ee',
        '#2dd4bf', '#34d399', '#4ade80', '#a3e635', '#facc15'];

    function makeNode(n, isRoot) {
        const ch = n.children || [];
        const wrapper = document.createElement('div');

        const row = document.createElement('div');
        row.className = 'tn-row' + (ch.length > 0 ? '' : ' tn-leaf');

        const dot = document.createElement('span');
        dot.className = 'tn-dot';
        if (n.unreachable) {
            dot.style.cssText = 'background:#374151;border:1px dashed #6b7280';
        } else {
            dot.style.background = depthColors[n.depth % depthColors.length];
        }
        row.appendChild(dot);

        const toggle = document.createElement('span');
        toggle.className = 'tn-toggle';
        toggle.textContent = ch.length > 0 ? '▼' : (n.unreachable ? '✕' : '·');
        row.appendChild(toggle);

        const keySpan = document.createElement('span');
        keySpan.className = 'tn-key' + (n.unreachable ? ' tn-key-unreachable' : '');
        if (isRoot) {
            keySpan.textContent = 'root';
            keySpan.style.cssText = 'color:' + depthColors[0] + ';font-weight:700';
        } else {
            keySpan.textContent = n.key ? n.key.substring(0, 16) + '\u2026' : '?';
        }
        if (n.key) keySpan.title = n.key;
        row.appendChild(keySpan);

        if (ch.length > 0) {
            const cnt = document.createElement('span');
            cnt.className = 'tn-cnt';
            cnt.textContent = ch.length;
            row.appendChild(cnt);
        }

        if (n.unreachable) {
            const badge = document.createElement('span');
            badge.className = 'tn-unreachable';
            badge.textContent = 'unreachable';
            row.appendChild(badge);
        }

        wrapper.appendChild(row);

        if (ch.length > 0) {
            const childrenEl = document.createElement('div');
            childrenEl.className = 'tn-children hidden';
            for (const c of ch) childrenEl.appendChild(makeNode(c, false));
            wrapper.appendChild(childrenEl);

            toggle.textContent = '▶';
            row.addEventListener('click', () => {
                const closing = !childrenEl.classList.contains('hidden');
                childrenEl.classList.toggle('hidden', closing);
                toggle.textContent = closing ? '▶' : '▼';
            });
        }

        return wrapper;
    }

    let total = 0;
    (function count(n) {
        total++;
        (n.children || []).forEach(count);
    })(root);
    const stats = document.createElement('div');
    stats.className = 'tn-stats';
    stats.textContent = total + ' nodes';
    container.appendChild(stats);
    container.appendChild(makeNode(root, true));
}

// // // // // // // // // //

// Traceroute

async function doTrace() {
    const keyInput = document.getElementById('trace-key');
    const btn = document.getElementById('trace-btn');
    const errorEl = document.getElementById('trace-error');
    const resultEl = document.getElementById('trace-result');

    const key = keyInput.value.trim();
    if (!key || key.length !== 64 || !/^[0-9a-fA-F]+$/.test(key)) {
        errorEl.textContent = 'Enter a valid 64-char hex public key';
        errorEl.classList.remove('hidden');
        resultEl.classList.add('hidden');
        return;
    }

    btn.disabled = true;
    btn.textContent = '…';
    errorEl.classList.add('hidden');
    resultEl.classList.add('hidden');

    try {
        const resp = await fetch('/traceroute.json?key=' + key);
        const data = await resp.json();

        if (data.error) {
            errorEl.textContent = data.error;
            errorEl.classList.remove('hidden');
            return;
        }

        document.getElementById('trace-duration').textContent =
            'Resolved in ' + data.duration_ms.toFixed(1) + ' ms';

        renderTraceHops(data.hops || []);
        renderTracePath(data.path || []);
        renderTraceSubtree(data.subtree);
        resultEl.classList.remove('hidden');
    } catch (e) {
        errorEl.textContent = 'Request failed: ' + e.message;
        errorEl.classList.remove('hidden');
    } finally {
        btn.disabled = false;
        btn.textContent = 'Trace';
    }
}

function renderTraceHops(hops) {
    const el = document.getElementById('trace-hops');
    if (hops.length === 0) {
        el.innerHTML = '<span class="trace-muted">No pathfinder route</span>';
        return;
    }
    el.innerHTML = hops.map((h, i) => {
        const label = h.key ? h.key.substring(0, 12) + '…' : 'port:' + h.port;
        const title = h.key || ('port ' + h.port);
        const arrow = i < hops.length - 1 ? ' <span class="trace-arrow">→</span> ' : '';
        return `<span class="trace-hop" title="${title}"><span class="trace-depth">${h.depth}</span>${label}</span>${arrow}`;
    }).join('');
}

function renderTracePath(path) {
    const el = document.getElementById('trace-path');
    if (path.length === 0) {
        el.innerHTML = '<span class="trace-muted">Not in spanning tree</span>';
        return;
    }
    el.innerHTML = path.map((n, i) => {
        const shortKey = n.key.substring(0, 12) + '…';
        const arrow = i < path.length - 1 ? ' <span class="trace-arrow">→</span> ' : '';
        return `<span class="trace-hop" title="${n.key}"><span class="trace-depth">${n.depth}</span>${shortKey}</span>${arrow}`;
    }).join('');
}

function renderTraceSubtree(tree) {
    const el = document.getElementById('trace-tree');
    if (!tree || !tree.children || tree.children.length === 0) {
        el.classList.add('hidden');
        return;
    }
    el.textContent = JSON.stringify(tree, null, 2);
    el.classList.remove('hidden');
}

// Enter key triggers trace
document.getElementById('trace-key').addEventListener('keydown', function (e) {
    if (e.key === 'Enter') doTrace();
});

// //

fetchInfo();
setInterval(fetchInfo, REFRESH_MS);
