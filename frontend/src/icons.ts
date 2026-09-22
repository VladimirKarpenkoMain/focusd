/**
 * Набор иконок приложения.
 *
 * Все иконки нарисованы здесь вручную: приложение офлайновое, тянуть иконочный
 * шрифт или CDN нельзя, а эмодзи в роли иконок выглядят ровно так, как выглядит
 * интерфейс, собранный на скорую руку. Каждая фигура — контур на сетке 24×24 с
 * толщиной штриха 1.7, поэтому иконки читаются как один набор, а не как
 * случайная подборка.
 *
 * `fill="none"` и `currentColor` обязательны: иконка наследует цвет текста и
 * одинаково работает в светлой и тёмной теме.
 */

export type IconName =
  | 'shield'
  | 'shield-off'
  | 'globe'
  | 'ban'
  | 'timer'
  | 'gear'
  | 'close'
  | 'check'
  | 'plus'
  | 'minus'
  | 'trash'
  | 'folder'
  | 'pin'
  | 'pin-off'
  | 'minimise'
  | 'maximise'
  | 'restore'
  // Выход из компактного режима: стрелки наружу. Квадрат «Развернуть» в
  // виджете читался как «свернуть в квадратик» — то есть ровно наоборот.
  | 'expand'
  | 'sun'
  | 'moon'
  | 'monitor'
  | 'alert'
  | 'info'
  | 'power'
  | 'brush'
  | 'wifi'
  | 'key'
  | 'chevron-right'
  | 'chevron-left'
  | 'refresh'
  | 'spark'
  | 'inbox'
  | 'sliders'
  // Формы компактного таймера: плашка с полосой и круг с кольцом.
  | 'bar-shape'
  | 'ring-shape'
  // Иконки групп блокировки — ключи приходят из Go-каталога.
  | 'users'
  | 'play'
  | 'zap'
  | 'gamepad'
  | 'message'
  | 'newspaper'
  | 'cart'
  | 'infinity'
  | 'heart'
  | 'generic';

