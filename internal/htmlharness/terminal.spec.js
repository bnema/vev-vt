import { test, expect } from '@playwright/test';
import fs from 'node:fs/promises';
import path from 'node:path';

const root = process.cwd();

async function mount(page) {
  await page.setContent('<main><div id="terminal"></div></main>');
  await page.addStyleTag({ path: path.join(root, 'html/terminal.css') });
  await page.addScriptTag({ path: path.join(root, 'html/browser/terminal.js') });
  await page.evaluate(() => {
    globalThis.events = [];
    globalThis.terminal = VevTerminal.mount(document.querySelector('#terminal'), {
      label: 'Development terminal',
      decide(event) {
        return { emit: true, preventDefault: event.type === 'key' && event.key === 'ArrowUp' };
      },
      send(event) { globalThis.events.push(event); }
    });
  });
}

test('applies the Go renderer contract fixture', async ({ page }) => {
  await mount(page);
  const fixture = JSON.parse(await fs.readFile(path.join(root, 'internal/htmlharness/testdata/snapshot.json'), 'utf8'));
  await page.evaluate(value => terminal.apply(value), fixture);
  await expect(page.locator('.vev-terminal__cell')).toHaveText(['A', '界']);
  await expect(page.locator('.vev-terminal__cursor')).toBeVisible();
  await page.evaluate(value => terminal.apply({
    ...value,
    snapshot: false,
    rows: [],
    styles: [],
    cursor: { ...value.cursor, visible: false }
  }), fixture);
  await expect(page.locator('.vev-terminal__cursor')).toBeHidden();
});

test('applies typed rows without interpreting terminal text as markup', async ({ page }) => {
  await mount(page);
  const hostile = '<img src=x onerror="globalThis.injected=true">';
  const update = {
    schemaVersion: 1,
    width: 3,
    height: 1,
    snapshot: true,
    styles: [{
      foreground: { kind: 2, rgb: { r: 1, g: 2, b: 3 } },
      background: { kind: 0 },
      underlineColor: { kind: 0 },
      bold: true
    }],
    rows: [{ row: 0, cells: [
      { column: 0, width: 1, text: hostile, style: 0 },
      { column: 1, width: 2, text: '界', style: 0 }
    ] }],
    cursor: { row: 0, column: 1, visible: true, style: 3, styleSet: true }
  };

  await page.evaluate(value => terminal.apply(value), update);
  await expect(page.locator('.vev-terminal__cell')).toHaveCount(2);
  await expect(page.locator('.vev-terminal__cell').first()).toHaveText(hostile);
  await expect(page.locator('img')).toHaveCount(0);
  await expect(page.locator('.vev-terminal')).toHaveAttribute('aria-label', 'Development terminal');
  await expect(page.getByRole('textbox', { name: 'Development terminal input' })).toBeAttached();
  await expect(page.locator('.vev-terminal__accessible-output')).toHaveText(`${hostile}界`);
  expect(await page.evaluate(() => globalThis.injected)).toBeUndefined();
});

test('keeps explicit terminal columns under fallback fonts and zoom', async ({ page }) => {
  await mount(page);
  await page.evaluate(() => {
    document.body.style.zoom = '125%';
    document.querySelector('#terminal').style.fontFamily = 'serif';
    terminal.apply({
      schemaVersion: 1, width: 4, height: 1, snapshot: true,
      styles: [{
        bold: true, italic: true, dim: true, blink: true, strikethrough: true,
        underline: true, underlineStyle: 3,
        foreground: { kind: 2, rgb: { r: 1, g: 2, b: 3 } },
        background: { kind: 1, index: 4 },
        underlineColor: { kind: 1, index: 12 }
      }],
      rows: [{ row: 0, cells: [
        { column: 0, width: 1, text: 'A', style: 0 },
        { column: 1, width: 2, text: '界', style: 0 },
        { column: 3, width: 1, text: 'B', style: 0 }
      ] }],
      cursor: { row: 0, column: 1, visible: true, style: 5, styleSet: true }
    });
  });
  const widths = await page.locator('.vev-terminal__cell').evaluateAll(nodes => nodes.map(node => node.getBoundingClientRect().width));
  expect(widths[1] / widths[0]).toBeCloseTo(2, 1);
  expect(widths[2] / widths[0]).toBeCloseTo(1, 1);
  const firstCell = page.locator('.vev-terminal__cell').first();
  await expect(firstCell).toHaveClass(/vev-terminal__cell--underline-wavy/);
  expect(await firstCell.evaluate(node => node.style.getPropertyValue('--vev-cell-fg'))).toBe('rgb(1 2 3)');
  await expect(page.locator('.vev-terminal__cursor')).toHaveClass(/vev-terminal__cursor--bar/);
});

