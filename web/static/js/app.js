──────────────────────────────────────────────────
const API = 'http://localhost:8080/api';

// ── State ─────────────────────────────────────────────────────
let monacoEditor = null;
let gridApi = null;
let currentConnId = null;
let queryAbortController = null;
let msgCount = 0;
let tabs = [{ id: 0, name: 'Query 1', sql: '' }];
let activeTabId = 0;
let tabCounter = 1;
let lastResults = null;

// ── Monaco init ───────────────────────────────────────────────
require.config({ paths: { vs: 'https://cdnjs.cloudflare.com/ajax/libs/monaco-editor/0.47.0/min/vs' } });
require(['vs/editor/editor.main'], function () {
  monacoEditor = monaco.editor.create(document.getElementById('monaco-container'), {
    value: '-- Welcome to tsqlnotes\n-- Connect to a database and start querying\n\nSELECT 1 AS hello;',
    language: 'sql',
    theme: 'vs-dark',
    fontSize: 13,
    fontFamily: "'JetBrains Mono', 'Fira Code', monospace",
    fontLigatures: true,
    minimap: { enabled: true, scale: 0.8 },
    scrollBeyondLastLine: false,
    lineNumbers: 'on',
    renderLineHighlight: 'line',
    cursorBlinking: 'smooth',
    smoothScrolling: true,
    tabSize: 2,
    wordWrap: 'off',
    automaticLayout: true,
    contextmenu: true,
    suggest: { showKeywords: true },
  });

  // Cursor position
  monacoEditor.onDidChangeCursorPosition(e => {
    document.getElementById('cursor-pos').textContent =
      `Ln ${e.position.lineNumber}, Col ${e.position.column}`;
  });

  // Selection info
  monacoEditor.onDidChangeCursorSelection(e => {
    const sel = monacoEditor.getModel().getValueInRange(e.selection);
    const info = document.getElementById('sel-info');
    if (sel.length > 0) {
      info.style.display = '';
      document.getElementById('sel-chars').textContent = sel.length;
    } else {
      info.style.display = 'none';
    }
  });

  // Ctrl+Enter / F5 → run
  monacoEditor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.Enter, runQuery);
  monacoEditor.addCommand(monaco.KeyCode.F5, runQuery);
  monacoEditor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyMod.Shift | monaco.KeyCode.KeyF, formatQuery);
});

// ── AG Grid init ──────────────────────────────────────────────
const gridOptions = {
  columnDefs: [],
  rowData: [],
  defaultColDef: {
    sortable: true, resizable: true, filter: true,
    minWidth: 80, flex: 1,
    cellRenderer: params => {
      if (params.value === null || params.value === undefined) {
        return '<span class="cell-null">NULL</span>';
      }
      if (typeof params.value === 'boolean') {
        return `<span class="${params.value ? 'cell-bool-true' : 'cell-bool-false'}">${params.value}</span>`;
      }
      if (typeof params.value === 'number') {
        return `<span class="cell-number">${params.value}</span>`;
      }
      return String(params.value);
    }
  },
  animateRows: true,
  suppressMovableColumns: false,
  enableCellTextSelection: true,
  suppressCellFocus: false,
  rowSelection: 'multiple',
  pagination: true,
  paginationPageSize: 500,
};

document.addEventListener('DOMContentLoaded', () => {
  const gridDiv = document.getElementById('results-grid');
  gridApi = agGrid.createGrid(gridDiv, gridOptions);
  initTree();
  loadConnections();
});

// ── jsTree ────────────────────────────────────────────────────
function initTree() {
  $('#jstree').jstree({
    core: {
      themes: { name: 'default-dark', dots: true, icons: true },
      data: function (node, callback) {
        if (!currentConnId) { callback([]); return; }
        if (node.id === '#') {
          fetchSchemas(callback);
        } else if (node.type === 'schema') {
          fetchTables(node.original.schema, callback);
        } else if (node.type === 'table' || node.type === 'view') {
          fetchColumns(node.original.schema, node.original.table, callback);
        } else {
          callback([]);
        }
      }
    },
    types: {
      schema: { icon: 'jstree-icon jstree-themeicon-custom' },
      table:  { icon: 'jstree-icon jstree-themeicon-custom' },
      view:   { icon: 'jstree-icon jstree-themeicon-custom' },
      column: { icon: false },
    },
    plugins: ['types', 'wholerow', 'contextmenu'],
    contextmenu: {
      items: treeContextMenu
    }
  }).on('select_node.jstree', function (e, data) {
    const n = data.node;
    if (n.type === 'table' || n.type === 'view') {
      const schema = n.original.schema;
      const table  = n.original.table;
      insertSnippet(`SELECT * FROM ${schema ? schema + '.' : ''}${table} LIMIT 100;`);
    }
  }).on('open_node.jstree', function(e, data) {
    // Add type class for CSS icons
    const el = $('#' + data.node.id);
    el.addClass('node-' + data.node.type);
  });
}