const PATHS: Record<IconName, string> = {
  shield: '<path d="M12 3.2 19 6v5.8c0 4.2-2.9 7.4-7 8.9-4.1-1.5-7-4.7-7-8.9V6l7-2.8Z"/><path d="M9.2 12.2l2 2 3.6-3.9"/>',
  'shield-off':
    '<path d="M12 3.2 19 6v5.8a8.6 8.6 0 0 1-.5 2.9M8.4 8.1 5 9.3v2.5c0 4.2 2.9 7.4 7 8.9a8.2 8.2 0 0 0 2.6-1.1"/><path d="M4 4l16 16"/>',
  globe:
    '<circle cx="12" cy="12" r="8.5"/><path d="M3.6 12h16.8"/><path d="M12 3.5c2.2 2.4 3.4 5.4 3.4 8.5S14.2 18.1 12 20.5c-2.2-2.4-3.4-5.4-3.4-8.5S9.8 5.9 12 3.5Z"/>',
  ban: '<circle cx="12" cy="12" r="8.5"/><path d="M6.2 6.2l11.6 11.6"/>',
  timer: '<circle cx="12" cy="13.5" r="7"/><path d="M12 10.2v3.6l2.4 1.5"/><path d="M9.5 2.8h5"/>',
  gear: '<circle cx="12" cy="12" r="3"/><path d="M12 2.8v2.4M12 18.8v2.4M4.5 7.4l2.1 1.2M17.4 15.4l2.1 1.2M4.5 16.6l2.1-1.2M17.4 8.6l2.1-1.2"/>',
  close: '<path d="M6.5 6.5l11 11M17.5 6.5l-11 11"/>',
  check: '<path d="M4.5 12.6 9.6 17.7 19.5 6.9"/>',
  plus: '<path d="M12 5.5v13M5.5 12h13"/>',
  minus: '<path d="M5.5 12h13"/>',
  trash: '<path d="M4.5 7h15"/><path d="M9.5 7V4.8h5V7"/><path d="M6.6 7l.9 12.2h9l.9-12.2"/><path d="M10.4 10.6v5.6M13.6 10.6v5.6"/>',
  folder: '<path d="M3.2 6.9A2 2 0 0 1 5.2 5h3.4l1.9 2.4h8.3a2 2 0 0 1 2 2v8.7a2 2 0 0 1-2 2H5.2a2 2 0 0 1-2-2V6.9Z"/>',
  pin: '<path d="M9.4 3.6h5.2l-.8 5 3 2.6v1.6H7.2v-1.6l3-2.6-.8-5Z"/><path d="M12 12.8v7.6"/>',
  'pin-off': '<path d="M9.4 3.6h5.2l-.8 5 3 2.6v1.6h-2.6M8.6 12.8H7.2v-1.6l2.2-1.9"/><path d="M12 12.8v7.6"/><path d="M4 4l16 16"/>',
  minimise: '<path d="M5.5 12h13"/>',
  maximise: '<rect x="5" y="5" width="14" height="14" rx="2"/>',
  restore: '<rect x="4.5" y="7.5" width="12" height="12" rx="2"/><path d="M8.5 7.5V6a1.5 1.5 0 0 1 1.5-1.5h8A1.5 1.5 0 0 1 19.5 6v8a1.5 1.5 0 0 1-1.5 1.5h-1.5"/>',
  expand:
    '<path d="M9 4.5H4.5V9M15 4.5h4.5V9M15 19.5h4.5V15M9 19.5H4.5V15"/>',
  sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2.8v2.2M12 19v2.2M4.6 4.6l1.6 1.6M17.8 17.8l1.6 1.6M2.8 12H5M19 12h2.2M4.6 19.4l1.6-1.6M17.8 6.2l1.6-1.6"/>',
  moon: '<path d="M20 14.2A8.2 8.2 0 0 1 9.8 4 8.5 8.5 0 1 0 20 14.2Z"/>',
  monitor: '<rect x="2.8" y="4.2" width="18.4" height="12.6" rx="2"/><path d="M8.5 20.5h7M12 16.8v3.7"/>',
  alert: '<path d="M12 3.6 21 19.4H3L12 3.6Z"/><path d="M12 10v4.2M12 17.2h.01"/>',
  info: '<circle cx="12" cy="12" r="8.5"/><path d="M12 11v5.4M12 7.9h.01"/>',
  power: '<path d="M12 3.6v7.6"/><path d="M6.4 6.9a8 8 0 1 0 11.2 0"/>',
  brush: '<path d="M4.5 19.5c2.6.6 4.6-.6 5.2-2.8.4-1.6 1.6-2.6 3.2-2.6"/><path d="M14.6 4.8l4.6 4.6-6.2 6.2a2.6 2.6 0 0 1-3.7 0 2.6 2.6 0 0 1 0-3.7l5.3-7.1Z"/>',
  wifi: '<path d="M2.9 9.1a13 13 0 0 1 18.2 0"/><path d="M6.4 12.6a8 8 0 0 1 11.2 0"/><path d="M9.8 16a3.2 3.2 0 0 1 4.4 0"/><path d="M12 19.4h.01"/>',
  key: '<circle cx="8.2" cy="15.8" r="3.4"/><path d="M10.7 13.3 19.4 4.6M16.4 7.6l2 2M14 10l2 2"/>',
  'chevron-right': '<path d="M9.5 6l6 6-6 6"/>',
  'chevron-left': '<path d="M14.5 6l-6 6 6 6"/>',
  refresh: '<path d="M20 11.5A8 8 0 0 0 6.3 6.9L4 9.2"/><path d="M4 4.8v4.4h4.4"/><path d="M4 12.5a8 8 0 0 0 13.7 4.6L20 14.8"/><path d="M20 19.2v-4.4h-4.4"/>',
  spark: '<path d="M12 3.4l1.9 5.1 5.1 1.9-5.1 1.9L12 17.4l-1.9-5.1L5 10.4l5.1-1.9L12 3.4Z"/><path d="M18.6 16.4l.7 1.9 1.9.7-1.9.7-.7 1.9-.7-1.9-1.9-.7 1.9-.7.7-1.9Z"/>',
  inbox: '<path d="M3.2 12.8 5.6 5.4A2 2 0 0 1 7.5 4h9a2 2 0 0 1 1.9 1.4l2.4 7.4v4.8a2 2 0 0 1-2 2H5.2a2 2 0 0 1-2-2v-4.8Z"/><path d="M3.2 12.8h5l1 2.2h5.6l1-2.2h5"/>',
  sliders: '<path d="M4.5 7.5h9M17 7.5h2.5M4.5 16.5h3M11 16.5h8.5"/><circle cx="15" cy="7.5" r="2.1"/><circle cx="9" cy="16.5" r="2.1"/>',
  // Миниатюры форм виджета: узнаются с одного взгляда, потому что повторяют
  // силуэт самой плашки и самого круга.
  'bar-shape': '<rect x="2.8" y="8" width="18.4" height="8" rx="2.4"/><path d="M7.2 12h4.4"/>',
  'ring-shape': '<circle cx="12" cy="12" r="8.4"/><path d="M12 3.6a8.4 8.4 0 0 1 0 16.8"/>',

  users:
    '<circle cx="9" cy="8.4" r="3.2"/><path d="M3.4 19.2c0-3 2.5-5.2 5.6-5.2s5.6 2.2 5.6 5.2"/><path d="M16 5.7a3.2 3.2 0 0 1 0 6.2M17.2 14.4c2 .6 3.4 2.4 3.4 4.8"/>',
  play: '<rect x="2.8" y="5.2" width="18.4" height="12.6" rx="2.4"/><path d="M10.2 9.2l4.6 2.8-4.6 2.8V9.2Z"/>',
  zap: '<path d="M13.2 2.8 5.4 13.2h5.8l-.4 8 7.8-10.4h-5.8l.4-8Z"/>',
  gamepad:
    '<path d="M8.4 7.6h7.2a5.4 5.4 0 0 1 5.2 4.1l.7 3.5a2.4 2.4 0 0 1-4.3 2l-1.6-2.1H8.4l-1.6 2.1a2.4 2.4 0 0 1-4.3-2l.7-3.5a5.4 5.4 0 0 1 5.2-4.1Z"/><path d="M7.4 10.8v2.4M6.2 12h2.4M15.8 11.4h.01M17.8 13.2h.01"/>',
  message: '<path d="M20.6 11.6a7.9 7.9 0 0 1-11.4 7.1L4.2 19.9l1.4-4.6a7.9 7.9 0 1 1 15-3.7Z"/>',
  newspaper:
    '<rect x="3" y="4.8" width="18" height="14.4" rx="2.2"/><path d="M7 9.4h7M7 12.6h10M7 15.8h6"/><rect x="15.4" y="8.2" width="2.6" height="2.6" rx="1"/>',
  cart: '<path d="M2.8 4.4h2.4l2.4 10.4h10.2"/><path d="M6.6 8.4h14.1l-1.6 6.4H7.9"/><circle cx="9.4" cy="18.8" r="1.5"/><circle cx="17.2" cy="18.8" r="1.5"/>',
  infinity:
    '<path d="M8.2 8.4c-2.1 0-3.6 1.6-3.6 3.6s1.5 3.6 3.6 3.6c2.6 0 4.1-3.4 7.4-3.4 2.1 0 3.6 1.6 3.6 3.6s-1.5 3.6-3.6 3.6c-3.3 0-4.8-3.4-7.4-3.4"/>',
  heart: '<path d="M12 20.2S3.6 15.2 3.6 9.6A4.6 4.6 0 0 1 12 7.2a4.6 4.6 0 0 1 8.4 2.4c0 5.6-8.4 10.6-8.4 10.6Z"/>',
  generic:
    '<circle cx="12" cy="12" r="8.5"/><path d="M12 8.4v7.2M8.4 12h7.2"/>',
};