test('rejects a malformed update before changing the DOM', async ({ page }) => {
  await mount(page);
  const snapshot = {
    schemaVersion: 1, width: 1, height: 1, snapshot: true,
    styles: [{ foreground: { kind: 0 }, background: { kind: 0 }, underlineColor: { kind: 0 } }],
    rows: [{ row: 0, cells: [{ column: 0, width: 1, text: 'A', style: 0 }] }],
    cursor: { row: 0, column: 0, visible: false, style: 0, styleSet: false }
  };
  await page.evaluate(value => terminal.apply(value), snapshot);

  const message = await page.evaluate(value => {
    try {
      terminal.apply({ ...value, rows: [{ row: 0, cells: [{ column: 0, width: 2, text: 'X', style: 0 }] }] });
      return '';
    } catch (error) {
      return error.message;
    }
  }, snapshot);
  expect(message).toContain('row coverage');
  await expect(page.locator('.vev-terminal__cell')).toHaveText('A');
  const staleVersion = await page.evaluate(value => {
    try {
      terminal.apply({ ...value, schemaVersion: 2 });
      return '';
    } catch (error) {
      return error.message;
    }
  }, snapshot);
  expect(staleVersion).toContain('unsupported update schema');

  const duplicateMount = await page.evaluate(() => {
    try {
      VevTerminal.mount(document.querySelector('#terminal'), { label: 'Duplicate' });
      return '';
    } catch (error) {
      return error.message;
    }
  });
  expect(duplicateMount).toContain('already owns a terminal');
});

test('emits text and synchronously decides key default prevention', async ({ page }) => {
  await mount(page);
  const input = page.locator('.vev-terminal__input');
  await input.focus();
  await input.evaluate(node => node.dispatchEvent(new InputEvent('beforeinput', {
    bubbles: true,
    cancelable: true,
    inputType: 'insertText',
    data: 'é'
  })));
  const keyDispatchResult = await input.evaluate(node => node.dispatchEvent(new KeyboardEvent('keydown', {
    bubbles: true,
    cancelable: true,
    key: 'ArrowUp',
    code: 'ArrowUp'
  })));

  const events = await page.evaluate(() => globalThis.events);
  expect(events.some(event => event.type === 'text' && event.text === 'é')).toBeTruthy();
  expect(events.some(event => event.type === 'key' && event.key === 'ArrowUp')).toBeTruthy();
  expect(keyDispatchResult).toBeFalsy();
});

test('emits one composed text event and bounded pointer, wheel, resize, and focus events', async ({ page }) => {
  await mount(page);
  const snapshot = {
    schemaVersion: 1, width: 2, height: 1, snapshot: true,
    styles: [{ foreground: { kind: 0 }, background: { kind: 0 }, underlineColor: { kind: 0 } }],
    rows: [{ row: 0, cells: [
      { column: 0, width: 1, text: 'A', style: 0 },
      { column: 1, width: 1, text: 'B', style: 0 }
    ] }],
    cursor: { row: 0, column: 0, visible: false, style: 0, styleSet: false }
  };
  await page.evaluate(value => terminal.apply(value), snapshot);
  const input = page.locator('.vev-terminal__input');
  await input.focus();
  await input.evaluate(node => {
    node.dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true }));
    node.dispatchEvent(new InputEvent('beforeinput', { bubbles: true, inputType: 'insertCompositionText', data: 'e' }));
    node.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true, data: 'é' }));
    const paste = new Event('paste', { bubbles: true, cancelable: true });
    Object.defineProperty(paste, 'clipboardData', { value: { getData: () => 'paste' } });
    node.dispatchEvent(paste);
  });

  const box = await page.locator('.vev-terminal__viewport').boundingBox();
  await page.mouse.click(box.x + 1, box.y + 1);
  await page.locator('.vev-terminal__viewport').dispatchEvent('wheel', { deltaX: 1, deltaY: 2, deltaMode: 0, clientX: box.x + 1, clientY: box.y + 1 });
  await expect.poll(() => page.evaluate(() => globalThis.events.some(event => event.type === 'resize'))).toBeTruthy();

  const events = await page.evaluate(() => globalThis.events);
  expect(events.filter(event => event.type === 'text' && event.text === 'é')).toHaveLength(1);
  expect(events.some(event => event.type === 'paste' && event.text === 'paste')).toBeTruthy();
  expect(events.some(event => event.type === 'pointer' && event.action === 'down')).toBeTruthy();
  expect(events.some(event => event.type === 'wheel' && event.row === 0 && event.column === 0)).toBeTruthy();
  expect(events.some(event => event.type === 'focus' && event.focused)).toBeTruthy();
});

