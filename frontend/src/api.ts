/**
 * Слой связи с бэкендом Wails v2.
 *
 * Wails инжектит в окно `window.go.main.App` (методы возвращают Promise)
 * и `window.runtime.EventsOn(name, cb)` для событий. Если бэкенда нет
 * (обычный браузер, `npm run dev`) — включается мок-режим, чтобы UI можно
 * было смотреть и кликать без Go.
 */

import { t } from './i18n';

export type Strictness = 'soft' | 'strict' | 'lock';

export interface Group {
  id: string;
  title: string;
  icon: string;
  note: string;
  domains: string[];
}

export interface Session {
  active: boolean;
  strictness: Strictness;
  startedAt: number; // unix seconds
  endsAt: number; // unix seconds
  durationSec: number;
  remaining: number; // seconds
  cancelable: boolean;
  cancelAt: number; // unix seconds, 0 если неактуально
  label: string;
}

export interface State {
  elevated: boolean;
  /** Сайты не открываются: работает хотя бы один слой блокировки браузеров. */
  browserBlock: boolean;
  /** Браузеры ходят через локальный прокси Focusd — этот слой и считает отсечённые запросы. */
  proxyBlock: boolean;
  /** Что сделать прямо сейчас, чтобы блокировка заработала (обычно — перезапустить браузер). */
  hint: string;
  activeGroups: string[];
  customDomains: string[];
  allowlist: string[];
  blockedToday: number;
  totalRules: number;
  /** Последний отсечённый сайт — по нему видно, что счётчик живой. */
  lastBlocked: string;
  session: Session;
  dataDir: string;
  version: string;
  lastError: string;
  /** Состояние слоёв блокировки. Показывается в настройках. */
  notice: string;
  /** Выбранное оформление: «system» | «light» | «dark». */
  theme: string;
  /** То же, но «как в системе» уже сведено к light или dark. */
  resolvedTheme: string;
  /** Выбранный язык: «auto» | «ru» | «en» | «zh». */
  language: string;
  /** То же, но «auto» уже сведён к конкретному языку. */
  resolvedLanguage: string;
  /** Компактный таймер поверх всех окон включён. */
  widget: boolean;
  /** Компактный таймер держится поверх остальных окон. */
  widgetOnTop: boolean;
  /** Форма компактного таймера, которую окно имеет прямо сейчас. */
  widgetShape: WidgetShape;
}

/** Форма компактного таймера: плашка с полосой или круг с кольцом. */
export type WidgetShape = 'bar' | 'ring';

export type ThemeChoice = 'system' | 'light' | 'dark';

/** Выбор языка: конкретный язык или «как в системе». */
export type LanguageChoice = 'auto' | 'ru' | 'en' | 'zh';

export interface Settings {
  autostart: boolean;
  defaultDurationMin: number;
  defaultStrictness: Strictness;
  defaultGroups: string[];
  customDomains: string[];
  allowlist: string[];
  theme: ThemeChoice;
  language: LanguageChoice;
  widget: boolean;
  widgetOnTop: boolean;
  widgetShape: WidgetShape;
  widgetX: number;
  widgetY: number;
}

export interface StartOptions {
  durationMin: number;
  strictness: Strictness;
  groups: string[];
  customDomains: string[];
  allowlist: string[];
}

export interface CancelInfo {
  waitSec: number;
  message: string;
}

interface AppBridge {
  GetState(): Promise<State>;
  GetCatalog(): Promise<Group[]>;
  GetSettings(): Promise<Settings>;
  SaveSettings(s: Settings): Promise<void>;
  StartSession(o: StartOptions): Promise<void>;
  RequestCancel(): Promise<CancelInfo>;
  ConfirmCancel(): Promise<void>;
  EmergencyRestore(): Promise<void>;
  AddCustomDomain(d: string): Promise<void>;
  RemoveCustomDomain(d: string): Promise<void>;
  AddAllowlistDomain(d: string): Promise<void>;
  RemoveAllowlistDomain(d: string): Promise<void>;
  TestDomain(d: string): Promise<boolean>;
  SetTheme(theme: string): Promise<void>;
  SetLanguage(lang: string): Promise<void>;
  SetWidget(on: boolean): Promise<void>;
  SetWidgetOnTop(on: boolean): Promise<void>;
  SetWidgetShape(shape: string): Promise<void>;
  OpenDataDir(): Promise<void>;
}

