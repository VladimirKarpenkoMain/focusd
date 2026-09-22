/**
 * Язык интерфейса: словари и выбор языка.
 *
 * Язык приходит из Go вместе с состоянием (`resolvedLanguage`): «как в системе»
 * разрешается там, где читается язык Windows. Здесь второй раз его разрешать
 * нельзя — иначе окно и подписи групп однажды разойдутся.
 *
 * Русские строки — исходные: они же лежат в разметке и в Go. Поэтому словарь
 * полон именно по ним, а английский и китайский — переводы. Ключа нет ни в
 * выбранном языке, ни в русском — возвращается сам ключ: так отсутствующий
 * перевод видно глазом, а не пустым местом.
 */

export type Lang = 'ru' | 'en' | 'zh';

/** Язык, который используется до первого состояния: исходный язык приложения. */
const FALLBACK: Lang = 'ru';

let current: Lang = FALLBACK;

type Vars = Record<string, string | number>;
type Dict = Record<string, string>;

/** true, если такой язык поддерживается. */
export function isLang(v: string): v is Lang {
  return v === 'ru' || v === 'en' || v === 'zh';
}

export function lang(): Lang {
  return current;
}

/** Ставит язык интерфейса. Строки перерисовывает вызывающий: см. main.ts. */
export function setLang(v: string): void {
  current = isLang(v) ? v : FALLBACK;
  // Атрибут lang нужен ради переносов и подбора шрифта: без него китайский
  // текст набирается шрифтом с кириллическим набором, и иероглифы выглядят
  // чужими.
  document.documentElement.lang = current === 'zh' ? 'zh-CN' : current;
}

export function t(key: string, vars?: Vars): string {
  const raw = dict[current][key] ?? dict[FALLBACK][key];
  if (raw === undefined) return key;
  if (!vars) return raw;
  return raw.replace(/\{(\w+)\}/g, (whole, name: string) =>
    name in vars ? String(vars[name]) : whole,
  );
}

/**
 * Форма слова по числу. Ключ указывает на набор форм: `word.domain` — это
 * `word.domain.one`, `.few`, `.many` и `.other`.
 *
 * У русского три формы, у английского две, у китайского слова по числу не
 * меняются вовсе — поэтому правило выбирается по языку, а не по числу.
 */
export function plural(n: number, key: string): string {
  const abs = Math.abs(n) % 100;
  const last = abs % 10;

  if (current === 'ru') {
    if (abs > 10 && abs < 20) return t(`${key}.many`);
    if (last > 1 && last < 5) return t(`${key}.few`);
    if (last === 1) return t(`${key}.one`);
    return t(`${key}.many`);
  }
  if (current === 'en') return n === 1 ? t(`${key}.one`) : t(`${key}.other`);
  return t(`${key}.other`);
}

/** Разделитель разрядов: у русского узкий неразрывный пробел. */
export function groupSeparator(): string {
  return current === 'ru' ? '\u2009' : ',';
}

/* ------------------------------------------------------------------ */
/* Словари                                                             */
/* ------------------------------------------------------------------ */