function treeContextMenu(node) {
  const items = {};
  if (node.type === 'table' || node.type === 'view') {
    const schema = node.original.schema;
    const table  = node.original.table;
    const fqt    = schema ? `${schema}.${table}` : table;
    items.select100 = {
      label: `SELECT * ... LIMIT 100`,
      action: () => setEditorSQL(`SELECT * FROM ${fqt} LIMIT 100;`)
    };
    items.count = {
      label: `SELECT COUNT(*)`,
      action: () => setEditorSQL(`SELECT COUNT(*) FROM ${fqt};`)
    };
    items.sep = { separator_before: true };
    items.ddl = {
      label: `Show DDL`,
      action: () => addMessage(`info`, `Tip: SHOW CREATE TABLE ${fqt}`)
    };
  }
  return items;
}

function fetchSchemas(callback) {
  fetch(`${API}/schema?connId=${currentConnId}`)
    .then(r => r.json())
    .then(data => {
      const nodes = (data || []).map(s => ({
        id: 'schema_' + s.text,
        text: s.text,
        type: 'schema',
        children: true,
        schema: s.text,
        li_attr: { class: 'node-schema' }
      }));
      callback(nodes);
    })
    .catch(() => callback([]));
}

function fetchTables(schema, callback) {
  fetch(`${API}/schema?connId=${currentConnId}&schema=${encodeURIComponent(schema)}`)
    .then(r => r.json())
    .then(data => {
      const nodes = (data || []).map(t => ({
        id: 'table_' + schema + '_' + t.text,
        text: t.text,
        type: t.type,
        children: true,
        schema: schema,
        table: t.text,
        li_attr: { class: 'node-' + t.type }
      }));
      callback(nodes);
    })
    .catch(() => callback([]));
}

function fetchColumns(schema, table, callback) {
  fetch(`${API}/schema?connId=${currentConnId}&schema=${encodeURIComponent(schema)}&table=${encodeURIComponent(table)}`)
    .then(r => r.json())
    .then(data => {
      const nodes = (data || []).map(c => {
        const pk = c.meta?.primaryKey ? ' 🔑' : '';
        const nullable = c.meta?.nullable === 'NO' ? ' *' : '';
        return {
          id: 'col_' + schema + '_' + table + '_' + c.text,
          text: `${c.text}${pk}<span class="col-type">${c.meta?.dataType || ''}</span>${nullable}`,
          type: 'column',
          children: false,
          icon: false,
          li_attr: { class: 'node-column' }
        };
      });
      callback(nodes);
    })
    .catch(() => callback([]));
}

function refreshTree() {
  $('#jstree').jstree(true).refresh();
}

function collapseTree() {
  $('#jstree').jstree('close_all');
}

function filterTree(val) {
  $('#jstree').jstree(true).search(val);
}

// ── Connections ───────────────────────────────────────────────
async function loadConnections() {
  try {
    const r = await fetch(`${API}/connections`);
    const data = await r.json();
    const sel = document.getElementById('conn-selector');
    // Keep placeholder
    while (sel.options.length > 1) sel.remove(1);
    (data || []).forEach(c => {
      const opt = new Option(`${c.name} (${c.driver})`, c.id);
      if (c.connected) opt.style.color = '#81c995';
      sel.add(opt);
      if (c.connected && !currentConnId) {
        currentConnId = c.id;
        sel.value = c.id;
        setConnected(true, c.name);
      }
    });
  } catch {}
}

function onConnChange(id) {
  if (!id) { currentConnId = null; setConnected(false); return; }
  currentConnId = id;
  setConnected(true);
  refreshTree();
}

