import './style.css';
import {
  GetCatalog,
  GetSettings,
  GetState,
  MOCK,
  SetWidget,
  minimiseWindow,
  onState,
  quitApp,
  toggleMaximiseWindow,
} from './api';
import type { Group, Settings, State } from './api';
import { formatClock, formatNumber, requestWord } from './format';
import { setLang, t } from './i18n';
import type { IconName } from './icons';
import { icon } from './icons';
import { applyTheme, resolveTheme, saveTheme, watchSystemTheme } from './theme';
import type { ThemeChoice } from './theme';
import { h, iconButton, relocalize, tr } from './ui';
import type { SettingsScreen } from './views/settings';
import { createRunningView } from './views/running';
import { createSettingsView } from './views/settings';
import { createSetupView } from './views/setup';
import { createWidgetView } from './views/widget';

interface Screen {
  el: HTMLElement;
  update(s: State): void;
}

function mustFind(id: string): HTMLElement {
  const el = document.getElementById(id);
  if (!el) throw new Error(t('main.noContainer', { id }));
  return el;
}

/** Текст ошибки для показа человеку: бросить можно что угодно, не только Error. */
function errText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

const appEl = mustFind('app');
appEl.dataset.shape = 'bar';

const BRAND_MARK =
  '<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"><circle cx="12" cy="12" r="8"/><circle cx="12" cy="12" r="2.6" fill="currentColor" stroke="none"/></svg>';

/* --- Каркас окна --------------------------------------------------------- */

const shell = h('div', 'app');

const titlebar = h('header', 'titlebar');
const brand = h('div', 'titlebar-brand');
const brandMark = h('span', 'brand-mark');
brandMark.innerHTML = BRAND_MARK;
const titlebarTitle = h('span', 'titlebar-title', 'Focusd');
const titlebarSub = h('span', 'titlebar-sub');
brand.append(brandMark, titlebarTitle, titlebarSub);

// Кнопка темы стоит в сайдбаре рядом с «Настройками», а не в заголовке окна:
// в заголовке она оказывалась в одном ряду со «Свернуть» и «Закрыть», и нажать
// её означало рискнуть закрыть приложение вместо смены оформления.
const titlebarActions = h('div', 'titlebar-actions');
const minimiseBtn = iconButton('minimise', t('win.minimise'), minimiseWindow, 'win-btn');
const maximiseBtn = iconButton('maximise', t('win.maximise'), toggleMaximiseWindow, 'win-btn');
const closeBtn = iconButton('close', t('win.close'), quitApp, 'win-btn is-close');
titlebarActions.append(minimiseBtn, maximiseBtn, closeBtn);
titlebar.append(brand, titlebarActions);

const body = h('div', 'body');
const sidebar = h('aside', 'sidebar');
const workspace = h('section', 'workspace');
const layers = h('div', 'layers');
body.append(sidebar, workspace);
shell.append(titlebar, body, layers);
appEl.append(shell);

/* --- Сайдбар: состояние ------------------------------------------------ */

const statusBox = h('div', 'side-status');
statusBox.append(tr('span', 'side-title', 'side.title'));

interface StatusRow {
  root: HTMLElement;
  val: HTMLElement;
  label: HTMLElement;
}

function statusRow(key: string, iconName: IconName, withDot: boolean): StatusRow {
  const root = h('div', 'side-row');
  const keyEl = h('span', 'side-key');
  keyEl.append(icon(iconName, { size: 14 }), tr('span', undefined, key));
  const val = h('span', 'side-val');
  let dot: HTMLElement | undefined;
  if (withDot) {
    dot = h('span', 'dot');
    val.append(dot);
  }
  const label = h('span');
  val.append(label);
  root.append(keyEl, val);
  statusBox.append(root);
  return { root, val, label };
}

const rowSites = statusRow('side.sites', 'shield', true);
const rowProxy = statusRow('side.proxy', 'timer', true);
const rowRights = statusRow('side.rights', 'key', false);