interface RuntimeBridge {
  EventsOn(name: string, cb: (...data: unknown[]) => void): void;
  WindowMinimise?(): void;
  WindowToggleMaximise?(): void;
  WindowUnminimise?(): void;
  Quit?(): void;
}

declare global {
  interface Window {
    go?: { main?: { App?: AppBridge } };
    runtime?: RuntimeBridge;
  }
}

/** Единственное место с `any` — защищённый доступ к мосту Wails. */
const realApp = (): AppBridge | undefined => (window as any).go?.main?.App;

/** true, когда Go-бэкенда нет и работает демо-режим. */
export const MOCK: boolean = !realApp();

const nowSec = (): number => Math.floor(Date.now() / 1000);

/* ------------------------------------------------------------------ */
/* Мок-бэкенд                                                          */
/* ------------------------------------------------------------------ */

/**
 * Группы демо-режима: домены и значки. Подписи берутся из словаря по
 * идентификатору группы — так же, как их переводит Go, — и читаются при каждом
 * запросе, а не при загрузке модуля: язык можно сменить на ходу.
 */
const MOCK_GROUPS: { id: string; icon: string; domains: string[] }[] = [
  {
    id: 'social',
    icon: 'users',
    domains: ['vk.com', 'ok.ru', 'facebook.com', 'instagram.com', 'x.com', 'twitter.com', 'threads.net'],
  },
  {
    id: 'video',
    icon: 'play',
    domains: ['youtube.com', 'youtu.be', 'rutube.ru', 'twitch.tv', 'vimeo.com', 'spotify.com'],
  },
  {
    id: 'shorts',
    icon: 'zap',
    domains: ['tiktok.com', 'douyin.com', 'likee.video', 'kwai.com'],
  },
  {
    id: 'games',
    icon: 'gamepad',
    domains: ['store.steampowered.com', 'steamcommunity.com', 'epicgames.com', 'gog.com', 'faceit.com'],
  },
  {
    id: 'messengers',
    icon: 'message',
    domains: ['web.telegram.org', 'discord.com', 'slack.com', 'web.whatsapp.com', 'messenger.com'],
  },
  {
    id: 'news',
    icon: 'newspaper',
    domains: ['lenta.ru', 'rbc.ru', 'habr.com', 'reddit.com', 'news.ycombinator.com'],
  },
  {
    id: 'shopping',
    icon: 'cart',
    domains: ['ozon.ru', 'wildberries.ru', 'avito.ru', 'aliexpress.ru', 'amazon.com'],
  },
  {
    id: 'adult',
    icon: 'shield',
    domains: ['pornhub.com', 'xvideos.com', 'redtube.com', 'onlyfans.com'],
  },
  {
    id: 'doomscroll',
    icon: 'infinity',
    domains: ['9gag.com', 'boredpanda.com', 'buzzfeed.com', 'imgur.com', 'pinterest.com'],
  },
];

/** Собирает каталог демо-режима с подписями на текущем языке. */
function mockCatalog(): Group[] {
  return MOCK_GROUPS.map((g) => ({
    id: g.id,
    icon: g.icon,
    domains: [...g.domains],
    title: t(`demo.group.${g.id}.title`),
    note: t(`demo.group.${g.id}.note`),
  }));
}

const MOCK_SETTINGS: Settings = {
  autostart: false,
  defaultDurationMin: 25,
  defaultStrictness: 'soft',
  defaultGroups: ['social', 'video', 'news'],
  customDomains: ['youtube.com'],
  allowlist: ['localhost'],
  theme: 'system',
  language: 'auto',
  widget: false,
  widgetOnTop: true,
  widgetShape: 'bar',
  widgetX: 0,
  widgetY: 0,
};

const listeners: ((s: State) => void)[] = [];
let ticker: number | null = null;

