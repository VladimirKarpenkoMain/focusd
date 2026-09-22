import {
  AddAllowlistDomain,
  AddCustomDomain,
  RemoveAllowlistDomain,
  RemoveCustomDomain,
  StartSession,
  TestDomain,
} from '../api';
import type { Group, Settings, State, Strictness } from '../api';
import {
  domainWord,
  formatDuration,
  formatNumber,
  groupWord,
  strictnessHint,
  strictnessLabel,
  strictnessMode,
} from '../format';
import { t } from '../i18n';
import type { IconName } from '../icons';
import { groupIcon, icon } from '../icons';
import { button, card, chipsEditor, h, looksLikeDomain, normalizeDomain } from '../ui';
import { RULER_STEP, createRuler } from './ruler';

export interface Screen {
  el: HTMLElement;
  update(s: State): void;
}

const PRESETS = [15, 25, 45, 60, 90];
/** Границы линейки: короче пяти минут фокус не имеет смысла, длиннее четырёх часов — уже не сессия. */
const MIN_MINUTES = 5;
const MAX_MINUTES = 240;

const STRICTNESS_ICON: Record<Strictness, IconName> = {
  soft: 'power',
  strict: 'timer',
  lock: 'shield',
};

export function createSetupView(state: State, catalog: Group[], defaults: Settings | null): Screen {
  // Списки приходят из Go и могут оказаться null, если конфигурация старая
  // или повреждена: подстраховываемся, чтобы экран не падал целиком.
  const groupsDefault = defaults?.defaultGroups ?? [];
  const selected = new Set<string>(
    groupsDefault.length > 0 ? groupsDefault : catalog.map((g) => g.id),
  );
  let durationMin = clampMinutes(defaults ? defaults.defaultDurationMin : 25);
  let strictness: Strictness = defaults ? defaults.defaultStrictness : 'soft';
  const customDomains = [...(defaults?.customDomains ?? [])];
  const allowlist = [...(defaults?.allowlist ?? [])];
  let actionError = '';

  const root = h('div', 'setup');

  const page = h('div', 'page');
  const inner = h('div', 'page-inner');
  const head = h('header', 'page-head');
  head.append(
    h('h1', 'title', t('setup.title')),
    h('p', 'subtitle', t('setup.subtitle')),
  );
  inner.append(head);

  const banners = h('div', 'banners');
  inner.append(banners);

  /* --- Длительность -------------------------------------------------------- */
  const durationCard = card(t('setup.duration'), t('setup.durationHint'), {
    icon: 'timer',
  });

  const rulerBlock = h('div', 'ruler-block');
  const rulerValue = h('div', 'ruler-value');
  const rulerNum = h('span', 'ruler-num num', String(durationMin));
  rulerValue.append(rulerNum, h('span', 'ruler-unit', t('setup.minutes')));
  const rulerSub = h('div', 'ruler-sub');
  rulerBlock.append(rulerValue, rulerSub);

  const ruler = createRuler({
    min: MIN_MINUTES,
    max: MAX_MINUTES,
    value: durationMin,
    label: t('setup.rulerAria'),
    onChange: (v) => {
      durationMin = v;
      syncDuration();
    },
  });
  rulerBlock.append(ruler.el);

  const presets = h('div', 'presets');
  const presetButtons = PRESETS.map((min) => {
    const b = h('button', 'chip', t('setup.preset', { min }));
    b.type = 'button';
    b.setAttribute('aria-pressed', 'false');
    b.addEventListener('click', () => {
      durationMin = min;
      ruler.set(min);
      syncDuration();
    });
    presets.append(b);
    return { min, el: b };
  });
  rulerBlock.append(presets);
  durationCard.body.append(rulerBlock);
  inner.append(durationCard.el);

  /* --- Строгость ----------------------------------------------------------- */
  const strictCard = card(t('setup.strictness'), '', { icon: 'shield' });
  const seg = h('div', 'seg');
  seg.setAttribute('role', 'radiogroup');
  seg.setAttribute('aria-label', t('setup.strictnessAria'));
  const modes: Strictness[] = ['soft', 'strict', 'lock'];
  const segButtons = modes.map((mode) => {
    const b = h('button', 'seg-opt');
    b.type = 'button';
    b.setAttribute('role', 'radio');
    b.setAttribute('aria-checked', 'false');
    const segHead = h('span', 'seg-head');
    segHead.append(icon(STRICTNESS_ICON[mode], { size: 16 }), h('span', 'seg-label', strictnessLabel(mode)));
    b.append(segHead, h('span', 'seg-hint', strictnessHint(mode)));
    b.addEventListener('click', () => {
      strictness = mode;
      syncStrictness();
      syncStartNote();
    });
    seg.append(b);
    return { mode, el: b };
  });
  strictCard.body.append(seg);
  inner.append(strictCard.el);

  /* --- Группы -------------------------------------------------------------- */
  const groupCard = card(
    t('setup.groups'),
    t('setup.groupsCount', { n: catalog.length, word: groupWord(catalog.length) }),
    { icon: 'globe' },
  );

  const grid = h('div', 'groups');
  const groupButtons: { id: string; el: HTMLElement; domains: number }[] = [];

  if (catalog.length === 0) {
    grid.append(h('div', 'card-note', t('setup.catalogMissing')));
  }

  for (const g of catalog) {
    const b = h('button', 'group');
    b.type = 'button';
    b.dataset.id = g.id;
    const check = h('span', 'group-check');
    check.append(icon('check', { size: 11, stroke: 3.2 }));
    b.append(
      check,
      groupIcon(g.icon, { size: 19 }),
      h('span', 'group-title', g.title),
      h(
        'span',
        'group-meta',
        t('setup.groupMeta', {
          n: g.domains.length,
          word: domainWord(g.domains.length),
          note: g.note,
        }),
      ),
    );
    b.addEventListener('click', () => {
      if (selected.has(g.id)) selected.delete(g.id);
      else selected.add(g.id);
      syncGroups();
      syncStartNote();
    });
    grid.append(b);
    groupButtons.push({ id: g.id, el: b, domains: g.domains.length });
  }
  groupCard.body.append(grid);
  inner.append(groupCard.el);

  /* --- Свои сайты ---------------------------------------------------------- */
  const sitesCard = card(t('setup.custom'), t('setup.customHint'), {
    icon: 'ban',
  });

  const customEditor = chipsEditor({
    initial: customDomains,
    placeholder: t('setup.customPlaceholder'),
    ariaLabel: t('setup.customAria'),
    addLabel: t('setup.add'),
    validate: validateDomain,
    onAdd: (d) => {
      customDomains.push(d);
      void AddCustomDomain(d);
      syncStartNote();
      syncGroups();
    },
    onRemove: (d) => {
      const i = customDomains.indexOf(d);
      if (i >= 0) customDomains.splice(i, 1);
      void RemoveCustomDomain(d);
      syncStartNote();
    },
  });
  sitesCard.body.append(customEditor.el);

  // Проверка домена: без неё человек не понимает, попал ли сайт под правило —
  // правило накрывает и поддомены, и угадать это на глаз невозможно.
  const checker = h('div', 'checker');
  const checkRow = h('div', 'field-row');
  const checkInput = h('input');
  checkInput.type = 'text';
  checkInput.placeholder = t('setup.checkPlaceholder');
  checkInput.autocomplete = 'off';
  checkInput.spellcheck = false;
  checkInput.setAttribute('aria-label', t('setup.checkAria'));
  const checkResult = h('span', 'checker-result');
  checkRow.append(checkInput, checkResult);
  checker.append(checkRow);
  sitesCard.body.append(checker);

  checkInput.addEventListener('input', () => {
    const v = normalizeDomain(checkInput.value);
    if (!v || !looksLikeDomain(v)) {
      checkResult.textContent = '';
      checkResult.className = 'checker-result';
      return;
    }
    void TestDomain(v).then((blocked) => {
      // Ответ мог прийти после того, как человек уже стёр или дописал ввод.
      if (normalizeDomain(checkInput.value) !== v) return;
      checkResult.textContent = blocked
        ? t('setup.check.blocked')
        : allowlist.length > 0
          ? t('setup.check.free')
          : t('setup.check.other');
      checkResult.className = blocked ? 'checker-result is-blocked' : 'checker-result is-free';
    });
  });

  inner.append(sitesCard.el);

  /* --- Исключения ---------------------------------------------------------- */
  const allowCard = card(t('setup.allow'), t('setup.allowHint'), {
    icon: 'shield-off',
  });
  const allowEditor = chipsEditor({
    initial: allowlist,
    placeholder: t('setup.allowPlaceholder'),
    ariaLabel: t('setup.allowAria'),
    addLabel: t('setup.allowAdd'),
    validate: validateDomain,
    onAdd: (d) => {
      allowlist.push(d);
      void AddAllowlistDomain(d);
    },
    onRemove: (d) => {
      const i = allowlist.indexOf(d);
      if (i >= 0) allowlist.splice(i, 1);
      void RemoveAllowlistDomain(d);
    },
  });
  allowCard.body.append(allowEditor.el);
  inner.append(allowCard.el);

  page.append(inner);

  /* --- Панель запуска ------------------------------------------------------ */
  const actionbar = h('footer', 'actionbar');
  const actionInner = h('div', 'actionbar-inner');
  const actionSummary = h('div', 'action-summary');
  const summaryStrong = h('strong');
  const summaryRest = h('span');
  const summaryBar = h('div', 'action-bar');
  const summaryFill = h('div', 'action-bar-fill');
  summaryBar.append(summaryFill);
  actionSummary.append(summaryStrong, summaryRest);

  const startBtn = button(t('setup.start'), () => void start(), { variant: 'primary', size: 'lg' });
  startBtn.setAttribute('aria-label', t('setup.startAria'));

  actionInner.append(actionSummary, startBtn);
  actionbar.append(actionInner);
  actionSummary.append(summaryBar);

  root.append(page, actionbar);

  async function start(): Promise<void> {
    actionError = '';
    startBtn.disabled = true;
    startBtn.textContent = t('setup.starting');
    try {
      await StartSession({
        durationMin,
        strictness,
        groups: [...selected],
        customDomains,
        allowlist,
      });
    } catch (err) {
      actionError = err instanceof Error ? err.message : String(err);
      syncBanners(state);
    } finally {
      startBtn.disabled = false;
      startBtn.textContent = t('setup.start');
    }
  }

  function syncDuration(): void {
    rulerNum.textContent = String(durationMin);
    rulerSub.textContent = durationMin >= 60 ? formatDuration(durationMin) : '';
    for (const c of presetButtons) {
      const on = c.min === durationMin;
      c.el.classList.toggle('is-on', on);
      c.el.setAttribute('aria-pressed', on ? 'true' : 'false');
    }
  }

  function syncStrictness(): void {
    for (const s of segButtons) {
      const on = s.mode === strictness;
      s.el.classList.toggle('is-on', on);
      s.el.setAttribute('aria-checked', on ? 'true' : 'false');
    }
  }

  function syncGroups(): void {
    for (const g of groupButtons) {
      const on = selected.has(g.id);
      g.el.classList.toggle('is-on', on);
      g.el.setAttribute('aria-pressed', on ? 'true' : 'false');
    }
  }

  function selectedRules(): number {
    let n = customDomains.length;
    for (const g of groupButtons) if (selected.has(g.id)) n += g.domains;
    return n;
  }

  function syncStartNote(): void {
    const n = selectedRules();
    summaryStrong.textContent = t('setup.summary', {
      duration: formatDuration(durationMin),
      mode: strictnessMode(strictness),
    });
    summaryRest.textContent = t('setup.summaryRules', {
      n: formatNumber(n),
      domainWord: domainWord(n),
      groups: selected.size,
      groupWord: groupWord(selected.size),
    });
    summaryFill.style.width = `${Math.min(100, (durationMin / MAX_MINUTES) * 100).toFixed(1)}%`;
  }

  function syncBanners(s: State): void {
    banners.replaceChildren();
    if (!s.elevated) {
      const warn = h('div', 'banner banner-warn');
      warn.append(
        icon('alert', { size: 15 }),
        h('span', undefined, t('setup.noRights')),
      );
      banners.append(warn);
    }
    // Состояние дополнительного слоя (DNS) сюда намеренно не выводится:
    // к блокировке сайтов оно отношения не имеет, а технические подробности
    // про svchost и Hyper-V на главном экране только пугают. Они живут
    // в настройках, в разделе «О приложении».
    const err = actionError || s.lastError;
    if (err) {
      const box = h('div', 'banner banner-error');
      box.append(icon('alert', { size: 15 }), h('span', undefined, err));
      banners.append(box);
    }
  }

  syncDuration();
  syncStrictness();
  syncGroups();
  syncStartNote();
  syncBanners(state);

  return {
    el: root,
    update(s: State): void {
      syncBanners(s);
    },
  };
}

/** Проверка домена до того, как он попадёт в правила. */
function validateDomain(v: string): string {
  if (!looksLikeDomain(v)) {
    return t('setup.badDomain');
  }
  return '';
}

function clampMinutes(v: number): number {
  if (!Number.isFinite(v)) return 25;
  const snapped = Math.round(v / RULER_STEP) * RULER_STEP;
  return Math.min(MAX_MINUTES, Math.max(MIN_MINUTES, snapped));
}
