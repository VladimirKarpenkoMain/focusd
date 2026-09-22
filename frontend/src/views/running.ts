import { ConfirmCancel, EmergencyRestore, RequestCancel, SetWidget } from '../api';
import type { State } from '../api';
import {
  formatClock,
  formatMinutes,
  formatNumber,
  formatSecondsShort,
  formatTimeOfDay,
  requestWord,
  siteWord,
  strictnessMode,
} from '../format';
import { t } from '../i18n';
import { icon } from '../icons';
import type { IconName } from '../icons';
import { button, card, h, openModal } from '../ui';

export interface Screen {
  el: HTMLElement;
  update(s: State): void;
}

/** Кольцо: тонкий штрих, как у системного индикатора, а не «прогресс-бар». */
const R = 150;
const CIRC = 2 * Math.PI * R;

export function createRunningView(state: State): Screen {
  const root = h('div', 'running');
  const page = h('div', 'page');
  const inner = h('div', 'page-inner');

  const head = h('header', 'page-head');
  const title = h('h1', 'title', t('run.title'));
  const subtitle = h('p', 'subtitle');
  head.append(title, subtitle);
  inner.append(head);

  const grid = h('div', 'run-grid');

  /* --- Кольцо и часы ------------------------------------------------------- */
  const pane = h('div', 'timer-pane');
  const wrap = h('div', 'ring-wrap');
  wrap.innerHTML = `
    <svg viewBox="0 0 320 320" role="img" aria-hidden="true" focusable="false">
      <defs>
        <linearGradient id="ringGrad" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0%" stop-color="var(--accent-2)" />
          <stop offset="100%" stop-color="var(--accent)" />
        </linearGradient>
      </defs>
      <circle class="ring-track" cx="160" cy="160" r="${R}" stroke-width="4" />
      <circle class="ring-bar" cx="160" cy="160" r="${R}" stroke-width="4"
        stroke-linecap="round" stroke-dasharray="${CIRC.toFixed(2)}" />
    </svg>`;

  const center = h('div', 'ring-center');
  const badge = h('div', 'badge');
  const clock = h('div', 'clock num', '00:00');
  clock.setAttribute('role', 'timer');
  clock.setAttribute('aria-live', 'off');
  const clockSub = h('div', 'clock-sub');
  center.append(badge, clock, clockSub);
  wrap.append(center);
  pane.append(wrap);

  /* --- Сведения о сессии --------------------------------------------------- */
  const side = h('div', 'run-side');
  const details = card(t('run.session'), '', { icon: 'timer' });

  function row(key: string, iconName: IconName, big: boolean): { el: HTMLElement; val: HTMLElement } {
    const el = h('div', 'row');
    const keyEl = h('span', 'row-key');
    keyEl.append(icon(iconName, { size: 14 }), h('span', undefined, key));
    const val = h('span', big ? 'row-val is-big' : 'row-val');
    el.append(keyEl, val);
    return { el, val };
  }

  const sites = row(t('run.rules'), 'globe', true);
  const requests = row(t('run.blocked'), 'ban', true);
  const lastSite = row(t('run.last'), 'shield', false);
  const started = row(t('run.started'), 'timer', false);
  const ends = row(t('run.ends'), 'timer', false);
  details.body.append(sites.el, requests.el, lastSite.el, started.el, ends.el);
  side.append(details.el);

  const widgetBtn = button(t('run.widget'), () => void SetWidget(true), {
    icon: 'pin',
    variant: 'ghost',
  });
  widgetBtn.classList.add('btn-compact');
  side.append(widgetBtn);

  const cancelArea = h('div', 'cancel-area');
  let cancelBtn: HTMLButtonElement | null = null;
  let cancelNote: HTMLElement | null = null;
  let cancelSignature = '';
  let lastState: State = state;

  const emergency = h('button', 'emergency', t('run.emergency'));
  emergency.type = 'button';
  emergency.setAttribute('aria-label', t('run.emergency'));
  emergency.addEventListener('click', () => {
    openModal({
      title: t('run.emergencyTitle'),
      text: t('run.emergencyText'),
      confirmLabel: t('run.continue'),
      danger: true,
      onConfirm: () => {
        void EmergencyRestore();
      },
    });
  });

  // Единственное, без чего блокировка не заработает: перезапуск уже открытого
  // браузера — он читает политику при запуске. Прячем, когда подсказывать нечего.
  const hint = h('div', 'hint');
  hint.hidden = true;

  side.append(hint, cancelArea, emergency);
  grid.append(pane, side);
  inner.append(grid);
  page.append(inner);
  root.append(page);

  const bar = root.querySelector<SVGCircleElement>('.ring-bar');

  /* --- Логика отмены ------------------------------------------------------- */
  function buildCancelArea(s: State): void {
    const session = s.session;
    cancelArea.replaceChildren();
    cancelBtn = null;
    cancelNote = null;

    if (session.strictness === 'lock') {
      cancelNote = h('div', 'cancel-note', t('run.lockedNote'));
      cancelArea.append(cancelNote);
      return;
    }

    const btn = button(t('run.cancel'), () => void handleCancel(btn), {
      variant: 'danger',
      size: 'lg',
    });
    btn.setAttribute('aria-label', t('run.cancelAria'));
    cancelBtn = btn;
    cancelArea.append(btn);
  }

  async function handleCancel(btn: HTMLButtonElement): Promise<void> {
    btn.disabled = true;
    try {
      const info = await RequestCancel();
      if (info.waitSec > 0) {
        // Отмена ещё недоступна — показываем обратный отсчёт.
        if (cancelNote === null) {
          cancelNote = h('div', 'cancel-note', info.message);
          cancelArea.append(cancelNote);
        } else {
          cancelNote.textContent = info.message;
        }
        update(lastState);
        return;
      }
      if (lastState.session.strictness === 'soft') {
        await ConfirmCancel();
        return;
      }
      openModal({
        title: t('run.confirmTitle'),
        text: info.message || t('run.confirmText'),
        confirmLabel: t('run.confirm'),
        cancelLabel: t('run.stay'),
        danger: true,
        onConfirm: () => {
          void ConfirmCancel();
        },
      });
    } finally {
      // Возвращаем кнопку в актуальное состояние (сессия могла остаться активной).
      if (!lastState.session.cancelable || !lastState.session.active) btn.disabled = true;
      update(lastState);
    }
  }

  /* --- Обновление ---------------------------------------------------------- */
  let lastBadgeKey = '';
  let lastSubtitle = '';
  let lastBlockedShown = '';

  function update(s: State): void {
    lastState = s;
    const session = s.session;
    const total = Math.max(1, session.durationSec);
    const frac = Math.min(1, Math.max(0, session.remaining / total));

    if (bar) bar.setAttribute('stroke-dashoffset', (CIRC * (1 - frac)).toFixed(2));

    const text = formatClock(session.remaining);
    if (clock.textContent !== text) clock.textContent = text;
    // Длинная строка (больше часа) не должна вылезать за кольцо.
    clock.classList.toggle('is-long', text.length > 5);
    clockSub.textContent = t('run.ofTotal', { time: formatMinutes(Math.round(total / 60)) });

    const nextSubtitle =
      session.strictness === 'lock'
        ? t('run.subtitle.lock')
        : t(session.cancelable ? 'run.subtitle.now' : 'run.subtitle.wait', {
            time: formatSecondsShort(
              Math.max(0, session.cancelAt - Math.floor(Date.now() / 1000)),
            ),
          });
    if (nextSubtitle !== lastSubtitle) {
      subtitle.textContent = nextSubtitle;
      lastSubtitle = nextSubtitle;
    }

    const badgeKey = session.strictness;
    if (badgeKey !== lastBadgeKey) {
      badge.className = `badge badge-${session.strictness}`;
      badge.textContent = strictnessMode(session.strictness);
      lastBadgeKey = badgeKey;
    }

    sites.val.textContent = formatNumber(s.totalRules);
    sites.el.setAttribute(
      'aria-label',
      t('run.rulesAria', { n: s.totalRules, word: siteWord(s.totalRules) }),
    );

    requests.val.textContent = formatNumber(s.blockedToday);
    requests.el.setAttribute(
      'aria-label',
      t('run.blockedAria', { n: s.blockedToday, word: requestWord(s.blockedToday) }),
    );

    const shown = s.lastBlocked || '—';
    if (shown !== lastBlockedShown) {
      lastBlockedShown = shown;
      lastSite.val.textContent = shown;
    }

    started.val.textContent = formatTimeOfDay(session.startedAt);
    ends.val.textContent = formatTimeOfDay(session.endsAt);

    wrap.classList.toggle('is-low', session.remaining > 0 && session.remaining <= 60);

    if (s.hint) {
      hint.replaceChildren(icon('alert', { size: 15 }), h('span', undefined, s.hint));
      hint.hidden = false;
    } else {
      hint.hidden = true;
    }

    const signature = `${session.strictness}|${session.cancelable}`;
    if (signature !== cancelSignature) {
      cancelSignature = signature;
      buildCancelArea(s);
    }

    if (cancelBtn && !session.cancelable) {
      const wait = Math.max(0, session.cancelAt - Math.floor(Date.now() / 1000));
      cancelBtn.disabled = true;
      cancelBtn.textContent = t('run.cancelIn', { n: wait });
      cancelBtn.setAttribute('aria-label', t('run.cancelInAria', { n: wait }));
    } else if (cancelBtn) {
      cancelBtn.disabled = false;
      cancelBtn.textContent = t('run.cancel');
      cancelBtn.setAttribute('aria-label', t('run.cancelAria'));
    }
  }

  update(state);

  return { el: root, update };
}