const ru = {
  /* Каркас окна и сайдбар */
  'side.title': 'Состояние',
  'side.sites': 'Блокировка',
  'side.proxy': 'Счётчик запросов',
  'side.rights': 'Права',
  'side.proxyTooltip':
    'Точный — браузеры идут через локальный прокси Focusd и каждый отсечённый запрос посчитан. ' +
    'Неполный — прокси недоступен, работает запасная политика браузеров.',
  'side.statLabel': '{word} отсечено',
  'side.last': 'последний: {site}',
  'side.none': 'попыток не было',
  'side.demo': 'демо-режим',
  'side.widget': 'Виджет поверх окон',
  'side.settings': 'Настройки',
  'side.settingsAria': 'Открыть настройки',
  'side.theme.light': 'Светлая тема',
  'side.theme.dark': 'Тёмная тема',
  'side.theme.aria': 'Переключить оформление: {theme}',
  'side.on': 'Включена',
  'side.off': 'Выключена',
  'side.exact': 'Точный',
  'side.incomplete': 'Неполный',
  'side.admin': 'Администратор',
  'side.limited': 'Ограничены',
  'side.busy': 'Идёт фокус · {time}',
  'side.ready': 'Готов к сессии',
  'side.needRights': 'Нужны права администратора',

  'win.minimise': 'Свернуть',
  'win.maximise': 'Развернуть',
  'win.close': 'Закрыть Focusd',

  /* Слова по числу и единицы */
  'word.domain.one': 'домен',
  'word.domain.few': 'домена',
  'word.domain.many': 'доменов',
  'word.domain.other': 'доменов',
  'word.site.one': 'сайт',
  'word.site.few': 'сайта',
  'word.site.many': 'сайтов',
  'word.site.other': 'сайтов',
  'word.request.one': 'запрос',
  'word.request.few': 'запроса',
  'word.request.many': 'запросов',
  'word.request.other': 'запросов',
  'word.minute.one': 'минута',
  'word.minute.few': 'минуты',
  'word.minute.many': 'минут',
  'word.minute.other': 'минут',
  'word.second.one': 'секунда',
  'word.second.few': 'секунды',
  'word.second.many': 'секунд',
  'word.second.other': 'секунд',
  'word.group.one': 'группа',
  'word.group.few': 'группы',
  'word.group.many': 'групп',
  'word.group.other': 'групп',
  'word.hour.one': 'час',
  'word.hour.few': 'часа',
  'word.hour.many': 'часов',
  'word.hour.other': 'часов',
  'unit.minute': 'мин',
  'unit.second': 'с',

  'strictness.soft': 'Мягкий',
  'strictness.strict': 'Строгий',
  'strictness.lock': 'Жёсткий',
  'strictness.soft.hint': 'Можно отменить в любой момент',
  'strictness.strict.hint': 'Отмена через 60 секунд и с подтверждением',
  'strictness.lock.hint': 'Отменить нельзя до конца сессии',
  'strictness.mode': '{label} режим',

  /* Общие кирпичики */
  'ui.cancel': 'Отмена',
  'ui.empty': 'Пока пусто',
  'ui.duplicate': '{v} уже в списке',
  'ui.remove': 'Убрать {v}',

  'theme.system': 'Как в системе',
  'theme.light': 'Светлая',
  'theme.dark': 'Тёмная',

  /* Экран запуска */
  'setup.title': 'Новая сессия',
  'setup.subtitle': 'Выберите длительность, строгость и то, что должно подождать.',
  'setup.duration': 'Длительность',
  'setup.durationHint': 'Тяните линейку, крутите колесо или жмите стрелки',
  'setup.minutes': 'мин',
  'setup.rulerAria': 'Длительность фокус-сессии в минутах',
  'setup.preset': '{min} мин',
  'setup.strictness': 'Строгость',
  'setup.strictnessAria': 'Строгость сессии',
  'setup.groups': 'Что блокируем',
  'setup.groupsCount': '{n} {word}',
  'setup.catalogMissing': 'Каталог групп недоступен.',
  'setup.groupMeta': '{n} {word} · {note}',
  'setup.custom': 'Свои сайты',
  'setup.customHint': 'Добавляются к группам во всех новых сессиях',
  'setup.customPlaceholder': 'например, example.com',
  'setup.customAria': 'Сайты, которые нужно блокировать',
  'setup.add': 'Добавить',
  'setup.checkPlaceholder': 'проверить домен',
  'setup.checkAria': 'Проверить, блокируется ли домен',
  'setup.check.blocked': 'блокируется',
  'setup.check.free': 'не блокируется',
  'setup.check.other': 'не под правило',
  'setup.allow': 'Исключения',
  'setup.allowHint': 'Работают всегда, даже во время жёсткой сессии',
  'setup.allowPlaceholder': 'например, localhost',
  'setup.allowAria': 'Домены-исключения',
  'setup.allowAdd': 'Разрешить',
  'setup.summary': '{duration} · {mode}',
  'setup.summaryRules': '{n} {domainWord} в {groups} {groupWord}',
  'setup.start': 'Начать фокус',
  'setup.startAria': 'Начать фокус-сессию',
  'setup.starting': 'Запускаю…',
  'setup.noRights':
    'Нет прав администратора. Блокировка работать не будет. Перезапустите Focusd от имени администратора.',
  'setup.badDomain': 'Нужен домен вида example.com — без пути и порта',

  /* Идущая сессия */
  'run.title': 'Идёт фокус',
  'run.subtitle.lock': 'Закрыть приложение не поможет: сторож вернёт блокировку.',
  'run.subtitle.now': 'Отмена доступна сейчас.',
  'run.subtitle.wait': 'Отмена доступна через {time}.',
  'run.session': 'Сессия',
  'run.rules': 'Правил блокировки',
  'run.blocked': 'Отсечено запросов',
  'run.last': 'Последний сайт',
  'run.started': 'Начало',
  'run.ends': 'Окончание',
  'run.rulesAria': 'Заблокировано {n} {word}',
  'run.blockedAria': 'Отсечено {n} {word}',
  'run.widget': 'Показать виджет',
  'run.emergency': 'Аварийно снять блокировку',
  'run.emergencyTitle': 'Аварийное снятие блокировки',
  'run.emergencyText':
    'Блокировка снимется, браузеры вернутся в обычное состояние. Сессия прервётся. Продолжить?',
  'run.continue': 'Продолжить',
  'run.lockedNote': 'Жёсткая сессия — отмена недоступна',
  'run.cancel': 'Отменить фокус',
  'run.cancelAria': 'Отменить фокус-сессию',
  'run.cancelIn': 'Отмена через {n} с',
  'run.cancelInAria': 'Отмена станет доступна через {n} секунд',
  'run.confirmTitle': 'Прервать фокус?',
  'run.confirmText': 'Сессия завершится досрочно, прогресс не сохранится.',
  'run.confirm': 'Прервать',
  'run.stay': 'Остаться',
  'run.ofTotal': 'из {time}',

  /* Виджет */
  'widget.pinOn': 'Держать поверх окон',
  'widget.pinOff': 'Не держать поверх окон',
  'widget.back': 'Вернуться в окно Focusd',
  'widget.backLabel': 'Показать окно',
  'widget.idle': 'Фокус не идёт',
  'widget.stopping': 'Блокировка снимается',
  'widget.free': 'Свободное время',
  'widget.until': 'до {time}',

  /* Настройки */
  'settings.title': 'Настройки',
  'settings.close': 'Закрыть настройки',
  'settings.section.look': 'Оформление',
  'settings.section.lookHint': 'Как выглядит окно Focusd',
  'settings.section.widget': 'Компактный таймер',
  'settings.section.widgetHint':
    'Маленькое окно с временем сессии, которое видно поверх остальных',
  'settings.section.newSession': 'Новая сессия по умолчанию',
  'settings.section.guard': 'Защита',
  'settings.section.guardHint': 'Что закрывает доступ. Изменения — по кнопке «Сохранить»',
  'settings.section.block': 'Что блокируем',
  'settings.section.blockHint': 'Отдельно от групп: действует во всех новых сессиях',
  'settings.section.app': 'Приложение',
  'settings.theme': 'Тема',
  'settings.themeAria': 'Тема оформления',
  'settings.language': 'Язык',
  'settings.languageAria': 'Язык интерфейса',
  'settings.languageHint': '«Как в системе» следует за языком Windows',
  'settings.lang.auto': 'Как в системе',
  'settings.lang.ru': 'Русский',
  'settings.lang.en': 'English',
  'settings.lang.zh': '中文',
  'settings.shapeCard': 'Вид и поведение',
  'settings.shape': 'Вид таймера',
  'settings.shapeAria': 'Вид компактного таймера',
  'settings.shape.bar': 'Плашка',
  'settings.shape.barHint': 'Полоса прогресса и время в одну строку',
  'settings.shape.ring': 'Круг',
  'settings.shape.ringHint': 'Кольцо прогресса вокруг времени',
  'settings.widgetOn': 'Показывать таймер поверх окон',
  'settings.widgetOnHint': 'Окно сжимается до таймера, его можно таскать мышью за любое место.',
  'settings.widgetOnAria': 'Показывать компактный таймер поверх окон',
  'settings.onTop': 'Держать поверх всех окон',
  'settings.onTopHint':
    'Выключите, если таймер должен вести себя как обычное окно и уходить под другие.',
  'settings.onTopAria': 'Держать компактный таймер поверх всех окон',
  'settings.duration': 'Длительность',
  'settings.durationHint': 'С неё начинается каждая новая сессия',
  'settings.durationAria': 'Длительность сессии по умолчанию в минутах',
  'settings.strictness': 'Строгость',
  'settings.strictnessAria': 'Строгость по умолчанию',
  'settings.layers': 'Слои защиты',
  'settings.layersHint': 'Как Focusd закрывает сайты',
  'settings.autostart': 'Запускать вместе с системой',
  'settings.autostartHint': 'Focusd стартует при входе в систему и сразу поднимает блокировку.',
  'settings.autostartAria': 'Автозапуск при входе в систему',
  'settings.note.proxy':
    'Сайты закрывает прокси Focusd: он видит каждый запрос и считает отсечённые. ' +
    'Браузерам он прописан политикой, а тем, кто политик не читает — например, ' +
    'Яндекс Браузеру, — системным прокси Windows. Пока Focusd работает, ' +
    'системный прокси указывает на него, а прежние настройки возвращаются при ' +
    'выходе. Если прокси поднять не удалось, работа переходит на запасную ' +
    'политику браузеров — она надёжнее, но снаружи ненаблюдаема, поэтому ' +
    'счётчик в этом случае неточен.',
  'settings.note.sessions':
    'Жёсткие сессии всегда охраняются задачей планировщика: она возвращает ' +
    'блокировку и перезапускает Focusd, если приложение закрыли. Обойти такую ' +
    'сессию нельзя — только через «Аварийно снять блокировку».',
  'settings.note.apps':
    'Закрываются браузеры и программы, которые ходят через системный прокси ' +
    'Windows. Приложения со своим способом подключения — Telegram, Steam и ' +
    'подобные — Focusd не ограничивает.',
  'settings.custom': 'Свои сайты',
  'settings.customHint': 'Добавляются к группам во всех новых сессиях',
  'settings.customPlaceholder': 'example.com',
  'settings.customAria': 'Свои домены для блокировки',
  'settings.add': 'Добавить',
  'settings.allow': 'Исключения',
  'settings.allowHint': 'Работают всегда, даже во время жёсткой сессии',
  'settings.allowPlaceholder': 'localhost',
  'settings.allowAria': 'Домены-исключения',
  'settings.allowAdd': 'Разрешить',
  'settings.about': 'Версия и данные',
  'settings.dataDir': 'Папка данных',
  'settings.version': 'Версия',
  'settings.openDataDir': 'Открыть папку данных',
  'settings.save': 'Сохранить',
  'settings.saveAria': 'Сохранить настройки',
  'settings.saved': 'Сохранено',
  'settings.error': 'Ошибка',

  /* Служебные сообщения интерфейса */
  'main.noContainer': 'Не найден контейнер #{id}',
  'main.renderFailed': 'Не удалось отрисовать интерфейс: {err}',
  'main.stateFailed': 'Не удалось получить состояние Focusd: {err}',

  /* Демо-режим: те же ключи, что приходят из Go */
  'demo.cancel.wait': 'Отмена станет доступна через {n} с.',
  'demo.cancel.lock': 'Жёсткую сессию нельзя прервать досрочно.',
  'demo.cancel.confirm': 'Прервать строгую сессию досрочно? Это будет записано в статистику.',
  'demo.session': 'Фокус-сессия',
  'demo.restored': 'Аварийное восстановление: блокировка снята.',
  'demo.group.social.title': 'Соцсети',
  'demo.group.social.note': 'Лента, рекомендации, бесконечный скролл',
  'demo.group.video.title': 'Видео и стриминг',
  'demo.group.video.note': 'Ролики, стримы, музыка',
  'demo.group.shorts.title': 'Короткие видео',
  'demo.group.shorts.note': 'Вертикальные ролики и клипы',
  'demo.group.games.title': 'Игры',
  'demo.group.games.note': 'Магазины и сообщества',
  'demo.group.messengers.title': 'Мессенджеры',
  'demo.group.messengers.note': 'Вебы мессенджеров и чаты',
  'demo.group.news.title': 'Новости и форумы',
  'demo.group.news.note': 'Ленты, агрегаторы, обсуждения',
  'demo.group.shopping.title': 'Маркетплейсы',
  'demo.group.shopping.note': 'Покупки и объявления',
  'demo.group.adult.title': '18+',
  'demo.group.adult.note': 'Контент для взрослых',
  'demo.group.doomscroll.title': 'Doomscroll-ловушки',
  'demo.group.doomscroll.note': 'Мемы и бесконечные подборки',
};

