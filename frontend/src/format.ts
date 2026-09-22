/**
 * Форматирование чисел, времени и слов по числу — на выбранном языке.
 *
 * Здесь нет Intl: приложение офлайновое и живёт в WebView2, где набор локалей
 * зависит от сборки Windows. Правила, которые нужны интерфейсу, сводятся к
 * форме слова по числу и разделителю разрядов, поэтому они записаны явно, а
 * не взяты у рантайма.
 */

import type { Strictness } from './api';
import { groupSeparator, plural, t } from './i18n';

export const domainWord = (n: number): string => plural(n, 'word.domain');
export const siteWord = (n: number): string => plural(n, 'word.site');
export const requestWord = (n: number): string => plural(n, 'word.request');
export const minuteWord = (n: number): string => plural(n, 'word.minute');
export const secondWord = (n: number): string => plural(n, 'word.second');
export const groupWord = (n: number): string => plural(n, 'word.group');

/** 1499 -> "1 499" (у русского — узкий неразрывный пробел, у остальных запятая). */
export function formatNumber(n: number): string {
  const s = String(Math.max(0, Math.round(n)));
  return s.replace(/\B(?=(\d{3})+(?!\d))/g, groupSeparator());
}

/** MM:SS, а если больше часа — H:MM:SS. */
export function formatClock(totalSeconds: number): string {
  const raw = Number.isFinite(totalSeconds) ? Math.floor(totalSeconds) : 0;
  const total = Math.max(0, raw);
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  const pad = (v: number): string => String(v).padStart(2, '0');
  return h > 0 ? `${h}:${pad(m)}:${pad(s)}` : `${pad(m)}:${pad(s)}`;
}

/** "42 с", "1 мин 05 с" — для подсказок и обратных отсчётов. */
export function formatSecondsShort(totalSeconds: number): string {
  const total = Math.max(0, Math.ceil(totalSeconds));
  const min = t('unit.minute');
  const sec = t('unit.second');
  if (total < 60) return `${total} ${sec}`;
  const m = Math.floor(total / 60);
  const s = total % 60;
  return s === 0 ? `${m} ${min}` : `${m} ${min} ${String(s).padStart(2, '0')} ${sec}`;
}

/** "25 минут" */
export function formatMinutes(min: number): string {
  return `${min} ${minuteWord(min)}`;
}

/** Время суток из unix-секунд: "14:05". */
export function formatTimeOfDay(unixSec: number): string {
  if (!Number.isFinite(unixSec) || unixSec <= 0) return '—';
  const d = new Date(unixSec * 1000);
  const hh = String(d.getHours()).padStart(2, '0');
  const mm = String(d.getMinutes()).padStart(2, '0');
  return `${hh}:${mm}`;
}

/** "45 минут", "2 часа", "2 часа 15 минут" — для подписи выбранной длительности. */
export function formatDuration(min: number): string {
  const total = Math.max(0, Math.round(min));
  if (total < 60) return formatMinutes(total);
  const h = Math.floor(total / 60);
  const rest = total % 60;
  const hours = `${h} ${plural(h, 'word.hour')}`;
  return rest === 0 ? hours : `${hours} ${rest} ${minuteWord(rest)}`;
}

export function strictnessLabel(s: Strictness): string {
  return t(`strictness.${s}`);
}

export function strictnessHint(s: Strictness): string {
  return t(`strictness.${s}.hint`);
}

/** «Мягкий режим» / «Soft mode» / «宽松模式» — подпись режима целиком. */
export function strictnessMode(s: Strictness): string {
  return t('strictness.mode', { label: strictnessLabel(s) });
}