function setConnected(yes, name) {
  document.getElementById('global-dot').className = 'status-dot' + (yes ? ' connected' : '');
  document.getElementById('global-status').textContent = yes ? (name || 'Connected') : 'Not connected';
  document.getElementById('btn-disconnect').style.display = yes ? '' : 'none';
}

async function disconnectCurrent() {
  if (!currentConnId) return;
  await fetch(`${API}/disconnect`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ id: currentConnId })
  });
  currentConnId = null;
  setConnected(false);
  document.getElementById('conn-selector').value = '';
  $('#jstree').jstree(true).refresh();
}

// ── Connection modal ──────────────────────────────────────────
function openNewConnModal() {
  document.getElementById('conn-modal').style.display = 'flex';
}

function closeModal(id) {
  document.getElementById(id).style.display = 'none';
}

function onDriverChange(driver) {
  const isSqlite = driver === 'sqlite3';
  document.getElementById('c-host-row').style.display = isSqlite ? 'none' : '';
  document.getElementById('c-sqlite-row').style.display = isSqlite ? '' : 'none';
  document.getElementById('c-auth-row').style.display = isSqlite ? 'none' : '';
  document.getElementById('c-ssl-row').style.display = driver === 'postgres' ? '' : 'none';

  const defaultPorts = { postgres: 5432, mysql: 3306, sqlserver: 1433 };
  document.getElementById('c-port').value = defaultPorts[driver] || '';
}

async function testAndConnect() {
  const btn = document.getElementById('btn-connect');
  const spinner = document.getElementById('connect-spinner');
  btn.disabled = true;
  spinner.style.display = '';

  const driver = document.getElementById('c-driver').value;
  const cfg = {
    name:     document.getElementById('c-name').value,
    driver,
    host:     document.getElementById('c-host').value,
    port:     parseInt(document.getElementById('c-port').value) || 5432,
    database: document.getElementById('c-database').value,
    username: document.getElementById('c-username').value,
    password: document.getElementById('c-password').value,
    sslMode:  document.getElementById('c-sslmode').value,
    filePath: document.getElementById('c-filepath').value,
  };

  try {
    const r = await fetch(`${API}/connect`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(cfg)
    });
    const data = await r.json();
    if (!r.ok) throw new Error(data.error || 'Connection failed');

    currentConnId = data.id;
    closeModal('conn-modal');
    await loadConnections();
    document.getElementById('conn-selector').value = currentConnId;
    setConnected(true, cfg.name);
    refreshTree();
    addMessage('ok', `Connected to ${cfg.name}`);
  } catch (err) {
    addMessage('err', err.message);
    alert('Connection failed: ' + err.message);
  } finally {
    btn.disabled = false;
    spinner.style.display = 'none';
  }
}

// ── Query execution ───────────────────────────────────────────
async function runQuery() {
  const sql = monacoEditor?.getValue() || '';
  await executeSQL(sql);
}

async function runSelection() {
  if (!monacoEditor) return;
  const selection = monacoEditor.getModel().getValueInRange(monacoEditor.getSelection());
  const sql = selection.trim() || monacoEditor.getValue();
  await executeSQL(sql);
}

async function executeSQL(sql) {
  if (!currentConnId) {
    alert('No active connection. Please connect to a database first.');
    return;
  }
  if (!sql.trim()) return;

  setRunning(true);
  const t0 = performance.now();
  addMessage('info', `Executing: ${sql.slice(0, 80)}${sql.length > 80 ? '…' : ''}`);

  queryAbortController = new AbortController();

  try {
    const r = await fetch(`${API}/query`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ connId: currentConnId, sql, limit: 10000 }),
      signal: queryAbortController.signal,
    });

    const data = await r.json();
    if (!r.ok) throw new Error(data.error || 'Query failed');

    const elapsed = ((performance.now() - t0) / 1000).toFixed(3);
    handleResults(data.results, elapsed);
  } catch (err) {
    if (err.name === 'AbortError') {
      addMessage('info', 'Query cancelled by user');
    } else {
      addMessage('err', err.message);
      setResultsEmpty(false);
      showResultsInfo(`Error: ${err.message}`);
    }
  } finally {
    setRunning(false);
    queryAbortController = null;
  }
}