/** Набор ключей задаёт русский словарь: он исходный. */
type Key = keyof typeof ru;

const en: Record<Key, string> = {
  'side.title': 'Status',
  'side.sites': 'Blocking',
  'side.proxy': 'Request counter',
  'side.rights': 'Permissions',
  'side.proxyTooltip':
    'Exact — browsers go through the local Focusd proxy and every blocked request is counted. ' +
    'Incomplete — the proxy is unavailable and the fallback browser policy is doing the work.',
  'side.statLabel': '{word} blocked',
  'side.last': 'last: {site}',
  'side.none': 'no attempts yet',
  'side.demo': 'demo mode',
  'side.widget': 'Always-on-top widget',
  'side.settings': 'Settings',
  'side.settingsAria': 'Open settings',
  'side.theme.light': 'Light theme',
  'side.theme.dark': 'Dark theme',
  'side.theme.aria': 'Switch appearance: {theme}',
  'side.on': 'On',
  'side.off': 'Off',
  'side.exact': 'Exact',
  'side.incomplete': 'Incomplete',
  'side.admin': 'Administrator',
  'side.limited': 'Limited',
  'side.busy': 'Focusing · {time}',
  'side.ready': 'Ready for a session',
  'side.needRights': 'Administrator rights required',

  'win.minimise': 'Minimise',
  'win.maximise': 'Maximise',
  'win.close': 'Close Focusd',

  'word.domain.one': 'domain',
  'word.domain.few': 'domains',
  'word.domain.many': 'domains',
  'word.domain.other': 'domains',
  'word.site.one': 'site',
  'word.site.few': 'sites',
  'word.site.many': 'sites',
  'word.site.other': 'sites',
  'word.request.one': 'request',
  'word.request.few': 'requests',
  'word.request.many': 'requests',
  'word.request.other': 'requests',
  'word.minute.one': 'minute',
  'word.minute.few': 'minutes',
  'word.minute.many': 'minutes',
  'word.minute.other': 'minutes',
  'word.second.one': 'second',
  'word.second.few': 'seconds',
  'word.second.many': 'seconds',
  'word.second.other': 'seconds',
  'word.group.one': 'group',
  'word.group.few': 'groups',
  'word.group.many': 'groups',
  'word.group.other': 'groups',
  'word.hour.one': 'hour',
  'word.hour.few': 'hours',
  'word.hour.many': 'hours',
  'word.hour.other': 'hours',
  'unit.minute': 'min',
  'unit.second': 's',

  'strictness.soft': 'Soft',
  'strictness.strict': 'Strict',
  'strictness.lock': 'Locked',
  'strictness.soft.hint': 'Can be cancelled at any moment',
  'strictness.strict.hint': 'Cancelling unlocks after 60 seconds and needs confirmation',
  'strictness.lock.hint': 'Cannot be cancelled until the session ends',
  'strictness.mode': '{label} mode',

  'ui.cancel': 'Cancel',
  'ui.empty': 'Nothing yet',
  'ui.duplicate': '{v} is already on the list',
  'ui.remove': 'Remove {v}',

  'theme.system': 'System',
  'theme.light': 'Light',
  'theme.dark': 'Dark',

  'setup.title': 'New session',
  'setup.subtitle': 'Pick the duration, the strictness and what should wait.',
  'setup.duration': 'Duration',
  'setup.durationHint': 'Drag the ruler, turn the wheel or use the arrow keys',
  'setup.minutes': 'min',
  'setup.rulerAria': 'Focus session duration in minutes',
  'setup.preset': '{min} min',
  'setup.strictness': 'Strictness',
  'setup.strictnessAria': 'Session strictness',
  'setup.groups': 'What to block',
  'setup.groupsCount': '{n} {word}',
  'setup.catalogMissing': 'The group catalog is unavailable.',
  'setup.groupMeta': '{n} {word} · {note}',
  'setup.custom': 'Custom sites',
  'setup.customHint': 'Added to the groups in every new session',
  'setup.customPlaceholder': 'e.g. example.com',
  'setup.customAria': 'Sites to block',
  'setup.add': 'Add',
  'setup.checkPlaceholder': 'check a domain',
  'setup.checkAria': 'Check whether a domain is blocked',
  'setup.check.blocked': 'blocked',
  'setup.check.free': 'not blocked',
  'setup.check.other': 'no matching rule',
  'setup.allow': 'Exceptions',
  'setup.allowHint': 'Always allowed, even during a locked session',
  'setup.allowPlaceholder': 'e.g. localhost',
  'setup.allowAria': 'Allowed domains',
  'setup.allowAdd': 'Allow',
  'setup.summary': '{duration} · {mode}',
  'setup.summaryRules': '{n} {domainWord} in {groups} {groupWord}',
  'setup.start': 'Start focus',
  'setup.startAria': 'Start a focus session',
  'setup.starting': 'Starting…',
  'setup.noRights':
    'No administrator rights. Blocking will not work. Restart Focusd as administrator.',
  'setup.badDomain': 'Enter a domain like example.com — without a path or port',

  'run.title': 'Focusing',
  'run.subtitle.lock': 'Closing the app will not help: the guard restores blocking.',
  'run.subtitle.now': 'Cancelling is available now.',
  'run.subtitle.wait': 'Cancelling is available in {time}.',
  'run.session': 'Session',
  'run.rules': 'Blocking rules',
  'run.blocked': 'Requests blocked',
  'run.last': 'Last site',
  'run.started': 'Started',
  'run.ends': 'Ends',
  'run.rulesAria': '{n} {word} blocked',
  'run.blockedAria': '{n} {word} blocked',
  'run.widget': 'Show widget',
  'run.emergency': 'Emergency unblock',
  'run.emergencyTitle': 'Emergency unblock',
  'run.emergencyText':
    'Blocking will be removed and browsers will return to normal. The session will end. Continue?',
  'run.continue': 'Continue',
  'run.lockedNote': 'Locked session — cancelling is unavailable',
  'run.cancel': 'Cancel focus',
  'run.cancelAria': 'Cancel the focus session',
  'run.cancelIn': 'Cancel in {n} s',
  'run.cancelInAria': 'Cancelling becomes available in {n} seconds',
  'run.confirmTitle': 'Interrupt focus?',
  'run.confirmText': 'The session will end early and its progress will not be saved.',
  'run.confirm': 'Interrupt',
  'run.stay': 'Stay',
  'run.ofTotal': 'of {time}',

  'widget.pinOn': 'Keep on top',
  'widget.pinOff': 'Do not keep on top',
  'widget.back': 'Back to the Focusd window',
  'widget.backLabel': 'Show window',
  'widget.idle': 'No focus session',
  'widget.stopping': 'Blocking is being removed',
  'widget.free': 'Free time',
  'widget.until': 'until {time}',

  'settings.title': 'Settings',
  'settings.close': 'Close settings',
  'settings.section.look': 'Appearance',
  'settings.section.lookHint': 'How the Focusd window looks',
  'settings.section.widget': 'Compact timer',
  'settings.section.widgetHint':
    'A small window with the session time, visible above the other windows',
  'settings.section.newSession': 'Default new session',
  'settings.section.guard': 'Protection',
  'settings.section.guardHint': 'What closes access. Changes apply with “Save”',
  'settings.section.block': 'What to block',
  'settings.section.blockHint': 'Separate from the groups: applies to every new session',
  'settings.section.app': 'Application',
  'settings.theme': 'Theme',
  'settings.themeAria': 'Colour theme',
  'settings.language': 'Language',
  'settings.languageAria': 'Interface language',
  'settings.languageHint': '“System” follows the Windows language',
  'settings.lang.auto': 'System',
  'settings.lang.ru': 'Russian',
  'settings.lang.en': 'English',
  'settings.lang.zh': 'Chinese',
  'settings.shapeCard': 'Shape and behaviour',
  'settings.shape': 'Timer shape',
  'settings.shapeAria': 'Compact timer shape',
  'settings.shape.bar': 'Bar',
  'settings.shape.barHint': 'Progress bar and time on one line',
  'settings.shape.ring': 'Ring',
  'settings.shape.ringHint': 'Progress ring around the time',
  'settings.widgetOn': 'Show the timer above other windows',
  'settings.widgetOnHint': 'The window shrinks to the timer and can be dragged by any free spot.',
  'settings.widgetOnAria': 'Show the compact timer above other windows',
  'settings.onTop': 'Keep above all windows',
  'settings.onTopHint': 'Turn off if the timer should behave like an ordinary window.',
  'settings.onTopAria': 'Keep the compact timer above all windows',
  'settings.duration': 'Duration',
  'settings.durationHint': 'Every new session starts with it',
  'settings.durationAria': 'Default session duration in minutes',
  'settings.strictness': 'Strictness',
  'settings.strictnessAria': 'Default strictness',
  'settings.layers': 'Protection layers',
  'settings.layersHint': 'How Focusd closes sites',
  'settings.autostart': 'Start with the system',
  'settings.autostartHint': 'Focusd starts at sign-in and raises blocking right away.',
  'settings.autostartAria': 'Start automatically at sign-in',
  'settings.note.proxy':
    'Sites are closed by the Focusd proxy: it sees every request and counts the blocked ' +
    'ones. Browsers get it through a policy, and those that ignore policies — Yandex ' +
    'Browser, for one — through the Windows system proxy. While Focusd runs, the system ' +
    'proxy points at it, and the previous settings are restored on exit. If the proxy ' +
    'cannot be raised, the fallback browser policy takes over: it is more reliable but ' +
    'invisible from the outside, so the counter is inexact in that case.',
  'settings.note.sessions':
    'Locked sessions are always guarded by a scheduled task: it restores blocking and ' +
    'restarts Focusd if the app was closed. Such a session cannot be bypassed — only ' +
    'through “Emergency unblock”.',
  'settings.note.apps':
    'Browsers and programs that use the Windows system proxy are closed. Apps with their ' +
    'own way of connecting — Telegram, Steam and the like — are not restricted by Focusd.',
  'settings.custom': 'Custom sites',
  'settings.customHint': 'Added to the groups in every new session',
  'settings.customPlaceholder': 'example.com',
  'settings.customAria': 'Custom domains to block',
  'settings.add': 'Add',
  'settings.allow': 'Exceptions',
  'settings.allowHint': 'Always allowed, even during a locked session',
  'settings.allowPlaceholder': 'localhost',
  'settings.allowAria': 'Allowed domains',
  'settings.allowAdd': 'Allow',
  'settings.about': 'Version and data',
  'settings.dataDir': 'Data folder',
  'settings.version': 'Version',
  'settings.openDataDir': 'Open the data folder',
  'settings.save': 'Save',
  'settings.saveAria': 'Save settings',
  'settings.saved': 'Saved',
  'settings.error': 'Error',

  'main.noContainer': 'Container #{id} not found',
  'main.renderFailed': 'Could not render the interface: {err}',
  'main.stateFailed': 'Could not read the Focusd state: {err}',

  'demo.cancel.wait': 'Cancelling will be available in {n} s.',
  'demo.cancel.lock': 'A locked session cannot be interrupted early.',
  'demo.cancel.confirm': 'Interrupt the strict session early? This will be recorded in the stats.',
  'demo.session': 'Focus session',
  'demo.restored': 'Emergency restore: blocking removed.',
  'demo.group.social.title': 'Social networks',
  'demo.group.social.note': 'Feed, recommendations, endless scroll',
  'demo.group.video.title': 'Video & streaming',
  'demo.group.video.note': 'Clips, streams, music',
  'demo.group.shorts.title': 'Short videos',
  'demo.group.shorts.note': 'Vertical clips and shorts',
  'demo.group.games.title': 'Games',
  'demo.group.games.note': 'Stores and communities',
  'demo.group.messengers.title': 'Messengers',
  'demo.group.messengers.note': 'Web messengers and chats',
  'demo.group.news.title': 'News & forums',
  'demo.group.news.note': 'Feeds, aggregators, discussions',
  'demo.group.shopping.title': 'Marketplaces',
  'demo.group.shopping.note': 'Shopping and classifieds',
  'demo.group.adult.title': '18+',
  'demo.group.adult.note': 'Adult content',
  'demo.group.doomscroll.title': 'Doomscroll traps',
  'demo.group.doomscroll.note': 'Memes and endless collections',
};

