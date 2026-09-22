/**
 * Виджет — компактный таймер, который висит поверх остальных окон.
 *
 * Это тот же экран приложения, но окно уменьшено до размеров виджета, поэтому
 * здесь нет ничего лишнего: режим, время, прогресс и две кнопки.
 * Перетаскивается виджет за что угодно, кроме кнопок: Wails считает областью
 * перетаскивания элемент с `--wails-draggable: drag`, а этот стиль наследуется,
 * так что «нет» достаточно сказать только кнопкам.
 *
 * Формы две, и разметка у них общая. Кольцо прогресса — это SVG размером ровно
 * с окно: дуга рисуется по тем же 200 единицам, что и пиксели окна, поэтому
 * линия остаётся ровной без пересчёта координат. Размеры окна задаёт Go
 * (`ringWidth`/`barWidth`), здесь они только используются.
 */

import { SetWidget, SetWidgetOnTop } from '../api';
import type { State, WidgetShape } from '../api';
import { formatClock, formatTimeOfDay, strictnessMode } from '../format';
import { t } from '../i18n';
import { icon } from '../icons';
import { h, iconButton } from '../ui';

export interface Screen {
  el: HTMLElement;
  update(s: State): void;
}

/**
 * Кольцо прогресса в единицах окна. Размер совпадает с ringWidth в app.go:
 * дуга рисуется в тех же числах, что и пиксели, поэтому линия не пересчитывается
 * и остаётся ровной. 224 - 2*12 = 200, то есть радиус 100 при обводке 7: между
 * кольцом и краем окна остаётся 12 px, а внутренний край кольца (96.5) оставляет
 * место под кнопку возврата внизу.
 */
const RING_RADIUS = 100;
const RING_CIRCUMFERENCE = 2 * Math.PI * RING_RADIUS;
const RING_SVG = 224;