/** Разрешает «auto» по языку браузера — тем же правилом, что Go по языку Windows. */
function mockLang(setting: string): string {
  if (setting === 'ru' || setting === 'en' || setting === 'zh') return setting;
  const sys = (navigator.language || '').toLowerCase().replace(/_/g, '-');
  if (sys.startsWith('ru') || sys === 'rus') return 'ru';
  if (sys.startsWith('zh') || sys === 'chs' || sys === 'cht') return 'zh';
  return 'en';
}

let mSettings: Settings = cloneSettings(MOCK_SETTINGS);

let mState: State = {
  elevated: true,
  browserBlock: false,
  proxyBlock: false,
  hint: '',
  activeGroups: ['social', 'video', 'news'],
  customDomains: ['youtube.com'],
  allowlist: ['localhost'],
  blockedToday: 128,
  totalRules: 19,
  lastBlocked: 'youtube.com',
  session: {
    active: false,
    strictness: 'soft',
    startedAt: 0,
    endsAt: 0,
    durationSec: 0,
    remaining: 0,
    cancelable: true,
    cancelAt: 0,
    label: '',
  },
  dataDir: 'C:\\Users\\you\\AppData\\Roaming\\Focusd',
  version: '0.1.0-demo',
  lastError: '',
  notice: '',
  theme: 'system',
  resolvedTheme: 'light',
  language: 'auto',
  resolvedLanguage: 'ru',
  widget: false,
  widgetOnTop: true,
  widgetShape: 'bar',
};

function cloneSettings(s: Settings): Settings {
  return {
    autostart: s.autostart,
    defaultDurationMin: s.defaultDurationMin,
    defaultStrictness: s.defaultStrictness,
    defaultGroups: [...s.defaultGroups],
    customDomains: [...s.customDomains],
    allowlist: [...s.allowlist],
    theme: s.theme ?? 'system',
    language: s.language ?? 'auto',
    widget: s.widget ?? false,
    widgetOnTop: s.widgetOnTop ?? true,
    widgetShape: s.widgetShape ?? 'bar',
    widgetX: s.widgetX ?? 0,
    widgetY: s.widgetY ?? 0,
  };
}

function snapshot(): State {
  return {
    elevated: mState.elevated,
    browserBlock: mState.session.active,
    proxyBlock: mState.session.active,
    hint: '',
    activeGroups: [...mState.activeGroups],
    customDomains: [...mState.customDomains],
    allowlist: [...mState.allowlist],
    blockedToday: mState.blockedToday,
    totalRules: mState.totalRules,
    lastBlocked: mState.lastBlocked,
    session: { ...mState.session },
    dataDir: mState.dataDir,
    version: mState.version,
    lastError: mState.lastError,
    notice: mState.notice,
    theme: mSettings.theme,
    resolvedTheme: mState.resolvedTheme,
    language: mSettings.language,
    resolvedLanguage: mockLang(mSettings.language),
    widget: mSettings.widget,
    widgetOnTop: mSettings.widgetOnTop,
    widgetShape: mSettings.widgetShape,
  };
}

function emit(): void {
  const s = snapshot();
  for (const l of listeners) l(s);
}

function countRules(groups: string[], extra: string[]): number {
  let n = extra.length;
  for (const id of groups) {
    const g = MOCK_GROUPS.find((x) => x.id === id);
    if (g) n += g.domains.length;
  }
  return n;
}

function endSession(): void {
  mState = {
    ...mState,
    activeGroups: [],
    session: {
      active: false,
      strictness: mState.session.strictness,
      startedAt: mState.session.startedAt,
      endsAt: 0,
      durationSec: mState.session.durationSec,
      remaining: 0,
      cancelable: true,
      cancelAt: 0,
      label: '',
    },
  };
  if (ticker !== null) {
    window.clearInterval(ticker);
    ticker = null;
  }
  emit();
}

function mockTick(): void {
  if (!mState.session.active) return;
  const now = nowSec();
  const remaining = Math.max(0, mState.session.endsAt - now);
  const cancelable = mState.session.cancelAt === 0 || now >= mState.session.cancelAt;

  if (remaining <= 0) {
    endSession();
    return;
  }

  mState = {
    ...mState,
    blockedToday: mState.blockedToday + 3 + Math.floor(Math.random() * 12),
    session: { ...mState.session, remaining, cancelable },
  };
  emit();
}

