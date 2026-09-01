(() => {
  'use strict';

  /** @typedef {{r: number, g: number, b: number}} RGB */
  /** @typedef {{kind: 0}|{kind: 1, index: number}|{kind: 2, rgb: RGB}} Color */
  /** @typedef {{bold?: boolean, italic?: boolean, inverse?: boolean, dim?: boolean, blink?: boolean, strikethrough?: boolean, underline?: boolean, underlineStyle?: number, foreground: Color, background: Color, underlineColor: Color}} TerminalStyle */
  /** @typedef {{column: number, width: number, text: string, style: number}} CellUpdate */
  /** @typedef {{row: number, cells: CellUpdate[]}} RowUpdate */
  /** @typedef {{row: number, column: number, visible: boolean, style: number, styleSet: boolean}} Cursor */
  /** @typedef {{schemaVersion: number, width: number, height: number, snapshot: boolean, rows: RowUpdate[], styles: TerminalStyle[], cursor: Cursor}} Update */
  /** @typedef {{emit: boolean, preventDefault: boolean}} CaptureDecision */
  /** @typedef {{schemaVersion?: number, type: string, [key: string]: unknown}} BrowserEvent */
  /** @typedef {{maxCells?: number, maxRowsPerUpdate?: number, maxStyles?: number, maxTextBytes?: number, maxUpdateBytes?: number, maxPasteBytes?: number}} BrowserLimits */
  /** @typedef {{foreground: RGB, background: RGB, cursor: RGB, selection: RGB, selectionText: RGB, palette: RGB[]}} TerminalTheme */
  /** @typedef {{label: string, decide?: (event: BrowserEvent) => CaptureDecision, send?: (event: BrowserEvent) => void, limits?: BrowserLimits}} MountOptions */
  /** @typedef {{apply(update: Update): void, focus(): void, setMouseCapture(enabled: boolean): void, setTheme(theme: TerminalTheme): void, destroy(): void}} Terminal */

  const SCHEMA_VERSION = 1;
  const DEFAULT_LIMITS = Object.freeze({
    maxCells: 1_000_000,
    maxRowsPerUpdate: 10_000,
    maxStyles: 65_536,
    maxTextBytes: 64 << 10,
    maxUpdateBytes: 64 << 20,
    maxPasteBytes: 1 << 20
  });
  const encoder = new TextEncoder();

  function fail(message) {
    throw new TypeError(`VevTerminal: ${message}`);
  }

  function object(value, name) {
    if (value === null || typeof value !== 'object' || Array.isArray(value)) fail(`${name} must be an object`);
    return value;
  }

  function keys(value, allowed, name) {
    for (const key of Object.keys(value)) {
      if (!allowed.includes(key)) fail(`${name} contains unknown field ${key}`);
    }
  }

  function integer(value, minimum, maximum, name) {
    if (!Number.isSafeInteger(value) || value < minimum || value > maximum) fail(`${name} is out of range`);
    return value;
  }

  function boolean(value, name, fallback = false) {
    if (value === undefined) return fallback;
    if (typeof value !== 'boolean') fail(`${name} must be boolean`);
    return value;
  }

  /** @returns {{value: string, byteLength: number}} */
  function boundedString(value, limit, name) {
    if (typeof value !== 'string') fail(`${name} must be a string`);
    const byteLength = encoder.encode(value).byteLength;
    if (byteLength > limit) fail(`${name} exceeds its byte limit`);
    return { value, byteLength };
  }

  function limits(requested = {}) {
    object(requested, 'limits');
    keys(requested, Object.keys(DEFAULT_LIMITS), 'limits');
    const result = {};
    for (const [name, fallback] of Object.entries(DEFAULT_LIMITS)) {
      result[name] = requested[name] === undefined ? fallback : integer(requested[name], 1, Number.MAX_SAFE_INTEGER, `limits.${name}`);
    }
    return Object.freeze(result);
  }

  function validateRGB(value, name) {
    object(value, name);
    keys(value, ['r', 'g', 'b'], name);
    return {
      r: integer(value.r, 0, 255, `${name}.r`),
      g: integer(value.g, 0, 255, `${name}.g`),
      b: integer(value.b, 0, 255, `${name}.b`)
    };
  }

  function validateColor(value, name) {
    object(value, name);
    keys(value, ['kind', 'index', 'rgb'], name);
    const kind = integer(value.kind, 0, 2, `${name}.kind`);
    if (kind === 0) {
      if (value.index !== undefined || value.rgb !== undefined) fail(`${name} default color has a payload`);
      return { kind };
    }
    if (kind === 1) {
      if (value.rgb !== undefined) fail(`${name} indexed color has an rgb payload`);
      return { kind, index: integer(value.index, 0, 255, `${name}.index`) };
    }
    if (value.index !== undefined) fail(`${name} rgb color has an index payload`);
    return { kind, rgb: validateRGB(value.rgb, `${name}.rgb`) };
  }

  function validateStyle(value, index) {
    const name = `styles[${index}]`;
    object(value, name);
    keys(value, [
      'bold', 'italic', 'inverse', 'dim', 'blink', 'strikethrough', 'underline',
      'underlineStyle', 'foreground', 'background', 'underlineColor'
    ], name);
    return {
      bold: boolean(value.bold, `${name}.bold`),
      italic: boolean(value.italic, `${name}.italic`),
      inverse: boolean(value.inverse, `${name}.inverse`),
      dim: boolean(value.dim, `${name}.dim`),
      blink: boolean(value.blink, `${name}.blink`),
      strikethrough: boolean(value.strikethrough, `${name}.strikethrough`),
      underline: boolean(value.underline, `${name}.underline`),
      underlineStyle: value.underlineStyle === undefined ? 0 : integer(value.underlineStyle, 0, 5, `${name}.underlineStyle`),
      foreground: validateColor(value.foreground, `${name}.foreground`),
      background: validateColor(value.background, `${name}.background`),
      underlineColor: validateColor(value.underlineColor, `${name}.underlineColor`)
    };
  }

  function validateCursor(value, width, height) {
    object(value, 'cursor');
    keys(value, ['row', 'column', 'visible', 'style', 'styleSet'], 'cursor');
    const styleSet = boolean(value.styleSet, 'cursor.styleSet');
    const style = integer(value.style, 0, 6, 'cursor.style');
    if (!styleSet && style !== 0) fail('cursor style must be zero when unset');
    return {
      row: integer(value.row, 0, height - 1, 'cursor.row'),
      column: integer(value.column, 0, width - 1, 'cursor.column'),
      visible: boolean(value.visible, 'cursor.visible'),
      style,
      styleSet
    };
  }

  function validateRow(value, width, height, styleCount, configured, textBudget, seen) {
    object(value, 'row');
    keys(value, ['row', 'cells'], 'row');
    const row = integer(value.row, 0, height - 1, 'row.row');
    if (seen.has(row)) fail(`duplicate row ${row}`);
    seen.add(row);
    if (!Array.isArray(value.cells)) fail(`row ${row} cells must be an array`);
    let column = 0;
    const cells = value.cells.map((candidate, index) => {
      const name = `row ${row} cell ${index}`;
      object(candidate, name);
      keys(candidate, ['column', 'width', 'text', 'style'], name);
      const current = integer(candidate.column, 0, width - 1, `${name}.column`);
      const cellWidth = integer(candidate.width, 1, 2, `${name}.width`);
      if (current !== column) fail(`row coverage is not contiguous at row ${row} column ${column}`);
      if (current + cellWidth > width) fail(`row coverage exceeds width at row ${row}`);
      const text = boundedString(candidate.text, configured.maxTextBytes, `${name}.text`);
      const cell = {
        column: current,
        width: cellWidth,
        text: text.value,
        style: integer(candidate.style, 0, styleCount - 1, `${name}.style`)
      };
      textBudget.value += text.byteLength;
      if (textBudget.value > configured.maxUpdateBytes) fail('update text exceeds its aggregate byte limit');
      column += cellWidth;
      return cell;
    });
    if (column !== width) fail(`row coverage is ${column}, expected ${width}`);
    return { row, cells };
  }

  /**
   * Validate an entire update before any DOM mutation.
   * @param {unknown} value
   * @param {Readonly<Record<string, number>>} configured
   * @returns {Update}
   */
  function validateUpdate(value, configured) {
    object(value, 'update');
    keys(value, ['schemaVersion', 'width', 'height', 'snapshot', 'rows', 'styles', 'cursor'], 'update');
    if (value.schemaVersion !== SCHEMA_VERSION) fail(`unsupported update schema ${value.schemaVersion}`);
    const width = integer(value.width, 1, configured.maxCells, 'update.width');
    const height = integer(value.height, 1, configured.maxRowsPerUpdate, 'update.height');
    if (width > Math.floor(configured.maxCells / height)) fail('update exceeds the cell limit');
    const snapshot = boolean(value.snapshot, 'update.snapshot');
    if (!Array.isArray(value.styles) || value.styles.length > configured.maxStyles) fail('update styles exceed their limit');
    const styles = value.styles.map(validateStyle);
    if (!Array.isArray(value.rows) || value.rows.length > configured.maxRowsPerUpdate) fail('update rows exceed their limit');
    if (value.rows.length > Math.floor(configured.maxCells / width)) fail('update represented cells exceed their limit');
    if (value.rows.length > 0 && styles.length === 0) fail('an update with rows requires styles');
    if (snapshot && value.rows.length !== height) fail('snapshot must contain every row');
    const seen = new Set();
    const textBudget = { value: 0 };
    const rows = value.rows.map(row => validateRow(row, width, height, styles.length, configured, textBudget, seen));
    if (snapshot) {
      rows.sort((left, right) => left.row - right.row);
      rows.forEach((row, index) => { if (row.row !== index) fail('snapshot rows are incomplete'); });
    }
    return { schemaVersion: SCHEMA_VERSION, width, height, snapshot, rows, styles, cursor: validateCursor(value.cursor, width, height) };
  }

  function xtermColor(index) {
    if (index < 16) return `var(--vev-color-${index})`;
    if (index < 232) {
      const value = index - 16;
      const levels = [0, 95, 135, 175, 215, 255];
      const r = levels[Math.floor(value / 36)];
      const g = levels[Math.floor(value / 6) % 6];
      const b = levels[value % 6];
      return `rgb(${r} ${g} ${b})`;
    }
    const gray = 8 + (index - 232) * 10;
    return `rgb(${gray} ${gray} ${gray})`;
  }

  function colorValue(color, fallback) {
    if (color.kind === 0) return fallback;
    if (color.kind === 1) return xtermColor(color.index);
    return `rgb(${color.rgb.r} ${color.rgb.g} ${color.rgb.b})`;
  }

  function addStyleClasses(node, style) {
    if (style.bold) node.classList.add('vev-terminal__cell--bold');
    if (style.italic) node.classList.add('vev-terminal__cell--italic');
    if (style.dim) node.classList.add('vev-terminal__cell--dim');
    if (style.blink) node.classList.add('vev-terminal__cell--blink');
    if (style.strikethrough) node.classList.add('vev-terminal__cell--strike');
    if (style.underline) {
      node.classList.add('vev-terminal__cell--underline');
      const suffix = ['single', 'single', 'double', 'wavy', 'dotted', 'dashed'][style.underlineStyle];
      if (suffix !== 'single') node.classList.add(`vev-terminal__cell--underline-${suffix}`);
    }
  }

  function buildRow(document, row, styles) {
    const node = document.createElement('div');
    node.className = 'vev-terminal__row';
    node.dataset.row = String(row.row);
    let text = '';
    for (const cell of row.cells) {
      const style = styles[cell.style];
      const child = document.createElement('span');
      child.className = 'vev-terminal__cell';
      child.dataset.column = String(cell.column);
      child.dataset.width = String(cell.width);
      child.style.gridColumn = `${cell.column + 1} / span ${cell.width}`;
      let foreground = colorValue(style.foreground, 'var(--vev-fg)');
      let background = colorValue(style.background, 'var(--vev-bg)');
      if (style.inverse) [foreground, background] = [background, foreground];
      child.style.setProperty('--vev-cell-fg', foreground);
      child.style.setProperty('--vev-cell-bg', background);
      child.style.setProperty('--vev-cell-underline', colorValue(style.underlineColor, foreground));
      addStyleClasses(child, style);
      child.textContent = cell.text;
      node.append(child);
      text += cell.text;
    }
    return { node, text };
  }

  function validateTheme(theme) {
    object(theme, 'theme');
    keys(theme, ['foreground', 'background', 'cursor', 'selection', 'selectionText', 'palette'], 'theme');
    const result = {
      foreground: validateRGB(theme.foreground, 'theme.foreground'),
      background: validateRGB(theme.background, 'theme.background'),
      cursor: validateRGB(theme.cursor, 'theme.cursor'),
      selection: validateRGB(theme.selection, 'theme.selection'),
      selectionText: validateRGB(theme.selectionText, 'theme.selectionText')
    };
    if (!Array.isArray(theme.palette) || theme.palette.length !== 16) fail('theme.palette must contain 16 colors');
    result.palette = theme.palette.map((color, index) => validateRGB(color, `theme.palette[${index}]`));
    return result;
  }

  function rgbValue(color) { return `rgb(${color.r} ${color.g} ${color.b})`; }

  /**
   * Mount one interactive terminal adapter.
   * @param {Element} root
   * @param {MountOptions} options
   * @returns {Terminal}
   */
  function mount(root, options = {}) {
    if (!(root instanceof Element)) fail('mount root must be an Element');
    if (root.classList.contains('vev-terminal')) fail('mount root already owns a terminal');
    object(options, 'options');
    keys(options, ['label', 'decide', 'send', 'limits'], 'options');
    const label = boundedString(options.label, 1024, 'options.label').value.trim();
    if (label === '') fail('options.label must not be empty');
    if (options.decide !== undefined && typeof options.decide !== 'function') fail('options.decide must be a function');
    if (options.send !== undefined && typeof options.send !== 'function') fail('options.send must be a function');
    const configured = limits(options.limits);
    let decide = options.decide || (() => ({ emit: true, preventDefault: false }));
    let send = options.send || (() => {});
    const document = root.ownerDocument;
    const abort = new AbortController();
    const signal = abort.signal;

    const previousRole = root.hasAttribute('role') ? root.getAttribute('role') : null;
    const previousLabel = root.hasAttribute('aria-label') ? root.getAttribute('aria-label') : null;
    root.classList.add('vev-terminal');
    root.setAttribute('role', 'group');
    root.setAttribute('aria-label', label);

    const input = document.createElement('textarea');
    input.className = 'vev-terminal__input';
    input.setAttribute('aria-label', `${label} input`);
    input.setAttribute('autocomplete', 'off');
    input.setAttribute('autocapitalize', 'off');
    input.setAttribute('spellcheck', 'false');

    const viewport = document.createElement('div');
    viewport.className = 'vev-terminal__viewport';
    viewport.setAttribute('aria-hidden', 'true');

    const cursor = document.createElement('div');
    cursor.className = 'vev-terminal__cursor';
    cursor.hidden = true;
    viewport.append(cursor);

    const accessible = document.createElement('pre');
    accessible.className = 'vev-terminal__accessible-output';
    accessible.setAttribute('aria-live', 'off');
    accessible.setAttribute('aria-label', `${label} output`);

    root.append(input, viewport, accessible);

    let destroyed = false;
    let composing = false;
    let mouseCapture = false;
    let width = 0;
    let height = 0;
    let rowNodes = [];
    let rowText = [];
    let resizeFrame = 0;
    let pointerFrame = 0;
    let pendingPointer = null;
    let lastResize = '';
    const previousProperties = new Map();

    function setOwnedProperty(name, value) {
      if (!previousProperties.has(name)) {
        previousProperties.set(name, {
          value: root.style.getPropertyValue(name),
          priority: root.style.getPropertyPriority(name)
        });
      }
      root.style.setProperty(name, value);
    }

    function alive() {
      if (destroyed) fail('instance is destroyed');
    }

    /** @returns {Readonly<BrowserEvent>|null} */
    function capture(event, nativeEvent) {
      const decision = decide(Object.freeze({ ...event }));
      object(decision, 'capture decision');
      keys(decision, ['emit', 'preventDefault'], 'capture decision');
      const emit = boolean(decision.emit, 'capture decision.emit');
      const preventDefault = boolean(decision.preventDefault, 'capture decision.preventDefault');
      if (preventDefault && nativeEvent && nativeEvent.cancelable) nativeEvent.preventDefault();
      return emit ? Object.freeze({ schemaVersion: SCHEMA_VERSION, ...event }) : null;
    }

    function dispatch(event, nativeEvent) {
      const captured = capture(event, nativeEvent);
      if (captured) send(captured);
    }

    function updateCursor(state) {
      cursor.hidden = !state.visible;
      cursor.className = 'vev-terminal__cursor';
      if (!state.visible) return;
      const style = state.styleSet ? state.style : 0;
      const bar = style === 5 || style === 6;
      const underline = style === 3 || style === 4;
      if (bar) cursor.classList.add('vev-terminal__cursor--bar');
      if (underline) cursor.classList.add('vev-terminal__cursor--underline');
      if (style === 0 || style === 1 || style === 3 || style === 5) cursor.classList.add('vev-terminal__cell--blink');
      cursor.style.left = `calc(${state.column} * var(--vev-cell-width))`;
      cursor.style.top = `calc(${state.row} * var(--vev-cell-height))`;
      cursor.style.width = bar ? '' : 'var(--vev-cell-width)';
      cursor.style.height = underline ? '' : 'var(--vev-cell-height)';
      if (underline) cursor.style.marginTop = 'calc(var(--vev-cell-height) * 0.88)';
      else cursor.style.marginTop = '';
    }

    /** @param {Update} value */
    function apply(value) {
      alive();
      const update = validateUpdate(value, configured);
      if (!update.snapshot && (update.width !== width || update.height !== height || rowNodes.length === 0)) fail('incremental update dimensions do not match the mounted snapshot');
      if (!update.snapshot) {
        for (let row = 0; row < height; row += 1) {
          if (!rowNodes[row] || rowNodes[row].parentNode !== viewport) fail(`mounted row ${row} is unavailable`);
        }
      }
      const built = update.rows.map(row => ({ row: row.row, ...buildRow(document, row, update.styles) }));

      if (update.snapshot) {
        const fragment = document.createDocumentFragment();
        const nextNodes = new Array(update.height);
        const nextText = new Array(update.height);
        for (const item of built) {
          nextNodes[item.row] = item.node;
          nextText[item.row] = item.text;
          fragment.append(item.node);
        }
        viewport.replaceChildren(fragment, cursor);
        rowNodes = nextNodes;
        rowText = nextText;
      } else {
        for (const item of built) {
          rowNodes[item.row].replaceWith(item.node);
          rowNodes[item.row] = item.node;
          rowText[item.row] = item.text;
        }
      }
      width = update.width;
      height = update.height;
      setOwnedProperty('--vev-cols', String(width));
      setOwnedProperty('--vev-rows', String(height));
      accessible.textContent = rowText.join('\n');
      updateCursor(update.cursor);
      scheduleResize();
    }

    function position(event) {
      if (width === 0 || height === 0) return { row: 0, column: 0, x: 0, y: 0 };
      const bounds = viewport.getBoundingClientRect();
      if (bounds.width <= 0 || bounds.height <= 0) return { row: 0, column: 0, x: 0, y: 0 };
      const clientX = Number.isFinite(event.clientX) ? event.clientX : bounds.left;
      const clientY = Number.isFinite(event.clientY) ? event.clientY : bounds.top;
      const x = Math.max(0, Math.min(bounds.width, clientX - bounds.left));
      const y = Math.max(0, Math.min(bounds.height, clientY - bounds.top));
      const column = Math.min(width - 1, Math.floor(x / (bounds.width / width)));
      const row = Math.min(height - 1, Math.floor(y / (bounds.height / height)));
      return { row, column, x, y };
    }

    function modifierFields(event) {
      return { alt: event.altKey, ctrl: event.ctrlKey, meta: event.metaKey, shift: event.shiftKey };
    }

    function pointerPayload(event, action) {
      const invalidButton = !Number.isInteger(event.button) || event.button < -1 || event.button > 4;
      const invalidButtons = !Number.isInteger(event.buttons) || event.buttons < 0 || event.buttons > 31;
      if (invalidButton || invalidButtons) return null;
      return { type: 'pointer', action, button: event.button, buttons: event.buttons, ...position(event), ...modifierFields(event) };
    }

    function flushPointer() {
      pointerFrame = 0;
      const captured = pendingPointer;
      pendingPointer = null;
      if (captured) send(captured);
    }

    function scheduleResize() {
      if (resizeFrame || width === 0 || height === 0) return;
      resizeFrame = requestAnimationFrame(() => {
        resizeFrame = 0;
        const bounds = viewport.getBoundingClientRect();
        const cellWidth = bounds.width / width;
        const cellHeight = bounds.height / height;
        if (bounds.width <= 0 || bounds.height <= 0 || cellWidth <= 0 || cellHeight <= 0) return;
        const payload = {
          type: 'resize', columns: width, rows: height,
          pixelWidth: bounds.width, pixelHeight: bounds.height,
          cellWidth, cellHeight, devicePixelRatio: window.devicePixelRatio
        };
        const identity = JSON.stringify(payload);
        if (identity !== lastResize) {
          lastResize = identity;
          dispatch(payload);
        }
      });
    }

    input.addEventListener('beforeinput', event => {
      if (composing || event.inputType === 'insertCompositionText' || event.inputType === 'insertFromPaste' || !event.data) return;
      if (encoder.encode(event.data).byteLength > configured.maxTextBytes) {
        event.preventDefault();
        return;
      }
      dispatch({ type: 'text', text: event.data }, event);
    }, { signal });
    input.addEventListener('input', event => {
      if (!composing && !event.isComposing) input.value = '';
    }, { signal });
    input.addEventListener('compositionstart', () => { composing = true; }, { signal });
    input.addEventListener('compositionend', event => {
      composing = false;
      input.value = '';
      if (event.data && encoder.encode(event.data).byteLength <= configured.maxTextBytes) {
        dispatch({ type: 'text', text: event.data }, event);
      }
    }, { signal });
    input.addEventListener('keydown', event => {
      if (composing || event.isComposing) return;
      const key = event.key || 'Unidentified';
      const code = event.code || 'Unidentified';
      const nonText = key.length !== 1 || event.ctrlKey || event.altKey || event.metaKey;
      if (!nonText) return;
      if (key.length > 128 || code.length > 128) {
        event.preventDefault();
        return;
      }
      dispatch({ type: 'key', key, code, repeat: event.repeat, location: event.location, ...modifierFields(event) }, event);
    }, { signal });
    input.addEventListener('paste', event => {
      const text = event.clipboardData?.getData('text/plain') || '';
      if (encoder.encode(text).byteLength > configured.maxPasteBytes) {
        event.preventDefault();
        return;
      }
      dispatch({ type: 'paste', text }, event);
    }, { signal });
    input.addEventListener('focus', event => dispatch({ type: 'focus', focused: true }, event), { signal });
    input.addEventListener('blur', event => dispatch({ type: 'focus', focused: false }, event), { signal });

    viewport.addEventListener('pointerdown', event => {
      const payload = pointerPayload(event, 'down');
      if (!payload) return;
      if (mouseCapture && viewport.setPointerCapture) viewport.setPointerCapture(event.pointerId);
      dispatch(payload, event);
    }, { signal });
    viewport.addEventListener('pointerup', event => {
      const payload = pointerPayload(event, 'up');
      if (payload) dispatch(payload, event);
    }, { signal });
    viewport.addEventListener('pointercancel', event => {
      const payload = pointerPayload(event, 'cancel');
      if (payload) dispatch(payload, event);
    }, { signal });
    viewport.addEventListener('pointermove', event => {
      const payload = pointerPayload(event, 'move');
      if (!payload) return;
      const captured = capture(payload, event);
      if (!captured || destroyed) return;
      pendingPointer = captured;
      if (!pointerFrame) pointerFrame = requestAnimationFrame(flushPointer);
    }, { signal });
    viewport.addEventListener('wheel', event => {
      const invalidDelta = !Number.isFinite(event.deltaX) || !Number.isFinite(event.deltaY);
      const invalidMode = !Number.isInteger(event.deltaMode) || event.deltaMode < 0 || event.deltaMode > 2;
      if (invalidDelta || invalidMode) return;
      const { row, column } = position(event);
      dispatch({ type: 'wheel', deltaX: event.deltaX, deltaY: event.deltaY, deltaMode: event.deltaMode, row, column, ...modifierFields(event) }, event);
    }, { signal, passive: false });

    const observer = new ResizeObserver(scheduleResize);
    observer.observe(root);

    return Object.freeze({
      apply,
      focus() { alive(); input.focus(); },
      setMouseCapture(enabled) {
        alive();
        mouseCapture = Boolean(enabled);
        root.classList.toggle('vev-terminal--mouse-capture', mouseCapture);
      },
      setTheme(theme) {
        alive();
        const value = validateTheme(theme);
        setOwnedProperty('--vev-fg', rgbValue(value.foreground));
        setOwnedProperty('--vev-bg', rgbValue(value.background));
        setOwnedProperty('--vev-cursor', rgbValue(value.cursor));
        setOwnedProperty('--vev-selection-bg', rgbValue(value.selection));
        setOwnedProperty('--vev-selection-fg', rgbValue(value.selectionText));
        value.palette.forEach((color, index) => setOwnedProperty(`--vev-color-${index}`, rgbValue(color)));
      },
      destroy() {
        if (destroyed) return;
        destroyed = true;
        abort.abort();
        observer.disconnect();
        if (resizeFrame) cancelAnimationFrame(resizeFrame);
        if (pointerFrame) cancelAnimationFrame(pointerFrame);
        input.value = '';
        accessible.textContent = '';
        viewport.replaceChildren();
        input.remove();
        viewport.remove();
        accessible.remove();
        rowNodes = [];
        rowText = [];
        pendingPointer = null;
        width = 0;
        height = 0;
        lastResize = '';
        decide = () => ({ emit: false, preventDefault: false });
        send = () => {};
        for (const [name, previous] of previousProperties) {
          if (previous.value === '') root.style.removeProperty(name);
          else root.style.setProperty(name, previous.value, previous.priority);
        }
        previousProperties.clear();
        root.classList.remove('vev-terminal', 'vev-terminal--mouse-capture');
        if (previousRole === null) root.removeAttribute('role');
        else root.setAttribute('role', previousRole);
        if (previousLabel === null) root.removeAttribute('aria-label');
        else root.setAttribute('aria-label', previousLabel);
      }
    });
  }

  globalThis.VevTerminal = Object.freeze({ schemaVersion: SCHEMA_VERSION, mount });
})();