export function createWidgetView(state: State): Screen {
  const root = h('div', 'widget');

  /* --- Кольцо прогресса ---------------------------------------------------- */
  const ring = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  ring.setAttribute('class', 'widget-ring');
  ring.setAttribute('viewBox', `0 0 ${RING_SVG} ${RING_SVG}`);
  ring.setAttribute('aria-hidden', 'true');
  const track = document.createElementNS('http://www.w3.org/2000/svg', 'circle');
  const arc = document.createElementNS('http://www.w3.org/2000/svg', 'circle');
  for (const [el, cls] of [
    [track, 'ring-track'],
    [arc, 'ring-arc'],
  ] as const) {
    el.setAttribute('cx', String(RING_SVG / 2));
    el.setAttribute('cy', String(RING_SVG / 2));
    el.setAttribute('r', String(RING_RADIUS));
    el.setAttribute('fill', 'none');
    el.setAttribute('class', cls);
  }
  arc.setAttribute('stroke-dasharray', RING_CIRCUMFERENCE.toFixed(2));
  ring.append(track, arc);

  /* --- Верхняя строка: режим и кнопки -------------------------------------- */
  const top = h('div', 'widget-top');

  const mode = h('div', 'widget-mode');
  const dot = h('span', 'dot');
  const modeText = h('span', 'widget-mode-text');
  mode.append(dot, modeText);

  const actions = h('div', 'widget-actions');
  const pinBtn = iconButton('pin', t('widget.pinOff'), () => {
    void SetWidgetOnTop(!last.widgetOnTop);
  });
  actions.append(pinBtn);
  top.append(mode, actions);

  /* --- Время и полоса ------------------------------------------------------ */
  const main = h('div', 'widget-main');
  const clock = h('div', 'widget-clock num', '--:--');
  clock.setAttribute('role', 'timer');
  clock.setAttribute('aria-live', 'off');
  const note = h('div', 'widget-note');
  main.append(clock, note);

  const bar = h('div', 'widget-bar');
  const fill = h('div', 'widget-bar-fill');
  bar.append(fill);

  /* --- Выход из компактного режима ----------------------------------------- */
  // Иконка «Развернуть» — квадрат — читалась в виджете как «свернуть в
  // квадратик», то есть ровно наоборот. Теперь это кнопка со словами: она либо
  // делит нижнюю строку плашки с полосой прогресса, либо стоит отдельной
  // строкой в круге, где полосы нет.
  const backBtn = h('button', 'btn btn-ghost btn-compact widget-back');
  backBtn.type = 'button';
  backBtn.setAttribute('aria-label', t('widget.back'));
  backBtn.title = t('widget.back');
  backBtn.append(icon('expand', { size: 15 }), h('span', undefined, t('widget.backLabel')));
  backBtn.addEventListener('click', () => {
    void SetWidget(false);
  });

  const bottom = h('div', 'widget-bottom');
  bottom.append(bar, backBtn);

  /* --- Пустое состояние ---------------------------------------------------- */
  // Без сессии показывать нечего, кроме строки простоя, но выход в обычное окно
  // обязан остаться: сайдбара с тумблером здесь нет, окно размером с плашку, и
  // без кнопки компактный режим превращался в ловушку — вернуться было нечем.
  // Поэтому кнопка живёт в строке простоя, а с началом сессии переезжает в
  // нижнюю строку, к полосе прогресса.
  const idle = h('div', 'widget-idle');
  const idleText = h('div', 'widget-idle-text', t('widget.idle'));
  idle.append(idleText);

  root.append(ring, top, main, bottom, idle);

  let last: State = state;
  let lastModeKey = '';
  let lastPinIcon = '';
  let lastShape: WidgetShape | '' = '';
  // Где сейчас лежит кнопка выхода: в нижней строке или в строке простоя.
  let backPlace: 'idle' | 'bottom' | '' = '';

  /** Форма — это атрибут, а не отдельная разметка: переключается мгновенно. */
  function syncShape(s: State): void {
    const shape: WidgetShape = s.widgetShape === 'ring' ? 'ring' : 'bar';
    if (shape === lastShape) return;
    lastShape = shape;
    root.dataset.shape = shape;
  }

  function update(s: State): void {
    last = s;
    const session = s.session;
    syncShape(s);

    pinBtn.classList.toggle('is-active', s.widgetOnTop);
    const pinLabel = t(s.widgetOnTop ? 'widget.pinOff' : 'widget.pinOn');
    pinBtn.setAttribute('aria-label', pinLabel);
    pinBtn.title = pinLabel;
    const pinIcon = s.widgetOnTop ? 'pin' : 'pin-off';
    if (pinIcon !== lastPinIcon) {
      lastPinIcon = pinIcon;
      pinBtn.replaceChildren(icon(pinIcon, { size: 16 }));
    }

    const running = session.active;
    main.hidden = !running;
    bottom.hidden = !running;
    ring.classList.toggle('is-on', running);
    idle.hidden = running;

    // Кнопка выхода переезжает между строками. append переносит узел, а не
    // копирует его: обработчик и подпись остаются те же.
    const place = running ? 'bottom' : 'idle';
    if (place !== backPlace) {
      backPlace = place;
      if (running) bottom.append(bar, backBtn);
      else idle.append(idleText, backBtn);
    }

    if (!running) {
      dot.classList.remove('is-on');
      root.classList.add('is-idle');
      const stopped = s.browserBlock;
      modeText.textContent = stopped ? t('widget.stopping') : t('widget.free');
      idleText.textContent = stopped ? t('widget.stopping') : t('widget.idle');
      // Кольцо без сессии не должно выглядеть как «почти всё прошло».
      arc.setAttribute('stroke-dashoffset', RING_CIRCUMFERENCE.toFixed(2));
      return;
    }

    root.classList.remove('is-idle');
    dot.classList.add('is-on');

    const modeKey = session.strictness;
    if (modeKey !== lastModeKey) {
      lastModeKey = modeKey;
      modeText.textContent = strictnessMode(session.strictness);
    }

    const text = formatClock(session.remaining);
    if (clock.textContent !== text) clock.textContent = text;
    clock.classList.toggle('is-long', text.length > 5);
    clock.classList.toggle('is-low', session.remaining > 0 && session.remaining <= 60);

    note.textContent = t('widget.until', { time: formatTimeOfDay(session.endsAt) });

    const total = Math.max(1, session.durationSec);
    const frac = Math.min(1, Math.max(0, session.remaining / total));
    fill.style.width = `${(frac * 100).toFixed(2)}%`;
    // Кольцо идёт по часовой стрелке от верхней точки: смещение дуги — это
    // доля, которой ещё не прошло время, поэтому она совпадает с полосой.
    arc.setAttribute('stroke-dashoffset', (RING_CIRCUMFERENCE * (1 - frac)).toFixed(2));
  }

  update(state);
  return { el: root, update };
}