const mock: AppBridge = {
  async GetState() {
    return snapshot();
  },
  async GetCatalog() {
    return mockCatalog();
  },
  async GetSettings() {
    return cloneSettings(mSettings);
  },
  async SaveSettings(s) {
    mSettings = cloneSettings(s);
  },
  async StartSession(o) {
    const durationSec = Math.max(1, Math.round(o.durationMin * 60));
    const startedAt = nowSec();
    const strict = o.strictness === 'strict';
    const lock = o.strictness === 'lock';
    mState = {
      ...mState,
      activeGroups: [...o.groups],
      customDomains: [...o.customDomains],
      allowlist: [...o.allowlist],
      totalRules: countRules(o.groups, o.customDomains),
      session: {
        active: true,
        strictness: o.strictness,
        startedAt,
        endsAt: startedAt + durationSec,
        durationSec,
        remaining: durationSec,
        cancelable: !strict && !lock,
        cancelAt: strict ? startedAt + 60 : 0,
        label: t('demo.session'),
      },
    };
    if (ticker === null) ticker = window.setInterval(mockTick, 1000);
    emit();
  },
  async RequestCancel() {
    const s = mState.session;
    if (!s.active) return { waitSec: 0, message: '' };
    if (s.strictness === 'lock') {
      return { waitSec: 0, message: t('demo.cancel.lock') };
    }
    const waitSec = Math.max(0, s.cancelAt - nowSec());
    if (waitSec > 0) {
      return { waitSec, message: t('demo.cancel.wait', { n: waitSec }) };
    }
    if (s.strictness === 'strict') {
      return { waitSec: 0, message: t('demo.cancel.confirm') };
    }
    return { waitSec: 0, message: '' };
  },
  async ConfirmCancel() {
    endSession();
  },
  async EmergencyRestore() {
    mState = { ...mState, lastError: t('demo.restored') };
    endSession();
  },
  async AddCustomDomain(d) {
    if (!mState.customDomains.includes(d)) {
      mState = {
        ...mState,
        customDomains: [...mState.customDomains, d],
        totalRules: mState.totalRules + 1,
      };
      emit();
    }
  },
  async RemoveCustomDomain(d) {
    mState = {
      ...mState,
      customDomains: mState.customDomains.filter((x) => x !== d),
      totalRules: Math.max(0, mState.totalRules - 1),
    };
    emit();
  },
  async AddAllowlistDomain(d) {
    if (!mState.allowlist.includes(d)) {
      mState = { ...mState, allowlist: [...mState.allowlist, d] };
      emit();
    }
  },
  async RemoveAllowlistDomain(d) {
    mState = { ...mState, allowlist: mState.allowlist.filter((x) => x !== d) };
    emit();
  },
  async TestDomain(d) {
    const v = d.toLowerCase();
    if (mState.allowlist.some((x) => v === x || v.endsWith('.' + x))) return false;
    if (mState.customDomains.some((x) => v === x || v.endsWith('.' + x))) return true;
    for (const id of mState.activeGroups.length > 0 ? mState.activeGroups : mSettings.defaultGroups) {
      const g = MOCK_GROUPS.find((x) => x.id === id);
      if (g?.domains.some((x) => v === x || v.endsWith('.' + x))) return true;
    }
    return false;
  },
  async SetTheme(theme) {
    mSettings = { ...mSettings, theme: theme === 'light' || theme === 'dark' ? theme : 'system' };
    emit();
  },
  async SetLanguage(lang) {
    const known =
      lang === 'ru' || lang === 'en' || lang === 'zh' ? (lang as LanguageChoice) : 'auto';
    mSettings = { ...mSettings, language: known };
    emit();
  },
  async SetWidget(on) {
    mSettings = { ...mSettings, widget: on };
    mState = { ...mState, widget: on };
    emit();
  },
  async SetWidgetOnTop(on) {
    mSettings = { ...mSettings, widgetOnTop: on };
    mState = { ...mState, widgetOnTop: on };
    emit();
  },
  async SetWidgetShape(shape) {
    const s: WidgetShape = shape === 'ring' ? 'ring' : 'bar';
    mSettings = { ...mSettings, widgetShape: s };
    mState = { ...mState, widgetShape: s };
    emit();
  },
  async OpenDataDir() {
    /* в демо-режиме открывать нечего */
  },
};