test('applies dynamic terminal styles under a self-only CSP', async ({ page }) => {
  const violations = [];
  page.on('console', message => {
    if (message.text().includes('Content Security Policy')) violations.push(message.text());
  });
  const [css, script] = await Promise.all([
    fs.readFile(path.join(root, 'html/terminal.css'), 'utf8'),
    fs.readFile(path.join(root, 'html/browser/terminal.js'), 'utf8')
  ]);
  await page.route('http://vev.test/**', async route => {
    const pathname = new URL(route.request().url()).pathname;
    if (pathname === '/terminal.css') return route.fulfill({ contentType: 'text/css', body: css });
    if (pathname === '/terminal.js') return route.fulfill({ contentType: 'text/javascript', body: script });
    return route.fulfill({
      contentType: 'text/html',
      headers: { 'Content-Security-Policy': "default-src 'none'; script-src 'self'; style-src 'self'; base-uri 'none'" },
      body: '<link rel="stylesheet" href="/terminal.css"><div id="terminal"></div><script src="/terminal.js"></script>'
    });
  });
  await page.goto('http://vev.test/');
  await page.waitForFunction(() => globalThis.VevTerminal);
  await page.evaluate(() => {
    globalThis.terminal = VevTerminal.mount(document.querySelector('#terminal'), { label: 'CSP terminal' });
    terminal.apply({
      schemaVersion: 1, width: 1, height: 1, snapshot: true,
      styles: [{
        foreground: { kind: 2, rgb: { r: 1, g: 2, b: 3 } },
        background: { kind: 1, index: 4 },
        underlineColor: { kind: 0 }
      }],
      rows: [{ row: 0, cells: [{ column: 0, width: 1, text: 'A', style: 0 }] }],
      cursor: { row: 0, column: 0, visible: false, style: 0, styleSet: false }
    });
  });
  await expect(page.locator('.vev-terminal__cell')).toHaveCSS('color', 'rgb(1, 2, 3)');
  expect(violations).toEqual([]);
});

test('restores host attributes and theme properties on destroy', async ({ page }) => {
  await page.setContent('<div id="terminal" role="region" aria-label="Host label" style="--vev-fg:rgb(9 8 7)"></div>');
  await page.addStyleTag({ path: path.join(root, 'html/terminal.css') });
  await page.addScriptTag({ path: path.join(root, 'html/browser/terminal.js') });
  await page.evaluate(() => {
    const instance = VevTerminal.mount(document.querySelector('#terminal'), { label: 'Mounted label' });
    const color = { r: 1, g: 2, b: 3 };
    instance.setTheme({
      foreground: color, background: color, cursor: color, selection: color,
      selectionText: color, palette: Array.from({ length: 16 }, () => color)
    });
    instance.destroy();
  });
  await expect(page.locator('#terminal')).toHaveAttribute('role', 'region');
  await expect(page.locator('#terminal')).toHaveAttribute('aria-label', 'Host label');
  expect(await page.locator('#terminal').evaluate(node => node.style.getPropertyValue('--vev-fg'))).toBe('rgb(9 8 7)');
});

test('updates complete rows, applies typed themes, and destroys owned state', async ({ page }) => {
  await mount(page);
  const style = { foreground: { kind: 0 }, background: { kind: 0 }, underlineColor: { kind: 0 } };
  await page.evaluate(value => terminal.apply(value), {
    schemaVersion: 1, width: 1, height: 1, snapshot: true, styles: [style],
    rows: [{ row: 0, cells: [{ column: 0, width: 1, text: 'A', style: 0 }] }],
    cursor: { row: 0, column: 0, visible: false, style: 0, styleSet: false }
  });
  await page.evaluate(value => terminal.apply(value), {
    schemaVersion: 1, width: 1, height: 1, snapshot: false, styles: [style],
    rows: [{ row: 0, cells: [{ column: 0, width: 1, text: 'B', style: 0 }] }],
    cursor: { row: 0, column: 0, visible: false, style: 0, styleSet: false }
  });
  await expect(page.locator('.vev-terminal__accessible-output')).toHaveText('B');

  const color = { r: 1, g: 2, b: 3 };
  await page.evaluate(value => terminal.setTheme(value), {
    foreground: color, background: color, cursor: color, selection: color,
    selectionText: color, palette: Array.from({ length: 16 }, () => color)
  });
  expect(await page.locator('.vev-terminal').evaluate(node => node.style.getPropertyValue('--vev-fg'))).toContain('1 2 3');

  await page.evaluate(() => terminal.destroy());
  await expect(page.locator('.vev-terminal__viewport')).toHaveCount(0);
  await expect(page.locator('#terminal')).not.toHaveClass(/vev-terminal/);
});