export interface IconOptions {
  /** Размер в пикселях. По умолчанию 18 — подходит и кнопкам, и строкам. */
  size?: number;
  /** Толщина штриха. Крупным иконкам нужен штрих тоньше, иначе они «жирнеют». */
  stroke?: number;
  /** Дополнительный CSS-класс. */
  className?: string;
}

function svgMarkup(name: IconName, o: IconOptions): string {
  const size = o.size ?? 18;
  const stroke = o.stroke ?? 1.7;
  const cls = o.className ? ` class="${o.className}"` : '';
  return (
    `<svg${cls} width="${size}" height="${size}" viewBox="0 0 24 24" fill="none" ` +
    `stroke="currentColor" stroke-width="${stroke}" stroke-linecap="round" ` +
    `stroke-linejoin="round" aria-hidden="true" focusable="false">${PATHS[name]}</svg>`
  );
}

/**
 * Возвращает элемент с иконкой. Иконка декоративна: `aria-hidden` уже выставлен,
 * а имя для скринридера должно быть у кнопки или подписи рядом.
 */
export function icon(name: IconName, o: IconOptions = {}): HTMLSpanElement {
  const span = document.createElement('span');
  span.className = o.className ? `ico ${o.className}` : 'ico';
  // Значение берётся только из белого списка PATHS — пользовательские данные
  // сюда не попадают.
  span.innerHTML = svgMarkup(name, { ...o, className: undefined });
  return span;
}

/** Тот же набор, но строкой — для мест, где удобнее innerHTML. */
export function iconHTML(name: IconName, o: IconOptions = {}): string {
  return svgMarkup(name, o);
}

/** Иконка группы блокировки по ключу из Go-каталога. */
export function groupIcon(name: string, o: IconOptions = {}): HTMLSpanElement {
  const known = name in PATHS ? (name as IconName) : 'generic';
  return icon(known, o);
}
