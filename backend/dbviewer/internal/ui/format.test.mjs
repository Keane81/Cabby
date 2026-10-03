import test from 'node:test';
import assert from 'node:assert/strict';
import { formatBytes, formatNumber, formatTotal } from './format.js';

const nb = (s) => s.replace(/ /g, ' ');

test('formatBytes uses binary units', () => {
  assert.equal(formatBytes(0), nb('0 Б'));
  assert.equal(formatBytes(1023), nb('1023 Б'));
  assert.equal(formatBytes(1024), nb('1 КБ'));
  assert.equal(formatBytes(1536), nb('1,5 КБ'));
  assert.equal(formatBytes(1024 ** 2), nb('1 МБ'));
  assert.equal(formatBytes(8 * 1024 ** 3), nb('8 ГБ'));
  assert.equal(formatBytes(3 * 1024 ** 4), nb('3 ТБ'));
  assert.equal(formatBytes(5000 * 1024 ** 4), nb('5000 ТБ'));
});

test('formatBytes rejects non-sizes', () => {
  for (const bad of [-1, NaN, Infinity, null, undefined, '12']) assert.equal(formatBytes(bad), '—');
});

test('formatNumber groups thousands', () => {
  assert.equal(formatNumber(0), '0');
  assert.equal(formatNumber(999), '999');
  assert.equal(formatNumber(1000), nb('1 000'));
  assert.equal(formatNumber(1234567), nb('1 234 567'));
  assert.equal(formatNumber(undefined), '—');
});

test('formatTotal marks estimates and lower bounds', () => {
  assert.equal(formatTotal({ value: 1500, kind: 'exact' }), nb('1 500'));
  assert.equal(formatTotal({ value: 2000000, kind: 'estimate' }), nb('≈ 2 000 000'));
  assert.equal(formatTotal({ value: 10000, kind: 'atLeast' }), nb('> 10 000'));
  assert.equal(formatTotal(null), '');
});