function handleResults(results, elapsed) {
  if (!results || results.length === 0) {
    addMessage('info', 'No statements executed');
    return;
  }

  lastResults = results;

  // Show last SELECT result in grid
  const selectResult = results.slice().reverse().find(r => r.isSelect && !r.error);
  if (selectResult) {
    showGridResult(selectResult);
    addMessage('ok', `${selectResult.rowCount} rows returned in ${selectResult.duration} (total ${elapsed}s)`);
  }

  // Log all results
  results.forEach((res, i) => {
    if (res.error) {
      addMessage('err', `Statement ${i+1}: ${res.error}`);
    } else if (!res.isSelect) {
      addMessage('ok', `Statement ${i+1}: ${res.affectedRows} rows affected in ${res.duration}`);
    }
  });

  // Switch to grid if we have SELECT results
  if (selectResult) {
    switchResultTab('grid');
  }
}

function showGridResult(result) {
  const colDefs = result.columns.map(c => ({
    field: c.name,
    headerName: c.name,
    headerTooltip: c.type,
  }));

  document.getElementById('results-empty').style.display = 'none';
  document.getElementById('results-grid').style.display = '';

  gridApi.setGridOption('columnDefs', colDefs);
  gridApi.setGridOption('rowData', result.rows || []);

  document.getElementById('row-badge').textContent = result.rowCount;
  showResultsInfo(`${result.rowCount} rows × ${result.columns.length} cols  ·  ${result.duration}`);
}

function showResultsInfo(msg) {
  document.getElementById('results-info').textContent = msg;
}

function setResultsEmpty(empty) {
  document.getElementById('results-empty').style.display = empty ? 'flex' : 'none';
  document.getElementById('results-grid').style.display = empty ? 'none' : '';
}

function stopQuery() {
  queryAbortController?.abort();
}

function setRunning(yes) {
  document.getElementById('btn-stop').style.display = yes ? '' : 'none';
  document.querySelector('.toolbar-btn.primary').disabled = yes;
}

// ── Format SQL (naïve beautifier) ─────────────────────────────
function formatQuery() {
  if (!monacoEditor) return;
  const sql = monacoEditor.getValue();
  const formatted = basicFormatSQL(sql);
  monacoEditor.setValue(formatted);
  addMessage('info', 'SQL formatted');
}

function basicFormatSQL(sql) {
  const keywords = ['SELECT', 'FROM', 'WHERE', 'JOIN', 'LEFT JOIN', 'RIGHT JOIN',
    'INNER JOIN', 'OUTER JOIN', 'CROSS JOIN', 'ON', 'GROUP BY', 'ORDER BY',
    'HAVING', 'LIMIT', 'OFFSET', 'UNION', 'UNION ALL', 'INSERT INTO', 'VALUES',
    'UPDATE', 'SET', 'DELETE FROM', 'CREATE TABLE', 'ALTER TABLE', 'DROP TABLE',
    'WITH', 'AS', 'AND', 'OR'];
  let out = sql.trim();
  keywords.forEach(kw => {
    const re = new RegExp(`\\b${kw}\\b`, 'gi');
    out = out.replace(re, '\n' + kw);
  });
  return out.split('\n').map(l => l.trim()).filter(Boolean).join('\n');
}

// ── Export ────────────────────────────────────────────────────
async function exportCSV() {
  if (!currentConnId) return;
  const sql = monacoEditor?.getValue() || '';
  if (!sql.trim()) return;

  try {
    const r = await fetch(`${API}/export`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ connId: currentConnId, sql })
    });
    if (!r.ok) { const d = await r.json(); throw new Error(d.error); }
    const blob = await r.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url; a.download = 'export.csv'; a.click();
    URL.revokeObjectURL(url);
    addMessage('ok', 'CSV exported');
  } catch (err) {
    addMessage('err', 'Export failed: ' + err.message);
  }
}

function copyResults() {
  if (!gridApi) return;
  const rows = [];
  gridApi.forEachNode(n => rows.push(n.data));
  if (!rows.length) return;
  const cols = gridApi.getColumnDefs().map(c => c.field);
  const lines = [cols.join('\t')];
  rows.forEach(r => lines.push(cols.map(c => r[c] ?? 'NULL').join('\t')));
  navigator.clipboard.writeText(lines.join('\n')).then(() => addMessage('ok', 'Results copied to clipboard'));
}

