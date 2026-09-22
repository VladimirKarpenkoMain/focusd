/**
 * Оформление: выбор темы и его применение к документу.
 *
 * Источник истины — настройки в Go: тема переживает перезапуск вместе с ними.
 * localStorage здесь не второй источник истины, а только эхо: его читает
 * встроенный скрипт в index.html, чтобы окно успело покраситься до первого
 * кадра и тёмная тема не начиналась с белой вспышки.
 */

import { SetTheme } from './api';
import { t } from './i18n';

export type ThemeChoice = 'system' | 'light' | 'dark';
export type ResolvedTheme = 'light' | 'dark';

const STORAGE_KEY = 'focusd.theme';

/** Записывает выбор темы в localStorage — для мгновенной покраски при запуске. */
export function cacheTheme(choice: ThemeChoice): void {
  try {
    localStorage.setItem(STORAGE_KEY, choice);
  } catch {
    // Приватный режим или запрет хранилища — не повод ломать запуск.
  }
}

export function resolveTheme(choice: ThemeChoice): ResolvedTheme {
  if (choice === 'light' || choice === 'dark') return choice;
  return window.matchMedia?.('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

/** Применяет тему к документу. Возвращает то, что реально получилось. */
export function applyTheme(choice: ThemeChoice): ResolvedTheme {
  const resolved = resolveTheme(choice);
  const root = document.documentElement;
  root.dataset.theme = resolved;
  root.dataset.themeChoice = choice;
  cacheTheme(choice);
  return resolved;
}

/** Сохраняет выбор в настройках приложения. Тема применяется сразу, до ответа. */
export async function saveTheme(choice: ThemeChoice): Promise<void> {
  applyTheme(choice);
  await SetTheme(choice);
}

/** Подписка на смену системной темы. Нужна, пока выбран режим «как в системе». */
export function watchSystemTheme(onChange: (t: ResolvedTheme) => void): void {
  const mq = window.matchMedia?.('(prefers-color-scheme: dark)');
  if (!mq) return;
  mq.addEventListener('change', () => {
    if (document.documentElement.dataset.themeChoice === 'system') {
      onChange(applyTheme('system'));
    }
  });
}

// Подписи берутся по ключу, а не читаются один раз при загрузке модуля: язык
// можно сменить, не перезапуская окно.
export function themeLabel(choice: ThemeChoice): string {
  return t(`theme.${choice}`);
}
