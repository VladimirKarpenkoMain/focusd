/**
 * Общие кирпичики интерфейса: карточки, кнопки, тумблеры, модалка, редактор
 * списка доменов.
 *
 * Вынесены отдельно, потому что раньше каждый экран собирал их заново — и
 * расходился с остальными то отступом, то радиусом, то поведением по Escape.
 * Пользовательские строки попадают в DOM только через textContent либо через
 * белый список иконок: innerHTML собирается исключительно из наших же строк.
 */

import { t } from './i18n';
import { icon } from './icons';
import type { IconName } from './icons';

export function h<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  cls?: string,
  text?: string,
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  if (cls) node.className = cls;
  if (text !== undefined) node.textContent = text;
  return node;
}

/**
 * Элемент с подписью по ключу словаря.
 *
 * Ключ запоминается в data-i18n, чтобы смена языка перечитала подпись без
 * пересборки узла: кнопки и строки каркаса создаются один раз за запуск, а язык
 * меняется на ходу. Экраны этим не пользуются — они собираются заново целиком.
 */
export function tr<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  cls: string | undefined,
  key: string,
): HTMLElementTagNameMap[K] {
  const el = h(tag, cls);
  setTextKey(el, key);
  return el;
}

/** Проставляет подпись по ключу и запоминает ключ. */
export function setTextKey(el: HTMLElement, key: string): void {
  el.dataset.i18n = key;
  el.textContent = t(key);
}

/** Перечитывает все подписи, проставленные через tr/setTextKey. */
export function relocalize(root: ParentNode): void {
  for (const el of root.querySelectorAll<HTMLElement>('[data-i18n]')) {
    el.textContent = t(el.dataset.i18n ?? '');
  }
}

/** Кнопка с иконкой и необязательной подписью. Подпись нужна скринридеру. */
export function iconButton(
  name: IconName,
  label: string,
  onClick: () => void,
  cls = 'icon-btn',
): HTMLButtonElement {
  const b = h('button', cls);
  b.type = 'button';
  b.setAttribute('aria-label', label);
  b.title = label;
  b.append(icon(name, { size: 16 }));
  b.addEventListener('click', onClick);
  return b;
}

export function button(
  label: string,
  onClick: () => void,
  opts: { variant?: 'primary' | 'danger' | 'ghost'; icon?: IconName; size?: 'lg' } = {},
): HTMLButtonElement {
  const variant = opts.variant ?? 'default';
  const cls = ['btn', variant !== 'default' ? `btn-${variant}` : '', opts.size === 'lg' ? 'btn-lg' : '']
    .filter(Boolean)
    .join(' ');
  const b = h('button', cls);
  b.type = 'button';
  if (opts.icon) b.append(icon(opts.icon, { size: 16 }));
  b.append(h('span', undefined, label));
  b.addEventListener('click', onClick);
  return b;
}

export interface Card {
  el: HTMLElement;
  body: HTMLElement;
  setHint(text: string): void;
}

/** Карточка раздела с заголовком, пояснением и телом. */
export function card(title: string, hint = '', opts: { icon?: IconName; wide?: boolean } = {}): Card {
  const el = h('section', opts.wide ? 'card is-wide' : 'card');
  const head = h('div', 'card-head');
  const titleWrap = h('div', 'card-title-wrap');
  if (opts.icon) titleWrap.append(icon(opts.icon, { size: 18, className: 'card-icon' }));
  titleWrap.append(h('h2', 'card-title', title));
  head.append(titleWrap);
  const hintEl = h('span', 'card-hint', hint);
  head.append(hintEl);
  el.append(head);

  const body = h('div', 'card-body');
  el.append(body);

  return {
    el,
    body,
    setHint(text: string) {
      hintEl.textContent = text;
    },
  };
}

/**
 * Заголовок раздела настроек.
 *
 * Разделы появились после того, как карточки смешались в одну сетку: подписи
 * карточек наезжали друг на друга, а по какому признаку они соседствуют —
 * понять было нельзя. Заголовок отвечает на «о чём эта группа целиком».
 */
export function section(title: string, hint = ''): HTMLElement {
  const el = h('div', 'section-head');
  el.append(h('h3', 'section-title', title));
  if (hint) el.append(h('p', 'section-hint', hint));
  return el;
}

export interface ToggleRow {
  el: HTMLElement;
  input: HTMLInputElement;
}

/** Строка с тумблером: название, пояснение и переключатель. */
export function toggleRow(
  checked: boolean,
  name: string,
  hint: string,
  ariaLabel: string,
  onChange: (v: boolean) => void,
): ToggleRow {
  const label = h('label', 'toggle');
  const input = h('input');
  input.type = 'checkbox';
  input.checked = checked;
  input.setAttribute('aria-label', ariaLabel);
  input.addEventListener('change', () => onChange(input.checked));

  const sw = h('span', 'switch');
  const text = h('span', 'toggle-text');
  text.append(h('span', 'toggle-name', name), h('span', 'toggle-hint', hint));
  label.append(input, text, sw);
  return { el: label, input };
}

/* ------------------------------------------------------------------ */
/* Модальное окно                                                      */
/* ------------------------------------------------------------------ */

export interface ModalOptions {
  title: string;
  text: string;
  confirmLabel: string;
  cancelLabel?: string;
  danger?: boolean;
  onConfirm: () => void;
}

