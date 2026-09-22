/**
 * Линейка выбора длительности — центральный элемент экрана настройки сессии.
 *
 * Это не «слайдер», а именно линейка: под центром стоит неподвижный указатель,
 * а шкала с рисками едет под ним мышью, колесом или стрелками клавиатуры.
 * Такой выбор точнее попадает под курсор и читается как измерительный прибор,
 * а не как мобильный ползунок.
 *
 * Зависимостей нет: только DOM и Pointer Events.
 */

import { formatMinutes } from '../format';

/** Пикселей шкалы на одну минуту. Определяет «шаг» линейки. */
const PX_PER_MIN = 8;
/** Шаг дискретизации значения в минутах. */
export const RULER_STEP = 5;
/** Каждая N-я минута получает длинную риску с подписью. */
const MAJOR_EVERY = 15;

export interface RulerOptions {
  min: number;
  max: number;
  value: number;
  /** Доступное имя для скринридера. */
  label: string;
  onChange: (value: number) => void;
}

export interface Ruler {
  el: HTMLElement;
  get(): number;
  set(value: number): void;
}

function clampTo(v: number, min: number, max: number): number {
  if (!Number.isFinite(v)) return min;
  const snapped = Math.round(v / RULER_STEP) * RULER_STEP;
  return Math.min(max, Math.max(min, snapped));
}

export function createRuler(o: RulerOptions): Ruler {
  const { min, max } = o;
  let value = clampTo(o.value, min, max);

  const el = document.createElement('div');
  el.className = 'ruler';
  el.tabIndex = 0;
  el.setAttribute('role', 'slider');
  el.setAttribute('aria-label', o.label);
  el.setAttribute('aria-valuemin', String(min));
  el.setAttribute('aria-valuemax', String(max));
  el.setAttribute('aria-orientation', 'horizontal');

  const track = document.createElement('div');
  track.className = 'ruler-track';

  for (let v = min; v <= max; v += RULER_STEP) {
    const tick = document.createElement('div');
    const major = v % MAJOR_EVERY === 0;
    tick.className = major ? 'ruler-tick is-major' : 'ruler-tick';
    tick.style.left = `${(v - min) * PX_PER_MIN}px`;
    if (major) {
      const text = document.createElement('span');
      text.className = 'ruler-label';
      text.textContent = String(v);
      tick.append(text);
    }
    track.append(tick);
  }

  const needle = document.createElement('div');
  needle.className = 'ruler-center';
  el.append(track, needle);

  function paint(): void {
    track.style.transform = `translateX(${-(value - min) * PX_PER_MIN}px)`;
    el.setAttribute('aria-valuenow', String(value));
    el.setAttribute('aria-valuetext', formatMinutes(value));
  }

  function commit(next: number): void {
    const v = clampTo(next, min, max);
    if (v === value) return;
    value = v;
    paint();
    o.onChange(value);
  }

  /* --- Мышь: перетаскивание шкалы ---------------------------------------- */
  let dragging = false;
  let startX = 0;
  let startValue = value;

  el.addEventListener('pointerdown', (e) => {
    if (e.button !== 0) return;
    dragging = true;
    startX = e.clientX;
    startValue = value;
    el.classList.add('is-dragging');
    el.setPointerCapture(e.pointerId);
    el.focus();
    e.preventDefault();
  });

  el.addEventListener('pointermove', (e) => {
    if (!dragging) return;
    // Шкала едет за курсором: сдвиг вправо показывает меньшие значения.
    const delta = (e.clientX - startX) / PX_PER_MIN;
    commit(startValue - delta);
  });

  function endDrag(e: PointerEvent): void {
    if (!dragging) return;
    dragging = false;
    el.classList.remove('is-dragging');
    if (el.hasPointerCapture(e.pointerId)) el.releasePointerCapture(e.pointerId);
  }

  el.addEventListener('pointerup', endDrag);
  el.addEventListener('pointercancel', endDrag);

  /* --- Колесо ------------------------------------------------------------- */
  let wheelAcc = 0;
  el.addEventListener(
    'wheel',
    (e) => {
      e.preventDefault();
      // deltaMode 1 — «строки», а не пиксели: у разных мышей масштаб разный.
      const delta = e.deltaMode === 1 ? e.deltaY * 16 : e.deltaY;
      wheelAcc += delta;
      const notches = Math.trunc(wheelAcc / 50);
      if (notches === 0) return;
      wheelAcc -= notches * 50;
      commit(value + Math.sign(notches) * RULER_STEP * Math.min(Math.abs(notches), 4));
    },
    { passive: false },
  );

  /* --- Клавиатура --------------------------------------------------------- */
  el.addEventListener('keydown', (e) => {
    let next = value;
    switch (e.key) {
      case 'ArrowLeft':
      case 'ArrowDown':
        next = value - RULER_STEP;
        break;
      case 'ArrowRight':
      case 'ArrowUp':
        next = value + RULER_STEP;
        break;
      case 'PageDown':
        next = value - RULER_STEP * 6;
        break;
      case 'PageUp':
        next = value + RULER_STEP * 6;
        break;
      case 'Home':
        next = min;
        break;
      case 'End':
        next = max;
        break;
      default:
        return;
    }
    e.preventDefault();
    e.stopPropagation();
    commit(next);
  });

  paint();

  return {
    el,
    get: () => value,
    set: (v: number) => commit(v),
  };
}
