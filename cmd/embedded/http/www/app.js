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

// Tree modal

async function openTreeModal() {
    const modal = document.getElementById('tree-modal');
    modal.classList.remove('hidden');
    await refreshTree();
}

async function refreshTree() {
    const body = document.getElementById('tree-modal-body');
    const depth = document.getElementById('tree-depth').value;
    body.innerHTML = '<span style="color:var(--muted)">Loading...</span>';

    try {
        const resp = await fetch('/tree.json?depth=' + depth);
        if (!resp.ok) throw new Error('HTTP ' + resp.status);
        const tree = await resp.json();
        if (tree.error) {
            body.textContent = tree.error;
            return;
        }
        body.innerHTML = '';
        const canvas = document.createElement('canvas');
        canvas.id = 'tree-canvas';
        body.appendChild(canvas);
        drawRadialTree(canvas, tree);
    } catch (e) {
        body.textContent = 'Failed: ' + e.message;
    }
}

function closeTreeModal(e) {
    if (e && e.target !== e.currentTarget) return;
    document.getElementById('tree-modal').classList.add('hidden');
}

document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') closeTreeModal();
});

// //

// Radial spanning tree on Canvas.
// Single-child chains are condensed into annotated edges so only actual
// branch points and leaves appear as nodes — this is the only correct way
// to visualize chain-heavy trees without everything collapsing into rays.