function clearResults() {
  gridApi?.setGridOption('rowData', []);
  gridApi?.setGridOption('columnDefs', []);
  setResultsEmpty(true);
  showResultsInfo('Ready');
  document.getElementById('row-badge').textContent = '0';
}

// ── Tabs ──────────────────────────────────────────────────────
function newTab() {
  tabCounter++;
  const tab = { id: tabCounter, name: `Query ${tabCounter}`, sql: '' };
  tabs.push(tab);
  renderTabs();
  switchTab(tabCounter);
}

function closeTab(e, id) {
  e.stopPropagation();
  if (tabs.length === 1) return;
  tabs = tabs.filter(t => t.id !== id);
  if (activeTabId === id) switchTab(tabs[tabs.length - 1].id);
  else renderTabs();
}

function switchTab(id) {
  if (activeTabId !== id && monacoEditor) {
    const current = tabs.find(t => t.id === activeTabId);
    if (current) current.sql = monacoEditor.getValue();
  }
  activeTabId = id;
  const tab = tabs.find(t => t.id === id);
  if (tab && monacoEditor) monacoEditor.setValue(tab.sql);
  renderTabs();
}

function renderTabs() {
  const bar = document.getElementById('tab-bar');
  const newBtn = document.getElementById('new-tab-btn');
  bar.innerHTML = '';
  tabs.forEach(t => {
    const el = document.createElement('div');
    el.className = 'tab' + (t.id === activeTabId ? ' active' : '');
    el.dataset.id = t.id;
    el.innerHTML = `
      <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>
      ${t.name}
      <span class="tab-close" onclick="closeTab(event,${t.id})">×</span>`;
    el.onclick = () => switchTab(t.id);
    bar.appendChild(el);
  });
  bar.appendChild(newBtn);
}

// ── Result tabs ───────────────────────────────────────────────
function switchResultTab(name) {
  document.querySelectorAll('.result-tab').forEach(t => {
    t.classList.toggle('active', t.dataset.resultTab === name);
  });
  document.getElementById('results-grid').style.display = name === 'grid' ? '' : 'none';
  document.getElementById('messages-panel').style.display = name === 'messages' ? '' : 'none';
  document.getElementById('results-empty').style.display =
    (name === 'grid' && (!gridApi || gridApi.getDisplayedRowCount() === 0)) ? 'flex' : 'none';
}

// ── Messages ──────────────────────────────────────────────────
function addMessage(type, text) {
  msgCount++;
  document.getElementById('msg-badge').textContent = msgCount;
  const panel = document.getElementById('messages-panel');
  const now = new Date().toLocaleTimeString();
  const div = document.createElement('div');
  div.className = `msg-line msg-${type}`;
  div.innerHTML = `<span class="msg-time">${now}</span><span class="msg-text">${escapeHtml(text)}</span>`;
  panel.appendChild(div);
  panel.scrollTop = panel.scrollHeight;
}

function escapeHtml(s) {
  return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
}

// ── Editor helpers ────────────────────────────────────────────
function insertSnippet(sql) {
  if (!monacoEditor) return;
  const pos = monacoEditor.getPosition();
  monacoEditor.executeEdits('', [{
    range: new monaco.Range(pos.lineNumber, pos.column, pos.lineNumber, pos.column),
    text: '\n' + sql + '\n',
  }]);
  monacoEditor.focus();
}

function setEditorSQL(sql) {
  if (!monacoEditor) return;
  monacoEditor.setValue(sql);
  monacoEditor.focus();
}

// ── Keyboard shortcuts ────────────────────────────────────────
document.addEventListener('keydown', e => {
  if (e.key === 'Escape') {
    document.querySelectorAll('.modal-overlay').forEach(m => m.style.display = 'none');
  }
});

// ── Resize ────────────────────────────────────────────────────
window.addEventListener('resize', () => monacoEditor?.layout());

// ── Split panes (editor vs results) ───────────────────────────
document.addEventListener('DOMContentLoaded', () => {
  Split(['#editor-pane', '#results-pane'], {
    sizes: [55, 45],
    minSize: [100, 80],
    direction: 'vertical',
    gutterSize: 5,
    onDrag: () => monacoEditor?.layout(),
  });
});
