import { formatBytes, formatNumber, formatTotal } from './format.js';

const app = document.getElementById('app');
const crumbs = document.getElementById('crumbs');
const DEFAULT_SIZE = 50;
const LONG_VALUE = 80;
const MAX_FILTERS = 10;

let overview = null; // last answer of /api/databases
let tableView = null; // the table page that is currently built
let requestSeq = 0; // answers of older requests are dropped

// ---------- helpers ----------

// el builds a DOM node. Text goes in as text nodes only: nothing from the database is ever parsed as HTML.
function el(tag, props = {}, ...children) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(props || {})) {
    if (value === null || value === undefined || value === false) continue;
    if (key === 'class') node.className = value;
    else if (key.startsWith('on')) node.addEventListener(key.slice(2), value);
    else if (key === 'value') node.value = value;
    else node.setAttribute(key, value === true ? '' : value);
  }
  for (const child of children.flat()) {
    if (child === null || child === undefined || child === false) continue;
    node.append(child instanceof Node ? child : document.createTextNode(String(child)));
  }
  return node;
}

function route() {
  const raw = location.hash.replace(/^#/, '') || '/';
  const [path, query = ''] = raw.split('?');
  return { parts: path.split('/').filter(Boolean).map(decodeURIComponent), params: new URLSearchParams(query) };
}

function href(parts, params) {
  const query = params ? params.toString() : '';
  return '#/' + parts.map(encodeURIComponent).join('/') + (query ? '?' + query : '');
}

async function getJSON(url) {
  let response;
  try {
    response = await fetch(url, { headers: { Accept: 'application/json' } });
  } catch {
    throw new Error('Не удалось связаться с просмотрщиком');
  }
  let body = null;
  try {
    body = await response.json();
  } catch {
    /* not JSON */
  }
  if (!response.ok) throw new Error((body && body.error && body.error.message) || 'Ошибка запроса (' + response.status + ')');
  return body;
}

function notice(kind, text) {
  return el('div', { class: 'notice ' + kind, role: kind === 'error' ? 'alert' : null }, text);
}

function setCrumbs(items) {
  crumbs.replaceChildren(...items.map((item) => (item.href ? el('a', { href: item.href }, item.label) : el('span', {}, item.label))));
}

async function loadOverview(refresh) {
  overview = await getJSON('/api/databases' + (refresh ? '?refresh=1' : ''));
  return overview;
}

function findTable(dbName, tableName) {
  const db = overview && overview.databases.find((d) => d.name === dbName);
  return { db, table: db && db.tables.find((t) => t.name === tableName) };
}

function time(iso) {
  const d = new Date(iso);
  return isNaN(d) ? '' : d.toLocaleTimeString('ru-RU');
}

// ---------- home and database pages ----------

async function showHome() {
  tableView = null;
  setCrumbs([]);
  app.replaceChildren(el('p', { class: 'muted' }, 'Загрузка…'));
  try {
    await loadOverview(false);
  } catch (e) {
    app.replaceChildren(notice('error', e.message));
    return;
  }
  renderHome();
}

function renderHome() {
  const refresh = el('button', { class: 'primary', onclick: async (ev) => {
    ev.target.disabled = true;
    try { await loadOverview(true); } catch (e) { app.prepend(notice('error', e.message)); }
    renderHome();
  } }, 'Обновить');
  app.replaceChildren(
    el('div', { class: 'toolbar' }, el('h1', {}, 'Базы данных'), refresh,
      el('span', { class: 'muted' }, 'обновлено ' + time(overview.refreshedAt))),
    el('div', { class: 'cards' }, overview.databases.map(dbCard)),
  );
}

function dbCard(db) {
  const head = el('header', {}, el('h2', {}, el('a', { href: href(['db', db.name]) }, db.name)),
    db.available ? el('span', {}, formatBytes(db.sizeBytes) + ' на диске') : el('span', { class: 'muted' }, 'недоступна'));
  if (!db.available) return el('section', { class: 'card' }, head, notice('error', 'База недоступна: ' + db.error));
  const rows = db.tables.map((t) => el('tr', {},
    el('td', {}, el('a', { href: href(['db', db.name, t.name]) }, t.name)),
    el('td', { class: 'num' }, (t.exact ? '' : '≈ ') + formatNumber(t.estimatedRows)),
    el('td', { class: 'num' }, formatBytes(t.totalBytes))));
  return el('section', { class: 'card' }, head,
    db.tables.length === 0 ? notice('empty', 'В базе нет таблиц') :
      el('table', {}, el('thead', {}, el('tr', {}, el('th', {}, 'Таблица'), el('th', { class: 'num' }, 'Строк'), el('th', { class: 'num' }, 'Размер'))),
        el('tbody', {}, rows)));
}

let sizeSort = { key: 'totalBytes', dir: 'desc' };

async function showDatabase(name) {
  tableView = null;
  setCrumbs([{ label: 'Базы', href: '#/' }, { label: name }]);
  app.replaceChildren(el('p', { class: 'muted' }, 'Загрузка…'));
  try {
    await loadOverview(false);
  } catch (e) {
    app.replaceChildren(notice('error', e.message));
    return;
  }
  renderDatabase(name);
}

function renderDatabase(name) {
  const db = overview.databases.find((d) => d.name === name);
  if (!db) { app.replaceChildren(notice('error', 'База не найдена')); return; }
  const refresh = el('button', { class: 'primary', onclick: async (ev) => {
    ev.target.disabled = true;
    try { await loadOverview(true); } catch (e) { app.prepend(notice('error', e.message)); }
    renderDatabase(name);
  } }, 'Обновить');
  const head = el('div', { class: 'toolbar' }, el('h1', {}, name), refresh,
    el('span', {}, db.available ? 'Размер базы: ' + formatBytes(db.sizeBytes) : ''),
    el('span', { class: 'muted' }, 'обновлено ' + time(overview.refreshedAt)));
  if (!db.available) { app.replaceChildren(head, notice('error', 'База недоступна: ' + db.error)); return; }

  const columns = [['name', 'Таблица', false], ['estimatedRows', 'Строк', true], ['dataBytes', 'Данные', true], ['indexBytes', 'Индексы', true], ['totalBytes', 'Всего', true]];
  const tables = [...db.tables].sort((a, b) => {
    const x = a[sizeSort.key], y = b[sizeSort.key];
    const cmp = typeof x === 'string' ? x.localeCompare(y) : x - y;
    return sizeSort.dir === 'asc' ? cmp : -cmp;
  });
  const header = columns.map(([key, label, numeric]) => el('th', {
    class: 'sortable' + (numeric ? ' num' : ''), 'aria-sort': sizeSort.key === key ? (sizeSort.dir === 'asc' ? 'ascending' : 'descending') : 'none',
    onclick: () => { sizeSort = { key, dir: sizeSort.key === key && sizeSort.dir === 'desc' ? 'asc' : 'desc' }; renderDatabase(name); },
  }, label + (sizeSort.key === key ? (sizeSort.dir === 'asc' ? ' ▲' : ' ▼') : '')));
  const body = tables.map((t) => el('tr', {},
    el('td', {}, el('a', { href: href(['db', name, t.name]) }, t.name)),
    el('td', { class: 'num' }, (t.exact ? '' : '≈ ') + formatNumber(t.estimatedRows)),
    el('td', { class: 'num' }, formatBytes(t.dataBytes)),
    el('td', { class: 'num' }, formatBytes(t.indexBytes)),
    el('td', { class: 'num' }, formatBytes(t.totalBytes))));
  app.replaceChildren(head, tables.length === 0 ? notice('empty', 'В базе нет таблиц') :
    el('div', { class: 'tablewrap' }, el('table', {}, el('thead', {}, el('tr', {}, header)), el('tbody', {}, body))));
}

// ---------- table page ----------

function readState(params) {
  const size = parseInt(params.get('size') || '', 10);
  const page = parseInt(params.get('page') || '', 10);
  return {
    q: params.get('q') || '',
    filters: params.getAll('f'),
    sort: params.get('sort') || '',
    dir: params.get('dir') === 'desc' ? 'desc' : 'asc',
    page: page >= 1 ? page : 1,
    size: size >= 1 && size <= 200 ? size : DEFAULT_SIZE,
  };
}

function stateToParams(s) {
  const p = new URLSearchParams();
  if (s.q) p.set('q', s.q);
  s.filters.forEach((f) => p.append('f', f));
  if (s.sort) { p.set('sort', s.sort); p.set('dir', s.dir); }
  if (s.page > 1) p.set('page', String(s.page));
  if (s.size !== DEFAULT_SIZE) p.set('size', String(s.size));
  return p;
}

function go(dbName, tableName, s) {
  location.hash = href(['db', dbName, tableName], stateToParams(s));
}

const OPS = {
  text: [['eq', 'равно'], ['contains', 'содержит'], ['is_null', 'пусто (NULL)']],
  number: [['eq', '='], ['gte', '≥'], ['lte', '≤'], ['is_null', 'пусто (NULL)']],
  timestamp: [['gte', 'с'], ['lte', 'по'], ['eq', 'равно'], ['is_null', 'пусто (NULL)']],
  uuid: [['eq', 'равно'], ['contains', 'содержит'], ['is_null', 'пусто (NULL)']],
  bool: [['eq', 'равно'], ['is_null', 'пусто (NULL)']],
};
const OP_LABEL = { eq: '=', contains: 'содержит', gte: '≥', lte: '≤', is_null: 'пусто' };
const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const TIME_RE = /^\d{4}-\d{2}-\d{2}([T ]\d{2}:\d{2}(:\d{2}(\.\d+)?)?(Z|[+-]\d{2}:\d{2})?)?$/;

// validateFilter returns a hint for a value the server would refuse, or '' when it looks fine.
function validateFilter(col, op, value) {
  if (op === 'is_null') return '';
  if (value === '') return 'Введите значение';
  if (op === 'contains') return '';
  if (col.type === 'number') return /^-?\d+(\.\d+)?$/.test(value) ? '' : 'Нужно число';
  if (col.type === 'uuid') return UUID_RE.test(value) ? '' : 'Нужен идентификатор вида 123e4567-e89b-12d3-a456-426614174000';
  if (col.type === 'timestamp') return TIME_RE.test(value) ? '' : 'Нужна дата: 2026-10-03 или 2026-10-03T10:00:00Z (без пояса — UTC)';
  if (col.type === 'bool') return value === 'true' || value === 'false' ? '' : 'true или false';
  return '';
}

async function showTable(dbName, tableName, params) {
  setCrumbs([{ label: 'Базы', href: '#/' }, { label: dbName, href: href(['db', dbName]) }, { label: tableName }]);
  if (!tableView || tableView.db !== dbName || tableView.table !== tableName) {
    app.replaceChildren(el('p', { class: 'muted' }, 'Загрузка…'));
    try {
      if (!overview) await loadOverview(false);
    } catch (e) {
      app.replaceChildren(notice('error', e.message));
      return;
    }
    const { db, table } = findTable(dbName, tableName);
    if (!db || !table) {
      try { await loadOverview(false); } catch { /* shown below */ }
      const retry = findTable(dbName, tableName);
      if (!retry.table) { app.replaceChildren(notice('error', 'Таблица не найдена')); return; }
      return showTable(dbName, tableName, params);
    }
    if (!db.available) { app.replaceChildren(notice('error', 'База недоступна: ' + db.error)); return; }
    buildTable(dbName, table);
  }
  const state = readState(params);
  tableView.state = state;
  tableView.syncControls();
  await loadRows();
}

function buildTable(dbName, table) {
  const view = { db: dbName, table: table.name, meta: table, state: readState(new URLSearchParams()) };
  tableView = view;

  const search = el('input', { type: 'search', class: 'grow', placeholder: 'Поиск по текстовым столбцам и идентификаторам', 'aria-label': 'Поиск' });
  let timer = null;
  const applySearch = () => {
    clearTimeout(timer);
    if (search.value === view.state.q) return;
    go(dbName, table.name, { ...view.state, q: search.value, page: 1 });
  };
  search.addEventListener('input', () => { clearTimeout(timer); timer = setTimeout(applySearch, 450); });
  search.addEventListener('keydown', (e) => { if (e.key === 'Enter') applySearch(); });

  const sizeSelect = el('select', { 'aria-label': 'Размер страницы', onchange: () => go(dbName, table.name, { ...view.state, size: Number(sizeSelect.value), page: 1 }) },
    [25, 50, 100, 200].map((n) => el('option', { value: n }, n + ' на странице')));

  const filterable = table.columns.filter((c) => c.filterable);
  const colSelect = el('select', { 'aria-label': 'Столбец фильтра' }, filterable.map((c) => el('option', { value: c.name }, c.name)));
  const opSelect = el('select', { 'aria-label': 'Условие' });
  const valueInput = el('input', { type: 'text', 'aria-label': 'Значение фильтра' });
  const hint = el('span', { class: 'hint', role: 'alert' });
  const syncOps = () => {
    const col = filterable.find((c) => c.name === colSelect.value);
    opSelect.replaceChildren(...(OPS[col ? col.type : 'text'] || OPS.text).map(([v, l]) => el('option', { value: v }, l)));
    syncValue();
  };
  const syncValue = () => {
    const isNull = opSelect.value === 'is_null';
    valueInput.disabled = isNull;
    if (isNull) valueInput.value = '';
    const col = filterable.find((c) => c.name === colSelect.value);
    valueInput.placeholder = col && col.type === 'timestamp' ? '2026-10-03 или 2026-10-03T10:00:00Z' : col && col.type === 'bool' ? 'true / false' : 'значение';
    hint.textContent = '';
  };
  colSelect.addEventListener('change', syncOps);
  opSelect.addEventListener('change', syncValue);
  const addFilter = () => {
    const col = filterable.find((c) => c.name === colSelect.value);
    if (!col) return;
    const value = valueInput.value.trim();
    const problem = validateFilter(col, opSelect.value, value);
    if (problem) { hint.textContent = problem; return; }
    if (view.state.filters.length >= MAX_FILTERS) { hint.textContent = 'Слишком много фильтров'; return; }
    const raw = col.name + ':' + opSelect.value + (opSelect.value === 'is_null' ? '' : ':' + value);
    valueInput.value = '';
    go(dbName, table.name, { ...view.state, filters: [...view.state.filters, raw], page: 1 });
  };
  valueInput.addEventListener('keydown', (e) => { if (e.key === 'Enter') addFilter(); });
  const filterBar = el('div', { class: 'toolbar' }, colSelect, opSelect, valueInput, el('button', { onclick: addFilter }, 'Добавить фильтр'), hint);
  if (filterable.length === 0) filterBar.replaceChildren(el('span', { class: 'muted' }, 'В таблице нет столбцов для фильтрации'));
  syncOps();

  const chips = el('div', { class: 'filters' });
  const status = el('div', { class: 'toolbar' });
  const results = el('div');
  const pager = el('div', { class: 'pager' });
  const dialog = el('dialog', { 'aria-label': 'Запись' });
  dialog.addEventListener('click', (e) => { if (e.target === dialog) dialog.close(); });

  Object.assign(view, { search, sizeSelect, chips, status, results, pager, dialog, filterable });
  view.syncControls = () => {
    const s = view.state;
    if (search.value !== s.q && document.activeElement !== search) search.value = s.q;
    sizeSelect.value = String(s.size);
    chips.replaceChildren(...s.filters.map((raw, i) => {
      const [col, op, ...rest] = raw.split(':');
      return el('span', { class: 'chip' }, col + ' ' + (OP_LABEL[op] || op) + (op === 'is_null' ? '' : ' ' + rest.join(':')),
        el('button', { 'aria-label': 'Убрать фильтр', onclick: () => go(dbName, table.name, { ...s, filters: s.filters.filter((_, j) => j !== i), page: 1 }) }, '×'));
    }));
    if (s.q || s.filters.length) {
      chips.append(el('button', { onclick: () => go(dbName, table.name, { ...s, q: '', filters: [], page: 1 }) }, 'Сбросить условия'));
    }
  };

  app.replaceChildren(
    el('div', { class: 'toolbar' }, el('h1', {}, table.name), el('span', { class: 'muted' }, dbName)),
    el('div', { class: 'toolbar' }, search, sizeSelect, el('button', { onclick: () => loadRows() }, 'Обновить')),
    filterBar, chips, status, results, pager, dialog);
}

async function loadRows() {
  const view = tableView;
  const s = view.state;
  const seq = ++requestSeq;
  const q = new URLSearchParams();
  if (s.q) q.set('q', s.q);
  s.filters.forEach((f) => q.append('filter', f));
  if (s.sort) { q.set('sort', s.sort); q.set('dir', s.dir); }
  q.set('page', String(s.page));
  q.set('pageSize', String(s.size));
  view.status.replaceChildren(el('span', { class: 'muted' }, 'Загрузка…'));
  let data;
  try {
    data = await getJSON('/api/databases/' + encodeURIComponent(view.db) + '/tables/' + encodeURIComponent(view.table) + '/rows?' + q);
  } catch (e) {
    if (seq !== requestSeq) return;
    view.status.replaceChildren();
    view.results.replaceChildren(notice('error', e.message));
    view.pager.replaceChildren();
    return;
  }
  if (seq !== requestSeq) return;
  renderRows(view, data);
}

function renderRows(view, data) {
  const s = view.state;
  const filtered = s.q || s.filters.length > 0;
  view.status.replaceChildren(el('span', {}, (filtered ? 'Найдено: ' : 'Всего строк: ') + formatTotal(data.total)));

  if (data.rows.length === 0) {
    view.results.replaceChildren(filtered
      ? el('div', { class: 'notice empty' }, 'Ничего не найдено. ', el('button', { onclick: () => go(view.db, view.table, { ...s, q: '', filters: [], page: 1 }) }, 'Сбросить условия'))
      : s.page > 1 ? el('div', { class: 'notice empty' }, 'На этой странице нет строк.') : notice('empty', 'Нет данных'));
  } else {
    view.results.replaceChildren(dataTable(view, data));
  }
  renderPager(view, data);
}

function dataTable(view, data) {
  const s = view.state;
  const meta = new Map(view.meta.columns.map((c) => [c.name, c]));
  const head = data.columns.map((name) => {
    const col = meta.get(name);
    if (!col || !col.sortable) return el('th', {}, name, col && col.sensitive ? el('span', { class: 'hiddenmark' }, ' 🔒') : '');
    const active = s.sort === name;
    return el('th', {
      class: 'sortable', 'aria-sort': active ? (s.dir === 'asc' ? 'ascending' : 'descending') : 'none', tabindex: '0',
      onclick: () => go(view.db, view.table, { ...s, sort: name, dir: active && s.dir === 'asc' ? 'desc' : 'asc', page: 1 }),
      onkeydown: (e) => { if (e.key === 'Enter') e.currentTarget.click(); },
    }, name + (active ? (s.dir === 'asc' ? ' ▲' : ' ▼') : ''));
  });
  const body = data.rows.map((row) => el('tr', { class: 'clickable', tabindex: '0', onclick: () => openRecord(view, data.columns, row),
    onkeydown: (e) => { if (e.key === 'Enter') openRecord(view, data.columns, row); } },
  row.map((cell) => el('td', {}, renderCell(cell)))));
  return el('div', { class: 'tablewrap' }, el('table', {}, el('thead', {}, el('tr', {}, head)), el('tbody', {}, body)));
}

function isMasked(cell) {
  return cell !== null && typeof cell === 'object' && cell.masked === true;
}

function cellText(cell) {
  return typeof cell === 'object' ? JSON.stringify(cell) : String(cell);
}

function renderCell(cell) {
  if (cell === null) return el('span', { class: 'null' }, 'NULL');
  if (isMasked(cell)) return el('span', { class: 'masked', title: 'Значение скрыто' }, 'скрыто');
  const text = cellText(cell);
  if (text === '') return el('span', { class: 'empty' }, 'пусто');
  const value = el('span', { class: 'v', title: text.length > LONG_VALUE ? null : text }, text);
  if (text.length <= LONG_VALUE) return value;
  const more = el('button', { class: 'more', 'aria-label': 'Развернуть значение', onclick: (ev) => {
    ev.stopPropagation();
    const open = value.classList.toggle('open');
    more.textContent = open ? 'свернуть' : '…';
  } }, '…');
  return el('span', {}, value, more);
}

function renderPager(view, data) {
  const s = view.state;
  const total = data.total;
  const pages = total.kind === 'exact' ? Math.max(1, Math.ceil(total.value / s.size)) : null;
  const hasNext = pages ? s.page < pages : data.rows.length === s.size;
  const jump = el('input', { type: 'number', min: '1', max: pages ? String(pages) : null, value: String(s.page), 'aria-label': 'Номер страницы' });
  const goTo = (page) => go(view.db, view.table, { ...s, page: Math.max(1, page) });
  jump.addEventListener('keydown', (e) => { if (e.key === 'Enter') goTo(parseInt(jump.value, 10) || 1); });
  view.pager.replaceChildren(
    el('button', { disabled: s.page <= 1, onclick: () => goTo(s.page - 1) }, '‹ Назад'),
    el('span', {}, 'Страница'), jump, el('span', { class: 'muted' }, pages ? 'из ' + formatNumber(pages) : ''),
    el('button', { disabled: !hasNext, onclick: () => goTo(s.page + 1) }, 'Вперёд ›'),
  );
}

// ---------- record card ----------

async function openRecord(view, columns, row) {
  const { dialog } = view;
  let record = { columns, row };
  const pk = view.meta.primaryKey || [];
  const idx = pk.map((name) => columns.indexOf(name));
  if (pk.length > 0 && idx.every((i) => i >= 0 && row[i] !== null && !isMasked(row[i]))) {
    // Reload the record by its key, so the card shows the current values.
    const q = new URLSearchParams({ page: '1', pageSize: '1' });
    pk.forEach((name, i) => q.append('filter', name + ':eq:' + cellText(row[idx[i]])));
    try {
      const fresh = await getJSON('/api/databases/' + encodeURIComponent(view.db) + '/tables/' + encodeURIComponent(view.table) + '/rows?' + q);
      if (fresh.rows.length === 1) record = { columns: fresh.columns, row: fresh.rows[0] };
    } catch {
      /* keep the row that is already on the page */
    }
  }
  const meta = new Map(view.meta.columns.map((c) => [c.name, c]));
  const list = el('dl', { class: 'record' });
  record.columns.forEach((name, i) => {
    const cell = record.row[i];
    let content;
    let copyText = null;
    if (cell === null) content = el('span', { class: 'null' }, 'NULL');
    else if (isMasked(cell)) content = el('span', { class: 'masked' }, 'скрыто');
    else {
      copyText = cellText(cell);
      content = copyText === '' ? el('span', { class: 'empty' }, 'пусто') : el('pre', {}, copyText);
    }
    const copy = copyText === null ? null : el('button', { 'aria-label': 'Копировать значение ' + name, onclick: async (ev) => {
      const button = ev.currentTarget;
      try { await navigator.clipboard.writeText(copyText); button.textContent = 'Скопировано'; } catch { button.textContent = 'Не удалось'; }
      setTimeout(() => { button.textContent = 'Копировать'; }, 1200);
    } }, 'Копировать');
    list.append(el('dt', {}, name, meta.get(name) && meta.get(name).sensitive ? ' 🔒' : ''), el('dd', {}, content, copy));
  });
  dialog.replaceChildren(
    el('div', { class: 'head' }, el('h2', {}, view.table), el('button', { onclick: () => dialog.close() }, 'Закрыть')),
    el('div', { class: 'body' }, list));
  if (!dialog.open) dialog.showModal();
}

// ---------- router ----------

async function render() {
  const { parts, params } = route();
  if (parts[0] === 'db' && parts[1] && parts[2]) await showTable(parts[1], parts[2], params);
  else if (parts[0] === 'db' && parts[1]) await showDatabase(parts[1]);
  else await showHome();
}

window.addEventListener('hashchange', render);
render();