const zh: Record<Key, string> = {
  'side.title': '状态',
  'side.sites': '屏蔽',
  'side.proxy': '请求计数',
  'side.rights': '权限',
  'side.proxyTooltip':
    '精确 —— 浏览器经由本地 Focusd 代理，每个被拦截的请求都会被计数。' +
    '不完整 —— 代理不可用，正在使用浏览器策略作为备用。',
  // В китайском слова по числу не меняются, поэтому подпись собирается без
  // подстановки: «已拦截 个请求» читалось бы как ошибка.
  'side.statLabel': '已拦截请求',
  'side.last': '最近：{site}',
  'side.none': '暂无拦截记录',
  'side.demo': '演示模式',
  'side.widget': '窗口置顶小组件',
  'side.settings': '设置',
  'side.settingsAria': '打开设置',
  'side.theme.light': '浅色主题',
  'side.theme.dark': '深色主题',
  'side.theme.aria': '切换外观：{theme}',
  'side.on': '已开启',
  'side.off': '已关闭',
  'side.exact': '精确',
  'side.incomplete': '不完整',
  'side.admin': '管理员',
  'side.limited': '受限',
  'side.busy': '专注中 · {time}',
  'side.ready': '可以开始专注',
  'side.needRights': '需要管理员权限',

  'win.minimise': '最小化',
  'win.maximise': '最大化',
  'win.close': '关闭 Focusd',

  'word.domain.one': '个域名',
  'word.domain.few': '个域名',
  'word.domain.many': '个域名',
  'word.domain.other': '个域名',
  'word.site.one': '个网站',
  'word.site.few': '个网站',
  'word.site.many': '个网站',
  'word.site.other': '个网站',
  'word.request.one': '个请求',
  'word.request.few': '个请求',
  'word.request.many': '个请求',
  'word.request.other': '个请求',
  'word.minute.one': '分钟',
  'word.minute.few': '分钟',
  'word.minute.many': '分钟',
  'word.minute.other': '分钟',
  'word.second.one': '秒',
  'word.second.few': '秒',
  'word.second.many': '秒',
  'word.second.other': '秒',
  'word.group.one': '组',
  'word.group.few': '组',
  'word.group.many': '组',
  'word.group.other': '组',
  'word.hour.one': '小时',
  'word.hour.few': '小时',
  'word.hour.many': '小时',
  'word.hour.other': '小时',
  'unit.minute': '分',
  'unit.second': '秒',

  'strictness.soft': '宽松',
  'strictness.strict': '严格',
  'strictness.lock': '锁定',
  'strictness.soft.hint': '随时可以取消',
  'strictness.strict.hint': '60 秒后可取消，且需要确认',
  'strictness.lock.hint': '会话结束前无法取消',
  'strictness.mode': '{label}模式',

  'ui.cancel': '取消',
  'ui.empty': '暂无内容',
  'ui.duplicate': '{v} 已在列表中',
  'ui.remove': '移除 {v}',

  'theme.system': '跟随系统',
  'theme.light': '浅色',
  'theme.dark': '深色',

  'setup.title': '新建专注',
  'setup.subtitle': '选择时长、严格程度，以及需要等待的事。',
  'setup.duration': '时长',
  'setup.durationHint': '拖动刻度、滚动滚轮或使用方向键',
  'setup.minutes': '分钟',
  'setup.rulerAria': '专注时长（分钟）',
  'setup.preset': '{min} 分钟',
  'setup.strictness': '严格程度',
  'setup.strictnessAria': '会话严格程度',
  'setup.groups': '屏蔽内容',
  'setup.groupsCount': '{n} {word}',
  'setup.catalogMissing': '分组目录不可用。',
  'setup.groupMeta': '{n} {word} · {note}',
  'setup.custom': '自定义网站',
  'setup.customHint': '每次新建会话时都会加入分组',
  'setup.customPlaceholder': '例如 example.com',
  'setup.customAria': '需要屏蔽的网站',
  'setup.add': '添加',
  'setup.checkPlaceholder': '检查域名',
  'setup.checkAria': '检查域名是否被屏蔽',
  'setup.check.blocked': '已屏蔽',
  'setup.check.free': '未屏蔽',
  'setup.check.other': '不匹配任何规则',
  'setup.allow': '例外',
  'setup.allowHint': '始终放行，即使在锁定会话期间',
  'setup.allowPlaceholder': '例如 localhost',
  'setup.allowAria': '例外域名',
  'setup.allowAdd': '放行',
  'setup.summary': '{duration} · {mode}',
  'setup.summaryRules': '{n} {domainWord}，共 {groups} {groupWord}',
  'setup.start': '开始专注',
  'setup.startAria': '开始专注会话',
  'setup.starting': '正在启动…',
  'setup.noRights': '没有管理员权限，屏蔽将无法生效。请以管理员身份重新启动 Focusd。',
  'setup.badDomain': '请输入 example.com 形式的域名 —— 不要带路径和端口',

  'run.title': '专注进行中',
  'run.subtitle.lock': '关闭应用也没用：守护任务会恢复屏蔽。',
  'run.subtitle.now': '现在可以取消。',
  'run.subtitle.wait': '{time} 后可以取消。',
  'run.session': '会话',
  'run.rules': '屏蔽规则',
  'run.blocked': '已拦截请求',
  'run.last': '最近拦截的网站',
  'run.started': '开始',
  'run.ends': '结束',
  'run.rulesAria': '已屏蔽 {n} {word}',
  'run.blockedAria': '已拦截 {n} {word}',
  'run.widget': '显示小组件',
  'run.emergency': '紧急解除屏蔽',
  'run.emergencyTitle': '紧急解除屏蔽',
  'run.emergencyText': '屏蔽将被解除，浏览器恢复正常状态，本次会话会中断。要继续吗？',
  'run.continue': '继续',
  'run.lockedNote': '锁定会话 —— 无法取消',
  'run.cancel': '取消专注',
  'run.cancelAria': '取消专注会话',
  'run.cancelIn': '{n} 秒后可取消',
  'run.cancelInAria': '{n} 秒后可以取消',
  'run.confirmTitle': '中断专注？',
  'run.confirmText': '会话将提前结束，进度不会保存。',
  'run.confirm': '中断',
  'run.stay': '继续专注',
  'run.ofTotal': '共 {time}',

  'widget.pinOn': '保持置顶',
  'widget.pinOff': '取消置顶',
  'widget.back': '返回 Focusd 窗口',
  'widget.backLabel': '显示窗口',
  'widget.idle': '未在专注',
  'widget.stopping': '正在解除屏蔽',
  'widget.free': '空闲时间',
  'widget.until': '至 {time}',

  'settings.title': '设置',
  'settings.close': '关闭设置',
  'settings.section.look': '外观',
  'settings.section.lookHint': 'Focusd 窗口的外观',
  'settings.section.widget': '迷你计时器',
  'settings.section.widgetHint': '显示会话时间的小窗口，可置于其他窗口之上',
  'settings.section.newSession': '默认新建会话',
  'settings.section.guard': '防护',
  'settings.section.guardHint': '屏蔽的方式。修改后需点击“保存”',
  'settings.section.block': '屏蔽内容',
  'settings.section.blockHint': '与分组独立：对所有新建会话生效',
  'settings.section.app': '应用',
  'settings.theme': '主题',
  'settings.themeAria': '主题外观',
  'settings.language': '语言',
  'settings.languageAria': '界面语言',
  'settings.languageHint': '“跟随系统”会使用 Windows 的语言',
  'settings.lang.auto': '跟随系统',
  'settings.lang.ru': '俄语',
  'settings.lang.en': '英语',
  'settings.lang.zh': '中文',
  'settings.shapeCard': '外观与行为',
  'settings.shape': '计时器形状',
  'settings.shapeAria': '迷你计时器形状',
  'settings.shape.bar': '条形',
  'settings.shape.barHint': '进度条与时间同排显示',
  'settings.shape.ring': '环形',
  'settings.shape.ringHint': '时间外圈显示进度环',
  'settings.widgetOn': '在其他窗口之上显示计时器',
  'settings.widgetOnHint': '窗口会缩小为计时器，可用鼠标拖动任意空白处。',
  'settings.widgetOnAria': '在其他窗口之上显示迷你计时器',
  'settings.onTop': '保持置顶',
  'settings.onTopHint': '如果希望计时器像普通窗口一样会被其他窗口覆盖，请关闭。',
  'settings.onTopAria': '保持迷你计时器置顶',
  'settings.duration': '时长',
  'settings.durationHint': '每次新建会话的初始时长',
  'settings.durationAria': '默认会话时长（分钟）',
  'settings.strictness': '严格程度',
  'settings.strictnessAria': '默认严格程度',
  'settings.layers': '防护层',
  'settings.layersHint': 'Focusd 如何屏蔽网站',
  'settings.autostart': '随系统启动',
  'settings.autostartHint': 'Focusd 会在登录系统时启动并立即启用屏蔽。',
  'settings.autostartAria': '登录时自动启动',
  'settings.note.proxy':
    '网站由 Focusd 代理屏蔽：它能看到每个请求并统计被拦截的数量。浏览器通过策略获知代理，' +
    '而不读取策略的浏览器 —— 例如 Yandex 浏览器 —— 则通过 Windows 系统代理。' +
    'Focusd 运行期间，系统代理指向它，退出时会恢复原有设置。如果代理无法启动，' +
    '则改用浏览器屏蔽策略作为备用：它更可靠，但从外部无法观测，因此这种情况下计数并不精确。',
  'settings.note.sessions':
    '锁定会话始终由计划任务守护：如果应用被关闭，它会恢复屏蔽并重新启动 Focusd。' +
    '这类会话无法绕过 —— 只能通过“紧急解除屏蔽”。',
  'settings.note.apps':
    '会被屏蔽的是浏览器以及通过 Windows 系统代理联网的程序。' +
    '使用自有连接方式的应用 —— 例如 Telegram、Steam 等 —— Focusd 不做限制。',
  'settings.custom': '自定义网站',
  'settings.customHint': '每次新建会话时都会加入分组',
  'settings.customPlaceholder': 'example.com',
  'settings.customAria': '要屏蔽的自定义域名',
  'settings.add': '添加',
  'settings.allow': '例外',
  'settings.allowHint': '始终放行，即使在锁定会话期间',
  'settings.allowPlaceholder': 'localhost',
  'settings.allowAria': '例外域名',
  'settings.allowAdd': '放行',
  'settings.about': '版本与数据',
  'settings.dataDir': '数据目录',
  'settings.version': '版本',
  'settings.openDataDir': '打开数据目录',
  'settings.save': '保存',
  'settings.saveAria': '保存设置',
  'settings.saved': '已保存',
  'settings.error': '错误',

  'main.noContainer': '找不到容器 #{id}',
  'main.renderFailed': '无法渲染界面：{err}',
  'main.stateFailed': '无法获取 Focusd 状态：{err}',

  'demo.cancel.wait': '还有 {n} 秒才能取消。',
  'demo.cancel.lock': '锁定会话无法提前中断。',
  'demo.cancel.confirm': '要提前中断严格会话吗？这会被记录到统计中。',
  'demo.session': '专注会话',
  'demo.restored': '紧急恢复：屏蔽已解除。',
  'demo.group.social.title': '社交网络',
  'demo.group.social.note': '信息流、推荐、无限滚动',
  'demo.group.video.title': '视频与流媒体',
  'demo.group.video.note': '视频、直播、音乐',
  'demo.group.shorts.title': '短视频',
  'demo.group.shorts.note': '竖屏短片与短视频',
  'demo.group.games.title': '游戏',
  'demo.group.games.note': '商店与社区',
  'demo.group.messengers.title': '即时通讯',
  'demo.group.messengers.note': '网页版通讯与聊天',
  'demo.group.news.title': '新闻与论坛',
  'demo.group.news.note': '资讯、聚合与讨论区',
  'demo.group.shopping.title': '电商平台',
  'demo.group.shopping.note': '购物与分类信息',
  'demo.group.adult.title': '18+',
  'demo.group.adult.note': '成人内容',
  'demo.group.doomscroll.title': '无尽刷屏陷阱',
  'demo.group.doomscroll.note': '梗图与无尽合集',
};

const dict: Record<Lang, Dict> = { ru, en, zh };

/** Список языков для переключателя — в порядке, в котором он показывается. */
export const LANG_CHOICES = ['auto', 'ru', 'en', 'zh'] as const;
export type LangChoice = (typeof LANG_CHOICES)[number];

/** Подпись языка в переключателе. */
export function langLabel(choice: LangChoice): string {
  return t(`settings.lang.${choice}`);
}