/** Точка состояния лежит внутри .side-val и остаётся на месте при смене текста. */
function setRow(row: StatusRow, text: string, on: boolean | null): void {
  row.label.textContent = text;
  row.val.classList.toggle('is-muted', on === false);
  row.val.querySelector('.dot')?.classList.toggle('is-on', on === true);
}

const statCard = h('div', 'side-stat');
const statValue = h('div', 'side-stat-value num', '0');
const statLabel = h('div', 'side-stat-label');
const statLast = h('div', 'side-stat-last');
statCard.append(statValue, statLabel, statLast);

/* --- Сайдбар: низ ------------------------------------------------------- */

const sideFoot = h('div', 'side-foot');
if (MOCK) {
  sideFoot.append(tr('span', 'tag-demo', 'side.demo'));
}

const widgetBtn = h('button', 'side-btn');
widgetBtn.type = 'button';
widgetBtn.append(icon('pin', { size: 16 }), tr('span', undefined, 'side.widget'));
widgetBtn.addEventListener('click', () => {
  void SetWidget(!(state?.widget ?? false));
});

const settingsBtn = h('button', 'side-btn');
settingsBtn.type = 'button';
settingsBtn.setAttribute('aria-label', t('side.settingsAria'));
// Устойчивый признак для проверки интерфейса: aria-label переводится, а стенд
// не должен зависеть от того, на каком языке собран кадр.
settingsBtn.dataset.action = 'settings';
settingsBtn.append(icon('gear', { size: 16 }), tr('span', undefined, 'side.settings'));
settingsBtn.addEventListener('click', () => {
  void openSettings();
});

// Смена темы — настройка, а не действие окна: место ей рядом с «Настройками».
const themeBtn = h('button', 'side-btn');
themeBtn.type = 'button';
themeBtn.addEventListener('click', () => {
  void toggleTheme();
});

sideFoot.append(widgetBtn, settingsBtn, themeBtn);
sidebar.append(statusBox, statCard, sideFoot);

/* --- Состояние приложения ---------------------------------------------- */

let state: State | null = null;
let catalog: Group[] = [];
let defaults: Settings | null = null;

let screenKind: 'setup' | 'running' | 'widget' | null = null;
let current: Screen | null = null;
let overlay: SettingsScreen | null = null;
// Язык, на котором собран текущий кадр. По нему замечается смена языка: она
// приходит вместе с состоянием, а не отдельным событием.
let renderedLang = '';

function renderWidgetToggle(s: State): void {
  widgetBtn.classList.toggle('is-active', s.widget);
  widgetBtn.setAttribute('aria-pressed', s.widget ? 'true' : 'false');
}

function renderStatus(s: State): void {
  setRow(rowSites, s.browserBlock ? t('side.on') : t('side.off'), s.browserBlock);
  // Счётчик врёт молча: если прокси не поднялся, отсечённые запросы просто
  // некому считать, и ноль здесь означал бы «ничего не заблокировано».
  setRow(rowProxy, s.proxyBlock ? t('side.exact') : t('side.incomplete'), s.proxyBlock);
  setRow(rowRights, s.elevated ? t('side.admin') : t('side.limited'), null);
  rowProxy.root.title = t('side.proxyTooltip');

  statValue.textContent = formatNumber(s.blockedToday);
  statLabel.textContent = t('side.statLabel', { word: requestWord(s.blockedToday) });
  if (s.lastBlocked) {
    statLast.textContent = t('side.last', { site: s.lastBlocked });
    statLast.hidden = false;
  } else if (s.session.active) {
    statLast.textContent = t('side.none');
    statLast.hidden = false;
  } else {
    statLast.hidden = true;
  }

  const running = s.session.active;
  titlebarSub.textContent = running
    ? t('side.busy', { time: formatClock(s.session.remaining) })
    : s.elevated
      ? t('side.ready')
      : t('side.needRights');

  renderWidgetToggle(s);
}