/** Своя модалка вместо confirm(): закрывается по Escape, фону и «Отмена». */
export function openModal(o: ModalOptions): void {
  const backdrop = h('div', 'modal-backdrop');
  const box = h('div', 'modal');
  box.setAttribute('role', 'dialog');
  box.setAttribute('aria-modal', 'true');
  box.setAttribute('aria-label', o.title);
  box.append(h('h2', 'modal-title', o.title), h('p', 'modal-text', o.text));

  const actions = h('div', 'modal-actions');
  const confirm = button(o.confirmLabel, () => {
    close();
    o.onConfirm();
  }, { variant: o.danger ? 'danger' : 'primary' });
  const cancel = button(o.cancelLabel ?? t('ui.cancel'), () => close(), { variant: 'ghost' });
  actions.append(cancel, confirm);
  box.append(actions);
  backdrop.append(box);

  const previous = document.activeElement as HTMLElement | null;

  function close(): void {
    document.removeEventListener('keydown', onKey, true);
    backdrop.remove();
    previous?.focus?.();
  }

  function onKey(e: KeyboardEvent): void {
    if (e.key === 'Escape') {
      e.stopPropagation();
      close();
    }
  }

  backdrop.addEventListener('mousedown', (e) => {
    if (e.target === backdrop) close();
  });
  document.addEventListener('keydown', onKey, true);

  document.body.append(backdrop);
  confirm.focus();
}

/* ------------------------------------------------------------------ */
/* Редактор домена                                                     */
/* ------------------------------------------------------------------ */

/** Нормализация домена: без схемы, пути, порта и завершающей точки. */
export function normalizeDomain(raw: string): string {
  return raw
    .trim()
    .toLowerCase()
    .replace(/^[a-z]+:\/\//, '')
    .replace(/[/?#].*$/, '')
    .replace(/:\d+$/, '')
    .replace(/^\.+|\.+$/g, '');
}

/** Проверка того, что строка похожа на домен, а не на что угодно. */
export function looksLikeDomain(v: string): boolean {
  if (v.length < 4 || v.length > 253) return false;
  if (!v.includes('.')) return false;
  return /^[a-z0-9][a-z0-9._-]*\.[a-z]{2,}$/.test(v);
}

export interface ChipsEditorOptions {
  initial: string[];
  placeholder: string;
  ariaLabel: string;
  addLabel: string;
  /** Пустой текст ошибки означает, что всё в порядке. */
  validate?: (value: string) => string;
  onAdd: (value: string) => void;
  onRemove: (value: string) => void;
}

export interface ChipsEditor {
  el: HTMLElement;
  get(): string[];
  /** Сообщает, есть ли незакоммиченный ввод: тогда сохранение подождёт. */
  isDirty(): boolean;
}

/**
 * Список значений в виде чипов с полем ввода.
 *
 * Ввод проверяется на месте: молча проглотить опечатку — худшее, что может
 * сделать редактор блокировок, потому что человек будет уверен, что сайт
 * закрыт.
 */
export function chipsEditor(o: ChipsEditorOptions): ChipsEditor {
  const values = o.initial.map(normalizeDomain).filter((v) => v.length > 0);

  const root = h('div', 'chips-editor');
  const chips = h('div', 'chips');
  const row = h('div', 'field-row');
  const input = h('input');
  input.type = 'text';
  input.placeholder = o.placeholder;
  input.setAttribute('aria-label', o.ariaLabel);
  input.autocomplete = 'off';
  input.spellcheck = false;

  const addBtn = button(o.addLabel, () => commit(), { icon: 'plus' });
  addBtn.classList.add('btn-compact');

  const error = h('p', 'field-error');
  error.hidden = true;

  row.append(input, addBtn);
  root.append(chips, row, error);

  function showError(text: string): void {
    error.textContent = text;
    error.hidden = text === '';
    input.classList.toggle('is-invalid', text !== '');
  }

  function render(): void {
    chips.replaceChildren();
    if (values.length === 0) {
      chips.append(h('p', 'chips-empty', t('ui.empty')));
      return;
    }
    for (const v of values) {
      const tag = h('span', 'tag');
      tag.append(icon('globe', { size: 13, className: 'tag-icon' }), h('span', 'tag-text', v));
      const x = h('button', 'tag-remove');
      x.type = 'button';
      x.setAttribute('aria-label', t('ui.remove', { v }));
      x.append(icon('close', { size: 12, stroke: 2.2 }));
      x.addEventListener('click', () => {
        const i = values.indexOf(v);
        if (i >= 0) values.splice(i, 1);
        render();
        o.onRemove(v);
      });
      tag.append(x);
      chips.append(tag);
    }
  }

  function commit(): void {
    const v = normalizeDomain(input.value);
    if (!v) {
      showError('');
      input.value = '';
      return;
    }
    if (values.includes(v)) {
      showError(t('ui.duplicate', { v }));
      return;
    }
    const problem = o.validate ? o.validate(v) : '';
    if (problem) {
      showError(problem);
      return;
    }
    input.value = '';
    showError('');
    values.push(v);
    render();
    o.onAdd(v);
  }

  addBtn.addEventListener('click', commit);
  input.addEventListener('input', () => showError(''));
  input.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault();
      commit();
    } else if (e.key === 'Backspace' && input.value === '' && values.length > 0) {
      const last = values[values.length - 1];
      values.pop();
      render();
      o.onRemove(last);
    }
  });
  input.addEventListener('blur', () => {
    if (input.value.trim() !== '') commit();
  });

  render();
  return {
    el: root,
    get: () => [...values],
    isDirty: () => input.value.trim() !== '',
  };
}
