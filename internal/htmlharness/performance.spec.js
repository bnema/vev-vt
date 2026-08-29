import { test, expect } from '@playwright/test';
import path from 'node:path';

const root = process.cwd();

/** @param {number} width @param {number} height @param {string} text */
function snapshot(width, height, text) {
  const cells = Array.from({ length: width }, (_, column) => ({ column, width: 1, text, style: 0 }));
  return {
    schemaVersion: 1,
    width,
    height,
    snapshot: true,
    styles: [{ foreground: { kind: 0 }, background: { kind: 0 }, underlineColor: { kind: 0 } }],
    rows: Array.from({ length: height }, (_, row) => ({ row, cells })),
    cursor: { row: 0, column: 0, visible: false, style: 0, styleSet: false }
  };
}

test('keeps sustained snapshot and row replacement work bounded', async ({ page, browserName }, testInfo) => {
  test.skip(browserName !== 'chromium', 'reference performance budget uses pinned Chromium');
  await page.setContent('<div id="terminal"></div>');
  await page.addStyleTag({ path: path.join(root, 'html/terminal.css') });
  await page.addScriptTag({ path: path.join(root, 'html/browser/terminal.js') });

  const metrics = await page.evaluate(({ first, second, large }) => {
    const terminal = VevTerminal.mount(document.querySelector('#terminal'), { label: 'Performance terminal' });
    terminal.apply(first);
    const snapshots = [];
    for (let index = 0; index < 10; index += 1) {
      const start = performance.now();
      terminal.apply(index % 2 === 0 ? second : first);
      snapshots.push(performance.now() - start);
    }

    const rowUpdate = {
      ...first,
      snapshot: false,
      rows: [second.rows[20]]
    };
    const rows = [];
    for (let index = 0; index < 50; index += 1) {
      const start = performance.now();
      terminal.apply(rowUpdate);
      rows.push(performance.now() - start);
    }
    const percentile = (values, ratio) => [...values].sort((a, b) => a - b)[Math.ceil(values.length * ratio) - 1];
    const largeStart = performance.now();
    terminal.apply(large);
    const largeApply = performance.now() - largeStart;
    return {
      snapshotP95: percentile(snapshots, 0.95),
      rowP95: percentile(rows, 0.95),
      largeApply,
      cells: document.querySelectorAll('.vev-terminal__cell').length,
      rows: document.querySelectorAll('.vev-terminal__row').length
    };
  }, { first: snapshot(120, 40, 'A'), second: snapshot(120, 40, 'B'), large: snapshot(240, 80, 'C') });

  testInfo.annotations.push({ type: 'performance', description: JSON.stringify(metrics) });
  expect(metrics.snapshotP95).toBeLessThan(100);
  expect(metrics.rowP95).toBeLessThan(33);
  expect(metrics.largeApply).toBeLessThan(500);
  expect(metrics.cells).toBe(240 * 80);
  expect(metrics.rows).toBe(80);
});