/* --- Тема --------------------------------------------------------------- */

let themeChoice: ThemeChoice = 'system';

function syncThemeButtons(): void {
  const resolved = resolveTheme(themeChoice);
  // Иконка показывает, что произойдёт: в тёмной теме — солнце, в светлой — луна.
  const label = resolved === 'dark' ? t('side.theme.light') : t('side.theme.dark');
  // Пересобираем содержимое кнопки целиком. Раньше здесь стоял replaceChildren
  // с одной иконкой, а подпись искалась через querySelector('span') — и находила
  // ту же иконку (она тоже span): текст затирал svg, и кнопка оставалась без
  // картинки. Иконка и подпись обязаны собираться в одном месте.
  themeBtn.replaceChildren(
    icon(resolved === 'dark' ? 'sun' : 'moon', { size: 16 }),
    h('span', undefined, label),
  );
  themeBtn.title = label;
  themeBtn.setAttribute('aria-label', t('side.theme.aria', { theme: label.toLowerCase() }));
}

async function toggleTheme(): Promise<void> {
  const next: ThemeChoice = resolveTheme(themeChoice) === 'dark' ? 'light' : 'dark';
  themeChoice = next;
  applyTheme(next);
  syncThemeButtons();
  try {
    await saveTheme(next);
  } catch {
    // Сохранить не удалось — тема всё равно уже применена к документу.
  }
}

/* --- Настройки поверх --------------------------------------------------- */

async function openSettings(draft?: Settings): Promise<void> {
  if (overlay || !state) return;
  let loaded: Settings;
  if (draft) {
    loaded = draft;
  } else {
    try {
      loaded = await GetSettings();
    } catch (err) {
      renderFatal(t('main.renderFailed', { err: errText(err) }));
      return;
    }
  }
  if (!state) return;
  const view = createSettingsView(
    state,
    loaded,
    closeSettings,
    (choice) => {
      themeChoice = choice;
      applyTheme(choice);
      syncThemeButtons();
    },
    draft,
  );
  overlay = view;
  layers.append(view.el);
  view.el.querySelector<HTMLElement>(`[aria-label="${t('settings.close')}"]`)?.focus();
}

function closeSettings(): void {
  if (!overlay) return;
  overlay.el.remove();
  overlay = null;
  settingsBtn.focus();
}

document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape' && overlay) {
    e.preventDefault();
    closeSettings();
  }
});

/* --- Рендер ------------------------------------------------------------- */

function mountScreen(kind: 'setup' | 'running' | 'widget', s: State): void {
  screenKind = kind;
  if (kind === 'widget') {
    current = createWidgetView(s);
  } else if (kind === 'running') {
    current = createRunningView(s);
  } else {
    current = createSetupView(s, catalog, defaults);
  }
  workspace.replaceChildren(current.el);
  const scroller = current.el.querySelector<HTMLElement>('.page');
  if (scroller) scroller.scrollTop = 0;
}

function applyState(s: State): void {
  state = s;

  // Язык приезжает состоянием, как и всё остальное. Сменился — кадр собирается
  // заново целиком: экраны строятся из состояния и своего ничего не хранят,
  // поэтому пересборка честнее точечной замены подписей — ни одна строка не
  // останется от прежнего языка. Черновик настроек переносится в новую панель.
  if (renderedLang !== '' && s.resolvedLanguage !== renderedLang) {
    relocalizeApp(s.resolvedLanguage);
    return;
  }
  renderedLang = s.resolvedLanguage;

  // Виджет — это тот же окно, уменьшенное до плашки. Рисовать в нём сайдбар и
  // панель заголовка бессмысленно: они не поместятся.
  const want: 'setup' | 'running' | 'widget' = s.widget
    ? 'widget'
    : s.session.active
      ? 'running'
      : 'setup';

  shell.classList.toggle('is-widget', want === 'widget');
  titlebar.hidden = want === 'widget';
  sidebar.hidden = want === 'widget';
  // Форма виджета — тоже атрибут: без него круглый таймер получил бы
  // прямоугольную подложку и рисовался бы углами поверх чужого окна.
  appEl.dataset.shape = s.widgetShape === 'ring' ? 'ring' : 'bar';  // Настройки живут в обычном окне: в плашке 320×140 им не место.
  if (want === 'widget') closeSettings();

  try {
    if (want !== 'widget') renderStatus(s);
    if (current === null || want !== screenKind) {
      mountScreen(want, s);
    }
    current?.update(s);
    if (want !== 'widget') overlay?.update(s);
  } catch (err) {
    renderFatal(t('main.renderFailed', { err: errText(err) }));
  }
}

