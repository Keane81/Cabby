// Display helpers. Plain functions with no DOM access, so Node's test runner can check them.

const UNITS = ['Б', 'КБ', 'МБ', 'ГБ', 'ТБ'];
const NBSP = ' ';

// formatBytes shows a size in binary units: 1536 → "1,5 КБ". A value that is not a size shows a dash.
export function formatBytes(bytes) {
  if (typeof bytes !== 'number' || !Number.isFinite(bytes) || bytes < 0) return '—';
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < UNITS.length - 1) {
    value /= 1024;
    unit++;
  }
  const text = unit === 0 ? String(Math.round(value)) : value.toFixed(1).replace(/\.0$/, '').replace('.', ',');
  return text + NBSP + UNITS[unit];
}

// formatNumber groups thousands with a non-breaking space: 1234567 → "1 234 567".
export function formatNumber(n) {
  if (typeof n !== 'number' || !Number.isFinite(n)) return '—';
  return String(Math.trunc(n)).replace(/\B(?=(\d{3})+(?!\d))/g, NBSP);
}

// formatTotal describes the row count of a page response: exact, an estimate or a lower bound.
export function formatTotal(total) {
  if (!total) return '';
  const n = formatNumber(total.value);
  if (total.kind === 'estimate') return '≈' + NBSP + n;
  if (total.kind === 'atLeast') return '>' + NBSP + n;
  return n;
}
