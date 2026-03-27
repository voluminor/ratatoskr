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
// Uses even angular distribution with minimum gap so branches are always visible,
// even when most nodes have only one child (common in Yggdrasil at low depth).

function drawRadialTree(canvas, root) {
    const ringGap = 100;
    const nodeR = 5;
    const branchR = 8;
    const minAngleGap = 0.04; // minimum radians between siblings

    // Flatten tree into arrays.
    const nodes = [];
    const edges = [];
    const childMap = {}; // parentIdx → [childIdx, ...]

    function countDesc(n) {
        const ch = n.children || [];
        if (ch.length === 0) return 1;
        let s = 0;
        for (const c of ch) s += countDesc(c);
        return s;
    }

    function collect(n, depth, parentIdx) {
        const idx = nodes.length;
        const desc = countDesc(n);
        const childCount = (n.children || []).length;
        nodes.push({key: n.key, depth, desc, childCount, angle: 0, x: 0, y: 0});
        if (parentIdx >= 0) {
            edges.push([parentIdx, idx]);
            if (!childMap[parentIdx]) childMap[parentIdx] = [];
            childMap[parentIdx].push(idx);
        }
        for (const c of (n.children || [])) collect(c, depth + 1, idx);
    }

    collect(root, 0, -1);

    // Assign angles: each leaf gets equal share of 2π, branches accumulate.
    // This ensures every fork is visible regardless of subtree size.
    let leafCounter = 0;
    const totalLeaves = nodes.filter(n => !childMap[n.key] && !(childMap[nodes.indexOf(n)])).length;

    // Count actual leaves (nodes with no children in our edges).
    let leafCount = 0;
    const hasChildren = new Set();
    for (const [pi] of edges) hasChildren.add(pi);
    for (let i = 0; i < nodes.length; i++) {
        if (!hasChildren.has(i)) leafCount++;
    }

    // Assign angular position via DFS: leaves get sequential slots.
    const leafAngle = (Math.PI * 2) / Math.max(leafCount, 1);
    let leafIdx = 0;

    function assignAngles(ni) {
        const ch = childMap[ni] || [];
        if (ch.length === 0) {
            // Leaf: assign next slot.
            nodes[ni].angle = leafIdx * leafAngle;
            leafIdx++;
            return;
        }
        // Recurse children first, then parent angle = midpoint of children's range.
        for (const ci of ch) assignAngles(ci);
        const first = nodes[ch[0]].angle;
        const last = nodes[ch[ch.length - 1]].angle;
        nodes[ni].angle = (first + last) / 2;
    }

    assignAngles(0);

    // Apply minimum angular gap: if siblings are too close, spread them.
    function spreadSiblings(ni) {
        const ch = childMap[ni] || [];
        if (ch.length < 2) {
            for (const ci of ch) spreadSiblings(ci);
            return;
        }
        // Check gap between consecutive siblings.
        for (let i = 1; i < ch.length; i++) {
            const gap = nodes[ch[i]].angle - nodes[ch[i - 1]].angle;
            if (gap < minAngleGap) {
                // Spread all children evenly with minAngleGap.
                const center = nodes[ni].angle;
                const totalSpan = minAngleGap * (ch.length - 1);
                const start = center - totalSpan / 2;
                for (let j = 0; j < ch.length; j++) {
                    nodes[ch[j]].angle = start + j * minAngleGap;
                }
                break;
            }
        }
        // Recalculate parent midpoint.
        const first = nodes[ch[0]].angle;
        const last = nodes[ch[ch.length - 1]].angle;
        nodes[ni].angle = (first + last) / 2;
        for (const ci of ch) spreadSiblings(ci);
    }

    spreadSiblings(0);

    // Convert polar to cartesian.
    for (const n of nodes) {
        n.x = n.depth * ringGap * Math.cos(n.angle);
        n.y = n.depth * ringGap * Math.sin(n.angle);
    }

    // Canvas size.
    const maxDepth = Math.max(...nodes.map(n => n.depth));
    const totalRadius = (maxDepth + 1) * ringGap + 80;
    const size = totalRadius * 2;
    const cx = totalRadius;
    const cy = totalRadius;

    const dpr = window.devicePixelRatio || 1;
    canvas.width = size * dpr;
    canvas.height = size * dpr;
    canvas.style.width = size + 'px';
    canvas.style.height = size + 'px';

    const ctx = canvas.getContext('2d');
    ctx.scale(dpr, dpr);

    // Depth ring guides.
    ctx.strokeStyle = 'rgba(42, 45, 62, 0.3)';
    ctx.lineWidth = 0.5;
    for (let d = 1; d <= maxDepth; d++) {
        ctx.beginPath();
        ctx.arc(cx, cy, d * ringGap, 0, Math.PI * 2);
        ctx.stroke();
    }

    // Edges — curved bezier to separate overlapping paths.
    for (const [pi, ci] of edges) {
        const p = nodes[pi];
        const c = nodes[ci];
        const px = cx + p.x, py = cy + p.y;
        const ex = cx + c.x, ey = cy + c.y;

        // Control point: midpoint pushed outward from center.
        const mx = (px + ex) / 2;
        const my = (py + ey) / 2;
        const mdist = Math.sqrt((mx - cx) ** 2 + (my - cy) ** 2) || 1;
        const pushOut = ringGap * 0.15;
        const cpx = mx + ((mx - cx) / mdist) * pushOut;
        const cpy = my + ((my - cy) / mdist) * pushOut;

        ctx.strokeStyle = hasChildren.has(ci) ? '#4a4d6e' : '#2a2d3e';
        ctx.lineWidth = hasChildren.has(ci) ? 1.5 : 0.8;
        ctx.beginPath();
        ctx.moveTo(px, py);
        ctx.quadraticCurveTo(cpx, cpy, ex, ey);
        ctx.stroke();
    }

    // Nodes.
    const colors = ['#6366f1', '#818cf8', '#60a5fa', '#38bdf8', '#22d3ee',
        '#2dd4bf', '#34d399', '#4ade80', '#a3e635', '#facc15'];

    ctx.textBaseline = 'middle';

    for (let i = 0; i < nodes.length; i++) {
        const n = nodes[i];
        const nx = cx + n.x;
        const ny = cy + n.y;
        const isBranch = hasChildren.has(i) && (childMap[i] || []).length > 1;
        const r = i === 0 ? branchR + 2 : (isBranch ? branchR : nodeR);

        // Node circle.
        const color = colors[n.depth % colors.length];
        ctx.fillStyle = color;
        ctx.beginPath();
        ctx.arc(nx, ny, r, 0, Math.PI * 2);
        ctx.fill();

        // Branch highlight ring.
        if (isBranch) {
            ctx.strokeStyle = '#fff';
            ctx.lineWidth = 1.5;
            ctx.beginPath();
            ctx.arc(nx, ny, r + 2, 0, Math.PI * 2);
            ctx.stroke();
        }

        // Label.
        const shortKey = n.key.substring(0, 8);
        ctx.fillStyle = '#e2e8f0';
        ctx.font = '9px monospace';

        if (i === 0) {
            ctx.textAlign = 'center';
            ctx.fillText('root', nx, ny - r - 6);
            ctx.fillText(shortKey, nx, ny - r - 16);
        } else {
            const dist = Math.sqrt(n.x * n.x + n.y * n.y) || 1;
            const dx = n.x / dist;
            const dy = n.y / dist;
            const lx = nx + dx * 12;
            const ly = ny + dy * 12;
            ctx.textAlign = dx >= 0 ? 'left' : 'right';
            ctx.fillText(shortKey, lx, ly);

            // Show child count for branch nodes.
            if (isBranch) {
                ctx.fillStyle = '#facc15';
                ctx.font = 'bold 8px sans-serif';
                ctx.textAlign = 'center';
                ctx.fillText((childMap[i] || []).length, nx, ny);
            }
        }
    }

    // Stats.
    ctx.fillStyle = '#64748b';
    ctx.font = '11px sans-serif';
    ctx.textAlign = 'left';
    ctx.fillText(nodes.length + ' nodes, ' + maxDepth + ' levels, ' +
        leafCount + ' leaves', 10, size - 10);
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