function drawRadialTree(canvas, root) {
    const edgeLen = 110;   // pixels per hop between rendered nodes
    const nodeR = 6;
    const branchR = 9;
    const minSectorRad = 0.18; // minimum sector per child in radians

    // Condense single-child chains: collapse sequences of nodes that have
    // exactly one child into a single edge annotated with hop count.
    // Returns a condensed tree node:
    //   { key, depth, hops (edge weight), children }
    function condense(n, depth) {
        const ch = n.children || [];
        if (ch.length !== 1) {
            return {key: n.key, depth, hops: 1, children: ch.map(c => condense(c, depth + 1))};
        }
        // Follow the single-child chain.
        let cur = n;
        let hops = 1;
        let d = depth;
        while ((cur.children || []).length === 1) {
            cur = cur.children[0];
            hops++;
            d++;
        }
        const tail = condense(cur, d);
        tail.hops = hops;
        return {key: n.key, depth, hops: 1, children: [tail]};
    }

    const ctree = condense(root, 0);

    // Flatten condensed tree into node/edge arrays.
    const nodes = [];
    const edges = []; // {pi, ci, hops}
    const childMap = {};

    function collect(n, parentIdx) {
        const idx = nodes.length;
        nodes.push({key: n.key, depth: n.depth, angle: 0, x: 0, y: 0});
        if (parentIdx >= 0) {
            edges.push({pi: parentIdx, ci: idx, hops: n.hops});
            if (!childMap[parentIdx]) childMap[parentIdx] = [];
            childMap[parentIdx].push(idx);
        }
        for (const c of (n.children || [])) collect(c, idx);
    }

    collect(ctree, -1);

    // Count leaves (condensed nodes with no children).
    const hasChildren = new Set();
    for (const {pi} of edges) hasChildren.add(pi);
    let leafCount = 0;
    for (let i = 0; i < nodes.length; i++) {
        if (!hasChildren.has(i)) leafCount++;
    }

    // Assign angles: leaves get equal shares of 2π, parents get midpoint
    // of their children's range. Enforces minimum sector per branch.
    const leafAngleStep = (Math.PI * 2) / Math.max(leafCount, 1);
    let leafIdx = 0;

    function assignAngles(ni) {
        const ch = childMap[ni] || [];
        if (ch.length === 0) {
            nodes[ni].angle = leafIdx * leafAngleStep;
            leafIdx++;
            return;
        }
        // Recurse first (bottom-up).
        for (const ci of ch) assignAngles(ci);

        // Enforce minimum angular gap between siblings.
        if (ch.length > 1) {
            let prevAngle = nodes[ch[0]].angle;
            for (let i = 1; i < ch.length; i++) {
                if (nodes[ch[i]].angle - prevAngle < minSectorRad) {
                    nodes[ch[i]].angle = prevAngle + minSectorRad;
                }
                prevAngle = nodes[ch[i]].angle;
            }
        }

        // Parent angle = midpoint of first..last child.
        nodes[ni].angle = (nodes[ch[0]].angle + nodes[ch[ch.length - 1]].angle) / 2;
    }
    assignAngles(0);

    // Place nodes: root at origin, each child at edgeLen*hops from parent
    // in its assigned angular direction.
    function place(ni, px, py) {
        nodes[ni].x = px;
        nodes[ni].y = py;
        for (const {pi, ci, hops} of edges) {
            if (pi !== ni) continue;
            const angle = nodes[ci].angle;
            const dist = edgeLen * hops;
            place(ci, px + dist * Math.cos(angle), py + dist * Math.sin(angle));
        }
    }

    place(0, 0, 0);

    // Canvas bounds from actual node positions.
    const xs = nodes.map(n => n.x);
    const ys = nodes.map(n => n.y);
    const pad = 60;
    const minX = Math.min(...xs) - pad;
    const maxX = Math.max(...xs) + pad;
    const minY = Math.min(...ys) - pad;
    const maxY = Math.max(...ys) + pad;
    const w = maxX - minX;
    const h = maxY - minY;
    const cx = -minX;
    const cy = -minY;

    const dpr = window.devicePixelRatio || 1;
    canvas.width = w * dpr;
    canvas.height = h * dpr;
    canvas.style.width = w + 'px';
    canvas.style.height = h + 'px';

    const ctx = canvas.getContext('2d');
    ctx.scale(dpr, dpr);

    // Edges.
    for (const {pi, ci, hops} of edges) {
        const p = nodes[pi];
        const c = nodes[ci];
        const px = cx + p.x, py = cy + p.y;
        const ex = cx + c.x, ey = cy + c.y;
        const isChain = hops > 1;

        if (isChain) {
            // Dashed line for condensed chains.
            ctx.setLineDash([5, 4]);
            ctx.strokeStyle = '#4a4d6e';
            ctx.lineWidth = 1.5;
        } else {
            ctx.setLineDash([]);
            ctx.strokeStyle = hasChildren.has(ci) ? '#5a5d7e' : '#3a3d5e';
            ctx.lineWidth = hasChildren.has(ci) ? 1.5 : 1;
        }

        ctx.beginPath();
        ctx.moveTo(px, py);
        ctx.lineTo(ex, ey);
        ctx.stroke();
        ctx.setLineDash([]);

        // Chain hop count label on the edge midpoint.
        if (isChain) {
            const mx = (px + ex) / 2;
            const my = (py + ey) / 2;
            ctx.fillStyle = '#1e2030';
            ctx.beginPath();
            ctx.arc(mx, my, 8, 0, Math.PI * 2);
            ctx.fill();
            ctx.fillStyle = '#94a3b8';
            ctx.font = 'bold 9px sans-serif';
            ctx.textAlign = 'center';
            ctx.textBaseline = 'middle';
            ctx.fillText(hops, mx, my);
        }
    }

    // Nodes.
    const colors = ['#6366f1', '#818cf8', '#60a5fa', '#38bdf8', '#22d3ee',
        '#2dd4bf', '#34d399', '#4ade80', '#a3e635', '#facc15'];

    for (let i = 0; i < nodes.length; i++) {
        const n = nodes[i];
        const nx = cx + n.x;
        const ny = cy + n.y;
        const isBranch = hasChildren.has(i) && (childMap[i] || []).length > 1;
        const r = i === 0 ? branchR + 3 : (isBranch ? branchR : nodeR);
        const color = colors[n.depth % colors.length];

        ctx.fillStyle = color;
        ctx.beginPath();
        ctx.arc(nx, ny, r, 0, Math.PI * 2);
        ctx.fill();

        // White ring on branch nodes to make forks visible.
        if (isBranch || i === 0) {
            ctx.strokeStyle = 'rgba(255,255,255,0.6)';
            ctx.lineWidth = 1.5;
            ctx.beginPath();
            ctx.arc(nx, ny, r + 2, 0, Math.PI * 2);
            ctx.stroke();
        }

        // Labels.
        const shortKey = n.key.substring(0, 8);
        ctx.textBaseline = 'middle';

        if (i === 0) {
            ctx.fillStyle = '#e2e8f0';
            ctx.font = '9px monospace';
            ctx.textAlign = 'center';
            ctx.fillText('root', nx, ny - r - 8);
            ctx.fillText(shortKey, nx, ny - r - 18);
        } else {
            const dx = n.x - nodes[edges.find(e => e.ci === i)?.pi ?? 0].x;
            const dy = n.y - nodes[edges.find(e => e.ci === i)?.pi ?? 0].y;
            const dist = Math.sqrt(dx * dx + dy * dy) || 1;
            const ux = dx / dist;
            const uy = dy / dist;
            ctx.fillStyle = '#cbd5e1';
            ctx.font = '9px monospace';
            ctx.textAlign = ux >= 0 ? 'left' : 'right';
            ctx.fillText(shortKey, nx + ux * (r + 5), ny + uy * (r + 5));

            if (isBranch) {
                ctx.fillStyle = '#facc15';
                ctx.font = 'bold 8px sans-serif';
                ctx.textAlign = 'center';
                ctx.fillText((childMap[i] || []).length, nx, ny);
            }
        }
    }

    // Stats footer.
    ctx.fillStyle = '#475569';
    ctx.font = '11px sans-serif';
    ctx.textAlign = 'left';
    ctx.textBaseline = 'alphabetic';
    const totalNodes = (function countAll(n) {
        return 1 + (n.children || []).reduce((s, c) => s + countAll(c), 0);
    })(root);
    ctx.fillText(totalNodes + ' nodes total, ' + nodes.length + ' branch/leaf points shown', 10, h - 6);
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