const backend = (): AppBridge => realApp() ?? mock;

/* ------------------------------------------------------------------ */
/* Публичные обёртки                                                   */
/* ------------------------------------------------------------------ */

export async function GetState(): Promise<State> {
  return backend().GetState();
}

export async function GetCatalog(): Promise<Group[]> {
  return backend().GetCatalog();
}

export async function GetSettings(): Promise<Settings> {
  return backend().GetSettings();
}

export async function SaveSettings(s: Settings): Promise<void> {
  return backend().SaveSettings(s);
}

export async function StartSession(o: StartOptions): Promise<void> {
  return backend().StartSession(o);
}

export async function RequestCancel(): Promise<CancelInfo> {
  return backend().RequestCancel();
}

export async function ConfirmCancel(): Promise<void> {
  return backend().ConfirmCancel();
}

export async function EmergencyRestore(): Promise<void> {
  return backend().EmergencyRestore();
}

export async function AddCustomDomain(d: string): Promise<void> {
  return backend().AddCustomDomain(d);
}

export async function RemoveCustomDomain(d: string): Promise<void> {
  return backend().RemoveCustomDomain(d);
}

export async function AddAllowlistDomain(d: string): Promise<void> {
  return backend().AddAllowlistDomain(d);
}

export async function RemoveAllowlistDomain(d: string): Promise<void> {
  return backend().RemoveAllowlistDomain(d);
}

/** Сообщает, заблокирован ли домен при текущих правилах. */
export async function TestDomain(d: string): Promise<boolean> {
  return backend().TestDomain(d);
}

/** Сохраняет выбранное оформление. */
export async function SetTheme(theme: string): Promise<void> {
  return backend().SetTheme(theme);
}

/** Сохраняет выбранный язык. Интерфейс перерисуется по новому состоянию. */
export async function SetLanguage(lang: string): Promise<void> {
  return backend().SetLanguage(lang);
}

/** Включает и выключает компактный таймер поверх всех окон. */
export async function SetWidget(on: boolean): Promise<void> {
  return backend().SetWidget(on);
}

/** Включает и выключает «поверх всех окон» для виджета. */
export async function SetWidgetOnTop(on: boolean): Promise<void> {
  return backend().SetWidgetOnTop(on);
}

/** Сохраняет форму компактного таймера: плашка или круг. */
export async function SetWidgetShape(shape: WidgetShape): Promise<void> {
  return backend().SetWidgetShape(shape);
}

export async function OpenDataDir(): Promise<void> {
  return backend().OpenDataDir();
}

/* ------------------------------------------------------------------ */
/* Управление окном                                                    */
/* ------------------------------------------------------------------ */

// Окно без системной рамки, поэтому кнопки заголовка вызывает интерфейс.
// В демо-режиме рантайма нет — вызовы становятся пустыми.

/** Сворачивает окно. */
export function minimiseWindow(): void {
  window.runtime?.WindowMinimise?.();
}

/** Разворачивает или возвращает окно в прежний размер. */
export function toggleMaximiseWindow(): void {
  window.runtime?.WindowToggleMaximise?.();
}

/** Закрывает приложение: снятие блокировки делает обработчик выхода. */
export function quitApp(): void {
  window.runtime?.Quit?.();
}

/** Подписка на событие `state`. Возвращает функцию отписки. */export function onState(cb: (s: State) => void): () => void {
  const rt = window.runtime;
  if (rt && typeof rt.EventsOn === 'function') {
    rt.EventsOn('state', (data: unknown) => {
      cb(data as State);
    });
    return () => {};
  }
  listeners.push(cb);
  if (ticker === null && mState.session.active) {
    ticker = window.setInterval(mockTick, 1000);
  }
  return () => {
    const i = listeners.indexOf(cb);
    if (i >= 0) listeners.splice(i, 1);
  };
}
