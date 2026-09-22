import {
  AddAllowlistDomain,
  AddCustomDomain,
  OpenDataDir,
  RemoveAllowlistDomain,
  RemoveCustomDomain,
  SaveSettings,
  SetLanguage,
  SetWidget,
  SetWidgetOnTop,
  SetWidgetShape,
} from '../api';
import type { Settings, State, Strictness, WidgetShape } from '../api';
import { formatDuration, strictnessHint, strictnessLabel } from '../format';
import { LANG_CHOICES, langLabel, t } from '../i18n';
import { icon } from '../icons';
import type { IconName } from '../icons';
import { themeLabel, saveTheme } from '../theme';
import type { ThemeChoice } from '../theme';
import {
  button,
  card,
  chipsEditor,
  h,
  iconButton,
  looksLikeDomain,
  normalizeDomain,
  section,
  toggleRow,
} from '../ui';
import { RULER_STEP, createRuler } from './ruler';

export interface Screen {
  el: HTMLElement;
  update(s: State): void;
}

const MIN_MINUTES = 5;
const MAX_MINUTES = 240;

const THEME_ICON: Record<ThemeChoice, IconName> = {
  system: 'monitor',
  light: 'sun',
  dark: 'moon',
};

const STRICTNESS_ICON: Record<Strictness, IconName> = {
  soft: 'power',
  strict: 'timer',
  lock: 'shield',
};

/** Форма компактного таймера: подпись и иконка для выбора. */
const SHAPE_ICON: Record<WidgetShape, IconName> = {
  bar: 'bar-shape',
  ring: 'ring-shape',
};

/** Панель настроек отдаёт наружу свой черновик: при смене языка её перерисовывают,
 * и накопленные, но не сохранённые правки обязаны пережить перерисовку. */
export interface SettingsScreen extends Screen {
  draft: Settings;
}