/**
 * Переводит каркас и пересобирает кадр на новом языке.
 *
 * Подписи сайдбара и заголовка живут в разметке с самого запуска, поэтому их
 * перечитывает relocalize; подсказки и aria-имена проставляет applyChrome.
 * Экраны и панель настроек собираются заново.
 */
function relocalizeApp(code: string): void {
  if (!state) return;
  setLang(code);
  renderedLang = code;
  relocalize(shell);
  applyChrome();

  const draft = overlay?.draft;
  if (overlay) {
    overlay.el.remove();
    overlay = null;
  }
  screenKind = null;
  current = null;
  applyState(state);
  if (draft) void openSettings(draft);
}

/** Подсказки и имена для скринридера: их в разметке не видно, и relocalize их не найдёт. */
function applyChrome(): void {
  for (const [btn, key] of [
    [minimiseBtn, 'win.minimise'],
    [maximiseBtn, 'win.maximise'],
    [closeBtn, 'win.close'],
  ] as const) {
    btn.setAttribute('aria-label', t(key));
    btn.title = t(key);
  }
  settingsBtn.setAttribute('aria-label', t('side.settingsAria'));
  syncThemeButtons();
}

function renderFatal(message: string): void {
  const page = h('div', 'page');
  const inner = h('div', 'page-inner');
  const box = h('div', 'banner banner-error');
  box.append(icon('alert', { size: 15 }), h('span', undefined, message));
  inner.append(box);
  page.append(inner);
  workspace.replaceChildren(page);
  current = null;
  screenKind = null;
}

/* --- Старт -------------------------------------------------------------- */

async function boot(): Promise<void> {
  const [stateRes, catalogRes, settingsRes] = await Promise.allSettled([
    GetState(),
    GetCatalog(),
    GetSettings(),
  ]);

  if (stateRes.status === 'rejected') {
    renderFatal(t('main.stateFailed', { err: errText(stateRes.reason) }));
    return;
  }

  // Язык ставится до первой отрисовки: иначе первый кадр мигнул бы русским.
  // Каркас окна собран ещё при загрузке модуля — то есть на исходном языке, —
  // поэтому его подписи перечитываются здесь же: смены языка в этом запуске
  // ещё не было, и ветка перерисовки в applyState не сработает.
  setLang(stateRes.value.resolvedLanguage);
  renderedLang = stateRes.value.resolvedLanguage;
  relocalize(shell);

  catalog = catalogRes.status === 'fulfilled' ? catalogRes.value : [];
  defaults = settingsRes.status === 'fulfilled' ? settingsRes.value : null;

  // Источник истины по теме — настройки в Go; localStorage только ускоряет
  // первую покраску, поэтому здесь значение всё равно приводится к настройкам.
  const stored = (defaults?.theme ?? stateRes.value.theme ?? 'system') as ThemeChoice;
  themeChoice = stored === 'light' || stored === 'dark' ? stored : 'system';
  applyTheme(themeChoice);
  applyChrome();
  watchSystemTheme(() => syncThemeButtons());

  applyState(stateRes.value);
  onState(applyState);
}

void boot();