export function createSettingsView(
  state: State,
  settings: Settings,
  onClose: () => void,
  onThemeChange: (choice: ThemeChoice) => void,
  /** Черновик из прошлого открытия: язык меняется на живой панели, и терять
   * несохранённые правки при её перерисовке нельзя. */
  initial?: Settings,
): SettingsScreen {
  const source = initial ?? settings;
  const draft: Settings = {
    autostart: source.autostart,
    defaultDurationMin: clampDuration(source.defaultDurationMin),
    defaultStrictness: source.defaultStrictness,
    defaultGroups: [...(source.defaultGroups ?? [])],
    customDomains: [...(source.customDomains ?? [])],
    allowlist: [...(source.allowlist ?? [])],
    theme: source.theme ?? 'system',
    language: source.language ?? 'auto',
    widget: source.widget ?? false,
    widgetOnTop: source.widgetOnTop ?? true,
    widgetShape: source.widgetShape === 'ring' ? 'ring' : 'bar',
    widgetX: source.widgetX ?? 0,
    widgetY: source.widgetY ?? 0,
  };

  const overlay = h('div', 'settings-overlay');
  overlay.setAttribute('role', 'dialog');
  overlay.setAttribute('aria-modal', 'true');
  overlay.setAttribute('aria-label', t('settings.title'));

  const panel = h('div', 'settings-panel');
  const head = h('header', 'settings-head');
  const closeBtn = iconButton('close', t('settings.close'), onClose);
  head.append(h('h2', undefined, t('settings.title')), closeBtn);

  const body = h('div', 'settings-body');
  const inner = h('div', 'page-inner');
  const stack = h('div', 'settings-stack');

  /* --- 1. Оформление -------------------------------------------------------- */
  const look = card(t('settings.theme'), '', { icon: 'brush' });
  const picker = h('div', 'theme-picker');
  picker.setAttribute('role', 'radiogroup');
  picker.setAttribute('aria-label', t('settings.themeAria'));
  const choices: ThemeChoice[] = ['system', 'light', 'dark'];
  const themeButtons = choices.map((choice) => {
    const b = h('button', 'theme-opt');
    b.type = 'button';
    b.setAttribute('role', 'radio');
    b.setAttribute('aria-checked', 'false');
    b.append(icon(THEME_ICON[choice], { size: 18 }), h('span', undefined, themeLabel(choice)));
    b.addEventListener('click', () => {
      draft.theme = choice;
      syncTheme();
      onThemeChange(choice);
      // Тема — не то, что нужно подтверждать кнопкой «Сохранить»: она
      // применяется сразу, поэтому сохраняем её отдельным вызовом.
      void saveTheme(choice);
    });
    picker.append(b);
    return { choice, el: b };
  });
  look.body.append(picker);

  // Язык рядом с темой и по той же причине: применяется сразу, а не по кнопке
  // «Сохранить». Изменение языка перерисовывает и эту панель — черновик
  // передаётся в новую копию, поэтому несохранённые правки не теряются.
  const lang = card(t('settings.language'), t('settings.languageHint'), { icon: 'globe' });
  const langPicker = h('div', 'theme-picker is-lang');
  langPicker.setAttribute('role', 'radiogroup');
  langPicker.setAttribute('aria-label', t('settings.languageAria'));
  // Устойчивый признак для стенда: aria-label переводится, а проверка должна
  // находить переключатель на любом языке.
  langPicker.dataset.action = 'language';
  const langButtons = LANG_CHOICES.map((choice) => {
    const b = h('button', 'theme-opt');
    b.type = 'button';
    b.setAttribute('role', 'radio');
    b.setAttribute('aria-checked', 'false');
    b.append(h('span', undefined, langLabel(choice)));
    b.addEventListener('click', () => {
      draft.language = choice;
      syncLanguage();
      void SetLanguage(choice);
    });
    langPicker.append(b);
    return { choice, el: b };
  });
  lang.body.append(langPicker);

  /* --- 2. Компактный таймер ------------------------------------------------- */
  const widgetSec = card(t('settings.shapeCard'), '', { icon: 'pin' });

  const shapeRow = h('div', 'shape-row');
  shapeRow.append(h('div', 'shape-label', t('settings.shape')));
  const seg = h('div', 'seg seg-shape');
  seg.setAttribute('role', 'radiogroup');
  seg.setAttribute('aria-label', t('settings.shapeAria'));
  const shapes: WidgetShape[] = ['bar', 'ring'];
  const shapeButtons = shapes.map((shape) => {
    const b = h('button', 'seg-opt');
    b.type = 'button';
    b.setAttribute('role', 'radio');
    b.setAttribute('aria-checked', 'false');
    const segHead = h('span', 'seg-head');
    segHead.append(
      icon(SHAPE_ICON[shape], { size: 16 }),
      h('span', 'seg-label', t(`settings.shape.${shape}`)),
    );
    b.append(segHead, h('span', 'seg-hint', t(`settings.shape.${shape}Hint`)));
    b.addEventListener('click', () => {
      if (draft.widgetShape === shape) return;
      draft.widgetShape = shape;
      syncShape();
      // Форма — это размер окна, а не правило блокировки: применяем сразу.
      void SetWidgetShape(shape);
    });
    seg.append(b);
    return { shape, el: b };
  });
  shapeRow.append(seg);

  const widgetToggle = toggleRow(
    draft.widget,
    t('settings.widgetOn'),
    t('settings.widgetOnHint'),
    t('settings.widgetOnAria'),
    (v) => {
      draft.widget = v;
      void SetWidget(v);
    },
  );
  const widgetOnTopRow = toggleRow(
    draft.widgetOnTop,
    t('settings.onTop'),
    t('settings.onTopHint'),
    t('settings.onTopAria'),
    (v) => {
      draft.widgetOnTop = v;
      void SetWidgetOnTop(v);
    },
  );
  // Оба параметра компактного режима выбираются заранее, при выключенном
  // виджете: панель настроек открывается только в обычном окне (в плашке она
  // закрывается, а сайдбара с кнопкой там нет). Пока виджет выключен, настройки
  // и настраивают, — значит «выключенный, пока виджет выключен» элемент не
  // включился бы никогда. Выбор сохраняется и применяется при включении.
  widgetSec.body.append(shapeRow, widgetToggle.el, widgetOnTopRow.el);

  /* --- 3. Новая сессия по умолчанию ----------------------------------------- */
  const durSec = card(t('settings.duration'), t('settings.durationHint'), {
    icon: 'timer',
  });
  const durBlock = h('div', 'ruler-block');
  const durValue = h('div', 'ruler-value');
  const durNum = h('span', 'ruler-num num', String(draft.defaultDurationMin));
  durValue.append(durNum, h('span', 'ruler-unit', t('setup.minutes')));
  const durSub = h('div', 'ruler-sub');
  durBlock.append(durValue, durSub);

  const ruler = createRuler({
    min: MIN_MINUTES,
    max: MAX_MINUTES,
    value: draft.defaultDurationMin,
    label: t('settings.durationAria'),
    onChange: (v) => {
      draft.defaultDurationMin = v;
      durNum.textContent = String(v);
      durSub.textContent = v >= 60 ? formatDuration(v) : '';
    },
  });
  durBlock.append(ruler.el);
  durSec.body.append(durBlock);

  const strictSec = card(t('settings.strictness'), '', { icon: 'shield' });
  const strictGroup = h('div', 'seg');
  strictGroup.setAttribute('role', 'radiogroup');
  strictGroup.setAttribute('aria-label', t('settings.strictnessAria'));
  const modes: Strictness[] = ['soft', 'strict', 'lock'];
  const segButtons = modes.map((mode) => {
    const b = h('button', 'seg-opt');
    b.type = 'button';
    b.setAttribute('role', 'radio');
    b.setAttribute('aria-checked', 'false');
    const segHead = h('span', 'seg-head');
    segHead.append(
      icon(STRICTNESS_ICON[mode], { size: 16 }),
      h('span', 'seg-label', strictnessLabel(mode)),
    );
    b.append(segHead, h('span', 'seg-hint', strictnessHint(mode)));
    b.addEventListener('click', () => {
      draft.defaultStrictness = mode;
      syncStrictness();
    });
    strictGroup.append(b);
    return { mode, el: b };
  });
  strictSec.body.append(strictGroup);

  /* --- 4. Защита ------------------------------------------------------------ */
  const guardSec = card(t('settings.layers'), t('settings.layersHint'), { icon: 'sliders' });
  guardSec.body.append(
    toggleRow(
      draft.autostart,
      t('settings.autostart'),
      t('settings.autostartHint'),
      t('settings.autostartAria'),
      (v) => {
        draft.autostart = v;
      },
    ).el,
  );
  guardSec.body.append(
    h('p', 'card-note', t('settings.note.proxy')),
    h('p', 'card-note', t('settings.note.sessions')),
    h('p', 'card-note', t('settings.note.apps')),
  );

  /* --- 5. Что блокируем ----------------------------------------------------- */
  const custom = card(t('settings.custom'), t('settings.customHint'), {
    icon: 'ban',
  });
  const customEditor = chipsEditor({
    initial: draft.customDomains,
    placeholder: t('settings.customPlaceholder'),
    ariaLabel: t('settings.customAria'),
    addLabel: t('settings.add'),
    validate: validateDomain,
    onAdd: (d) => {
      void AddCustomDomain(d);
    },
    onRemove: (d) => {
      void RemoveCustomDomain(d);
    },
  });
  custom.body.append(customEditor.el);

  const allow = card(t('settings.allow'), t('settings.allowHint'), {
    icon: 'shield-off',
  });
  const allowEditor = chipsEditor({
    initial: draft.allowlist,
    placeholder: t('settings.allowPlaceholder'),
    ariaLabel: t('settings.allowAria'),
    addLabel: t('settings.allowAdd'),
    validate: validateDomain,
    onAdd: (d) => {
      void AddAllowlistDomain(d);
    },
    onRemove: (d) => {
      void RemoveAllowlistDomain(d);
    },
  });
  allow.body.append(allowEditor.el);

  /* --- 6. О приложении ------------------------------------------------------ */
  const about = card(t('settings.about'), '', { icon: 'info' });
  const dirRow = h('div', 'about-row');
  const dirPath = h('span', 'about-path', state.dataDir || '—');
  dirRow.append(h('span', undefined, t('settings.dataDir')), dirPath);
  const verRow = h('div', 'about-row');
  const verValue = h('span', undefined, state.version || '—');
  verRow.append(h('span', undefined, t('settings.version')), verValue);
  // Состояние дополнительного слоя живёт здесь, а не на главном экране:
  // на блокировку сайтов оно не влияет, но человеку может быть интересно.
  const notice = h('p', 'card-note');
  notice.hidden = !state.notice;
  if (state.notice) notice.textContent = state.notice;
  const openBtn = button(t('settings.openDataDir'), () => void OpenDataDir(), { icon: 'folder' });
  openBtn.setAttribute('aria-label', t('settings.openDataDir'));
  about.body.append(dirRow, verRow, notice, openBtn);

  /* --- Сборка: разделы отдельными блоками ----------------------------------- */
  // Подсказка карточки «Тема» переехала в пояснение раздела: два раза подряд
  // «Оформление / Тема» выглядело как сбой вёрстки, а не как структура.
  stack.append(
    section(t('settings.section.look'), t('settings.section.lookHint')),
    look.el,
    lang.el,
    section(t('settings.section.widget'), t('settings.section.widgetHint')),
    widgetSec.el,
    section(t('settings.section.newSession')),
    durSec.el,
    strictSec.el,
    section(t('settings.section.guard'), t('settings.section.guardHint')),
    guardSec.el,
    section(t('settings.section.block'), t('settings.section.blockHint')),
    custom.el,
    allow.el,
    section(t('settings.section.app')),
    about.el,
  );
  inner.append(stack);
  body.append(inner);

  /* --- Подвал --------------------------------------------------------------- */
  const foot = h('footer', 'settings-foot');
  const footInner = h('div', 'page-inner');
  const flash = h('span', 'save-flash');
  const saveBtn = button(t('settings.save'), () => void save(), { variant: 'primary' });
  saveBtn.setAttribute('aria-label', t('settings.saveAria'));
  footInner.append(flash, saveBtn);
  foot.append(footInner);

  let flashTimer: number | null = null;
  function showFlash(text: string): void {
    flash.textContent = text;
    if (flashTimer !== null) window.clearTimeout(flashTimer);
    flashTimer = window.setTimeout(() => {
      flash.textContent = '';
    }, 2400);
  }

  async function save(): Promise<void> {
    saveBtn.disabled = true;
    try {
      draft.customDomains = customEditor.get();
      draft.allowlist = allowEditor.get();
      await SaveSettings(draft);
      showFlash(t('settings.saved'));
    } catch (err) {
      showFlash(err instanceof Error ? err.message : t('settings.error'));
    } finally {
      saveBtn.disabled = false;
    }
  }

  panel.append(head, body, foot);
  overlay.append(panel);

  function syncTheme(): void {
    for (const b of themeButtons) {
      const on = b.choice === draft.theme;
      b.el.classList.toggle('is-on', on);
      b.el.setAttribute('aria-checked', on ? 'true' : 'false');
    }
  }

  function syncLanguage(): void {
    for (const b of langButtons) {
      const on = b.choice === draft.language;
      b.el.classList.toggle('is-on', on);
      b.el.setAttribute('aria-checked', on ? 'true' : 'false');
    }
  }

  function syncStrictness(): void {
    for (const s of segButtons) {
      const on = s.mode === draft.defaultStrictness;
      s.el.classList.toggle('is-on', on);
      s.el.setAttribute('aria-checked', on ? 'true' : 'false');
    }
  }

  function syncShape(): void {
    for (const s of shapeButtons) {
      const on = s.shape === draft.widgetShape;
      s.el.classList.toggle('is-on', on);
      s.el.setAttribute('aria-checked', on ? 'true' : 'false');
    }
  }

  syncTheme();
  syncLanguage();
  syncStrictness();
  syncShape();
  durSub.textContent =
    draft.defaultDurationMin >= 60 ? formatDuration(draft.defaultDurationMin) : '';

  return {
    el: overlay,
    draft,
    update(s: State): void {
      dirPath.textContent = s.dataDir || '—';
      verValue.textContent = s.version || '—';
      notice.hidden = !s.notice;
      notice.textContent = s.notice;
      // Форма могла измениться из виджета или второго окна: панель обязана
      // показывать то, что человек увидит, а не то, что было при открытии.
      const shape: WidgetShape = s.widgetShape === 'ring' ? 'ring' : 'bar';
      if (shape !== draft.widgetShape) {
        draft.widgetShape = shape;
        syncShape();
      }
    },
  };
}

function validateDomain(v: string): string {
  if (!looksLikeDomain(normalizeDomain(v))) {
    return t('setup.badDomain');
  }
  return '';
}

function clampDuration(v: number): number {
  if (!Number.isFinite(v)) return 25;
  const snapped = Math.round(v / RULER_STEP) * RULER_STEP;
  return Math.min(MAX_MINUTES, Math.max(MIN_MINUTES, snapped));
}
