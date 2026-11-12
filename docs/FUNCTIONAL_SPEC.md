# Функциональная спецификация FarshMac 0.20.0

Документ описывает всё, что приложение делает сейчас, по исходному коду (не по README и parity-документам). Для каждой функции указаны способ вызова, условия доступности, пошаговое поведение, диалоги, точные строки сообщений и файлы исходников. Строки интерфейса приведены дословно на английском, как в коде. Расхождения между кодом и существующей документацией собраны в конце каждой части и сведены в разделе «Сводка расхождений».

Документ составлен 2026-11-11 по состоянию ветки `main` (коммит `b91313a`). При изменении поведения обновляйте соответствующую часть.

> **Обновление (ветка `backlog-fixes`, 2026-11-11).** Все 85 пунктов `docs/IMPLEMENTATION_BACKLOG.md` реализованы, поэтому поведение ряда функций изменилось относительно описанного ниже. Списки расхождений в конце каждой части и раздел «Сводка расхождений» закрыты: решения из раздела «Решения по расхождениям» backlog применены к коду, справке F1, README и parity-документам. Ниже обновлены только самые заметные записи (Tab, Ctrl+PgDn, меню сортировки, закладки, fast find); остальные части описывают состояние на коммит `b91313a` — при расхождении источником истины остаётся код и backlog.

## Содержание

1. [Часть 1. Файловые панели и навигация](#часть-1)
2. [Часть 2. Файловые операции, архивы, запуск программ](#часть-2)
3. [Часть 3. Внутренний просмотрщик и редактор](#часть-3)
4. [Часть 4. Терминал, командная строка, окна, нативная оболочка, renderer](#часть-4)
5. [Часть 5. Меню F9, диалоги, настройки, сетевые подключения, справка](#часть-5)
6. [Сводка расхождений](#сводка-расхождений)
7. [Приложения: дополнения по результатам gap analysis](#приложения)



<a id="часть-1"></a>
# Часть 1. Файловые панели и навигация

Источник истины — код в `internal/ui` (версия, отображаемая в строке меню: `FarshMac 0.20.0`, `internal/ui/draw.go`). Все строки интерфейса приведены дословно на английском. Везде ниже «cmd» означает модификатор Ctrl **или** Cmd (⌘), если явно не сказано иначе (`cmd := m.Ctrl || m.Meta` в `far_keys.go`).

Общие соглашения кода, важные для всего раздела:

- Экран — символьная сетка `Cols × Rows` (ограничения `60..300` × `15..120`, `app.go: Handle("resize")`). Координаты мыши — ячейки сетки.
- Панель — структура `Panel` (`app.go`): `Path`, `Entries []Entry{Name, Info, Dir}`, `Cursor`, `Offset` (индекс первой видимой записи), `Marked map[string]bool`, `Err` (текст, заменяющий футер панели), `panelOptions{View, Sort, Filter, Reverse, CaseSensitive, MixDirectories, SelectedFirst, Hidden, DisableSortGroups}`, `Remote`, `Archive`, `History`, `states` (запомненные курсор/верхняя строка/пометки для каждого посещённого пути).
- Все клавиши панели обрабатываются только когда `PanelKeys == true`: нет открытого документа, формы, диалога, меню и приложение не `Busy` (`Poll()`).
- Порядок обработки клавиш в `App.key()` (важно для конфликтов): Busy → Cmd+N / Cmd+Q → Ctrl+Tab / Cmd+Shift+[ ] → Cmd/Ctrl+O → терминал видим (ничего) → форма → диалог → документ → клавиши Info panel → fast find (печатные без Ctrl/Cmd, не Numpad) → клавиши выделения → закладки папок → Delete/Shift+F8 → Shift+Enter → Cmd+Shift+. → fast find (прочее) → Cmd/Ctrl+Shift+F3/F4 → `farPanelKey` → Cmd/Ctrl+F1/F2/=, Cmd+Shift+\ → Alt+F1/F2/F3/F4/F7/F8/F10 → Shift+F10 → Shift+F4 → F12 → меню → Cmd+цифра / Cmd+буква → F-клавиши → базовая навигация.

---

## 1. Раскладка экрана

#### Строка меню (y = 0)
- **Вызов:** всегда видна в режиме панелей.
- **Поведение:** фон Cyan, текст Black. Секции: `Left` (x=2), `Files` (x=11), `Commands` (x=21), `Options` (x=34), `Right` (x=46). При ширине ≥ 70 справа выводится `FarshMac 0.20.0` (x = W − len − 2). Клик левой кнопкой по имени секции открывает соответствующее выпадающее меню (`click()`: `a.Menu = &MenuState{Column: i}`).
- **Код:** `draw.go: Draw`, `menu.go: menuSections`, `app.go: click`.

#### Панели (y = 1 … Rows−5)
- **Поведение:** обе панели рисуются `drawPanel(g, i, x, 1, w, Rows-4)`. Геометрия из `panelRect(index)`:
  - По умолчанию split = `Cols/2`; при `panelSplit != 0` — `Cols*panelSplit/100` (левая `[0, split)`, правая `[split, Cols)`).
  - Если активная панель в режиме `Detailed` или `Long descriptions` и не скрыта, а неактивная панель **не** показывает `Quick view`/`Info panel`/`Tree panel` (либо скрыта), активная занимает всю ширину (`0, Cols`), другая получает ширину 0 и не рисуется.
  - Скрытая панель (`Hidden`) или `w < 1` — область заливается Gray на Black без рамки.
- Рамка: двойная (`╔═╗ ║ ╚═╝`) LightCyan на Blue.
- **Заголовок панели (y = 1):** строка ` <label> `, центрирована, обрезается слева с `…` до `w-8` символов (`tail`). `label`:
  - локальная: `p.Path`;
  - удалённая: `Remote.Label() + Path` (например `sftp://user@host` + путь);
  - архив: `"[Archive] " + label`;
  - Info panel: `Information`.
  Активная панель: Black на Cyan; неактивная — LightCyan на Blue.
- **Строка заголовка колонок (y = 2):** Yellow на Blue, зависит от режима (см. §2).
- **Строки файлов:** y = 3 … (1 + h − 4), т.е. `rows = Rows − 9` видимых строк на колонку.
- **Разделитель (y = Rows − 6):** `╟───╢`.
- **Футер панели (y = Rows − 5):** строка статуса панели (x+2, ширина w−4):
  - базовая: `"%s in %d items"` где `%s` = `panelSizeLabel(TotalBytes)` → `"<n> bytes"` или человекочитаемое (`1.5M`), `ItemCount` — записей без `..`;
  - если есть пометки: `"%d selected · " + базовая`;
  - если установлен фильтр: `"[" + Filter + "] " + …`;
  - если `p.Err != ""` — вместо всего выводится `Err` (ошибки загрузки, прогресс чтения `Reading folder… Esc stops`, статус дерева и т.д.).
- **Код:** `panel_draw.go: panelRect, drawPanel, loadInfo`.

#### Статусная строка (y = Rows − 3)
- Gray на Black. Выводит `a.Status`; если пусто — подсказка `Tab Switch panel   Space Select   ⌘O Terminal   ⌘⌥0…9 Bookmark`.
- Сбрасывается Esc (при пустой командной строке и отсутствии фоновых чтений). После завершения команды в терминале: `Exit %d · ⌘O panels / terminal`.
- **Код:** `draw.go: Draw`, `app.go: Poll`.

#### Командная строка (y = Rows − 2)
- `promptPath + ">" + Command`; для удалённой панели `promptPath = "local:" + filesystemPath()` (локальная папка терминала). Строка обрезается слева (`tail`) до `Cols−1`; курсор `▌` ставится после текста.
- Ввод: любой печатный символ без Ctrl/Alt добавляется (`default` в `key()`); Space при пустой строке — пометка файла, иначе пробел; Backspace удаляет символ (никогда не выходит из каталога); Esc очищает строку и статус; Enter при непустой строке — `execute()` (выполнение в терминале), при пустой — `enterFile()`. Paste (⌘V) при отсутствии модальных окон добавляет текст без переводов строк (control-символы → пробел).
- История команд: ⌘↑/⌘↓, Ctrl+↑/Ctrl+↓, Ctrl+E/Ctrl+X — предыдущая/следующая (`a.history`, лимит `Settings.HistoryLimit`, по умолчанию 200); Ctrl/⌘+Y — очистить строку; Alt+F8 — меню `Command history` (`Command history is empty` если пусто).
- **Код:** `draw.go: Draw`, `app.go: key, execute`, `far_keys.go`, `commands.go: historyMenu`.

#### Key bar (y = Rows − 1)
- **Вызов:** видим при `Settings.ShowKeys` (по умолчанию true); переключается Ctrl/⌘+B, `Options → General settings → Key bar`, `Interface settings`.
- **Поведение:** 10 ячеек шириной `Cols/10`; номер (Gray на Black) + подпись (Black на Cyan): `Help`, `UserMenu`, `View`, `Edit`, `Copy`, `RenMov`, `MkFold`, `Trash`, `Menu`, `` (10-я пустая). Для удалённой активной панели 8-я подпись — `Delete`. Подписи не меняются при удержании модификаторов.
- Клик левой кнопкой по key bar вызывает `function(min(10, X*10/Cols+1))`; если key bar скрыт — клик игнорируется.
- **Код:** `draw.go: Draw`, `app.go: click`.

#### Цветовые темы
- `Options → Advanced → Colors`: `Classic Far`, `Dark`, `Light`. Dark/Light — пост-обработка палитры всей сетки (Blue→`0x151b24`, LightCyan→`0xc7d7e9`, Cyan→`0x486987`; Light: Blue→`0xf1f3f5`, LightCyan→`0x182b49`, White→Black, Yellow→`0x885000`, Cyan→`0x80b8dd`).
- **Код:** `draw.go: Draw`, `settings.go: settingsMenu("Colors")`.

#### Заголовок окна
- `Update.Title = Panels[Active].Path` (при открытом документе — имя файла документа).
- **Код:** `app.go: Poll`.

---

## 2. Режимы панели (View)

Список режимов (`panel_options.go: panelViews`): `Brief`, `Medium`, `Full`, `Wide`, `Detailed`, `Descriptions`, `Long descriptions`, `File owners`, `File links`, `Alternative full`, `Info panel`, `Tree panel`, `Quick view`. Пустой `View` трактуется как `Full`.

#### Переключение режима через меню Left/Right
- **Вызов:** F9 → `Left` / `Right` → пункты `Brief`, `Medium`, `Full`, `Wide`, `Detailed`, (разделитель), `Info panel`, `Tree panel`, `Quick view`. Текущий режим помечен `✓ ` (при `View == ""` отмечен `Full`). F9 открывает меню на колонке `Left` для левой активной панели и `Right` для правой.
- **Где доступно:** любая панель; ограничения в `setView`.
- **Поведение (`setView(index, view)`):**
  1. Удалённая панель и view ∈ {`Tree panel`, `Info panel`, `Quick view`} → статус `Choose a file view for remote panels`, без изменений.
  2. Архивная панель и `Tree panel` → `Tree view is unavailable inside archives`.
  3. Иначе `View = view`, `Hidden = false`, перезагрузка `Load(Path)`, сброс ключа предпросмотра.
- Режимы `Descriptions`, `Long descriptions`, `File owners`, `File links`, `Alternative full` недоступны из меню (legacy; при загрузке setup/`configurePanel` они принудительно заменяются на `Full`), но код отрисовки для них сохранён.
- **Код:** `menu.go: menuSections`, `panel_options.go: setView`, `settings.go: configurePanel`.

#### Колонки и содержимое по режимам
- Число колонок (`panelColumns`): `Brief` = 3, `Medium` = 2, остальное = 1. Ширина колонки `cw = (w−2)/columns`; между колонками вертикальная `│`.
- Метаданные (`metadata`) рисуются только при 1 колонке, `View != "Tree panel"` и ширине ≥ 12: имя │ extra.
- Заголовок колонок (Yellow): `Name` (многоколоночные и Tree? — нет, см. ниже), `Name / Size / Modified` (одноколоночные по умолчанию: Full, Wide, Alternative full), `Name / Description` (Descriptions, Long descriptions), `Name / Size / UID` (File owners), `Name / Size / Hard links` (File links), `Name / Size / Modified / Accessed / Created / Mode` (Detailed), `Folder tree` (Tree panel).
- Ширина extra-колонки (`panelExtraWidth`, не зависит от значений строк): Full/Alternative full 24 (size 9 + пробел + дата 14), Wide 9, File links 15, File owners 20, Detailed 72, Descriptions/Long descriptions `w/2`; всё ограничено `w/2`. `sizeWidth` = max(9, длина `panelSize(TotalBytes)`, длины подсчитанных размеров папок с учётом префиксов `~`/`>=`).
- Поле extra для строки:
  - по умолчанию (Full): `"%*s %s"` размер + дата `02.01.06 15:04` (`ModTime`);
  - `Wide`: только размер;
  - `Descriptions`: описание из `descript.ion`;
  - `Long descriptions`: размер + описание;
  - `File owners`: размер + UID (10 позиций); `File links`: размер + `Nlink` (5 позиций); если размер не помещается — `humanSize`;
  - `Detailed`: размер, дата, `Mode()` (например `-rw-r--r--`), далее ` A:<atime sec> C:<birthtime sec>`.
- Размер: `<DIR>` для папок (или подсчитанный размер, см. §8), `<LINK>` для символических ссылок, иначе байты или человекочитаемое (`Settings.HumanReadableSizes`, формат `%.1f` + `K/M/G/T`).
- Цвет имени: папки White; при `Settings.Highlight` символические ссылки Cyan, исполняемые (`mode&0111`) `0x55ff55`, архивы по группе масок `<arc>` `0xff55ff`; помеченные — Yellow; строка курсора в активной панели — Black на Cyan (помеченная под курсором — Yellow на Cyan). Маркер пометки в первой ячейке строки: `√`.
- В `Tree panel` вместо имени выводится метка ветки `treeLabels[name]` (`├─`, `└─`, `│ `), `..` — как есть.
- **Код:** `panel_draw.go: drawPanel, panelExtraWidth, panelColumns, panelSize, humanSize`.

#### Quick view (быстрый просмотр)
- **Вызов:** Ctrl+Q (только Ctrl, не ⌘) — переключает **неактивную** панель; `Commands → Quick view on/off`; через меню Left/Right (`setView`) — для любой панели.
- **Где доступно:** Ctrl+Q и меню Left/Right → `Quick view` работают и для удалённой панели (после backlog `setView` разрешает Quick view для remote; Info/Tree по-прежнему требуют локальную папку). Для члена архива показывается `Archive member: press F3 to extract and view`. Ctrl+L (только Ctrl; ⌘L — Change directory) / Ctrl+T (также ⌘T) переключают Info/Tree; отдельных пунктов в Commands для них нет — виды доступны в Left/Right.
- **Поведение (`toggleOtherView("Quick view")`):**
  - если неактивная уже в этом режиме — восстанавливается запомненная пара `{View, Hidden}` из `viewReturns[view]` (вложенные переключения восстанавливаются по цепочке: Brief → Ctrl+L → Ctrl+Q → Ctrl+Q возвращает `Info panel` → Ctrl+L возвращает `Brief`);
  - иначе запоминается текущая пара, ставится `View = "Quick view"`, `Hidden = false`;
  - отменяются фоновые загрузки/дерево неактивной панели, сбрасывается превью (`previewKey`, `previewImage`, отмена `previewOperation`).
- Источник превью — файл под курсором **другой** панели (`updatePreview` в `Poll`, пока не открыт документ/терминал и не Busy). Ключ = путь + размер + mtime; смена ключа во время загрузки отменяет её.
- Текст превью (`previewText`), выводится построчно с y+2, максимум `h−3` строк, без прокрутки (колесо над Quick view игнорируется):
  - удалённая панель-источник: `Remote file: use F3 to download and view`;
  - пока грузится: `Loading…`;
  - папка: `"<path>\nDirectory (preview, up to 100 entries)\n"` + до 100 имён (скрытые пропускаются при `!ShowHidden`), затем `…`;
  - изображение/PDF (`preview.Supported`: `.jpg .jpeg .png .gif .tif .tiff .bmp .heic .heif .webp .pdf`): `"<name>\n<size> bytes · <2006-01-02 15:04>"` + `"\n<FORMAT> · <W> × <H> pixels"` или `"\nPDF · page 1 of <N>"`; при ошибке `"\nPreview unavailable (unsupported, damaged or limit exceeded)"`;
  - обычный файл: первые 16 KiB как текст;
  - ошибка `os.Stat` — текст ошибки (в частности, для членов архива превью не работает: виртуальный путь не существует на диске);
  - после Esc: `Preview cancelled`.
- Изображение: декодируется в отдельном процессе-помощнике (`farshmac --preview-helper <path>`, таймаут 4 с, вывод ≤ 5 MiB, PNG ≤ 3 MiB, thumbnail ≤ 768 px, исходник ≤ 100000×100000 и ≤ 500 Мпикс; для PDF рендерится 1-я страница с учётом поворота). Результат — data-URI PNG, передаётся рендереру как `Update.Preview{X: x+2, Y: 7, W: w−4, H: Rows−13}`; не показывается, если `w < 8` или `Rows < 14`, если источник — удалённая/архивная панель, если открыт документ/меню/форма/диалог/Busy, или пока превью `pending`.
- **Сообщения:** `This panel view requires a local folder` (toggleOtherView для remote с view ≠ Quick view).
- **Код:** `far_keys.go: toggleOtherView`, `commands.go: updatePreview`, `image_preview.go`, `preview/preview.go`, `preview/preview_darwin.go`, `panel_draw.go`.

#### Info panel (информационная панель)
- **Вызов:** Ctrl+L (только Ctrl) — неактивная панель; меню Left/Right → `Info panel`.
- **Где доступно:** локальные панели; для remote `setView` → `Choose a file view for remote panels`, `toggleOtherView` → `This panel view requires a local folder`.
- **Поведение:** заголовок `Information`. Источник — противоположная панель (если она тоже Info panel — сама панель): `path = source.filesystemPath()`; `plugin` = `Remote.Label()+Path` для удалённой или `"Archive: " + Archive.Path` для архивной. Чтение выполняется в одной фоновой горутине на панель (`readInfoPanel`), результат применяется в `Poll`; ключ включает путь, plugin и все настройки Info; повторное чтение того же ключа не раньше чем через 2 с. До первого результата — `Reading information…`.
- Строки (в порядке вывода, заголовки секций рисуются как `─── Title ───`, значения Yellow прижаты вправо):
  - `InfoHost` (`Computer and user`): `Computer name`, `User name`;
  - `InfoVolume` (`Drive information`): секция `Disk <mount> (<fstype>)`; `Total bytes`, `Free bytes` (statfs: `Blocks*Bsize`, `Bavail*Bsize`), `Volume label`, `Volume UUID` (CFURL volume keys), `Device` (`Mntfromname`);
  - `InfoMemory`: секция `Memory`; `Memory used` (`%.0f%%` = (Total − min(Total, Available))/Total), `Installed memory` (`hw.memsize`), `Free physical`, `Available (estimate)` (free+inactive pages), `Compressed`, `Swap total`, `Swap free` (`vm.swapusage`);
  - plugin ≠ "": секция `Filesystem panel` + строка plugin;
  - `InfoPower`: секция `Power status`; `Power source` (IOPS providing type или `Unknown`), `Charge status` (`Charging` / `Critical` <10% / `Low` <33% / `Normal` / `High` >66% / `Unknown` / `No battery`), `Battery` `<n>%` (если ≥ 0), `Time remaining` `<h>h <m>m` (если > 0);
  - `InfoDescription` и plugin == "": секция `Folder description`; перебираются имена из `Settings.FolderDescriptions` (по умолчанию `file_id.diz,README.md,README,readme.txt`; имена с путём пропускаются); первый существующий обычный файл: строка с именем файла, затем до 4096 байт содержимого построчно.
  Числа без human-readable форматируются с разделителем-пробелом по тысячам.
- Прокрутка (`InfoOffset`, панель активна): ↓/↑ на 1, PgDn/PgUp на `Rows−7`, Home/End, колесо мыши (любая панель под курсором, ±12 за событие). Индикаторы `▲`/`▼` на правой рамке.
- Ctrl/⌘+F12 в активной Info panel — форма `Info panel settings` с переключателями `Computer and user`, `Drive information`, `Memory information`, `Folder description`, `Power status` (сбрасывает кеш `infoKeys`).
- F3 / F4 в активной Info panel — открыть файл описания (первый кандидат, даже если ещё не существует? — нет: используется `infoDescriptionPaths`, который указывает на найденный файл либо на первого кандидата) через `viewPlan` (просмотр/редактирование).
- Мышь: клик по Info panel делает её активной; если строка относится к секции `Folder description` и есть путь описания: левая кнопка — просмотр, правая/Control-click — редактирование.
- Клавиши выделения, fast find через Numpad и т.п. в Info panel не действуют (`selectionKey` возвращает false; `infoPanelKey` перехватывает стрелки/PgUp/PgDn/Home/End/F3/F4 раньше остального).
- **Код:** `info_panel.go`, `sysinfo/info_darwin.go`, `settings.go: settingsMenu("Info panel settings")`.

#### Tree panel (дерево папок)
- **Вызов:** Ctrl/⌘+T — неактивная панель; меню Left/Right → `Tree panel`.
- **Где доступно:** только локальные панели вне архивов: `Tree view is unavailable inside archives`, `This panel view requires a local folder` / `Choose a file view for remote panels`.
- **Поведение:** `Load(path)` в этом режиме запускает фоновый скан (`loadTree → startTree → scanTree`) с корнем `path`:
  - глубина `Settings.TreeDepth` (1..12, по умолчанию 4; `Options → Advanced → Tree settings`, подсказка `Maximum levels for folder tree (1–12)`, ошибка `Enter a number from 1 to 12`);
  - лимиты: 50000 узлов (`treeNodeLimit`), 1 000 000 просмотренных записей (`treeEntryLimit`); чтение батчами `ReadDir(256)`; символические ссылки не обходятся; обычные файлы не stat-ятся; скрытые и `.DS_Store` фильтруются по настройкам панели (с учётом `ForcedVisiblePath`); подпапки сортируются текущими настройками сортировки панели;
  - во время скана футер: `Reading tree… Esc stops`, затем `Reading tree… %d entries · Esc stops`;
  - по завершении: записи — относительные пути (`a`, `a/b`), первая строка `..` (если корень не `/`), `ItemCount` = число папок, `TotalBytes = 0`; футер (поле `Err`, остаётся постоянно): `"%d folders · depth %d"`, при лимите `Partial tree · limit reached; open a subfolder`, при отмене `Stopped · partial tree`, при ошибке корня — текст ошибки; при пропусках ` · %d unreadable`.
  - Метки: `├─name` / `└─name` с префиксами `│ ` / `  `; управляющие символы имени экранируются (`\n`, `\r`, `\t`, `\uXXXX`).
- Enter на узле — `enterFile` → `Load(Join(Path, rel))` → дерево перестраивается с новым корнем; Enter на `..` — корень на уровень выше. Ctrl+PgUp/PgDn аналогично.
- Пометки Insert/Space/Shift+стрелки работают по именам узлов; Num+/−/* и Ctrl+M — нет (`selectionKey` отключён для Tree).
- Esc во время скана: `Stopping tree scan…`.
- Alt+F10 (`Commands → Folder tree`) — то же сканирование в фоновой задаче `Reading folder tree` с модальным списком `Folder tree` (или `Folder tree (partial)`), первая строка — корень; выбор загружает папку в активную панель. Недоступно для remote (`This command requires a local panel. Use F5 to copy remote files first.`).
- **Код:** `tree.go`, `commands.go: treeMenu`, `far_keys.go: toggleOtherView`.

#### Detailed / Long descriptions — полноэкранная панель
- При активной панели в `Detailed`/`Long descriptions` вторая панель не рисуется (ширина 0), кроме случаев, когда она показывает Quick view/Info/Tree (тогда обычный split). Клик мышью в область с шириной 0 не попадает ни в одну панель.
- **Код:** `panel_draw.go: panelRect`.

#### Human-readable sizes
- **Вызов:** ⌘/Ctrl+Shift+S; `Options → Panel settings → Human-readable sizes (off = bytes)`.
- **Поведение:** `Settings.HumanReadableSizes` переключается, `configurePanel` для обеих панелей без перечитывания; размеры в строках и футер пересчитываются сразу (`drawPanel` читает `Settings.HumanReadableSizes`), устаревает только неотображаемый `InfoText`.
- **Код:** `far_keys.go`, `panel_draw.go`.

---

## 3. Загрузка каталога и состояние

#### Panel.Load (общий механизм)
- **Поведение:**
  1. Путь приводится к абсолютному. Remote → `loadRemote` (или асинхронно `loadPanel`); Archive и путь внутри архива → `loadArchiveDirectory`; `View == "Tree panel"` → `loadTree`; иначе локальное чтение (`fsops.ReadDirectory`), в приложении всегда асинхронно (`Async == true`, `NewAsync`), с футером `Reading folder… Esc stops` и операцией `Reading folder`.
  2. Фильтрация при чтении: `.DS_Store` при `IgnoreDSStore`; скрытые (имя с `.` или флаг `UF_HIDDEN` 0x8000) при `HideHidden`, кроме `ForcedVisiblePath` (папка, созданная F7); для не-папок — маска панели `Filter` (`matchesMask`). Символические ссылки на папки считаются папками (`os.Stat` цели).
  3. `beginLoad`: сохраняет состояние предыдущего пути в `states[prev]` (имя под курсором, верхняя строка, пометки), восстанавливает состояние нового пути; **при переходе в родителя курсор всегда ставится на только что покинутую папку** (даже если у родителя было сохранённое состояние); путь добавляется в `History` панели (уникально, лимит 200); первой записью добавляется `..` (если `Dir(abs) != abs`).
  4. Читаются описания `descript.ion` (`Settings.DescriptionFile`), сортировка (`sortEntries`), `installEntries` (восстановление курсора/верхней строки по именам, пометок только для существующих имён; курсор клампится, `Offset ≤ Cursor`), `loadInfo` (`TotalBytes` — сумма размеров файлов, `ItemCount`, `InfoText`).
  5. Ошибка асинхронной загрузки: панель откатывается к предыдущему состоянию (`previous`), `Err` = текст ошибки (виден в футере панели). Удачная загрузка при ошибке синхронного `Load` — `a.error(err)` → строка статуса.
- Esc при активном чтении: `Stopping folder scan…` (отмена операции), Esc при этом не очищает командную строку.
- При асинхронной загрузке запросы позиции курсора (F7, matchOtherPanel) записываются в ожидающее состояние (`pendingPosition`).
- **Код:** `app.go: Load, beginLoad, installEntries`, `panel_load.go`, `hidden_darwin.go`, `remote.go: loadRemote`, `archive_panel.go: loadArchiveDirectory`.

#### Обновление панели (Re-read)
- **Вызов:** ⌘R или Ctrl+R (активная панель); меню `Left/Right → Re-read ⌘R` (конкретная панель).
- **Поведение (`rereadPanel`):** сбрасывает ключи Info panel; для архива — повторно открывает индекс архива (`openArchive`), иначе `Load(Path)`. Курсор/пометки сохраняются по именам.
- Полный `refresh()` обеих панелей происходит после возврата из терминала (⌘O), смены настроек панели, F7, создания ссылки и т.д.
- **Код:** `archive_panel.go: rereadPanel`, `app.go: refresh`.

---

## 4. Навигация клавишами

#### Tab — смена активной панели
- **Вызов:** Tab.
- **Поведение:** `Active = 1 − Active`; скрытая панель пропускается, при обеих скрытых активная панель показывается (P1-01). На Quick view / Info / Tree / скрытой панели файловые клавиши (Enter, F3–F8, Insert и т. п.) не действуют, статус `Switch to a file panel first (Tab)`.
- **Код:** `app.go: key`.

#### Стрелки ↑/↓
- Курсор ±1 с ограничением `[0, len−1]`. С Ctrl — история команд (см. §1). В Info panel — прокрутка. При открытом fast find — следующий/предыдущий match.
- **Код:** `app.go: key`.

#### ←/→
- Только в многоколоночных режимах (`Brief`, `Medium`): курсор ± `Rows−9` (одна колонка), с ограничением. В одноколоночных — ничего (командная строка не редактируется стрелками).
- С Ctrl/⌘ при пустой командной строке — ширина панелей (§12).
- **Код:** `app.go: key`.

#### PgUp / PgDn
- Курсор ± `(Rows−9) × columns` (страница), ограничение по границам. Shift+PgUp/PgDn — выделение диапазона (§5). Ctrl/⌘+PgUp/PgDn — родитель/вход (ниже).
- **Код:** `app.go: key`.

#### Home / End
- Курсор на 0 / `len−1`. Shift — выделение; в Info panel — прокрутка.

#### Enter — открыть
- **Вызов:** Enter при пустой командной строке; двойной клик левой кнопкой; Enter в fast find.
- **Поведение (`enterFile`):**
  - удалённая панель: папка → `Load`, файл → `openRemoteDocument(false)`;
  - архив: `enterArchiveMember` (§10);
  - локальная: `os.Stat` (ошибка → статус); папка с расширением `.app` → `/usr/bin/open <path>`; другая папка → `Load`; архив по `fsops.ArchiveFormat` (`.tar.gz .tgz .tar.bz2 .tbz2 .tbz .tar.xz .txz .tar .zip .jar .war .7z .rar .cpio`) → `openArchive`; ассоциация с `Command` по маске → запуск в терминале; обычный исполняемый файл (`mode&0111`) → выполняется в терминале как команда (`terminal.Quote(path)`); иначе → `/usr/bin/open <path>` (задача `Opening <name>`).
- На `..` — `Current()` = родитель → `Load(parent)`.
- **Код:** `launch.go: enterFile, externalPlan, launch`.

#### Ctrl/⌘+PgUp — в родительский каталог
- **Поведение:** `Load(filepath.Dir(Path))`. В корне архива выводит из архива в папку с архивом (путь оказывается вне архива → локальная загрузка, `Archive = nil`). Для remote — родитель на сервере. Курсор встаёт на покинутую папку.
- **Код:** `far_keys.go`.

#### Ctrl/⌘+PgDn — войти в папку/архив
- **Поведение:** если под курсором папка или `fsops.ArchiveFormat(name) != ""` — `enterFile()`. Входит ровно в поддерживаемые форматы (в т. ч. `.war .cpio .tbz .txz`); одиночные `.gz/.bz2/.xz` без `.tar` архивами не считаются.
- **Код:** `far_keys.go`, `launch.go`.

#### Backspace
- Удаляет последний символ командной строки; при пустой строке ничего не делает (не выходит в родителя). В fast find — удаляет символ запроса.

#### Cmd+L — Change directory
- **Диалог:** заголовок `Change directory`, текст `Enter a directory`, поле = текущий путь, кнопки `OK`/`Cancel`.
- **Поведение:** `expand(s, Path)`: `~`/`~/…` → домашний каталог; относительный путь — относительно текущего; `Load`; ошибка → статус.
- **Код:** `app.go: gotoPrompt, expand`.

#### Alt+F1 / Alt+F2 — выбор тома
- **Вызов:** Alt+F1 (левая), Alt+F2 (правая); меню `Left/Right → Change volume… AltF1/AltF2`; `Commands → Mounted volumes` (активная).
- **Диалог:** список `Volumes and home`: `/`, домашний каталог, затем все записи `/Volumes/*`.
- **Поведение:** выбор делает панель активной, сбрасывает `Remote = nil` (архив при этом не сбрасывается явно, но `Load` локального пути вне архива обнуляет `Archive`), загружает путь.
- **Код:** `panel_options.go: volumes`.

#### ⌘= / ⌘Shift+\ (Ctrl варианты) — та же папка в другой панели
- **Вызов:** Ctrl/⌘+`=`; Ctrl/⌘+Shift+`\`/`|`; `Commands → Same folder in other panel ⌘=`.
- **Поведение (`matchOtherPanel`):** целевой панели копируются `Remote`, `RemoteLocalPath`, `Archive` источника; если она была в Tree/Info/Quick view — переключается в `Full`; `Load(src.Path)`; при ошибке прежние Remote/Archive восстанавливаются, статус = ошибка; курсор ставится на то же имя, что в источнике (или в pending-позицию при асинхронной загрузке); `Hidden = false`.
- **Код:** `archive_panel.go: matchOtherPanel`.

#### ⌘U / Ctrl+U — поменять панели местами
- Меняет структуры `Panels[0]` и `Panels[1]` (включая режимы, пути, пометки); активный индекс не меняется (фокус переходит на другую папку). Сбрасывает превью.
- **Код:** `app.go: key`, `menu.go`.

#### F7 — новая папка (навигационная часть)
- **Диалог:** `Make folder`, `Folder name`, пусто; для remote — `remoteMkdir`. В архиве — запрещено (`Archive is read-only. Use F5 to copy files out.`).
- **Поведение:** `os.MkdirAll(expand(s, base), 0755)`; ошибка → подтверждение `Error` / `Cannot create folder\n<path>\n\n<err>` с кнопками `Retry`, `Cancel`; успех → `refresh()` + `focusCreatedDirectory`: `ForcedVisiblePath = path` (созданная скрытая папка показывается даже при скрытых файлах), загружается родитель созданной папки, курсор на неё (или pending), панель становится активной; если не найдена — `Folder created; hidden by panel settings`.
- **Код:** `app.go: function(7)`, `launch.go: focusCreatedDirectory`.

#### Folder history (история папок)
- **Вызов:** Alt+F12; `Commands → Folder history`.
- **Поведение:** объединяет `a.folderHistory` и `History` обеих панелей (уникально, лимит `HistoryLimit`), показывает в обратном порядке (последние сверху) в списке `Folder history`; выбор → `Load` в активную панель. История сохраняется в setup.json (`Folders`) при `Save setup`.
- **Код:** `commands.go: historyMenu("Folders")`, `settings.go: saveSetup`.

#### View/edit history
- **Вызов:** Alt+F11; `Commands → File view/edit history`. Список `View/edit history` (`View <path>` / `Edit <path>`), пусто → `View/edit history is empty`.

#### Find file / Find folder (навигация к результату)
- **Вызов:** Alt+F7 / `Commands → Find file…`, `Find folder…`. Только локальные панели.
- **Диалог:** `Find file` / `Find folder`: `Name masks` (`*`), `Containing text`, `Search subfolders` (вкл.), `Case sensitive text`; кнопка `Find`.
- **Поведение:** поиск от текущей папки (лимит 10000 совпадений, задача `Searching <root>`); результат — список `"<title> — %d found, %d unreadable"` (+ ` (limit)`), относительные пути; выбор → `revealPath`: папка → загрузить её, файл → загрузить родителя и поставить курсор (или `File is hidden by the current panel filter`). Нет совпадений: `No matches (%d unreadable paths)`.
- **Код:** `commands.go: findFiles, revealPath`.

#### Screens (F12, Ctrl+Tab)
- F12 — список `Screens`: `Panels` + открытые документы (`View `/`Edit `, `* ` для изменённых). Ctrl+Tab / Ctrl+Shift+Tab, ⌘Shift+] / ⌘Shift+[ — циклическое переключение между панелями и документами.
- **Код:** `document.go: screenMenu, cycleScreen`, `app.go: key`.

---

## 5. Выделение файлов

Общее: `..` никогда не помечается. `changeSelection` перед изменением сохраняет `PreviousMarked` и `RestoreMarked`. Папки включаются в массовые операции только при `Settings.SelectFolders` (`Panel settings → Select folders with file masks`), если не указано иное. Пометки хранятся по именам и восстанавливаются при возврате в папку (`states`).

#### Insert / Space — пометить текущий
- **Вызов:** Insert; Space при пустой командной строке.
- **Поведение (`mark`):** инвертирует пометку текущей записи (кроме `..`), курсор вниз на 1 (ограничен последним элементом). Работает в любом файловом режиме, включая Tree panel.
- **Код:** `app.go: mark`.

#### Shift+↑/↓/PgUp/PgDn/Home/End — выделение диапазона
- **Поведение:** целевая позиция вычисляется как при обычной навигации (шаг страницы `(Rows−9)×columns`); значение пометки `on = !Marked[текущий]`; все записи от старой позиции до целевой **не включая целевую** получают `on`; `RestoreMarked` = прежние пометки; курсор на целевую. В Tree panel тоже работает.
- **Код:** `far_keys.go: farPanelKey`.

#### Num + / Num − — Select/Deselect group по маске
- **Вызов:** NumpadAdd / NumpadSubtract (всегда), либо клавиши `+`/`-` основной клавиатуры при пустой командной строке; `Files → Select group… Num +`, `Deselect group… Num −`.
- **Где доступно:** не в Quick view/Info/Tree.
- **Диалог:** `Select group` / `Deselect group`, текст `Masks: *.go;*.md | *_test.go (exclude)`, значение по умолчанию `*`, `OK`/`Cancel`.
- **Поведение:** `validateMasks` (ошибка `unknown mask group` или ошибка `filepath.Match`); маски через `,`/`;`, `*.*` ≡ `*`, регистронезависимо, часть после `|` — исключения, группы `<name>` разворачиваются из `Settings.MaskGroups`. Совпавшие файлы помечаются/снимаются, прочие не меняются.
- **Код:** `selection_keys.go`, `panel_options.go: selectGroup, matchesMask, validateMasks`.

#### Num * — инвертировать
- **Вызов:** NumpadMultiply или `*` при пустой строке; `Files → Invert selection Num *`.
- **Поведение:** инвертирует файлы; папки включаются при `SelectFolders`, либо при Ctrl/⌘ или Alt; при Alt+Num* папки **снимаются** (`return false`), файлы инвертируются.
- **Код:** `selection_keys.go`.

#### Ctrl/⌘+Num +/− — по расширению; Alt+Num +/− — по базовому имени
- **Поведение:** берётся текущая запись (если `..` — ничего); сравнивается `strings.ToLower(ext)` (cmd) или `lower(name без ext)` (Alt); совпавшие помечаются (+) / снимаются (−); папки при `SelectFolders`.

#### Shift+Num +/− — все файлы
- Только с Numpad-кодами: пометить/снять все (папки при `SelectFolders`).

#### Ctrl/⌘+M — Restore selection
- Меняет местами `Marked` и `RestoreMarked` (повторное нажатие возвращает обратно). Также `Files → Restore selection ⌘M`.
- **Код:** `panel_options.go: selectGroup("Restore")`.

#### Compare folders
- `Commands → Compare folders`: в обеих панелях (пометки сохраняются в `RestoreMarked`) помечаются файлы, которых нет напротив, отличающиеся размером или более новые. Статус: `Marked files missing, different in size, or newer than the opposite panel (non-recursive)`.
- **Код:** `commands.go: compareFolders`.

#### Копирование имён в буфер (Ctrl+Insert и др.)
- **Вызов:** Ctrl/⌘+Insert; Alt+Shift+Insert; Ctrl+Alt+Shift+Insert. Также ⌘C при пустой командной строке (`copyPanelCommand`).
- **Поведение:** если командная строка непуста и нет Shift/Alt — копируется командная строка. Иначе: имена выбранных (`Selected()`: помеченные, либо текущий, если он не `..`) через `\n`; с Alt — полные пути; с Alt+Ctrl — пути с разрешёнными симлинками (`EvalSymlinks`, ошибка → статус) или `Remote.Label()+path` для remote. Если ничего нет — путь панели.
- **Код:** `far_keys.go`, `mouse.go: copyPanelCommand`.

---

## 6. Сортировка

#### Ctrl/⌘+F3…F11 — режим сортировки
- F3 `Name`, F4 `Extension`, F5 `Modified`, F6 `Size`, F7 `Unsorted`, F8 `Created`, F9 `Accessed`, F10 `Description`, F11 `Owner`. Повторное нажатие того же режима переключает `Reverse`; новый режим сбрасывает `Reverse = false`. Затем `Load(Path)`.
- Для remote режимы `Created`, `Accessed`, `Owner`, `Description` → `This sort mode requires local file metadata`.
- Примечание: ⌘+Shift+F3/F4 перехватываются раньше (принудительный внутренний просмотр/редактор), ⌘+Alt+F3 — подсчёт размеров.
- **Код:** `far_keys.go`.

#### Ctrl/⌘+F12 — меню сортировки
- **Диалог:** список `Sort mode`: `Name`, `Extension`, `Size`, `Modified`, `Reverse order`, `Case sensitive`, `Directories first`, `Selected first`, `Unsorted`, `Created`, `Accessed`, `Description`, `Owner`. Пункты 4–7 — переключатели; пункт `Directories first` переименован в `Mix directories and files` (переключает `MixDirectories`). Текущий режим и включённые переключатели отмечены `✓ `. Для remote-панели режимы, требующие локальных метаданных, отклоняются (`This sort mode requires local file metadata`). Также `Left/Right → Sort modes…`.
- В активной Info panel Ctrl+F12 открывает `Info panel settings`.
- **Код:** `panel_options.go: sortMenu`.

#### Shift+F12 — Selected first; Shift+F11 — Sort groups on/off
- Shift+F12 переключает `SelectedFirst`, Shift+F11 — `DisableSortGroups`; оба перечитывают панель.

#### Правила сравнения (`sortEntries`)
1. `..` всегда первым.
2. Папки перед файлами, если `!MixDirectories`.
3. `SelectedFirst`: помеченные раньше.
4. Группы сортировки (`Settings.SortGroups`, маски через `;` в порядке приоритета; `Options → Advanced → Sort groups`, подсказка `Masks in priority order, separated by semicolons`; `Commands → Edit sort groups…`): при `!DisableSortGroups` и режиме ≠ `Unsorted` индекс первой совпавшей маски (несовпавшие — 10000).
5. `Unsorted` — порядок чтения каталога (стабильная сортировка).
6. Ключ режима: `Created`/`Accessed` (Birthtime/Atime sec+nsec), `Owner` (UID), `Description` (строка описания), `Extension` (lower ext), `Size` (`entrySize`: для папок — подсчитанный размер или 0), `Modified` (ModTime).
7. Равенство → имя (регистронезависимо, если `!CaseSensitive`).
8. `Reverse` инвертирует результат шагов 6–7 (не влияет на `..`, папки/файлы, selected first, группы).
- **Код:** `panel_options.go: sortEntries`, `sort_stat.go: compareStat`, `folder_sizes.go: entrySize`.

---

## 7. Фильтры, маски, группы масок, скрытые файлы

#### File panel filter
- **Вызов:** `Commands → File panel filter…` (активная панель).
- **Диалог:** `File panel filter`, текст `Include masks | exclude masks; empty shows all`, значение — текущий фильтр.
- **Поведение:** `validateMasks`; `Filter` применяется только к файлам (папки видны всегда) при каждой загрузке — локальной, удалённой, архивной; в футере панели показывается `[<filter>]`. Фильтр сохраняется в setup (`panelOptions`).
- **Код:** `panel_options.go: filterPanel, matchesMask`.

#### Groups of file masks
- **Вызов:** `Options → Advanced → Groups of file masks`.
- **Диалог:** список `Add group…` + `name = masks`; форма `Mask group`: `Name`, `Masks (empty deletes)`. Ошибки: `Invalid group name` (пусто или содержит `<>,;|`), `Use literal masks in groups` (вложенные `<>`), ошибки масок.
- По умолчанию: `arc` = `*.zip,*.tar,*.gz,*.bz2,*.xz,*.tgz,*.tbz,*.tbz2,*.txz,*.7z,*.rar,*.jar,*.war,*.cpio`, `source` = `*.go,*.c,*.h,*.js,*.ts,*.py`, `exec` = `*.sh,*.command`. Используются в фильтре, выделении, sort groups, ассоциациях и подсветке (`<arc>`).
- **Код:** `settings.go`, `panel_options.go: expandMasks`.

#### Скрытые файлы
- **Вызов:** ⌘Shift+. (нативно перехватывается, keyCode 47), Ctrl+H; `Options → Panel settings → Show hidden files (Cmd+Shift+.)`.
- **Поведение:** переключает `Settings.ShowHidden` (по умолчанию true), сбрасывает `ForcedVisiblePath`, `configurePanel` + `refresh()` обеих панелей; курсор остаётся на том же имени. Скрытыми считаются имена с `.` и файлы с флагом `UF_HIDDEN` (для локальных; в архивах и remote — только по точке). Настройка сохраняется в setup (это не «display only» состояние).
- **Код:** `app.go: key`, `far_keys.go`, `hidden_darwin.go`, `settings.go`.

#### .DS_Store
- `Options → Panel settings → Hide and ignore .DS_Store` (`Settings.IgnoreDSStore`, по умолчанию false): `.DS_Store` скрывается в панелях, дереве, превью папок, а также исключается при подсчёте размеров (`sizeKey` включает флаг, поэтому кеш размеров раздельный) и в операциях (`o.IgnoreDSStore`).
- **Код:** `app.go: Load`, `tree.go`, `folder_size_cache.go`.

#### Panel settings (полный список)
- Форма `Panel settings`: `Show hidden files (Cmd+Shift+.)`, `Highlight files`, `Human-readable sizes (off = bytes)`, `Hide and ignore .DS_Store`, `Select folders with file masks`. Применение → `refresh()`.

---

## 8. Подсчёт размеров папок

#### F3 на папке — размеры помеченных/текущей
- **Вызов:** F3 / ⌘3 / Numpad5, когда под курсором папка; `Files → View / folder size F3`.
- **Где доступно:** локальные, удалённые (1 воркер) и архивные панели; не при `Busy`.
- **Поведение (`calculateFolderSizes(false)`):** список = помеченные папки; если нет — папка под курсором; на `..` без пометок считается **текущая** папка, результат показывается в строке `..` вместо `<DIR>`. Фоновая задача с прогрессом `Calculating folder sizes` (`ScanProgress` каждые 64 записи), до 4 воркеров (`min(4, n)`), для remote — 1. Обход: локально `fsops.WalkDir`, архив — по индексу, remote — рекурсивно (глубина ≤ 128: `remote directory depth exceeds 128`; ≤ 100000 записей в папке: `remote folder exceeds 100000 entries`; небезопасное имя: `server returned an unsafe filename`). Для каждой папки считаются `Bytes`, `Files`, `Dirs`, `Unreadable` (ошибки обхода/Info).
- Применение результата только если панель всё ещё на том же пути/источнике: результаты сливаются в `folderSizes`, панель пересортировывается (курсор/верх по именам сохраняются), статус `Calculated %d folders`, при нечитаемых `; %d unreadable entries (>= means partial size)`, при остановке ` · <ошибка>`. Отмена (Esc) сохраняет уже завершённые папки (`applyCompleted = true`).
- Отображение: в колонке размера вместо `<DIR>` — число; `>=` перед частичным (есть `Unreadable`), `~` перед устаревшим (кеш). Подсчитанные размеры участвуют в сортировке `Size`. Пометки не меняются.
- **Код:** `folder_sizes.go`, `launch.go: openConfigured`.

#### ⌘Option+F3 (Ctrl+Alt+F3) — все папки
- `calculateFolderSizes(true)`: все папки панели (кроме `..`), независимо от пометок. `Files → Calculate all folder sizes ⌘⌥F3`.

#### Кеш размеров
- При каждом `beginLoad` (`loadFolderSizes`): ключ `{Path, Source, IgnoreDSStore}` (`Source` = `local`, `Remote.Label()` или `archive:<generation>:<path>` — переоткрытый архив получает новое поколение и кеш не переиспользуется). При смене ключа текущая карта сохраняется в историю снапшотов (если ≤ 16384 записей и ≤ 2 MiB оценки), а для нового ключа восстанавливается сохранённая карта; все восстановленные значения помечаются `Stale` (`~`). Лимиты истории: 128 каталогов, 16384 записей суммарно, 2 MiB суммарно; вытесняются самые старые. Повторная загрузка того же пути (Re-read) также помечает значения `~`. После установки списка `pruneFolderSizes` удаляет размеры для исчезнувших папок. Ничего не сохраняется на диск.
- **Код:** `folder_size_cache.go`.

---

## 9. Закладки окна (bookmarks)

Десять слотов 0–9 на экземпляр окна: закладка папки (панель) или документа (просмотрщик/редактор). Не сохраняются, другим окнам не видны; при ⌘N новое окно начинает с пустыми слотами. Поля `Settings.Shortcuts/ShortcutRemotes/ShortcutArchives` остались только для чтения старых `setup.json` и не используются.

#### ⌘⌥0…9 — сохранить папку
- **Вызов:** ⌘⌥ + цифра (те же клавиши, что в просмотрщике/редакторе), не Numpad; цифра берётся из кода `DigitN`, раскладка и Option не мешают. Варианты с Shift и Ctrl не действуют (macOS забирает ⌘⇧3/4/5 под скриншоты и Ctrl+цифра под рабочие столы). ⌘1…9 больше не дублируют F1…F9. `Commands → Bookmarks… → Save current folder…` → диалог `Save bookmark`, `Slot 0–9`, значение `1`, ошибка `Choose 0–9`.
- **Поведение:** `Bookmarks[n] = {Kind: folder, Path, Cursor: имя файла под курсором (не `..`)}`; для remote — копия конфигурации без пароля/пассфразы/host key с `Directory = Path`; для архива — `Archive = Archive.Path`. Статус `Bookmark %d saved: <path/file>`.
- **Код:** `bookmarks.go: bookmarkKey, panelBookmarkKey, saveFolderBookmark`.

#### ⌘0…9 — перейти
- **Поведение:** пустой слот → `Bookmark %d is empty`. Закладка документа → см. раздел «Cmd+0..9 / Cmd+Option+0..9 — закладки» (открывает просмотрщик/редактор). Закладка папки: из документа сначала показываются панели (экраны документов остаются открытыми). Remote: если какая-то панель уже подключена к тому же серверу — переподключение активной панели в сохранённый каталог, иначе диалог соединения с заполненными полями. Архивная → `openArchiveAt(archive, path, cursor)`. Локальная: проверка `os.Stat` (ошибка → статус), не папка → `Bookmark is not a directory`; затем отмена загрузок, `Remote = nil`, `Archive = nil`, `loadSource(path)`. После загрузки курсор ставится на запомненный файл (`placeCursor`; для асинхронной загрузки через `pendingPosition`); если файла нет — курсор как обычно. Запомненные позиции курсора и пометки (`states`) сохраняются.
- Меню `Bookmarks`: `Save current folder…`, затем `0  <label>` … `9  <label>`: папка — `<путь>/<файл>` (для remote с префиксом `Label()`), документ — `Edit|View <путь>:<строка>`, справка — `Help`.
- **Код:** `bookmarks.go: goBookmark, goFolderBookmark, bookmarksMenu`.

---

## 10. Fast find (быстрый поиск по префиксу)

#### Alt+символ — начать поиск
- **Вызов:** Option+печатный символ (Cocoa переводит символ по текущей раскладке без Option-трансформации, `panel_keys_darwin.h`; Numpad `*`, `+`, `-` сохраняют коды). Также вставка текста (⌘V) при открытом окне поиска.
- **Поведение (`fastFindText`):** символы добавляются по одному; для каждого префикса ищется первое совпадение `filepath.Match(lower(query)+"*", lower(name))` от текущего курсора вперёд по кругу (кроме `..`); если префикс не найден и запрос уже не пуст — ввод останавливается на первом несовпавшем символе (первый символ принимается всегда). Управляющие символы завершают ввод. Запрос может содержать `*` и `?` (подстановочные знаки); `[` и `\` трактуются буквально; сравнение с NFC-нормализацией.
- Окно: рамка Black на Gray, 3 строки, ширина `min(40, W−4)`, у нижнего края (`y = H−7`), слева для левой панели и справа для правой; заголовок ` Find `, поле Black на Cyan с хвостом запроса и `▌`.
- Клавиши при открытом поиске: печатные без Ctrl/⌘ — добавить; Backspace — удалить символ и повторить поиск; ↓/↑ или ⌘Enter/Ctrl+Enter / ⌘Shift+Enter — следующее/предыдущее совпадение по кругу; Enter — закрыть и `enterFile()`; Esc — закрыть (позиция сохраняется); любая другая клавиша закрывает поиск и обрабатывается обычным образом. Клик мышью, Delete, Shift+Enter, ⌘Shift+., Num+/−/*, закладки также закрывают поиск.
- Приоритет: печатные Alt-символы и уже открытый поиск обрабатываются раньше клавиш выделения, кроме явных Numpad-кодов.
- **Код:** `fastfind.go`, `app.go: key, Handle("paste")`, `internal/native/panel_keys_darwin.h`.

---

## 11. Архивы в панели

#### Enter на архиве — открыть архив как папку
- **Поведение (`openArchive`):** фоновая задача `Opening archive` читает индекс (`fsops.ReadArchiveIndex`); затем `Remote = nil`, `Archive = catalog`, режим Tree/Info/Quick view заменяется на `Full`, `Load(target)` (если целевой путь не каталог архива — корень). Если панель уже показывает этот же архив, сохраняется текущий подкаталог (используется Re-read).
- Внутри: заголовок `[Archive] <path>`; записи — члены каталога (`Children[rel]`), с учётом `.DS_Store`/скрытых (по точке)/фильтра; `TotalBytes` — сумма размеров членов, `ItemCount`; `InfoText` = `Archive (read-only)\nF5 extracts selected files/folders\n.. exits the archive`; ошибки `outside archive`, `not an archive directory`.
- Enter в архиве (`enterArchiveMember`): папка → `Load`; вложенный архив → `Copy the nested archive out with F5 to open it`; файл → `viewArchiveMember` (извлечение во временный каталог `farshmac-archive-view-*`, задача `Opening archive member`, документ read-only; симлинк → `Use F5 to copy the archive link`).
- `..` в корне архива или Ctrl+PgUp → путь вне архива → обычная локальная загрузка (курсор на файле архива).
- F4/F6/F7/F8 → `Archive is read-only. Use F5 to copy files out.`; Shift+Enter на члене → `Use F5 to copy this archive item out before opening it with macOS`.
- `filesystemPath()` для архива — папка, содержащая архив (используется терминалом и командной строкой).
- Закладки и matchOtherPanel переносят `Archive`.
- **Код:** `archive_panel.go`, `launch.go: enterFile`, `fsops/formats.go`.

---

## 12. Вставка имён/путей в командную строку

#### ⌘Enter / Ctrl+J — имя текущего файла
- **Поведение (`insertFilename`):** имя записи под курсором (включая `..`); если содержит управляющие символы → `Cannot insert a filename containing control characters`; имя с ведущим `-` получает префикс `./`; если есть символы вне `[A-Za-z0-9_./-]` — `terminal.Quote`; перед вставкой добавляется пробел, если строка не оканчивается пробелом/табом; после имени — пробел. Команда не выполняется. Также `Commands → Insert filename ⌘Enter`. При открытом fast find ⌘Enter — следующее совпадение.
- **Код:** `app.go: insertFilename`.

#### Ctrl/⌘+F — полный путь текущего файла; Ctrl/⌘+; — текущего файла другой панели
- `appendPanelValue(p.Current())` / другой панели: всегда `terminal.Quote(value)` + пробел; пустое значение игнорируется; управляющие символы → `Cannot insert a path containing control characters`.

#### Ctrl/⌘+Shift+Enter — имя текущего файла другой панели
- `appendPanelValue(other.Entries[other.Cursor].Name)` (только имя, в кавычках при необходимости).

#### Ctrl/⌘+[ и Ctrl/⌘+] — путь левой / правой панели; с Shift — активной / неактивной
- Коды `BracketLeft`/`BracketRight` или символы `[ ] { }`. Примечание: ⌘Shift+[ / ] перехватываются раньше как переключение экранов (`cycleScreen`), поэтому Shift-варианты реально доступны только с Ctrl.
- **Код:** `far_keys.go: farPanelKey, appendPanelValue`.

#### ⌘Space — автодополнение
- `Settings.CompleteFiles` (по умолчанию true, `Terminal settings`/`AutoComplete settings`). Дополняет последнее слово командной строки относительно пути активной панели; кавычки/`$`/`\` в слове → `Completion: use an unquoted path prefix`; одно совпадение вставляется (в кавычках при необходимости, папки с `/`), несколько — список `Complete filename`; нет → `No matching paths`. Регистр по `CompleteIgnoreCase`.
- **Код:** `completion.go`.

#### Shift+Insert — вставка из буфера
- Обрабатывается рендерером (`pasteRequest`) → `paste` → командная строка/диалог/поиск. Особенность архитектуры: системный pasteboard доступен только рендереру, поэтому в Go нет обработки Shift+Insert; headless-тесты используют сообщение `paste`.

---

## 13. Мышь

#### Попадание (`panelAt`, `panelItem`)
- Панель определяется по `panelRect`: `1 ≤ y < Rows−4`, `px < x < px+w−1`, панель не скрыта. Строка файла: `3 ≤ y < Rows−6`; колонка `min(columns−1, (x−px−1)/((w−2)/columns))`; индекс `Offset + column×(Rows−9) + (y−3)`; клики по заголовку, рамке, футеру, за пределами списка игнорируются (курсор не меняется). Quick view и Info panel не имеют элементов (Info обрабатывается отдельно).
- **Код:** `mouse.go`.

#### Клик левой кнопкой
- Активирует панель, ставит курсор на элемент, запоминает якорь (`mouseAnchors[panel]`, сбрасывается при смене пути), начинает drag-сессию. Пометки не меняются.
- **Shift+клик:** помечает диапазон от якоря до элемента (добавляет к существующим).
- **⌘+клик или правая кнопка / Control+клик (рендерер шлёт `Button: 2`):** инвертирует пометку элемента и ставит курсор; якорь = элемент.
- **Двойной клик левой (без Shift/⌘):** завершает drag и посылает Enter (`enterFile`).
- Клик, когда открыты меню/диалог/форма/документ/Busy с кнопкой ≠ 0 — игнорируется; при `Busy` левый клик по кнопке `Cancel` прогресса — отмена. Любой клик закрывает fast find.
- **Код:** `mouse.go: panelMouse`, `app.go: click`.

#### Drag (перетаскивание с зажатой кнопкой)
- Левая: пометки сбрасываются к состоянию на момент нажатия (`base`) и помечается диапазон якорь…текущий; курсор следует. Правая/Control: диапазон от последнего элемента до текущего получает значение первого клика (пометить или снять). Выход за верх/низ списка прокручивает `Offset` на 1 за событие; x ограничивается панелью. Drag прекращается при смене пути панели или `mouseUp`.
- **Код:** `mouse.go: dragMouse`.

#### Колесо / scroll
- `wheel` (`Delta`) и `scroll` (`Y×3`), ограничение ±12 за событие. Над файловой панелью: `Offset` смещается в пределах `[0, len−capacity]`, курсор клампится в видимую область. Над Info panel — `InfoOffset`. Над Quick view — ничего. Панель определяется позицией указателя (не активная). При диалоге/Busy — игнорируется; в меню/форме — перемещение выбора.
- **Код:** `mouse.go: scrollMouse`.

#### Клик по строке меню / key bar / командной строке
- y=0 левой: открывает секцию меню под курсором (при открытом меню клик вне его закрывает меню; внутри — выполняет пункт).
- y=Rows−1 левой: функциональная клавиша по позиции (только при видимом key bar).
- Клик по статусной/командной строке (y = Rows−3, Rows−2) ничего не делает.

---

## 14. Видимость панелей, ширина, зум

#### ⌘F1 / ⌘F2 (Ctrl+F1/F2) — скрыть/показать левую/правую; Ctrl/⌘+P — неактивную
- **Поведение (`togglePanel`):** `Hidden` инвертируется; если скрывается активная и другая видима — активной становится другая. Меню `Left/Right → Panel on/off ⌘F1/⌘F2`. Скрытая область — серая заливка; Tab может вернуть фокус на скрытую панель. Toggle view (`toggleOtherView`, `setView`, `matchOtherPanel`) снимает `Hidden`.
- **Код:** `archive_panel.go: togglePanel`, `far_keys.go`.

#### Ctrl/⌘+← / → — ширина панелей; Ctrl/⌘+Num5 — сброс
- Только при пустой командной строке и без Shift. `panelSplit` (0 = 50/50) меняется на ±2 в пределах `20..80` процентов ширины левой панели. Num5 с cmd — `panelSplit = 0`. Не сохраняется в setup.
- **Код:** `far_keys.go`, `panel_draw.go: panelRect`.

#### Ctrl/⌘+O — панели ↔ терминал
- Скрывает панели и показывает встроенный терминал (закрывает меню и fast find); при показе синхронизирует cwd shell с активной локальной панелью (когда shell у prompt). Возврат: при готовом shell активная панель перечитывается в cwd shell (`syncCwd`), иначе — отложено до prompt; затем `refresh()` обеих панелей. Футер терминала: `⌘O  Panels    ·    Shell ready    ·    <cwd>` / `⌘O  Panels    ·    ⌘.  Interrupt` / `⌘O  Panels    ·    Starting <shell> (loading shell configuration)…    ·    ⌘. Interrupt`.
- **Код:** `app.go: key, showTerminal, returnFromTerminal, syncCwd`.

#### Alt+F9 — зум окна
- `Update.Zoom = true` → нативное `[window zoom:]` (развернуть/восстановить окно macOS). Масштаб шрифта: `Options → General settings → Font size (10–24)` (`Font size must be 10–24`); отдельных клавиш масштабирования в панелях нет.
- **Код:** `far_keys.go`, `window_darwin.m`.

#### ⌃⌘F — Full screen
- `Commands → Full screen`: `Update.Fullscreen = true` (нативное переключение).

---

## 15. Прочее, относящееся к панелям

#### Ctrl/⌘+B — key bar on/off
- Переключает `Settings.ShowKeys`.

#### Shift+Enter — открыть в macOS
- `openSystemSelected`: remote → `Copy remote files to a local panel before opening them with macOS`; член архива → сообщение выше; иначе `/usr/bin/open <path>` (папки — в Finder) в задаче `Opening <name>`.
- **Код:** `launch.go`.

#### Описания файлов (descript.ion)
- `Files → Describe files…`: диалог `Describe files`, `Description (empty removes it)`, значение — текущее описание первого выбранного; запись файла `Settings.DescriptionFile` (`Options → Advanced → File descriptions`, `Description file name`, ошибка `Enter a file name without a path`) атомарно; имена с пробелами в кавычках; ошибка `this filename cannot be stored in descript.ion`. Чтение: строки `name desc` или `"name with spaces" desc`, лимит строки 1 MiB. Не для remote/архива.
- **Код:** `panel_options.go: readDescriptions, describeFiles`.

#### Save setup (Shift+F9, Options → Save setup)
- Сохраняет `Settings`, `panelOptions` обеих панелей (режим, сортировка, фильтр, флаги, Hidden), пути (`filesystemPath`), историю команд/папок/просмотра в `~/Library/Application Support/FarshMac/setup.json`; статус `Setup saved`. При `AutoSave` — при закрытии. Папки восстанавливаются через session.json (отдельно).
- **Код:** `settings.go`.

#### Alt+F6 — Create link
- Форма `Create link`: `Link name` (путь в другой панели + имя), `Symbolic link` (вкл.); `os.Symlink`/`os.Link`; `refresh()`. Не для remote/архива.

#### F1 — справка
- Открывает документ `FarshMac — Help` с текстом `help` (см. расхождения ниже).

---

## Расхождения между справкой F1 / README / парити-документами и кодом

> **Закрыто на ветке `backlog-fixes`:** решения по каждому пункту — в разделе «Решения по расхождениям» `docs/IMPLEMENTATION_BACKLOG.md`.

1. **F1: `Cmd+R Refresh panels`** — код перечитывает только активную панель (`rereadPanel(a.Active)`); обе панели перечитываются лишь при `refresh()` (возврат из терминала, смена настроек). Меню `Left/Right → Re-read ⌘R` вызывает перечитывание конкретной панели независимо от активности.
2. **F1: `Cmd+Shift+.  Show / hide hidden files (display only)`** — на деле переключается персистентная настройка `Settings.ShowHidden` (та же, что в `Panel settings`), и она сохраняется в setup.json; «display only» неверно. Ctrl+H делает то же, но в справке/README не упомянут.
3. **F1: `Ctrl+Q`, `Ctrl+L / Ctrl+T`, `Ctrl+P / Ctrl+B`, `Ctrl+F3..F11`, `Ctrl+F12`, `Ctrl+F / Ctrl+;`, `Ctrl+PgUp/PgDn`, `Ctrl+Left/Right`, `Ctrl+Insert`, `Ctrl+E/X/Y`** — в коде большинство этих сочетаний принимают и ⌘ (`cmd = Ctrl || Meta`): Ctrl+T, Ctrl+P, Ctrl+B, Ctrl+F3..F12, Ctrl+F, Ctrl+;, Ctrl+PgUp/PgDn, Ctrl+←/→, Ctrl+Y, Ctrl+Insert работают и с ⌘. Исключения — только Ctrl: Ctrl+Q, Ctrl+L (⌘L = Change directory), Ctrl+H, Ctrl+E/X, Ctrl+1..9 / Ctrl+Shift+1..9. Справка это не отражает.
4. **Не задокументированы в F1/README, но есть в коде:** Ctrl+J (вставить имя файла), Ctrl/⌘+Shift+Enter (имя файла другой панели), Ctrl+[ / Ctrl+] (путь левой/правой панели) и Ctrl+Shift+[ / ] (активной/неактивной), ⌘/Ctrl+Shift+S (human-readable sizes), Shift+F11 (sort groups on/off), Alt+F6 (создать ссылку), Alt+F9 (zoom окна), Alt+F7 (find file), Alt+F8 (command history), Alt+F10 (folder tree), Numpad5 = F3, Ctrl+Alt+Shift+Insert (пути с разрешёнными симлинками), Ctrl+U (swap), Ctrl+R, Ctrl+↑/↓ (история команд), Shift+F10 (последнее меню), Compare folders, Shift+клик/⌘+клик/правая кнопка/drag мышью.
5. **F1: `Ctrl+Shift+1..9 Save folder shortcut (panels)` / `Ctrl+1..9`** — корректно, но переход по локальной закладке дополнительно **сбрасывает все запомненные позиции курсора панели** (`p.states = nil`), что нигде не описано.
6. **F1: `Num * Invert files; Cmd/Ctrl+Num * includes folders`** — не упомянуто, что Alt+Num* инвертирует файлы и одновременно снимает пометки с папок.
7. **Меню `Sort mode → Directories first`** — пункт переключает `MixDirectories` (т.е. при выборе папки *перестают* идти первыми, если флаг был выключен); индикаторов состояния у переключателей `Reverse order`, `Case sensitive`, `Directories first`, `Selected first` нет.
8. **F1: `Ctrl+PgUp/PgDown Parent / enter folder or archive`** — `isArchiveName` для Ctrl+PgDn включает `.gz`, `.bz2`, `.xz` без `.tar`, которые `fsops.ArchiveFormat` не поддерживает: Ctrl+PgDn на таком файле вызывает `enterFile`, который откроет файл системным приложением, а не как архив.
9. **BEHAVIOR_PARITY: «Esc retains the found position»** и **«Search does not replace the command line»** — соответствует коду. Но Paste в fast find описан как «until the first unmatched character» — код принимает первый символ всегда, даже без совпадения (`TestFastFindIncludesFirstLetterWithoutMatch`).
10. **README: `F3 / F4 View / edit; F3 on a folder calculates selected folder sizes`** — верно; дополнительно F4 на папке открывает `File attributes` (не задокументировано в README/F1).
11. **Легаси-режимы** `Descriptions`, `Long descriptions`, `File owners`, `File links`, `Alternative full` упомянуты в F1 (`File links view   Names, sizes and hard-link counts`), но недоступны ни из меню, ни из настроек: `configurePanel` принудительно переводит их в `Full` при старте/refresh. Режим `Medium` (2 колонки) есть в меню, но не упомянут в справке.
12. **F1: `Tab Switch active panel`** — Tab переключает фокус и на скрытую (⌘F1/⌘F2) панель; в таком состоянии курсор и заголовок не видны.
13. **Справка F1 и меню `Commands → Quick view on/off Ctrl+Q`** — Quick view через Ctrl+Q допускается для удалённой неактивной панели (показывает `Remote file: use F3 to download and view`), тогда как через меню Left/Right для remote запрещён (`Choose a file view for remote panels`); для членов архива Quick view показывает ошибку `stat`, т.к. виртуальный путь не существует (нигде не описано).


<a id="часть-2"></a>
# Часть 2. Файловые операции, архивы, запуск программ

Описание составлено по текущему коду (`internal/ui`, `internal/fsops`, `internal/remotefs`), а не по документации. Строки интерфейса приведены дословно (английский, как в коде). Расхождения между документацией и кодом собраны в конце.

## Общая инфраструктура операций

### Фоновые задачи и «Busy»

#### Фоновая задача (`backgroundTask` / `workProgress` / `backgroundProgress`)
- **Вызов:** внутренне, любой длительной операцией (копирование, удаление, архивы, поиск, открытие архива, список процессов и т. д.).
- **Где доступно:** только если `a.Busy == false` и окно не закрыто; иначе вызов молча игнорируется (одна операция на окно).
- **Поведение:**
  1. Создаётся `fsops.Operation(label)` с `IgnoreDSStore = Settings.IgnoreDSStore`; `a.Busy = true`, `a.Operation = o`, `a.Status = label`.
  2. Работа выполняется в goroutine; результат (`apply`, `cleanup`, `err`) принимается в `pollTask` (из `Poll`, на engine queue).
  3. Пока `Busy`: `PanelKeys=false`; клавиши — только `Escape`/`Alt+C` (отмена) и клавиши открытого диалога-вопроса; клики — только по кнопке `Cancel` прогресса или по вопросу; `Cmd+Q` отвечает статусом `Wait for the file operation to finish`.
  4. По завершении: `a.LastProgress` = снимок прогресса, `Busy=false`, `Status=""`; при ошибке → `a.error(err)` (статусная строка = текст ошибки) и, если `task.reportError` (всегда для `workProgress`), модальное подтверждение `"<Label> failed"` с текстом ошибки и кнопкой `OK` — кроме отмены (`operation cancelled`/`context canceled`), для которой диалога нет, только статус `operation cancelled`.
  5. `workProgress` дополнительно: подключает `operationQuestions(o)` (вопросы Retry/Skip и read-only), игнорирует `ErrSkipped` как успех; при успехе статус `Done` или `Done — N item(s) skipped`; `cleanup` всегда делает `a.refresh()` (перечитывает обе панели).
  6. `backgroundProgress` — та же задача без обработчика вопросов и без финального диалога ошибки (ошибка только в статусе).
- **Код:** `internal/ui/task.go` (`backgroundTask`, `pollTask`), `internal/ui/progress.go` (`workProgress`), `internal/ui/commands.go` (`backgroundProgress`).

#### Окно прогресса
- **Вызов:** рисуется автоматически, пока `a.Operation != nil`.
- **Поведение (отрисовка, `drawProgress`):** модальное окно 88×15 (меньше при малом экране) с заголовком = label операции. Содержимое:
  - строка 1: текущий путь (`Progress.Path`, обрезанный слева `tail`).
  - Пока `Scanning` или `Streaming` (итог неизвестен): `Working / calculating… ▰▱▱▱  N items` с анимацией кадров `▰▱▱▱ ▱▰▱▱ ▱▱▰▱ ▱▱▱▰` (150 мс/кадр), где N = `TotalFiles`. В режиме `Streaming` дополнительно строка `File  ████░░░░` (прогресс текущего файла).
  - Иначе: `Total %5.1f%%  <Files> / <TotalFiles> items`, под ней общий бар (`█`/`░`, по байтам; если `TotalBytes==0` — по файлам), затем `File  <бар текущего файла>`.
  - Если `Workers > 1`, строка файла заменяется на `Parallel transfers: <Active> active / <Workers> workers`.
  - Статистика: `<bytes> / <total>  ·  <rate>/s  ·  <elapsed>s` (+ `  ETA Ns`, если скорость > 0 и итог известен); в `Streaming`: `<bytes> processed · <rate>/s · <elapsed>s`.
  - Кнопка `{ Cancel }` (мнемоника `c`).
- **Отмена:** `Esc`, `Alt+C` или клик по `Cancel` → `cancelOperation()`: `Operation.Cancel()`, закрывается открытый диалог-вопрос (`OperationQuestion`, либо с заголовком `Overwrite`/`Cannot preserve metadata`), статус `Cancelling…`. Worker видит отмену через `o.Check()`/контекст (включая закрытие окна `Close()`).
- **Код:** `internal/ui/progress.go`, `internal/fsops/progress.go` (`Progress`, `Operation`, `Streaming`, `Scan`, `Ready`).

#### Вопросы во время операции (общий механизм)
- **Поведение:** worker ставит closure в `a.jobs`, которая открывает `confirm(...)` с `Form.OperationQuestion = true`, и блокируется на канале ответа; параллельные воркеры сериализуются мьютексом `o.Questions`. Отмена операции (Esc, закрытие окна) разблокирует вопрос — возвращается последний вариант (`Cancel`). Esc в диалоге-вопросе = `cancelOperation()` всей операции (т. к. `Busy`), а не только закрытие диалога. `PageUp/PageDown` листают длинные сообщения; Alt+буква — мнемоники кнопок.
- **Код:** `internal/ui/operation_questions.go` (`operationChoice`), `internal/ui/app.go` (`key` при `Busy`).

#### Диалог ошибки Retry/Skip (`o.Failure`)
- **Диалоги:** заголовок `Error`; текст: `Cannot <operation>\n<source>` + (если есть) `\nTo: <destination>` + `\n\n<текст ошибки ОС>`; кнопки `Retry`, `Skip`, `Skip all`, `Cancel`.
- **Поведение:** `Retry` повторяет только тот шаг, который упал (`o.Attempt`), а не весь пакет; `Skip` помечает элемент пропущенным (`Skipped++`, `CompleteTree`); `Skip all` — все последующие ошибки в этой операции пропускаются без вопросов; `Cancel`/Esc — `ErrCancelled`. Ошибки `context.Canceled` превращаются в `ErrCancelled`. Операции-шаги, обёрнутые в `Attempt`: `copy`, `move`, `read folder`, `read attributes`, `create folder`, `delete`, `delete source`, `delete source folder`, `move to Trash`, `open destination`, `read destination`, `connect`, `create archive`, `extract archive`, `create folder` (remote).
- **Код:** `internal/fsops/recovery.go` (`Attempt`, `SkipItem`, `IgnoreSkipped`), `internal/ui/operation_questions.go`.

#### Вопрос «read-only» при удалении (`o.BeforeDelete`)
- **Когда:** перед удалением/перемещением в Trash обычного файла без битов записи (`mode & 0222 == 0`); для каталогов и других типов не задаётся.
- **Диалог:** `Warning`: `The file is read only\n<path>\nDelete it?`; кнопки `Delete`, `All`, `Skip`, `Skip all`, `Cancel`. `All`/`Skip all` запоминаются до конца операции.
- **Код:** `operation_questions.go` (`operationQuestions`), применяется в `fsops.removeTreeStep`, `ui/delete.go` (Trash), `remotefs.removeTreeStep`.

## Копирование и перемещение

#### F5 — Copy
- **Вызов:** `F5`, `Cmd+5`, меню `Files → Copy…` (`F5`); `Shift+F5` — только текущий элемент (см. ниже).
- **Где доступно:** активная панель локальная, удалённая или архивная; **недоступно**, если противоположная панель — архив (статус `Choose a regular destination panel; writing into archives is not supported`). Если выбор пуст (пустая панель или курсор на `..` без отметок) — ничего.
- **Выбор элементов:** `Panel.Selected()` — все отмеченные (кроме `..`), иначе текущий элемент (не `..`). Перед операцией сохраняется `RestoreMarked` (для `Cmd+M`).
- **Диалог (форма, заголовок `Copy`):**
  - `Copy N item(s) to:` — значение по умолчанию: путь противоположной панели; если выбран один элемент — `<путь другой панели>/<имя элемента>`. Текст выделен (`Form.Selected = Settings.SelectDialogInput`, по умолчанию on): набор символа заменяет значение, `Cmd+A` выделяет, вставка заменяет.
  - `[x] Preserve permissions (mode, owner, group, ACL)` — по умолчанию `Settings.CopyPermissions` (default true).
  - `[x] Preserve attributes (xattrs, flags, timestamps)` — `Settings.CopyAttributes` (default true).
  - `Parallel transfers (1–8):` — `Settings.CopyWorkers` (default 4, нормализуется в 1..8).
  - Кнопки `{ Copy }` `[ Cancel ]` (Primary = `Copy`). Флажки переключаются Space/←/→/мышью/Alt+мнемоника.
  - Валидация: нечисло или вне 1..8 → статус `parallel transfers must be between 1 and 8`, форма закрывается, операция не стартует. Значения флажков и workers сохраняются в `Settings` (персистируются с setup).
- **Интерпретация назначения:**
  - `expand(s, base)`: `~`/`~/…` → домашний каталог; относительный путь — относительно `base` (путь активной панели; для архива — каталог файла архива; при участии удалённой панели — путь панели назначения); `filepath.Clean`.
  - Назначение проверяется `Lstat` через `targetEndpoint` (локально или на сервере). Если выбрано >1 элементов и назначение не существующий каталог → ошибка `destination must be an existing directory`.
  - `transferIntoDirectory(input, dst, suggested, count, isDir)`: элементы кладутся **внутрь** каталога `dst` (как `dst/<basename>`), если `dst` — существующий каталог **и** (элементов > 1, **или** введённый путь отличается от предложенного, **или** ввод оканчивается на `/`). Предложенный одиночный путь, даже если это существующий каталог, означает сам этот объект (слияние каталога `folder` в существующий `folder`, не `folder/folder`).
- **Выполнение (`remotefs.Copy`, используется и для локальных копий):**
  1. `o.CopyMetadata = &{Permissions, Attributes}`; подключаются `metadataQuestions(o, false)`, `collisionQuestions(o, Settings.ConfirmOverwrite)`.
  2. Из архива: сначала `catalog.ExtractSelection(names, <tmp>/content, o)` во временный каталог `farshmac-archive-copy-*` (удаляется `defer`), далее обычное копирование из temp.
  3. `o.Parallel(workers)`, `o.Streaming()` (итог заранее неизвестен — индикатор «Working / calculating…» и счётчик). Воркеры — отдельные goroutine с собственными сессиями (для FTP/SFTP — отдельные соединения); очередь `workers*2`.
  4. Обход (walk): пропускаются `Ignored` (`.DS_Store` при `IgnoreDSStore`), лимиты `1 000 000` записей / глубина `256` → `transfer tree limit exceeded`. Каталоги создаются обходчиком (локально с Permissions: `Mkdir 0700`, затем финальные метаданные; без Permissions — `Mkdir 0777` через umask); существующий каталог назначения используется (слияние), не-каталог → `destination is not a directory: <path>`. Файлы ставятся в очередь воркеров.
  5. Локально→локально каждый файл идёт через `fsops.Transfer(src, dst, move=false, …)`; при участии сервера — `copyFile` (см. «Удалённые панели»).
  6. Проверки до старта: `destination is inside source` (одинаковый endpoint и назначение внутри источника), `validateLocalPair`.
  7. После всех файлов — метаданные каталогов в обратном порядке (`ApplyMetadata`).
- **Локальный файл (`fsops.Copy` → `copyEntry`):**
  - Символические ссылки никогда не разыменовываются: копируется ссылка (`Readlink`+`Symlink`).
  - Жёсткие ссылки: внутри одной операции учитываются только при `moving` (перемещение между томами), см. F6.
  - Обычный файл: если оба флажка метаданных включены (или опции не заданы) — `fclonefileat(... CLONE_NOOWNERCOPY)` (APFS clone, атомарно, без замены существующего). При `ENOTSUP/EXDEV/EINVAL/ENOSYS/EPERM` — потоковое копирование: временный файл `.farshmac-copy-<rand>` в каталоге назначения (режим 0600, или 0666 при выключенном Permissions), буфер 256 КиБ с прогрессом, затем публикация через `renamex_np(RENAME_EXCL)` — существующий файл никогда не перезаписывается «вслепую». Если один из флажков выключен — clonefile не используется (чтобы не «протекли» метаданные).
  - Спецфайлы (сокеты, устройства…) → `unsupported special file: <path>`.
  - Метаданные (`preserveMetadata`, `metadata_darwin.go`): при включённых опциях после копии — `lchown` (owner/group), `copyfile(COPYFILE_NOFOLLOW | ACL | XATTR)`, `lchmod`, `setattrlist` (creation/modification/access time с наносекундами, и для симлинков), `lchflags`. После этого сверка `lstat` источника и копии: различия → ошибка `<src>: destination did not preserve <permissions|owner/group|file flags|modification time|creation time>` и диалог «Cannot preserve metadata» (см. ниже). При отказе результат копии удаляется (`RemoveAll(dst)`), источник не трогается.
  - Каталог при неявной политике (`CopyMetadata == nil`, например append-префикс) — `Chmod`+`Chtimes` как у источника.
- **Коллизии:** см. «Диалог коллизии».
- **Отмена:** `Esc` → `Cancelling…`; незавершённый временный файл удаляется; уже завершённые файлы остаются («не откатываются»); каталоги, созданные обходчиком, остаются (при обходе из `remotefs.Copy`); при `fsops.Copy` каталога целиком (путь cross-volume move/append-префикс) незавершённый новый каталог удаляется `RemoveAll`.
- **Сообщения:** статус `Copy` → `Done` / `Done — N item(s) skipped`; ошибка → статус + диалог `Copy failed` `[OK]`; `operation cancelled`.
- **Особые случаи:** `.DS_Store` пропускается при `Settings.IgnoreDSStore` (`Hide and ignore .DS_Store` в Panel settings); F5 из архива извлекает (см. «Архивы»); удалённые панели — см. ниже. Тест `TestCopyOptionsDialogAndPersistence` подтверждает: выключенные флажки → права/дата не сохраняются; выбор запоминается в setup.
- **Код:** `internal/ui/transfers.go` (`transferDialog`, `transferIntoDirectory`), `internal/remotefs/transfer.go` (`Copy`), `internal/fsops/files.go` (`Copy`, `copyEntry`, `copyRegularStream`, `createCopyTemp`), `internal/fsops/clone_darwin.go`, `internal/fsops/metadata_darwin.go`, `internal/fsops/parallel.go`, `internal/fsops/ignore.go`, `internal/fsops/walk.go`.

#### F6 — Move / rename
- **Вызов:** `F6`, `Cmd+6`, меню `Files → Rename or move…`; `Shift+F6` — только текущий элемент.
- **Где доступно:** локальная панель; в архиве → статус `Archive is read-only. Use F5 to copy files out.`; если любая из панелей удалённая → `remoteMoveDialog` (см. ниже); противоположная панель-архив → `Choose a regular destination panel; writing into archives is not supported`.
- **Диалог:** простой `prompt` (Dialog) с заголовком `Move / rename`, текстом `N item(s) to`, полем ввода (значение как у F5: путь другой панели или `<другая панель>/<имя>`), кнопками `OK` / `Cancel`. Флажков метаданных и воркеров нет (перемещение всегда сохраняет все метаданные и выполняется последовательно, без параллельных воркеров). Если `Settings.RememberDialogValues` и значение пустое — подставляется прошлый ввод.
- **Поведение (`fsops.Transfer(src, dst, move=true)` по каждому элементу, последовательно):**
  1. `o.IgnoreDSStore = false` — при перемещении `.DS_Store` переносится вместе с каталогом (`Ignored` учитывает `!o.moving`). `o.Streaming()` — дерево не сканируется заранее (same-volume rename мгновенный).
  2. Подключаются `moveMetadataQuestions` и `collisionQuestions`. Та же логика назначения/`transferIntoDirectory`, что у F5.
  3. Проверки: `destination is inside source`; `source and destination are the same file`.
  4. Назначение не существует → `fsops.Move` = `renamex_np(RENAME_EXCL)` (атомарно, без замены). `EXDEV` → `moveAcrossVolumes`: `o.Scan` (подсчёт итога), `Copy` с `o.moving=true` (всегда Permissions+Attributes), затем `Remove(src)` через `Attempt("delete source")`. Источник удаляется только после полной копии и явного согласия на потерю метаданных.
  5. Каталог → существующий каталог: рекурсивное слияние по элементам; после обхода, если источник опустел — `Attempt("delete source", os.Remove)`; при пропусках источник-каталог остаётся.
  6. Файл → существующий файл: диалог коллизии. `Replace`: проверка, что назначение не изменилось (`destination changed during move`), затем `os.Rename` (same volume) или staging-копия в `.farshmac-transfer-*` + `Rename` поверх + удаление источника. `Append`: `appendFile` затем удаление источника.
  7. Жёсткие ссылки (`hardlinks.go`): при `moving` запоминаются (dev, inode) скопированных файлов с `Nlink ≥ 2`; повторная встреча того же inode → `os.Link` на уже скопированный файл вместо копирования; при неудаче — вопрос метаданных с ошибкой `cannot preserve hard-link relationship: …`. После публикации через staging пути перепривязываются (`relocateHardlinks`).
- **Диалог «Cannot preserve metadata» (`metadataQuestions`):** заголовок `Cannot preserve metadata`; текст: `Move with changed metadata and remove the source?\n<src>\nTo: <dst>\n\n<err>` (для F5: `Copy with changed metadata?`); кнопки `Move anyway` (F5: `Copy anyway`), `For all`, `Retry`, `Skip`, `Skip all`, `Cancel`. `For all`/`Skip all` запоминаются; `Retry` → `ErrRetryMetadata` (повтор только метаданных); `Skip` → элемент пропущен, источник остаётся; `Cancel`/Esc → отмена всей операции.
- **Сообщения:** статус `Move / rename` → `Done`/`Done — N item(s) skipped`; `Move / rename failed` `[OK]`; ошибки `destination changed during move`, `cannot replace a file with a directory or a directory with a file`, `choose a different destination name`, `destination must be an existing directory`.
- **Код:** `internal/ui/transfers.go`, `internal/ui/move_metadata.go`, `internal/fsops/transfer.go` (`transfer`, `moveAcrossVolumes`), `internal/fsops/move_darwin.go`, `internal/fsops/hardlinks.go`, `internal/fsops/append.go`.

#### Shift+F5 / Shift+F6 — копировать/переместить только текущий элемент
- **Вызов:** `Shift+F5`, `Shift+F6` (`farPanelKey`).
- **Поведение:** временно обнуляется `p.Marked`, вызывается `transferDialog`, затем отметки и `RestoreMarked` восстанавливаются — диалог предлагает `<другая панель>/<имя текущего>` и оперирует только текущим элементом. Все остальные правила как у F5/F6.
- **Код:** `internal/ui/far_keys.go` (`currentTransfer`).

#### Диалог коллизии (файл уже существует)
- **Когда:** назначение существует и хотя бы одна сторона не каталог (каталог→каталог сливается без вопроса). Если `Settings.ConfirmOverwrite` выключен (`Confirm overwrite` в General settings / `Overwrite files` в Confirmations) — автоматически `Replace` (но вопрос о read-only всё равно задаётся).
- **Диалог:** заголовок `Warning`; текст:
  ```
  The file already exists
  <destination>

  New: <N> bytes  2006-01-02 15:04:05
  Existing: <N> bytes  2006-01-02 15:04:05
  ```
  (для каталога вместо размера `Folder`; при отсутствии info — `New: unavailable`). Кнопки: `Overwrite`, `Skip`, `Rename`, `Append` (только если обе стороны — обычные файлы), `Cancel`; флажок `[ ] Remember choice` (Space/Enter на нём, `Alt+M`, клик). Запомненный выбор действует до конца этой операции (не сохраняется в настройках); `Cancel` не запоминается; запомненный `Append` для пары, где append невозможен, вызывает повторный вопрос.
- **Rename:** кандидат `<stem> (copy)<ext>` в том же каталоге. Без «Remember» — форма `Rename` с полем `New name` (предзаполнено кандидатом, выделено), `{ OK } [ Cancel ]`; пустое/Esc → отмена операции; относительный ввод раскрывается относительно каталога назначения. С «Remember» — кандидат применяется без вопросов для каждой коллизии; если и кандидат существует, передача снова проверяет его (новая коллизия → очередной кандидат `… (copy) (copy)`), никогда не перезаписывая вслепую. Пустое имя/то же имя → `choose a different destination name`.
- **Append (`appendFile`):** только два обычных файла (`append requires two regular files`). Старое назначение копируется в staging `.farshmac-append-*` (с его метаданными через дочернюю операцию), `chmod 0600`, дописывается источник с прогрессом, `chmod` в права назначения, проверка `destination changed during append`/`source size changed during append`, `Rename` поверх; при move затем удаляется источник. Отмена оставляет старый файл нетронутым.
- **Overwrite (Replace):** `cannot replace a file with a directory or a directory with a file`; копия ставится в staging `.farshmac-transfer-*/item` рядом, проверяется, что назначение не изменилось (`destination changed during copy`), затем `os.Rename` поверх.
- **Read-only назначение:** при `Overwrite`/`Append` на обычный файл без битов записи — `Warning`: `The file is read only\n<dst>\nOverwrite it?`, кнопки `Overwrite`, `All`, `Skip`, `Skip all`, `Cancel` (All/Skip all запоминаются).
- **Код:** `internal/ui/operation_questions.go` (`collisionQuestions`, `fileComparison`), `internal/fsops/transfer.go`, `internal/fsops/append.go`, `internal/fsops/recovery.go` (`Resolve`, константы `Replace/Skip/Cancel/Append/Rename`).

#### Ошибки во время копирования/перемещения
- См. «Диалог ошибки Retry/Skip». Пример (тест `TestLocalTransferPermissionErrorStaysVisible`): каталог назначения без прав записи → диалог `Error` с `permission denied`; Esc → источник цел, назначение не создано.
- Дополнительные тексты: `transfer tree limit exceeded`, `unsupported special file: …`, `destination is not a directory: …`, `source size changed while copying …` (remote), `outside archive`.

## Создание папок

#### F7 — Make folder (локально)
- **Вызов:** `F7`, `Cmd+7`, меню `Files → Make folder…`.
- **Где доступно:** локальная панель; в архиве → `Archive is read-only. Use F5 to copy files out.`; удалённая панель → см. remote.
- **Диалог:** `prompt` заголовок `Make folder`, текст `Folder name`, пустое поле (или прошлое значение при `Remember dialog values`), кнопки `OK`/`Cancel`.
- **Поведение:** пустая/пробельная строка — ничего. `expand(s, <путь панели>)` (поддерживаются `~`, относительные и абсолютные пути), `os.MkdirAll(path, 0755)` — вложенные пути `a/b/c` создаются целиком; существующий каталог — не ошибка. Затем `refresh()` и `focusCreatedDirectory`: панель загружает родителя созданного каталога и ставит курсор на него (`ForcedVisiblePath` делает его видимым несмотря на фильтры); если всё же скрыт — статус `Folder created; hidden by panel settings`.
- **Ошибка:** подтверждение `Error`: `Cannot create folder\n<path>\n\n<err>`, кнопки `Retry`, `Cancel` (Retry повторяет MkdirAll).
- **Код:** `internal/ui/app.go` (`function` case 7), `internal/ui/launch.go` (`focusCreatedDirectory`).

#### F7 на удалённой панели — Make remote folder
- **Диалог:** `Make remote folder` / `Folder name` / пусто.
- **Поведение:** имя должно быть одним компонентом (`remotefs.ValidName`), иначе статус `enter one folder name`; фоновая задача `Creating remote folder`: `Dial`, `Mkdir(<путь панели>/<имя>)` через `Attempt("create folder")` с диалогом `Error` Retry/Skip/Skip all/Cancel; Skip → статус `Folder creation skipped`; ошибка → `<err> (create folder <target>)` + диалог `Creating remote folder failed`; успех → refresh и курсор на новой папке.
- **Код:** `internal/ui/remote.go` (`remoteMkdir`).

## Удаление

#### F8 / Delete / Cmd+8 — Move to Trash
- **Вызов:** `F8`, `Delete` (без модификаторов), `Cmd+8`, меню `Files → Move to Trash…`. Key bar: `8Trash` (на удалённой панели `8Delete`).
- **Где доступно:** локальная панель; архив → `Archive is read-only…`; удалённая → `remoteDelete`.
- **Диалог (если `Settings.ConfirmDelete`, default on; `Confirm deletion`):** заголовок `Move to Trash`, текст `Move <name1>, <name2>, … to Trash?` (basename всех выбранных через `, `), кнопки `Move to Trash`, `Cancel`.
- **Поведение:** `workProgress("Moving to Trash")`, `o.Streaming()`; для каждого пути: `o.Check()`, `o.Add(path,0)`, `Attempt("move to Trash")`: `Lstat`, вопрос read-only (`BeforeDelete`, только для обычных файлов без прав записи), затем `fsops.Trash(path)`: проверка UTF-8/NUL (`invalid filename encoding`), `Lstat`, `o.Begin`, вызов ObjC `farshmac_trash_item` → `[[NSFileManager defaultManager] trashItemAtURL:resultingItemURL:error:]` (Trash соответствующего тома; каталог перемещается целиком, без обхода; прервать перенос одного элемента нельзя — отмена срабатывает перед следующим). Ошибка → `trash <path>: <localizedDescription или "Unable to move item to Trash">` в диалоге `Error` (Retry/Skip/Skip all/Cancel). **Сбой Trash никогда не переходит в постоянное удаление**; после ошибки/отмены остальные элементы не трогаются (тест `TestTrashFailureNeverFallsBackAndCancelStopsBatch`).
- **Сообщения:** `Done`, `Done — N item(s) skipped`, `Moving to Trash failed` `[OK]`, `operation cancelled`.
- **Код:** `internal/ui/delete.go`, `internal/fsops/trash_darwin.go`, `internal/fsops/trash_darwin.m`.

#### Shift+F8 / Shift+Delete — Delete permanently
- **Вызов:** `Shift+F8`, `Shift+Delete`, меню `Files → Delete permanently…` (`⇧F8`).
- **Диалог:** `Delete permanently`: `Permanently delete <names>?`, кнопки `Delete`, `Cancel` (при `ConfirmDelete`).
- **Поведение:** `workProgress("Deleting permanently")`, `Streaming`; `fsops.Remove(path, o)` → `removeTree`: обход без следования симлинкам, `Attempt("delete")` на каждом элементе (Retry/Skip/Skip all/Cancel), `BeforeDelete` read-only вопрос для файлов, `.DS_Store` при `IgnoreDSStore` не учитывается в прогрессе, но каталог удаляется целиком. Пропуск дочернего элемента сохраняет родительский каталог (`ErrSkipped` вверх по дереву) и продолжает с соседями. При `ENOTEMPTY` каталога (Finder успел дописать `.DS_Store`) — повторное чтение и удаление до 3 попыток, но только если это тот же inode (не следует за подменой каталога симлинком). Большие каталоги (тест на 2200 записей) удаляются потоково батчами по 256.
- **Код:** `internal/ui/delete.go`, `internal/fsops/progress.go` (`Remove`, `removeTreeStep`), `internal/fsops/remove_large_test.go`.

#### Удаление на удалённой панели
- **Вызов:** те же клавиши (`F8`, `Delete`, `Shift+F8`, `Shift+Delete`) — везде одно поведение, Trash нет.
- **Диалог (всегда, независимо от `ConfirmDelete`):** `Delete remote files permanently`: `Delete N selected item(s) on <label>? No remote Trash is available.`, кнопки `Delete`, `Cancel`.
- **Поведение:** `workProgress("Deleting remote files")`, `remotefs.RemoveTree`: рекурсивно, глубина ≤ 256 (`remote tree depth exceeds 256`), `Attempt("delete")`, `BeforeDelete` read-only вопрос, `unsafe server filename` для небезопасных имён с сервера; пропуски сохраняют родителя.
- **Код:** `internal/ui/remote.go` (`remoteDelete`), `internal/remotefs/transfer.go` (`RemoveTree`).

## Удалённые панели (FTP/FTPS/SFTP) в файловых операциях

#### F5 с участием удалённой панели
- **Поведение:** тот же диалог `Copy` (флажки и воркеры). Воркеры открывают отдельные соединения. Файл копируется в временное имя `.farshmac-part-<rand>` в каталоге назначения, потом `Rename` (при отмене временный файл удаляется, для remote — отдельной короткой сессией с таймаутом 2 с). Перед публикацией проверяется, что назначение не изменилось (`destination changed during transfer` / `destination appeared during transfer`), `source size changed while copying …`. Симлинки переносятся как ссылки (`Readlink`/`Symlink`); спецфайлы → `unsupported remote special file`. Удалённые каталоги > 100 000 записей → `remote directory exceeds 100000 entries`; имена проверяются `ValidName` (`unsafe filename from server`).
- **Метаданные (portable):** при `Preserve permissions` — `Chmod` + **всегда** добавляется ошибка `remote transfer cannot preserve owner/group and ACL`; при `Preserve attributes` — `Chtimes(mtime)` + **всегда** `remote transfer cannot preserve xattrs, flags and creation time`; для симлинков — `remote transfer cannot preserve symbolic-link metadata`. Поэтому при включённых флажках для каждого файла задаётся вопрос `Cannot preserve metadata` (`Copy anyway` / `For all` / …); `For all` подавляет дальнейшие вопросы. Чтобы вопросов не было — выключить оба флажка в диалоге.
- **Коллизии:** тот же диалог (Overwrite/Skip/Rename/Append/Cancel); Append реализован копированием старого назначения в temp и дописыванием (`destination size changed during append`); после Append — `Chmod` в права назначения (с вопросом метаданных при ошибке).
- **Код:** `internal/remotefs/transfer.go` (`Copy`, `copyFile`, `portableMetadata`).

#### F6 с удалённой панелью — Remote move / rename
- **Где доступно:** только если **обе** панели на одном сервере (`Endpoint.Same` по label). Иначе статус `Remote move is available within the same server. Between servers use F5 Copy, then Delete.`
- **Диалог:** `prompt` `Remote move / rename`, текст `Destination on the same server`, значение как у F6.
- **Поведение:** `workProgress("Renaming remote files")`; назначение `Lstat`; `destination must be an existing directory` при нескольких элементах; `transferIntoDirectory` как локально; `remotefs.Move`: `destination is inside source`, `Rename` если назначения нет; каталог→каталог — слияние с рекурсией (`delete source folder` после опустошения); файл→файл — коллизия: Skip/Rename/Append (копия+`delete source`)/Replace (проверка `destination changed during move`, затем `Rename`); `cannot replace a file with a directory or a directory with a file`. Сервер, не умеющий переименовывать между томами, вернёт ошибку (деградации в copy+delete нет).
- **Код:** `internal/ui/remote.go` (`remoteMoveDialog`), `internal/remotefs/move.go`.

#### Команды, недоступные на удалённой панели
- `Shift+F1/F2/F3` (архивы), `Alt+F7` (поиск), `Alt+F6` (link), `File attributes…`, `Apply command…`, `F2` user menu (запуск), `Shift+F4` new file, `Alt+F10` tree → статус `This command requires a local panel. Use F5 to copy remote files first.`
- `Shift+Enter` → `Copy remote files to a local panel before opening them with macOS`.
- Quick view на удалённый файл: `Remote file: use F3 to download and view`.
- **Код:** `internal/ui/remote.go` (`remoteUnsupported`), `launch.go`, `commands.go`.

## Архивы

#### Поддерживаемые форматы (`formats.go`)
- `ArchiveFormat(name)` по суффиксу (без учёта регистра): `.tar.gz`/`.tgz` → tar.gz; `.tar.bz2`/`.tbz2`/`.tbz` → tar.bz2; `.tar.xz`/`.txz` → tar.xz; `.tar`; `.zip`/`.jar`/`.war` → zip; `.7z`; `.rar`; `.cpio`. Остальное — не архив.
- Чтение: ZIP — Go `archive/zip` по central directory (должен быть валиден; `archive exceeds 100000 entries`); всё остальное (и ZIP для list/test/нестандартных методов сжатия) — `/usr/bin/tar -c -P -f - --format=pax @<src>` (bsdtar/libarchive macOS) в отдельной process group с `COPYFILE_DISABLE=1`, без `TAR_OPTIONS`; на Esc — `SIGKILL` всей группе. gzip-потоки дополнительно проверяются Go (`gzip integrity: …`, `nonzero data after gzip stream`). RAR/CPIO — только чтение, насколько умеет системный libarchive.
- Создание: `.zip` — Go; `.tar`, `.tar.gz` (`-z`), `.tar.bz2` (`-j`), `.tar.xz` (`-J`), `.7z` (`--format=7zip`) — Go генерирует PAX-поток, bsdtar упаковывает; иное расширение → `creation supports .zip, .tar, .tar.gz, .tar.bz2, .tar.xz and .7z`.
- Ошибка декодера: `archive operation failed (damaged, encrypted or unsupported archive): <err>: <stderr до 32 КиБ>`.
- Лимиты: 100 000 записей (`archiveEntryLimit`), отчёт до 10 000 строк / 4 МиБ.
- **Код:** `internal/fsops/formats.go`, `archive.go`, `archive_index.go`, `archive_zip_selection.go`.

#### Вход в архив (Enter / Ctrl+PgDn / двойной клик)
- **Вызов:** `Enter` на файле, для которого `ArchiveFormat != ""` (даже если файл исполняемый — проверка архива идёт раньше ассоциаций и exec-бита); `Ctrl/Cmd+PageDown` — если имя подходит под `isArchiveName` (список `.zip .tar .gz .bz2 .xz .7z .rar .tgz .jar`); двойной клик = Enter.
- **Поведение:** `backgroundTask("Opening archive")` → `fsops.ReadArchiveIndex` (только метаданные: ZIP — central directory; иначе потоковое чтение через bsdtar с прогрессом `Streaming`). Ограничения индекса: `archive index limit (100,000 entries / 16 MiB names)`, `archive path too long/deep` (> 4096 байт или > 64 `/`), `duplicate/conflicting archive entry`, `archive parent is not a directory`, `unsupported archive entry` (только dir/reg/symlink/hardlink), `unsafe archive path: …` (абсолютные, `..`, `\`, `C:`), `not a regular archive`, `invalid archive root`. По готовности панель переключается в режим архива: `p.Archive = catalog`, `Remote=nil`, вид Tree/Info/Quick → `Full`; загружается корень (или ранее открытый подкаталог при Cmd+R). Info panel для архива: `Archive (read-only)\nF5 extracts selected files/folders\n.. exits the archive`.
- **Навигация:** Enter на каталоге — вход; `..` в корне архива загружает родительский каталог файла архива (обычная загрузка, `p.Archive = nil`, курсор на архиве). Внутри работают отметки, сортировка, фильтр (`Filter`/`MaskGroups`), скрытие `.`-файлов и `.DS_Store`. `Cmd+R` перечитывает каталог архива (`openArchive`). Если файл архива изменился (`SameFile`/размер/mtime) — `archive changed on disk; leave and reopen it` при любой операции.
- **Ограничения:** `Ctrl+T` (Tree) на архивной панели → `Tree view is unavailable inside archives`.
- **Код:** `internal/ui/archive_panel.go` (`openArchive`, `openArchiveAt`, `loadArchiveDirectory`, `enterArchiveMember`), `internal/ui/launch.go` (`enterFile`), `internal/ui/far_keys.go` (`isArchiveName`), `internal/fsops/archive_index.go`.

#### Просмотр члена архива (F3 / Enter)
- **Вызов:** `F3`, `Enter` на обычном файле внутри архива (двойной клик).
- **Поведение:** `Enter` на вложенном архиве → статус `Copy the nested archive out with F5 to open it`; член-симлинк → `Use F5 to copy the archive link`. Иначе `backgroundTask("Opening archive member")`: `ExtractSelection([name], <tmp farshmac-archive-view-*>/content)`, документ открывается внутренним viewer'ом в режиме только чтения (`ReadOnly`, `ArchiveSource`, `Path` = виртуальный путь); временный каталог удаляется при закрытии документа/окна. `F4` в архиве → `Archive is read-only. Use F5 to copy files out.`; `Shift+F4`, `F6`, `F7`, `F8`, `Alt+F6`, `File attributes…`, `Apply command…`, `Shift+F1` — то же сообщение. `F3` на каталоге архива — расчёт размеров (`calculateFolderSizes`).
- **Код:** `internal/ui/archive_panel.go` (`viewArchiveMember`, `archiveReadOnly`), `internal/ui/document.go` (`finishCloseDocument`).

#### F5 из архива — извлечение выбранного
- **Поведение:** см. F5: `catalog.Relative` каждого пути (`outside archive`), `ExtractSelection(names, tmp)` — извлекаются выбранные поддеревья плюс цели их жёстких ссылок (`missing hard-link target`, `cyclic archive hard link`, `archive entry not found`, `no archive entries selected`); для ZIP (Store/Deflate) читаются только выбранные члены с проверкой CRC, иначе полный проход bsdtar. Затем обычное копирование из temp в назначение (слияние каталогов, коллизии, прогресс, Esc); temp удаляется всегда. Запись в архив невозможна (противоположная панель-архив как назначение → `Choose a regular destination panel; writing into archives is not supported`).
- **Код:** `internal/ui/transfers.go`, `internal/fsops/archive_index.go` (`ExtractSelection`), `archive_zip_selection.go`.

#### Shift+F1 — Create archive
- **Вызов:** `Shift+F1`, меню `Files → Create archive…`.
- **Где доступно:** локальная активная панель и локальная противоположная; внутри архива → `Archive is read-only…`; пустой выбор — ничего.
- **Диалог:** `prompt` `Create archive`, текст `New filename: .zip / .tar.gz / .tar.bz2 / .tar.xz / .7z`, значение `<путь другой панели>/<basename первого выбранного>.zip`.
- **Поведение:** `workProgress("Creating archive")` → `Attempt("create archive", <пути через \n>, dst)` → `fsops.CreateArchive(paths, expand(s, <путь активной панели>))`. Проверки: `no files selected`, `archive already exists: <dst>` (никогда не перезаписывает), `duplicate archive root: <name>` (одинаковые basename), `archive destination is inside the source directory`. Сначала `o.Scan(paths)` (итог известен → проценты/ETA), запись во временный файл `.farshmac-zip-*`/`.farshmac-archive-*` рядом, публикация `os.Link` (без замены). Имена в архиве — относительно родителя каждого корня. Симлинки сохраняются как ссылки; спецфайлы → `unsupported special file: <path>`; > 100 000 записей → `archive exceeds 100000 entries`. ZIP: Deflate, каталоги с `/`. Сжатие bsdtar может продолжаться после отправки всех данных — отмена доступна.
- **Код:** `internal/ui/transfers.go` (`archiveCreate`), `internal/fsops/formats.go` (`CreateArchive`, `checkArchiveDestination`, `writeArchiveSources`), `archive.go` (`CreateZIP`).

#### Shift+F2 — Extract archive
- **Вызов:** `Shift+F2`, меню `Files → Extract archive…`, пункт `Extract to folder…` в Shift+F3.
- **Где доступно:** локальные панели (активная и противоположная не remote). Источник — текущий элемент; внутри архивной панели — сам файл архива. Формат текущего файла **не проверяется** (неархив даст ошибку декодера).
- **Диалог:** `prompt` `Extract archive`, текст `New destination folder`, значение `<filesystemPath другой панели>/<ArchiveStem(имя)>` (`ArchiveStem` снимает известный суффикс; пустая основа → `archive`).
- **Поведение:** `workProgress("Extracting archive")` → `Attempt("extract archive")` → `fsops.ExtractArchive`: назначение должно отсутствовать (`destination already exists: <dst>`, слияния нет); staging `.farshmac-extract-*` рядом (0700) через `os.Root`; валидация имён (`unsafe archive path`, `invalid root archive entry`, `duplicate archive entry`, `unsupported archive entry type`, `unsafe archive link`, `hard link target is not a regular file`); файлы создаются `O_EXCL` с правами `mode|0600`, каталоги `mode|0700`, mtime файлов восстанавливается; ссылки создаются после файлов (жёсткие — только на уже извлечённые обычные файлы, символические — относительные внутри корня, потом проверка `Stat`); `.DS_Store` пропускается при `IgnoreDSStore`; публикация `Move(staging, dst)` (RENAME_EXCL). Ошибка/отмена удаляет staging. Прогресс `Streaming` (итог неизвестен).
- **Код:** `internal/ui/transfers.go` (`archiveExtract`, `archiveExtractPath`), `internal/fsops/formats.go` (`extractArchive`, `safeArchiveName`, `ArchiveStem`).

#### Shift+F3 — Archive commands
- **Вызов:** `Shift+F3`, меню `Files → Archive commands…`.
- **Где доступно:** локальная панель; путь = текущий элемент (в архиве — сам архив); пусто — ничего.
- **Диалог:** список `Archive: <basename>` с пунктами `List contents`, `Test archive`, `Extract to folder…` (кнопки `Select`/`Cancel`).
- **Поведение:** `List`/`Test` → `backgroundProgress("Reading archive")` → `fsops.ArchiveContents(path, test)` через bsdtar (все члены читаются полностью даже для списка); результат показывается как документ-отчёт с заголовком `Archive: <basename>`: строки `%12d  <имя>` (управляющие символы → `�`, каталоги с `/`, ссылки ` -> "target"`), при усечении `Listing truncated (10,000 rows / 4 MiB); all entries were read.`, при тесте `Archive integrity: OK (all entries decoded; available format checksums verified)`. `Extract to folder…` → Shift+F2.
- **Код:** `internal/ui/transfers.go` (`archiveCommands`), `internal/ui/commands.go` (`report`), `internal/fsops/formats.go` (`ArchiveContents`). (`ZIPContents`/`ExtractZIP` в `archive.go` из UI не вызываются.)

#### Ограничения архивов
- Только чтение: нет записи/редактирования/удаления/перемещения членов, записи в архив как назначение.
- Вложенные архивы не открываются напрямую (скопировать F5).
- Зашифрованные/многотомные — не поддерживаются (ошибка декодера; скрытый запрос пароля невозможен — нет терминала).
- `Shift+Enter` на члене → `Use F5 to copy this archive item out before opening it with macOS`.
- Восстановление сессии после перезапуска не возвращает в архив (см. ARCHIVE_SUPPORT.md; в коде `openArchiveAt` не вызывается при старте).

## Запуск и открытие файлов

#### Enter на элементе панели
- **Вызов:** `Enter` при пустой командной строке, двойной клик, `Ctrl/Cmd+PageDown` (только каталоги/архивы), выбор в fast find.
- **Поведение (локальная панель, `enterFile`):**
  1. Каталог с расширением `.app` (без учёта регистра) → `/usr/bin/open <path>` (запуск приложения); другой каталог → вход.
  2. Файл с `ArchiveFormat != ""` → вход в архив.
  3. Первая ассоциация (`Settings.Associations`) с непустым `Command`, чья маска подходит (`matchesMask`, с группами масок) → команда выполняется в терминале: `{}` заменяется на shell-quoted путь, иначе путь добавляется в конец; командная строка пользователя не затирается (`submitCommand(..., record=false)`), панели сменяются терминалом.
  4. Обычный файл с битом исполнения (`0111`) → в терминале выполняется `'<path>'` (quoted).
  5. Иначе → `/usr/bin/open <path>` (ассоциация macOS) в фоне `Opening <name>`; при ошибке статус и диалог `Opening <name> failed` с `<err>: <stdout/stderr>`.
- **Удалённая панель:** каталог → вход; файл → `openRemoteDocument(false)` (скачивание `Downloading remote file` во временный каталог и внутренний viewer; `viewer requires a regular remote file`).
- **Архив:** см. выше.
- **Ошибки:** `os.Stat` ошибка → статус.
- **Код:** `internal/ui/launch.go` (`enterFile`, `externalPlan`, `launch`, `submitCommand`), `internal/ui/app.go` (`execute`: `Shell unavailable`, `Shell is busy; ⌘O to return to it`).

#### Shift+Enter — Open with macOS
- **Вызов:** `Shift+Enter`, меню `Files → Open with macOS` (`⇧Enter`).
- **Поведение:** всегда `/usr/bin/open <абсолютный путь>` одним аргументом (без shell; ассоциации, exec-бит, viewer-настройки и командная строка игнорируются; черновик команды сохраняется). Каталог открывается в Finder, `.app` запускается, `..` открывает родителя. Выполняется как `workProgress("Opening <name>")` с отменой по Esc; ошибка → статус + диалог `Opening <name> failed` `[OK]` (тест: `No application is registered`).
- **Ограничения:** удалённая панель → `Copy remote files to a local panel before opening them with macOS`; член архива → `Use F5 to copy this archive item out before opening it with macOS`; пустая панель — ничего.
- **Код:** `internal/ui/launch.go` (`openSystemSelected`), `system_open_test.go`.

#### F3 / F4 — просмотр / правка (выбор внутреннего или внешнего)
- **Вызов:** `F3`/`F4`, `Cmd+3`/`Cmd+4`, `Numpad5` (=F3), меню `Files → View / folder size`, `Files → Edit`; `Alt+F3`/`Alt+F4` — инвертировать выбор внутренний/внешний; `Cmd/Ctrl+Shift+F3`/`F4` — принудительно внутренний.
- **Поведение (`openConfigured`):**
  - Каталог под курсором: F3 → расчёт размера папки (`calculateFolderSizes(false)`); F4 → диалог `File attributes` (см. ниже).
  - Удалённая панель → скачивание и внутренний документ (F4 — редактирование с последующей отправкой; детали у агента remote).
  - Архив → F3 просмотр члена; F4 → read-only сообщение.
  - Файл: `viewPlan`: базово `Settings.ExternalViewerDefault`/`ExternalViewer` (Viewer settings: `External by default`, `Command (blank=system)`) или `ExternalEditorDefault`/`ExternalEditor` (Editor settings). Первая подходящая ассоциация с непустым `Viewer`/`Editor` переопределяет команду и делает запуск внешним. `Alt` инвертирует результат. Внешний запуск: пустая команда → `/usr/bin/open <path>` (viewer) или `/usr/bin/open -t <path>` (editor, TextEdit/дефолтный текстовый редактор); иначе команда выполняется в терминале (`{}` или добавление quoted-пути). Внутренний → `openDocument(path, edit)`.
- **Код:** `internal/ui/launch.go` (`openConfigured`, `viewPlan`, `externalPlan`), `internal/ui/settings.go`.

#### File associations… (меню Commands)
- **Диалог:** список `File associations`: `Add association…`, `Open current file with macOS default app`, далее `<mask> → <command>` для каждой. Второй пункт запускает `/usr/bin/open <текущий путь>` (`workProgress("Opening")`).
- **Форма** `File association — {} inserts filename`: поля `Masks`, `Run command`, `Viewer command`, `Editor command`; маски валидируются (`validateMasks`, ошибка в статус); пустая маска или все три команды пустые — ассоциация удаляется; сохраняется `saveSetup()`.
- **Код:** `internal/ui/commands.go` (`fileAssociations`).

#### F2 — User menu
- **Вызов:** `F2`, `Cmd+2`, меню `Commands → User menu`.
- **Поведение:** если `Settings.UserMenu` пуст — сразу открывается редактор меню. Иначе список `User menu` с именами команд; выбор → `runUserCommand`: на удалённой панели → `This command requires a local panel…`; `{}` заменяется на shell-quoted путь текущего элемента (если `{}` нет — путь **не** добавляется), команда записывается в `a.Command`, выполняется `execute()` (попадает в историю команд, открывается терминал).
- **Редактирование (`Commands → Edit user menu…`):** список `Edit user menu`: `Add command…` + существующие; форма `User command — {} inserts filename` с полями `Label`, `Command (empty deletes)`; пустое имя или команда удаляет запись; `saveSetup()`.
- **Код:** `internal/ui/commands.go` (`userMenu`, `editUserMenu`, `runUserCommand`).

#### Apply command… (меню Files)
- **Где доступно:** локальная панель, не архив, есть выбор.
- **Диалог:** `prompt` `Apply command`, текст `Use {} for a shell-quoted filename`, пусто.
- **Поведение:** для каждого выбранного пути формируется команда (`{}` → quoted путь, иначе ` <quoted путь>` в конец); все команды объединяются через `\n` в `a.Command` и выполняются одной отправкой в терминал.
- **Код:** `internal/ui/commands.go` (`applyCommand`).

## Поиск и истории

#### Alt+F7 — Find file / Find folder
- **Вызов:** `Alt+F7`, меню `Commands → Find file…` (`AltF7`); `Commands → Find folder…` (без клавиши).
- **Где доступно:** локальная панель (remote → `This command requires a local panel…`); корень поиска — путь активной панели (в архиве — виртуальный путь, поиск по нему завершится ошибкой `Lstat`).
- **Диалог (форма `Find file` / `Find folder`):** `Name masks:` (`*`), `Containing text:` (пусто), `[x] Search subfolders`, `[ ] Case sensitive text` (флажки в 2 колонки), кнопки `{ Find }` `[ Cancel ]`. Маски валидируются (`validateMasks`, ошибка в статус).
- **Поведение:** `backgroundProgress("Searching <root>")`, `fsops.WalkDir` (без следования симлинкам, батчи по 256, отменяемо): нечитаемые пути считаются `skipped`; без `Search subfolders` обходится только корень; совпадение маски (для Find folder — только каталоги); текст ищется в обычных файлах блоками 64 КиБ с хвостом (`len(needle)*4+4`), без учёта регистра — через `ToLower`; лимит 10 000 результатов (`fs.SkipAll`, пометка `(limit)`).
- **Результат:** пусто → статус `No matches (N unreadable paths)`; иначе список `<title> — N found, M unreadable[ (limit)]` с относительными путями. Enter/`Select` → `revealPath`: каталог — вход в него; файл — загрузка его каталога и курсор на нём (или статус `File is hidden by the current panel filter`). Отдельных действий F3/F4 в списке результатов нет (это обычный список выбора).
- **Код:** `internal/ui/commands.go` (`findFiles`, `fileContains`, `revealPath`).

#### Alt+F8 — Command history
- **Вызов:** `Alt+F8`, `Commands → Command history` (`AltF8`).
- **Поведение:** список `Command history` (новые сверху) из `a.history` (команды, выполненные из командной строки панелей, лимит `HistoryLimit`, default 200, сохраняется в setup.json); выбор помещает команду в командную строку (**не** выполняет). Пусто → статус `Command history is empty`. Также `Cmd/Ctrl+↑/↓`, `Ctrl+E/X` листают историю в строке.
- **Код:** `internal/ui/commands.go` (`historyMenu("Commands")`).

#### Alt+F11 — View/edit history
- **Вызов:** `Alt+F11`, `Commands → File view/edit history`.
- **Поведение:** список `View/edit history` (новые сверху), элементы `View <path>` / `Edit <path>`; выбор → `openDocument(path, edit)` внутренним viewer/editor. Пусто → `View/edit history is empty`.
- **Код:** `commands.go` (`historyMenu("Files")`).

#### Alt+F12 — Folder history
- **Вызов:** `Alt+F12`, `Commands → Folder history`.
- **Поведение:** объединяет `a.folderHistory` с `History` обеих панелей (`appendUnique`, лимит `HistoryLimit`), список `Folder history` (новые сверху); выбор → `Load` в активной панели (ошибка в статус). Если пусто — статус `Folder history: empty`.
- **Код:** `commands.go` (`historyMenu("Folders")`, `appendUnique`).

## Прочие команды из `commands.go`

#### Task list (меню Commands → Task list)
- **Поведение:** `backgroundProgress("Reading processes")` → `/bin/ps -axo pid,ppid,%cpu,%mem,comm`; вывод показывается как документ-отчёт `Processes` (только просмотр; завершать процессы нельзя).
- **Код:** `commands.go` (`taskList`).

#### Compare folders (Commands → Compare folders)
- **Поведение:** без диалога; в обеих панелях отмечаются файлы (не каталоги), которых нет в другой панели, которые там каталог, отличаются размером или новее по mtime; предыдущие отметки сохраняются в `RestoreMarked`. Статус: `Marked files missing, different in size, or newer than the opposite panel (non-recursive)`.
- **Код:** `commands.go` (`compareFolders`).

#### File attributes… (Files → File attributes…, F4 на каталоге)
- **Где доступно:** локальная панель, не архив, есть выбор.
- **Диалог:** форма `File attributes`: `Permissions (octal):` (`%03o` первого выбранного), `Modified (local):` (`2006-01-02 15:04:05`).
- **Поведение:** валидация: `Permissions must be 000–777` (статус), ошибка разбора даты → статус; `workProgress("Updating attributes")` для всех выбранных: `Chmod` + `Chtimes(atime=mtime=stamp)`; симлинк → ошибка `attributes of symlinks are not changed: <path>` (операция прерывается).
- **Код:** `commands.go` (`fileAttributes`).

#### Alt+F6 / Files → Link… — Create link
- **Диалог:** форма `Create link`: `Link name:` (`<другая панель>/<имя текущего>`), `[x] Symbolic link`.
- **Поведение:** `os.Symlink(src, target)` или `os.Link` (жёсткая); ошибка в статус; refresh. Путь раскрывается относительно активной панели.
- **Код:** `commands.go` (`createLink`), `far_keys.go`.

#### Alt+F10 — Folder tree
- **Поведение:** `backgroundProgress("Reading folder tree")`, сканирование дерева (`scanTree`, `TreeDepth`), список `Folder tree` (`(partial)` при лимите/пропусках); выбор → переход в активной панели. Remote — недоступно.
- **Код:** `commands.go` (`treeMenu`), `tree.go`.

#### Quick view (`updatePreview`)
- **Поведение:** для панели в режиме `Quick view` при смене текущего элемента в другой панели: каталог — список до 100 записей (`Directory (preview, up to 100 entries)`); изображения/PDF — через helper (`Preview unavailable (unsupported, damaged or limit exceeded)`, `PDF · page 1 of N`, `<fmt> · W × H pixels`); обычный файл — первые 16 КиБ текста; remote → `Remote file: use F3 to download and view`. Не выполняется, пока `Busy`/открыт документ/терминал.
- **Код:** `commands.go` (`updatePreview`).

## Расхождения между документацией и кодом

> **Закрыто на ветке `backlog-fixes`:** решения по каждому пункту — в разделе «Решения по расхождениям» `docs/IMPLEMENTATION_BACKLOG.md`.

1. **Help (F1) в `app.go`:** «Cross-volume move is refused. Copy, verify, then delete explicitly.» — в коде `moveAcrossVolumes` выполняет копирование с полным сохранением метаданных и удаление источника (с диалогом `Cannot preserve metadata` при потерях). `docs/FILE_OPERATIONS.md` описывает это верно. Комментарий в `move_darwin.go` («Cross-volume moves are refused») тоже устарел.
2. **Help:** «Copies preserve permissions, timestamps and links, not ACLs/xattrs» — код при включённых флажках сохраняет ACL (`COPYFILE_ACL`), xattr (`COPYFILE_XATTR`), флаги и creation time, а диалог F5 прямо предлагает «Preserve permissions (mode, owner, group, ACL)» / «Preserve attributes (xattrs, flags, timestamps)».
3. **Help:** «F5 / F6 Copy / move or rename (no overwriting)» — перезапись существует через диалог коллизии (`Overwrite`/`Append`/`Rename`).
4. **Help:** «Shift+Enter Open selected item with macOS (folders in Finder)» и README — соответствует; но Help перечисляет `Alt+F11/F12`, а не `Alt+F7/F8` (поиск/история команд) — они есть в коде и в меню, в справке отсутствуют.
5. **ARCHIVE_SUPPORT.md:** «Enter opens an archive as a file panel, even when the archive has its executable bit set» — верно; однако `Ctrl+PgDn` использует отдельный список `isArchiveName` (`.zip .tar .gz .bz2 .xz .7z .rar .tgz .jar`), который не совпадает с `ArchiveFormat`: `.war`, `.cpio`, `.tbz`, `.tbz2`, `.txz` через Ctrl+PgDn не открываются, а одиночные `.gz/.bz2/.xz` проходят проверку, но `enterFile` их архивом не считает (откроет через macOS/ассоциацию).
6. **ARCHIVE_SUPPORT.md:** «F9 → Files → Archive commands also provides …» — верно, но Shift+F2 (Extract archive) в коде не проверяет, что текущий элемент — архив; неархив даёт ошибку декодера `archive operation failed (damaged, encrypted or unsupported archive)`. В документации это не описано.
7. **docs/FILE_OPERATIONS.md:** «Esc cancels a pending decision» — в коде Esc в диалоге-вопросе во время `Busy` отменяет **всю операцию** (`cancelOperation`), а не только текущее решение; результат тот же (`Cancel`), но формулировка неточна.
8. **docs/FILE_OPERATIONS.md:** «Read-only confirmation uses POSIX mode bits» — верно; но документ не упоминает, что для удалённых копий с включёнными флажками метаданных вопрос «Cannot preserve metadata» задаётся **на каждый файл** (ошибки `remote transfer cannot preserve owner/group and ACL` / `xattrs, flags and creation time` добавляются безусловно), пока не выбран `For all`.
9. **README:** «Cmd+Shift+H Shell command history: Enter to run, F4 to edit» относится к терминальной истории; история команд панелей (`Alt+F8`) только подставляет команду, не выполняет. README не упоминает Alt+F7/F8/F11/F12 и Shift+F1/F2/F3.
10. **ARCHIVE_SUPPORT.md:** «Restart restores the containing directory and selects the archive» — в коде восстановление сессии не рассматривалось в рамках этой области; подтвердить не удалось (нет вызовов `openArchiveAt` при старте — согласуется).
11. **ARCHIVE_SUPPORT.md:** таблица обещает «List / test / extract» для ZIP через общий интерфейс — в коде функции `ZIPContents`/`ExtractZIP` (Go-реализация для ZIP) существуют в `archive.go`, но UI их не использует: список/тест/полное извлечение всегда идут через bsdtar (`ArchiveContents`/`extractArchive`), а Go-ZIP используется только для индекса и выборочного извлечения (F5/F3) и для создания.
12. **Help:** «F7 New directory» — заголовок диалога в коде `Make folder`; «F8 / Delete Move selected items to Trash» — верно; `Cmd+8` тоже Trash (не документировано нигде, кроме общего «Cmd+1 … Cmd+9»).
13. **docs/FILE_OPERATIONS.md:** «Rename asks for a destination path» — в коде форма называется `Rename` с полем `New name`, кандидат `<stem> (copy)<ext>`; документ не описывает формат кандидата.


<a id="часть-3"></a>
# Часть 3. Внутренний просмотрщик и редактор

Источник истины — код в `internal/ui/document*.go`, `internal/ui/remote_document.go`, `internal/ui/launch.go`, `internal/textfile/*.go`, `internal/highlight/*.go` на момент коммита `b91313a`. Строки интерфейса приведены дословно (английский). Все пути — относительно корня репозитория `/Users/ikim/src/remote-codex/farshmac`.

## 0. Модель данных и общие правила

- Документ описан структурой `Document` (`internal/ui/document.go`). Ключевые поля:
  - `*textfile.Buffer` — текст (`Text`, всегда с `\n`-окончаниями после `Normalize`), `Cursor`/`Anchor` (байтовые смещения UTF-8), `Rect` (прямоугольное выделение), `Overwrite`, `ReadOnly`, стеки `UndoStack`/`RedoStack`.
  - `Path` — локальный путь (для удалённых — путь временного файла-кэша; для архивов — «виртуальный» путь элемента внутри архива); `displayPath()` даёт отображаемое имя (`Remote.Label()+RemotePath` для удалённых).
  - `Edit` (редактор/просмотрщик), `Dirty`, `Mode` (`"text"`, `"hex"`, `"dump"`), `Wrap`, `WordWrap`, `Codepage`, `EOL` (`"LF"`, `"CRLF"`, `"CR"`), `BOM`.
  - `Raw` — исходные байты (только для файлов ≤ 64 MiB), `SavedRaw` — байты последней загруженной/сохранённой версии, `File *os.File` — открытый дескриптор для постраничного чтения больших файлов (в память не загружаются), `Size`, `ByteOffset` (позиция в hex/dump), `BytesPerLine`.
  - `Offset` (номер первой видимой строки), `Column` (горизонтальная прокрутка), `ViewStart` (байтовое смещение первой видимой визуальной строки — используется в режиме переноса).
  - `HideKeys`, `HideStatus`, `Scrollbar` (по умолчанию `true`), `Bookmarks map[int]docPosition`, `Positions []docPosition` (история позиций, ≤100), `Search textfile.Search`, `HexQuery []byte`, `MatchStart/MatchEnd` (подсветка hex-совпадения), `Help` (документ справки), `ScreenID`, `Remote/RemotePath/RemoteInfo`, `ArchiveSource`, `TempDir`, `TabSize`, `ManualScroll`.
- `Dirty` пересчитывается в `Document.modified()`: `Text != SavedText || Codepage != SavedCP || EOL != SavedEOL || BOM != SavedBOM`. Т.е. смена кодировки/EOL/BOM в редакторе тоже делает документ «грязным».
- `markSaved()` фиксирует `SavedRaw/SavedText/SavedCP/SavedEOL/SavedBOM`, сбрасывает `Dirty`.
- Константа `maxTextBytes = 64 * 1024 * 1024` (64 MiB) — предел загрузки текста в память.
- В `App` открытый документ — `a.Doc`; остальные открытые — стек `a.Documents []*Document`. Пока `a.Doc != nil`, все клавиши идут в `documentKey` (кроме глобальных: Cmd+N, Cmd+Q, Ctrl+Tab/Cmd+Shift+[ ] — переключение экранов, Cmd+O — терминал; при открытой форме/диалоге — в них).
- Режим `a.Async` (боевой режим приложения, `NewAsync`): открытие, сохранение, поиск, замена всех, смена кодировки и F6 выполняются как фоновые задачи `backgroundTask` на клоне документа (`cloneDocument`), результат применяется на engine queue (`*d = *copy`). Пока задача идёт, приложение `Busy`, показан прогресс с кнопкой `Cancel`, Esc отменяет. В тестах (`New`) всё синхронно.
- Статусные сообщения (`a.Status`) показываются в документе в правом верхнем углу (см. §3.1).

---

## 1. Открытие документов

### 1.1 Открытие из панели

#### F3 / F4 — просмотр / редактирование текущего файла
- **Вызов:** F3 / F4; Cmd+3 / Cmd+4; клик по key bar `3View` / `4Edit`; меню Files → `View / folder size` (F3), `Edit` (F4).
- **Где доступно:** панели (локальная, удалённая, архивная).
- **Поведение (`App.openConfigured(edit, alternate=false, forceInternal=false)`, `internal/ui/launch.go`):**
  1. F3 на папке → подсчёт размера папки (`folderSize`), документ не открывается. F4 на папке (локальной) → диалог атрибутов файла (`fileAttributes`).
  2. Удалённая панель → `openRemoteDocument(edit)` (§1.4); настройки внешних программ игнорируются.
  3. Архивная панель → F3: `viewArchiveMember()` (§1.5); F4: `archiveReadOnly()` → статус `Archive is read-only. Use F5 to copy files out.`
  4. Локальная панель: `os.Stat(path)`; ошибка → статус с текстом ошибки. Далее `viewPlan` (§2) решает — внутренний документ (`openDocument(path, edit)`) или внешняя программа.
- **Код:** `launch.go: openConfigured, viewPlan, launch`; `app.go: function(3/4)`, обработка `F3`/`F4` в `key`.

#### Alt+F3 / Alt+F4 — «альтернативный» просмотр/редактирование
- **Вызов:** Alt+F3 / Alt+F4 в панелях.
- **Поведение:** `openConfigured(edit, alternate=true, false)` — результат выбора «внутренний/внешний» инвертируется: если по настройкам/ассоциациям должен был открыться внешний — откроется внутренний, и наоборот (§2).
- **Код:** `app.go` (ветка `if m.Alt { case "F3","F4": ... }`), `launch.go: viewPlan`.

#### Cmd+Shift+F3 / Cmd+Shift+F4 (также Ctrl+Shift) — принудительно внутренний
- **Вызов:** F3/F4 с Shift и Meta или Ctrl.
- **Поведение:** `openConfigured(edit, false, forceInternal=true)` — всегда внутренний просмотрщик/редактор, независимо от настроек и ассоциаций.
- **Код:** `app.go` (`if (m.Key == "F3" || m.Key == "F4") && m.Shift && (m.Meta || m.Ctrl)`).

#### Shift+F4 — новый файл / открыть по имени
- **Вызов:** Shift+F4 в панелях или в редакторе; меню Files → `New file…` (`ShiftF4`).
- **Где доступно:** только локальная панель. Удалённая → статус `This command requires a local panel. Use F5 to copy remote files first.`; архив → `Archive is read-only. Use F5 to copy files out.`
- **Диалог:** prompt, заголовок `Edit file`, подпись `Existing file or new file name`, значение по умолчанию пустое (если включено `RememberDialogValues` — последнее введённое значение для этого заголовка).
- **Поведение:** пустая строка — ничего. Путь разворачивается относительно пути активной панели (`expand`). Если файл существует — `openDocument(path, true)`; если не существует — создаётся пустой документ в памяти (`newDocument(path, "")`, `Edit=true`, `Codepage = Settings.DefaultCodepage`, EOL `LF`), файл на диске создаётся только при сохранении (через `fsops.Create`, права 0644). Другая ошибка `os.Stat` → статус.
- **Код:** `document.go: newFile`; `settings.go` (`Code pages` → `Default encoding for new files`).

#### Enter на файле (не вьюер)
- Enter в панели не открывает внутренний просмотрщик: архивы открываются как папки, ассоциации с `Command` запускаются, исполняемые файлы (`0111`) запускаются в терминале, остальное — `/usr/bin/open` (системное приложение). Shift+Enter → всегда macOS-приложение (`openSystemSelected`); для удалённой панели статус `Copy remote files to a local panel before opening them with macOS`, для элемента архива — `Use F5 to copy this archive item out before opening it with macOS`.
- **Код:** `launch.go: enterFile, openSystemSelected`.

#### Alt+F11 — история просмотра/редактирования
- **Вызов:** Alt+F11 в панелях; меню Commands → `File view/edit history` (в меню без указанной клавиши).
- **Поведение:** список `View/edit history` (choose-форма), элементы `View <path>` / `Edit <path>`, новые сверху. Выбор → `openDocument(path, edit)` напрямую (внутренний документ, минуя настройки внешних программ и удалённые/архивные панели). Пустая история → статус `View/edit history is empty`.
- **История:** `a.viewHistory []viewRecord{Path, Edit}` пополняется при каждом открытии (`openDocument`/`openDocumentAsync`), обрезается до `Settings.HistoryLimit` (по умолчанию 200, 1–10000), сохраняется в setup (`savedSetup.Viewed`).
- **Код:** `commands.go: historyMenu("Files")`, `far_keys.go` (Alt+F11), `document.go: openDocument`, `document_tasks.go: openDocumentAsync`, `settings.go`.

### 1.2 Загрузка локального файла (`openDocument`)

#### Алгоритм открытия
- **Поведение (`App.openDocument(path, edit)`, `document.go`):**
  1. В Async-режиме → `openDocumentAsync` (фоновая задача `Opening file`; рабочая копия `App` с теми же `Settings` выполняет синхронный `openDocument`; если документ не открылся, текст `worker.Status` становится ошибкой задачи; `ScreenID` выделяется в окне-владельце).
  2. `filepath.EvalSymlinks(path)` — документ открывается по разрешённому пути. Ошибка → статус.
  3. `os.Open`, `Stat`; не обычный файл → статус `Choose a regular file`.
  4. Создаётся `newDocument(resolved, "")`: `Codepage "UTF-8"`, `EOL "LF"`, `Mode "text"`, `Scrollbar true`. `d.Wrap = Settings.ViewerWrap` (настройка `Wrap lines` в `Viewer settings`). Запись в `viewHistory`.
  5. **Размер > 64 MiB:**
     - редактор (`edit=true`) → статус `Editor limit is 64 MiB. Large files open in paged hex/dump viewer.`, документ не открывается;
     - просмотрщик → `d.File = f` (дескриптор остаётся открытым), `Mode = "hex"`, `Raw` пустой; данные читаются постранично `ReadAt` (`dataAt`). Текстовый режим для таких файлов недоступен.
  6. **Размер ≤ 64 MiB:** читается целиком через `io.LimitReader(f, maxTextBytes+1)` с учётом прогресса/отмены операции. Если прочитано больше лимита (файл вырос) → `File grew while opening; reopen it`.
  7. `Codepage = textfile.Detect(raw)` (§7.1); `Decode` — ошибка → статус (`invalid UTF-8; choose another codepage` и т.п.) и документ не открывается.
  8. `binary := strings.ContainsRune(decoded, 0)` (есть NUL).
  9. Для редактора: проверка обратимости — `Encode(decoded, cp, bom)` должен побайтно совпасть с `raw`, иначе статус `Encoding is not lossless; inspect in viewer and choose a codepage before editing`. Для бинарного файла → `Binary data: open with F3 in hex/dump mode`.
  10. `BOM`, `EOL = DetectEOL(decoded)`, `Buffer = textfile.New(decoded)` (CRLF/CR нормализуются в `\n`), бинарный файл в просмотрщике → `Mode = "hex"`. `markSaved()`, строится индекс строк, `showDocument(d)`.
- **Показ (`showDocument`):** выделяет `ScreenID` (счётчик `a.nextScreenID`), текущий `a.Doc` (если другой) кладётся в стек `a.Documents`, закрывает меню.
- **Особые случаи:** открытие по ссылке → реальный путь; при повторном открытии того же файла создаётся отдельный экран (дубликаты не объединяются).
- **Код:** `document.go: openDocument, newDocument, showDocument`; `document_tasks.go: openDocumentAsync`.

### 1.3 Внешние просмотрщик и редактор — см. §2.

### 1.4 Открытие из удалённой панели (FTP/FTPS/SFTP)

#### Скачивание во временный файл
- **Вызов:** F3/F4 (и Alt/Cmd+Shift-варианты — все ведут сюда), Enter на файле удалённой панели (просмотр).
- **Поведение (`remote_document.go: openRemoteDocument(edit)`):** фоновая задача `Downloading remote file`:
  1. `config.Dial`, `Lstat(remotePath)`; не обычный файл → ошибка `viewer requires a regular remote file`.
  2. `os.MkdirTemp("", "farshmac-remote-view-*")`, файл `<tmp>/<basename>` создаётся `O_CREATE|O_EXCL`, права 0600.
  3. Копирование с прогрессом (буфер 256 KiB); если скопировано не `info.Size()` байт → `remote file changed during download`.
  4. Рабочий `App` выполняет `openDocument(local, edit)` — все проверки §1.2 (лимит 64 MiB, кодировка, бинарность) применяются; текст статуса рабочего `App` становится ошибкой задачи.
  5. `doc.Remote = &config`, `RemotePath`, `RemoteInfo = info` (используется при сохранении), `TempDir = dir`.
  6. Применение: `showDocument(doc)`, статус `<label><remotePath>` (`config.Label()+remotePath`).
- **Очистка:** при отмене/ошибке временный каталог удаляется; при закрытии документа `finishCloseDocument` удаляет `TempDir`.
- **Ограничения:** редактирование разрешено (F4), сохранение — см. §9.9; F3 с Alt не переключает на внешнюю программу.
- **Код:** `remote_document.go: openRemoteDocument`, `launch.go: openConfigured, enterFile`.

### 1.5 Открытие из архива — только просмотр

#### Просмотр элемента архива
- **Вызов:** F3 в архивной панели; Enter на файле внутри архива (`enterArchiveMember`).
- **Поведение (`archive_panel.go: viewArchiveMember`):**
  - Папка/пустая панель → ничего. Символическая ссылка внутри архива → статус `Use F5 to copy the archive link`. Enter на вложенном архиве → `Copy the nested archive out with F5 to open it` (F3 на вложенном архиве просматривает его как файл).
  - Фоновая задача `Opening archive member`: `MkdirTemp("", "farshmac-archive-view-*")`, `catalog.ExtractSelection([name], <tmp>/content)`, `openDocument(<tmp>/content/<name>, false)`.
  - `doc.Path = <виртуальный путь элемента>`, `doc.ReadOnly = true`, `doc.ArchiveSource = true`, `doc.TempDir = temp`.
- **Ограничения для архивных документов:** F4 в панели → `Archive is read-only. Use F5 to copy files out.`; F6 в документе → `Copy the file out with F5 before editing`; `+`/`-` → `Return to the archive panel to choose another file`.
- **Код:** `archive_panel.go: viewArchiveMember, enterArchiveMember, archiveReadOnly`; `document.go: documentKey` (первая проверка), `document_keys.go: adjacentFile`.

---

## 2. Внешние просмотрщик/редактор и ассоциации

#### Настройки внешних программ
- **Вызов:** F9 → Options → `Viewer settings…` / `Editor settings…`.
- **Диалоги:**
  - `Viewer settings`: `Wrap lines` (toggle, `Settings.ViewerWrap`), `External by default` (toggle, `ExternalViewerDefault`), `Command (blank=system)` (`ExternalViewer`).
  - `Editor settings`: `Auto indent` (toggle), `Syntax highlighting (Chroma)` (toggle), `Tab width (1–16)`, `Expand tabs to spaces` (toggle), `External by default` (`ExternalEditorDefault`), `Command (blank=system)` (`ExternalEditor`). Ошибка валидации → `Tab width must be 1–16`.
  - Options → `Code pages` → choose `Default encoding for new files` (список `textfile.Codepages`).
- **Значения по умолчанию:** `EditorAutoIndent true`, `EditorSyntax true`, `EditorTabSize 4`, `EditorExpandTabs false`, `ViewerWrap false`, внешние — выключены, команды пустые, `DefaultCodepage "UTF-8"`.
- **Код:** `settings.go` (формы), `menu.go` (Options).

#### Выбор внутренний/внешний (`viewPlan`)
- **Поведение (`launch.go: viewPlan(path, edit, alternate, forceInternal)`):**
  1. `forceInternal` → внутренний.
  2. Базовая пара: для просмотра `ExternalViewerDefault`/`ExternalViewer`, для редактирования `ExternalEditorDefault`/`ExternalEditor`.
  3. Ассоциации (`Settings.Associations`, Commands → `File associations…`): первая запись, чья маска (`matchesMask`, с группами `MaskGroups`) совпадает с именем файла; если у неё непустой `Viewer` (для F3) / `Editor` (для F4) — эта команда используется и файл считается «ассоциированным» (`useExternal = true`).
  4. `useExternal = external || associated`; `alternate` инвертирует.
  5. Внутренний → `LaunchPlan{Internal: true}`; иначе `externalPlan(path, command, edit)`.
- **Запуск внешней программы (`externalPlan`, `launch`):**
  - Пустая команда: просмотр → `/usr/bin/open <path>`; редактирование → `/usr/bin/open -t <path>`. Выполняется как фоновая операция `Opening <basename>` (`workProgress`), ожидается завершение процесса `open` (он возвращается сразу после передачи файла приложению), ошибка → `<err>: <вывод>`.
  - Непустая команда: путь экранируется (`terminal.Quote`); если в команде есть `{}` — подставляется вместо каждого `{}`, иначе добавляется через пробел. Команда выполняется в встроенном терминале через `submitCommand(cmd, record=false)` → `execute()` (панели сменяются терминалом, команда не попадает в историю командной строки). Ожидания завершения нет — это обычная shell-команда.
- **Код:** `launch.go: viewPlan, externalPlan, launch, submitCommand`.

---

## 3. Экран документа

### 3.1 Статусная строка (верхняя)
- **Где:** строка 0, чёрный текст на `Cyan`; скрывается `HideStatus` (тогда текст начинается со строки 0).
- **Текст (text-режим и редактор):** `" %s  %s  %s %s  Ln %d Col %d"` = `tail(displayPath, 32)` (длинные пути обрезаются слева с `…`), режим, кодировка, EOL, номер строки и колонки (1-based; колонка — в рунах, табуляция считается одним символом).
  - Режим: просмотрщик — `TEXT` / `HEX` / `DUMP`; редактор — `EDIT INS` или `EDIT OVR`; добавляется ` RO` при `ReadOnly` и ` *` при `Dirty`.
  - Если включена подсветка и для документа есть статус подсветки — добавляется `  [<status>]`, где status — имя языка Chroma (например `[Go]`), `syntax off: >1 MiB` или `syntax paused (limit/error)`.
- **Текст (hex/dump в просмотрщике):** `" %s  %s  Offset %d / %d"` — путь, режим, `ByteOffset`, `Size`.
- **Сообщения `a.Status`:** рисуются поверх правого края строки 0 жёлтым (`Yellow`) на `Blue`, обрезаются до `min(32, W/3)` символов слева с `…`. Не показываются при `HideStatus`.
- **Код:** `document_draw.go: drawDoc`, `grid.go: tail`.

### 3.2 Key bar (нижняя строка)
- **Где:** строка `H-1`; скрывается `HideKeys`. 10 ячеек шириной `W/10`, номер серым на чёрном, подпись чёрным на `Cyan`.
- **Подписи просмотрщика:** `1Help 2Wrap 3Close 4Hex 5 6Edit 7Search 8Codepg 9Options 10Close`.
- **Подписи редактора:** `1Help 2Save 3 4Close 5 6View 7Search 8Codepg 9Options 10Close`.
- **Мышь:** клик по строке key bar → `documentKey(F<n>)`, `n = min(10, X*10/Cols+1)` (без модификаторов).
- **Код:** `document_draw.go: drawDoc, documentClick`.

#### Cmd+B / Cmd+Shift+B — скрыть key bar / статус
- **Вызов:** Cmd+B (или Ctrl+B) переключает `HideKeys`; Cmd+Shift+B — `HideStatus`.
- **Где:** оба режима. Настройка — на документ, не сохраняется.
- **Код:** `document.go: documentKey` (`case "b"`).

### 3.3 Полоса прокрутки
- **Где:** правая колонка (`W-1`), только в text-режиме/редакторе, если `Scrollbar` и число строк > высоты. Трек `░` (`Cyan`), ползунок `█` (`LightCyan`) на `top + Offset*(height-1)/(lines-1)`.
- **Переключение:** Cmd+S в просмотрщике (в редакторе Cmd+S — сохранить); F9 → `Scrollbar`. По умолчанию включена.
- **Перетаскивание:** клик/drag по колонке `W-1` → `Offset = (Y-top)*(lines-1)/(Rows-top-2)`, `ViewStart = At(Offset,0)`, `ManualScroll = true`; флаг `documentScrollbarDrag` держит X на полосе до `mouseUp`.
- **Код:** `document_draw.go: drawDoc, documentClick`; `app.go: Handle("mouseUp")`; `mouse.go: dragMouse`.

### 3.4 Отрисовка текста
- Фон всего экрана `LightCyan` на `Blue`. Ширина области текста `W-1` (колонка под полосу прокрутки).
- Табуляция расширяется до следующей позиции `tabWidth()` (1–16, по умолчанию 4; берётся из `Settings.EditorTabSize` при каждой отрисовке и для просмотрщика тоже). Управляющие символы рисуются как `·` шириной 1. Широкие (CJK) символы занимают 2 колонки; символы нулевой ширины приклеиваются к предыдущей ячейке.
- Курсор редактора — ячейка `Blue` на `Gray`. Выделение — `Black` на `Cyan` (поверх цветов подсветки синтаксиса).
- **Автопрокрутка редактора:** если не `ManualScroll`, `Offset` подтягивается так, чтобы строка курсора была видна; `Column` подтягивается, чтобы курсор был в `[Column, Column+W-1)` (`Column = col-W+2` при выходе вправо). `ManualScroll` ставится колесом/полосой прокрутки и сбрасывается при любой клавише.
- В редакторе перенос строк (`Wrap`) не применяется — при отрисовке временно отключается.
- **Код:** `document_draw.go: drawDoc, visualRows, displayWidthTabs`.

### 3.5 Темы
- `Settings.ColorTheme`: пусто/классика — цвета как есть; `"Dark"` — `Blue→0x151b24`, `LightCyan→0xc7d7e9`, `Cyan→0x486987`; `"Light"` — `Blue→0xf1f3f5`, `LightCyan→0x182b49`, `White→0x000000`, `Yellow→0x885000`, `Cyan→0x80b8dd`. Подмена выполняется по всей сетке после отрисовки (включая документ, диалоги). Цвета подсветки синтаксиса, совпадающие с константами (`0xffffff` keyword/operator, `0xffff55` strings), тоже подменяются; остальные (`0x55ff55`, `0xaaaaaa`, `0xff88ff`, `0xff8888`) остаются.
- **Код:** `draw.go: Draw` (конец), `grid.go` (константы).

---

## 4. Режимы просмотра

#### F4 — Text / Hex; Shift+F4 — выбор режима
- **Вызов:** F4 (просмотрщик). Shift+F4 → choose `View mode` со списком `Text`, `Hex`, `Dump`.
- **Где:** просмотрщик. (В редакторе F4 закрывает документ, Shift+F4 — новый файл.)
- **Поведение:** F4: `text → hex`; из hex/dump → `text`, но для постраничного файла (`File != nil`) → `dump`. Shift+F4 для постраничного файла при выборе `Text` → статус `Large files currently support paged hex/dump only`.
- **Код:** `document_keys.go: viewerKey`.

#### Hex-режим (вид)
- Строка: `"%010X: "` (смещение, серым, 12 колонок) + байты `"%02X "` (по 3 колонки с позиции 12) + ASCII-колонка с позиции `13+width*3` (байты <32 и >126 → `·`).
- Ширина строки `binaryWidth()`: если `BytesPerLine>0` — `min(BytesPerLine, (Cols-14)/4)`; иначе `min(16, (Cols-14)/4)`, минимум 1.
- Подсветка совпадения hex-поиска `[MatchStart, MatchEnd)` — `Black` на `Cyan`.
- Данные: `dataAt(ByteOffset, width*height)` — из `Raw` или `File.ReadAt`.
- **Код:** `document_draw.go: drawBinary, binaryWidth`.

#### Dump-режим (вид)
- Строка: смещение + по одному символу на байт. Каждый байт декодируется отдельно через `textfile.Decode([]byte{b}, Codepage)`: для однобайтовых кодировок — символ кодировки; для UTF-8 байты >126 (не валидны поодиночке) → `·`; для UTF-16 (нечётная длина → ошибка) байты >126 → `·`. Байты <32 и 127 → `·`.
- Ширина: `Cols-14` (если `BytesPerLine` не задан; иначе действует hex-ограничение `min(BytesPerLine,(Cols-14)/4)` — побочный эффект Alt+Left/Right).
- **Код:** `document_draw.go: drawBinary`.

#### Alt+Left / Alt+Right — байт на строку
- **Вызов:** Alt+← / Alt+→ в hex/dump.
- **Поведение:** `BytesPerLine = max(1, width-1)` / `min(64, width+1)`, где `width` — текущая `binaryWidth()`. Фактическая ширина всегда ограничена `(Cols-14)/4`.
- **Код:** `document_keys.go: viewerKey`.

#### F2 — перенос строк; Shift+F2 — перенос по словам
- **Вызов:** F2 / Shift+F2 (просмотрщик).
- **Поведение:**
  - Если документ в hex/dump: F2 (с любым Shift) возвращает в `text` (для постраничного — в `dump` и статус `Large files currently support paged hex/dump only`), перенос не меняется.
  - F2: `Wrap = !Wrap`. Shift+F2: `WordWrap = !WordWrap` и `Wrap = true`.
  - После переключения `ViewStart = At(Offset, 0)`.
  - Исходное значение `Wrap` берётся из `Settings.ViewerWrap`; `WordWrap` по умолчанию false (перенос по символам).
- **Алгоритм переноса (`visualRows(width, count)`):** строка режется, когда суммарная ширина колонок превысит `width = Cols-1`; при `WordWrap` разрез переносится к последнему пробельному символу строки-отрезка (если он есть после начала отрезка). Табуляции учитываются по `tabWidth()`.
- **Код:** `document_keys.go: viewerKey`, `document_draw.go: visualRows, previousVisualStart`.

#### Большие файлы (> 64 MiB)
- Только просмотр, только hex/dump, постраничное чтение `ReadAt`. Поиск недоступен (`Search in paged files larger than 64 MiB is not implemented yet`). F6 → `Editor limit is 64 MiB`. F8 — молча игнорируется; Shift+F8 меняет только `Codepage` (влияет на dump). Подсветка синтаксиса отключена. Дескриптор закрывается при закрытии документа / переходе на соседний файл.
- **Код:** `document.go: openDocument, dataAt, finishCloseDocument`.

---

## 5. Навигация в просмотрщике

#### Текстовый режим без переноса
- **Клавиши (`viewerKey`):** ↓/↑ — `Offset ±1`; PgDn/PgUp — `±(Rows-2)`; Home — `Offset=0, Column=0`; End — `Offset = max(0, lines-Rows+2)` (последняя страница); ←/→ — `Column ∓1`, с Cmd/Ctrl — на 20 колонок; `Column` не ограничен сверху. `Offset` зажимается в `[0, lines-1]`, `ViewStart = At(Offset,0)`.
- **Код:** `document_keys.go: viewerKey`.

#### Текстовый режим с переносом
- ↓/↑ — на одну визуальную строку (`visualRows(...,2)[1].Start` / `previousVisualStart`); PgDn/PgUp — `Rows-2` визуальных строк; Home — `ViewStart=0`; End — начало последней логической строки. ←/→ не действуют. `Offset = Pos(ViewStart).Row`.
- **Код:** `document_keys.go: viewerKey`, `document_draw.go: previousVisualStart`.

#### Hex/dump
- ↑/↓ — `±width` байт; PgUp/PgDn — `±width*(Rows-2)`; Home — 0; End — `max(0, Size-page)`; ←/→ — `∓1` байт (Alt — см. §4). `ByteOffset` зажимается в `[0, Size-1]`.
- **Код:** `document_keys.go: viewerKey`.

#### Колесо мыши / трекпад
- **Сообщения:** `wheel` (Delta) и `scroll` (Delta = Y*3); `delta` зажимается в `[-12, 12]`.
- **Поведение (`scrollMouse`):** hex/dump — `ByteOffset += delta*width` (в `[0, Size-1]`); text с переносом (просмотрщик) — по визуальным строкам; иначе `Offset += delta` в `[0, lines-1]`. Ставит `ManualScroll = true` (в редакторе курсор не возвращает экран обратно до следующей клавиши). Не работает при открытом диалоге/занятости; при открытой форме/меню прокручивает их.
- **Код:** `mouse.go: scrollMouse`.

#### Клик и перетаскивание в тексте
- **Поведение (`documentClick(m, drag)`):** в hex/dump клики (кроме key bar) игнорируются. Иначе вычисляется позиция по строке `Y-top` среди `visualRows` и колонке `X+Column` (при переносе — `X`), с учётом табуляций и широких символов; `d.Move(target, selecting = drag || Shift, rect = Alt)` — т.е. клик ставит курсор, Shift+клик/перетаскивание выделяют, Alt — прямоугольно. В просмотрщике курсор/выделение тоже существуют (видны как выделение; используются для Cmd+C и как стартовая точка поиска).
- **Двойной клик:** выделяет «слово» — непрерывный ряд символов одного класса (буквы/цифры/`_`; пробелы; прочее), в пределах строки.
- **Тройной клик:** выделяет строку целиком вместе с `\n`.
- **Автопрокрутка при drag** за верх/низ области — на 1 строку за событие (`dragMouse`).
- **Код:** `document_draw.go: documentClick`, `mouse.go: dragMouse`, `app.go: click`.

#### `+` / `-` — следующий / предыдущий файл панели
- **Вызов:** клавиши `+` / `-` в просмотрщике (в редакторе это обычный ввод).
- **Поведение (`adjacentFile`):** архивный документ → `Return to the archive panel to choose another file`. Ищется ближайшая не-папка в активной панели вперёд/назад от курсора. Если текущий документ `Dirty` → confirm `Save changes` / `Save changes before viewing the next file?` / кнопки `Save`, `Discard`, `Cancel`. Далее курсор панели переставляется и файл открывается **во внутреннем просмотрщике** (`open(false)` → `openDocument`, минуя внешние настройки). `adjacentFile` вызывается только из `viewerKey`: в редакторе `+`/`-` — обычный ввод (Dirty-просмотрщик возможен после F6); старый документ заменяется (его `File` закрывается). Если открыть не удалось — остаётся старый документ и курсор панели. Нет файла → `No next file` / `No previous file`.
- **Код:** `document_keys.go: adjacentFile`.

#### Alt+F8 / Cmd+G — переход (Go to)
- **Вызов:** Alt+F8, Cmd+G (Ctrl+G).
- **Диалог:** prompt `Go to`; подпись `Line:column (1-based), or percentage e.g. 50%` (text/редактор) или `Byte offset (decimal / 0xHEX) or percentage` (hex/dump в просмотрщике); значение пустое.
- **Поведение:** позиция запоминается (`remember()` → Cmd+Z в просмотрщике вернёт). `NN%`: 0–100, иначе `Enter a percentage from 0 to 100`; в hex → `ByteOffset = (Size-1)*v/100`; в тексте → строка `(lines-1)*v/100`, курсор в её начало, `Offset`/`ViewStart` на неё. Hex: число (десятичное или `0x…`), вне `[0, Size)` → `Offset outside file`. Текст: `row[:col]`, 1-based; ошибка/выход за число строк → `Invalid line / column` (колонка > длины строки — не ошибка, курсор в конец строки); выделение снимается, `Offset = row-1`.
- **Код:** `document.go: gotoDocument`.

#### Cmd+0..9 / Cmd+Option+0..9 — закладки
- **Вызов:** Cmd (или Ctrl) + цифра 0–9: восстановить; Cmd+Option+цифра (или Ctrl+Shift+цифра) — сохранить. Цифра берётся из `m.Code` (`DigitN`), поэтому раскладка и Option не мешают. Cmd+Shift+цифра закладку не ставит: macOS забирает Cmd+Shift+3/4/5 под скриншоты.
- **Поведение:** сохраняет `bookmark{Kind: document, Path, Edit, Help, ScreenID, Row, Col, View: docPosition}` в `App.Bookmarks[n]` — те же десять слотов окна, что и закладки папок (раздел 9). Статус `Bookmark %d saved`. Переход (из панелей или любого экрана документа): если экран закладки (`ScreenID`) или другой экран с тем же файлом открыт — фокус на него, текущая позиция в `Positions`, затем `restoreBookmark`; иначе файл открывается заново в том же режиме (viewer/editor) и позиция восстанавливается. Закладка папки из документа → панели (см. раздел 9). ⌘0 сбрасывает размер шрифта только в терминале. Если текст в точке закладки не изменился — восстанавливается точный вид; иначе курсор ставится на запомненные строку и колонку, а если файл укоротился ниже этой строки — в конец файла. Файла нет или это не обычный файл — статус `Bookmark %d: <path>: <reason>`, ничего не открывается. Нет закладки — `Bookmark %d is empty`; закладка в закрытой справке — `Bookmark %d: help is closed`. Закладки в памяти, не сохраняются.
- **Код:** `bookmarks.go: documentBookmarkKey, saveDocumentBookmark, goDocumentBookmark, restoreBookmark`.

#### Cmd+Z в просмотрщике — предыдущая позиция
- **Поведение:** `Positions` (стек до 100) пополняется перед поиском (`findDocument`), Go to и переходом к закладке. Cmd+Z в просмотрщике снимает последнюю и восстанавливает. В редакторе Cmd+Z — undo (§9.5).
- **Код:** `document.go: remember, documentKey` (`case "z"`).

---

## 6. Поиск и замена

#### F7 / Cmd+F — поиск
- **Вызов:** F7; Cmd+F (Ctrl+F) — в обоих режимах всегда простой поиск. Cmd+F7 — в редакторе замена (§ниже), в просмотрщике простой поиск.
- **Где:** файлы ≤ 64 MiB. Постраничный → `Search in paged files larger than 64 MiB is not implemented yet`.
- **Диалог:** форма `Search`, поля: `Search` (значение — последний запрос `d.Search.Query`), toggle `Case sensitive`, `Whole words`, `Regex (Go/RE2)` (помнят прошлые значения), `Backwards` (всегда выкл.), `Hex bytes` (включён по умолчанию, если просмотрщик в hex-режиме). Esc — отмена, Enter — выполнить; Alt+буква — мнемоники полей/кнопок; Tab/стрелки — навигация.
- **Поведение текстового поиска (`findDocument` / `Buffer.Find`):**
  - Регулярное выражение компилируется как `(?m)` + (`(?i)` если не Case) + (QuoteMeta если не Regex). Пустой запрос → ошибка `enter search text`. Ошибка компиляции → её текст в статусе.
  - Все совпадения ищутся `FindAllStringIndex` по всему тексту; `Whole words` отбрасывает совпадения, у которых сосед — буква/цифра/`_`.
  - Старт: в просмотрщике без выделения курсор ставится в начало первой видимой строки (`At(Offset,0)`); при выделении старт = конец выделения (вперёд) или начало (назад); для пустого выделения вперёд — следующий символ. Вперёд — первое совпадение с `start >= старт`, иначе **первое в файле (циклически)**; назад — последнее с `start < старт`, иначе последнее в файле.
  - Найденное выделяется (`Anchor=start`, `Cursor=end`). `Offset` = строка совпадения, `Column = max(0, col-5)`, `ViewStart = Anchor`; в просмотрщике режим переключается в `text`. Статус `Match found`; нет совпадений → `Not found`. Поиск отменяем (`Operation.Check`), в Async — фоновая задача `Searching document` на клоне документа.
- **Hex-поиск (`Hex bytes`):** строка разбирается как hex-пары (пробелы игнорируются); ошибка/пусто → `Enter pairs of hex digits, e.g. 48 65 6c 6c 6f`. Ищется в `d.Raw` (исходные байты): вперёд от `ByteOffset` (или от `MatchEnd` предыдущего совпадения), с переходом на начало; назад — `LastIndex` до `MatchStart`, с переходом на конец. Найдено → `MatchStart/End`, `ByteOffset = hit`, `Mode = "hex"`. `Not found`. С `Replace` → `Hex replacement is not available in text editor`.
- **Код:** `document.go: searchDocument, findDocument`, `document_tasks.go: findDocumentThen`, `textfile/buffer.go: Search, Find, Matches`.

#### Shift+F7 / Alt+F7 / Space — следующее / предыдущее совпадение
- **Вызов:** Shift+F7 — вперёд; Alt+F7 — назад; Space в просмотрщике — вперёд.
- **Поведение:** `findDocument(backward)` с сохранённым `d.Search` или `HexQuery` (если последний поиск был hex — продолжается hex-поиск). Без предыдущего запроса → `enter search text`.
- **Код:** `document.go: documentKey`, `document_keys.go: viewerKey`.

#### Cmd+F7 / Cmd+H — поиск и замена
- **Вызов:** Cmd+F7 (Ctrl+F7) или Cmd+H (Ctrl+H) в редакторе. В просмотрщике Cmd+H ничего не делает.
- **Диалог:** форма `Search` с теми же 6 полями плюс `Replace with` (пусто) и toggle `Replace all` (выкл.).
- **Поведение:**
  - Без `Replace all`: ищется первое совпадение, затем confirm `Replace` / `Replace this match?` / кнопки `Replace`, `Skip`, `Cancel`. `Replace` заменяет выделенное совпадение (для Regex — `re.ReplaceAllString(matchText, replacement)`, т.е. поддерживаются `$1`, `${name}`), затем ищется следующее и диалог повторяется; `Skip` — следующее без замены; `Cancel`/Esc — стоп. Каждая замена — отдельная запись undo.
  - `Replace all`: `Buffer.ReplaceAll` — все совпадения за один `Replace(0, len)` (одна запись undo; для Regex — `ExpandString` с группами); статус `Replaced %d matches` (в т.ч. `Replaced 0 matches`); в Async — фоновая задача `Replacing matches`. При `ReadOnly` → ошибка `editor is read-only`.
- **Код:** `document.go: searchDocument, confirmReplacement`, `document_tasks.go: replaceAllAsync`, `textfile/buffer.go: ReplaceAll`.

---

## 7. Кодировки, BOM, окончания строк

### 7.1 Определение при открытии
- `textfile.Detect(raw)`: префикс `FF FE` → `UTF-16LE`; `FE FF` → `UTF-16BE`; валидный UTF-8 → `UTF-8`; иначе `Windows-1251` (эвристика по умолчанию для «не UTF-8»).
- `Decode(raw, cp)`: снимает BOM (`EF BB BF` для UTF-8, 2 байта для UTF-16; флаг `BOM=true`); ошибки: `invalid UTF-8; choose another codepage`, `incomplete UTF-16 code unit`, `unsupported codepage %s`. Для однобайтовых кодировок BOM не распознаётся.
- `DetectEOL`: есть `\r\n` → `CRLF`; иначе есть `\r` → `CR`; иначе `LF`. Внутри буфера все окончания нормализуются в `\n` (`Normalize`).
- Поддерживаемые кодировки (`textfile.Codepages`, в порядке списка): `UTF-8`, `UTF-16LE`, `UTF-16BE`, `Windows-1251`, `CP866`, `Windows-1252`, `ISO-8859-1`, `KOI8-R`.
- **Код:** `textfile/encoding.go`.

### 7.2 Кодирование при сохранении
- `Encode(Endings(Text, EOL), Codepage, BOM)`: BOM пишется только для UTF-8/UTF-16; для однобайтовых кодировок непредставимый символ → ошибка `text cannot be represented in %s: …` (сохранение не выполняется, файл не трогается). Если текст/кодировка/EOL/BOM не менялись — на диск пишутся ровно `SavedRaw` (сохранение побайтно, смешанные окончания не нормализуются).
- **Код:** `textfile/encoding.go: Encode, Endings`, `document.go: saveDocument`.

#### F8 — переключение CP866 / Windows-1251
- **Вызов:** F8 (без Alt/Shift).
- **Поведение:** целевая `cp = "CP866"`, а если текущая уже `CP866` — `Windows-1251` (из UTF-8 первое нажатие даёт CP866).
  - Редактор: проверка `Encode(d.Text, cp, BOM)`; ошибка → статус; иначе `Codepage = cp`, `modified()` (документ становится Dirty — изменится выходная кодировка).
  - Просмотрщик: только если файл не постраничный и не Dirty: байты `Raw` перекодируются заново (`Decode`), `Buffer` пересоздаётся, `Offset=0`, `markSaved()`. Ошибка → статус. Постраничный/грязный — молча ничего.
  - В Async — фоновая задача `Converting document`.
- **Код:** `document.go: documentKey` (`key == "F8"`).

#### Shift+F8 — выбор кодировки
- **Вызов:** Shift+F8.
- **Диалог:** choose `Code page` — список из 8 кодировок.
- **Поведение:** редактор — как F8 (проверка кодируемости с учётом EOL, `modified()`). Просмотрщик: Dirty → `Save edited text before reinterpreting bytes`; постраничный → меняется только `Codepage` (влияет на dump); иначе перекодирование `Raw`, обновление `BOM`, `EOL`, `Buffer`, `Offset=0`, `markSaved()`. В Async — задача `Converting code page`.
- **Код:** `document.go: chooseCodepage`.

#### F9 — параметры документа (Options)
- **Вызов:** F9 в документе (в панелях F9 — главное меню).
- **Диалог:** форма `Viewer / editor options`: `Line endings` (текст, текущий EOL), `Write BOM` (toggle), `Read only` (toggle), `Scrollbar` (toggle), `Syntax highlighting (Chroma)` (toggle, глобальная настройка), `Auto indent` (toggle, глобальная), `Tab width (1–16)` (текст, глобальная), `Expand tabs to spaces` (toggle, глобальная).
- **Поведение:** валидация: `Tab width must be 1–16`; `Line endings must be LF, CRLF or CR` (регистр не важен). `EOL`/`BOM` применяются только в редакторе (с `modified()`); `ReadOnly`, `Scrollbar` — на документ; остальные четыре — в `a.Settings` (действуют на все документы; сохраняются с настройками при `AutoSave`/`Save setup`).
- **Код:** `document.go: documentSettings`.

---

## 8. Переключение просмотрщик ↔ редактор

#### F6
- **Вызов:** F6 (key bar `6Edit` / `6View`).
- **Поведение (`documentKey`):**
  1. Архивный документ → `Copy the file out with F5 before editing`. Документ справки → ничего.
  2. Постраничный → `Editor limit is 64 MiB`.
  3. Просмотрщик → редактор: текст с NUL → `Binary data cannot be edited as text`; если не Dirty — проверка обратимости (`Decode(Raw)` → `Encode` == `Raw`), иначе `Encoding is not lossless; choose a codepage before editing`.
  4. Редактор → просмотрщик: текущий текст кодируется (`Encode(Endings(Text,EOL), Codepage, BOM)`) и становится `Raw`/`Size` (чтобы hex показывал актуальные байты); если не Dirty — берётся `SavedRaw`. Ошибка кодирования → статус, переключения нет.
  5. `Edit = !Edit`; при переходе в просмотрщик `Offset` = строка курсора, `ViewStart = Cursor`. Текст, курсор, Dirty, undo-история сохраняются.
  6. В Async — фоновая задача `Converting document`.
- **Код:** `document.go: documentKey` (`key == "F6"`).

---

## 9. Редактор

### 9.1 Ввод текста
- Любой одиночный символ (`utf8.RuneCountInString(m.Key) == 1`, без Cmd/Ctrl/Alt) вставляется `Buffer.Insert`. При выделении — заменяет его. В режиме `Overwrite` (Insert) вставка затирает символы до конца строки (не через `\n`). При прямоугольном выделении — вставка во все строки блока в колонку `left` с заменой диапазона колонок (строки короче дополняются пробелами).
- `Insert` переключает `Overwrite` (`EDIT INS`/`EDIT OVR` в статусе). Проверка модификаторов отсутствует: Ctrl+Insert/Shift+Insert, дошедшие до `editorKey`, тоже переключают (Shift+Insert перехватывается рендерером как paste и до Go-ключа не доходит).
- При `ReadOnly` любая правка → статус `Read-only mode (⌘L to unlock)` (навигация, F2, Insert, Shift+F4 работают).
- **Код:** `document_keys.go: editorKey`, `textfile/buffer.go: Insert, Replace`.

### 9.2 Навигация в редакторе
- ←/→ — символ (Cmd/Ctrl — слово: `Word(at, dir)`, слово = буквы/цифры/`_`); ↑/↓ — строка с сохранением колонки в рунах (`At(row±1, col)`); PgUp/PgDn — `Rows-2` строк (Cmd — начало/конец файла); Home/End — начало/конец строки (Cmd — файла). Shift — расширение выделения, Alt (с Shift) — прямоугольное (`Move(target, Shift, Alt)`).
- **Код:** `document_keys.go: editorKey`, `textfile/buffer.go: Move, Word, At`.

### 9.3 Выделение
- Shift+стрелки/PgUp/PgDn/Home/End — потоковое; Alt+Shift+… — прямоугольное (`Rect`; колонки в рунах, табуляция = 1). Cmd+A (и меню Edit → Select All, сообщение `selectAll`) — всё (`Anchor=0, Cursor=len`, `Rect=false`). Cmd+U — снять выделение. Мышь — §5. `Selected()` прямоугольного блока — строки, соединённые `\n`.
- **Код:** `document.go: documentKey` (`a`, `u`), `app.go: Handle("selectAll")`, `textfile/buffer.go: Selected`.

### 9.4 Буфер обмена
- **Вызов:** Cmd+C / Cmd+X (Ctrl+C / Ctrl+X) → `copyDocument(cut)`; меню Edit → Copy/Cut (JS `farshmacCopy/farshmacCut` → сообщения `copy`/`cut`); Cmd+V, Shift+Insert, меню Edit → Paste → `pasteRequest` → сообщение `paste` с текстом системного буфера.
- **Поведение `copyDocument`:** если открыта форма — копируется/вырезается значение поля в фокусе (toggle — ничего); если диалог — его `Input`; иначе документ: берётся `Selected()`; если выделения нет — в редакторе копируется текущая строка целиком вместе с `\n` (при cut — удаляется), в просмотрщике без выделения ничего не происходит. Cut в просмотрщике = copy. Текст уходит в систему через `Update.Clipboard`.
- **Paste:** в редакторе `Insert(text)` + `modified()` (многострочный текст — как есть, окончания нормализуются); в просмотрщике игнорируется; при открытой форме — в поле формы; в диалоге — в поле ввода.
- Shift+Delete в редакторе — **не** вырезание, а обычное удаление (см. 9.6).
- **Код:** `document.go: copyDocument`, `app.go: Handle` (`copy`, `cut`, `paste`, `selectAll`), `internal/native/assets/app.js`, `window_darwin.m` (меню Edit).

### 9.5 Undo / Redo
- **Вызов:** Cmd+Z / Cmd+Shift+Z (Ctrl тоже) в редакторе.
- **Поведение:** каждая `Replace` — запись (`At, Old, New, Before/After cursor, Anchor`). Undo восстанавливает текст, курсор и `Anchor` (выделение); Redo — снимает выделение. Новая правка после undo очищает redo. Лимиты: `MaxUndoBytes = 32 MiB` суммарно и 10 000 записей (старые выбрасываются); одиночная правка дороже 32 MiB очищает всю историю и сама не откатывается. При `ReadOnly` undo/redo не работают. После undo/redo `modified()` — если текст совпал с сохранённым, Dirty снимается.
- **Код:** `textfile/buffer.go: Replace, Undo, Redo`.

### 9.6 Удаление
- Backspace — выделение или предыдущий символ; Delete — выделение или следующий символ. Cmd+Backspace / Cmd+Delete — слово назад/вперёд. Cmd+Y — вся текущая строка вместе с `\n`. Cmd+K — от курсора до конца строки. Cmd+D — удалить выделение (`Insert("")`).
- **Код:** `document_keys.go: editorKey`.

### 9.7 Enter, табуляция, отступы
- **Enter:** `Newline(auto)` — заменяет выделение на `\n` + (при `Auto indent`) ведущие пробелы/табы текущей строки, но только те, что находятся до курсора/начала выделения. Shift+Enter — вставляет имя (basename) текущего файла активной панели.
- **Tab:** с выделением (курсор ≠ якорь) — отступ блока; `Expand tabs to spaces` → пробелы до следующей позиции табуляции (по отображаемой ширине строки до курсора); иначе `\t`. Shift+Tab — снятие отступа.
- **Alt+I / Alt+U** — отступ / снятие отступа (`IndentWithOptions(reverse, width, spaces)`): строки от первой до последней строки выделения (последняя исключается, если выделение заканчивается в её колонке 0); отступ — `\t` или `width` пробелов; снятие — один ведущий `\t` или до `width` пробелов. Курсор и якорь пересчитываются, прямоугольность сохраняется; одна запись undo.
- **Код:** `document_keys.go: editorKey`, `textfile/buffer.go: Newline, IndentWithOptions`.

### 9.8 Только чтение
- Cmd+L (Ctrl+L) переключает `ReadOnly` в обоих режимах; F9 → `Read only`. Архивные документы — `ReadOnly` изначально. Индикатор ` RO` в статусе. Сохранение при `ReadOnly` → ошибка `editor is read-only`.
- **Код:** `document.go: documentKey` (`l`), `saveDocument`.

### 9.9 Сохранение

#### F2 / Cmd+S — сохранить
- **Вызов:** F2, Cmd+S (Ctrl+S), сообщение `save`; key bar `2Save`. Только в редакторе (в просмотрщике Cmd+S — полоса прокрутки, F2 — перенос).
- **Поведение (`saveDocumentThen(d, d.Path, false, nil)` → `saveDocument`):**
  1. `ReadOnly` → `editor is read-only`.
  2. Кодирование (§7.2); если ничего не менялось — `SavedRaw`.
  3. Путь совпадает с `d.Path` и есть `Info` (файл существовал) → `fsops.Save(path, data, Info)`: проверка, что файл на диске не менялся (`SameFile` по inode, размер, mtime) иначе ошибка `file changed on disk; reopen it before saving`; запись во временный `.farshmac-save-*` в том же каталоге, `chmod` по правам оригинала, `fsync`, `rename` (атомарная замена; ACL/xattr/hardlink-топология не сохраняются).
  4. Нового файла (`Info == nil`, создан через Shift+F4) → `fsops.Create` (временный `.farshmac-new-*`, права 0644, `os.Link` — ошибка, если путь уже занят).
  5. После успеха: `Path`, `Info = Stat`, `Raw = data`, `Size`, `markSaved()`, статус `Saved`.
  6. Ошибка записи → статус с её текстом; файл на диске не меняется.
  - В Async: сохраняется клон (`Saving file`), по завершении `*d = *copy`, `Saved`; `done` (закрытие) выполняется только если операция не отменена.
  - Удалённый документ при `path == d.Path` → `saveRemoteDocument` (ниже).
- **Код:** `document.go: save, saveDocument`, `document_tasks.go: saveDocumentThen`, `fsops/files.go: Save, Create`.

#### Shift+F2 / Cmd+Shift+S — Save as
- **Диалог:** prompt `Save as`, подпись `Local file name`, значение — `d.Path`; для удалённого документа — `<путь активной панели>/<basename удалённого файла>`. Относительный путь разворачивается от пути активной панели.
- **Поведение:** тот же путь → обычное сохранение. Существующий файл → confirm `Overwrite` / `<basename> already exists. Replace it?` / кнопки `Overwrite`, `Cancel`; при подтверждении `fsops.Save` с `Lstat` цели (перезапись с сохранением её прав). Несуществующий → `fsops.Create`. После успешного Save as удалённого документа в локальный файл документ становится локальным (`Remote/RemoteInfo/RemotePath` очищаются; временный кэш удалится при закрытии).
- **Код:** `document.go: saveAs`, `document_tasks.go: saveDocumentThen`.

#### Сохранение на удалённый сервер
- **Вызов:** F2/Cmd+S, Shift+F10, `Save` в диалогах закрытия для документа с `Remote != nil`.
- **Поведение (`remote_document.go: saveRemoteDocument`), фоновая задача `Saving remote file`:**
  1. Клон документа; staged-файл `<TempDir>/save-<random>`.
  2. `Dial`, `Lstat(RemotePath)`; `sameRemoteInfo(current, RemoteInfo)` — совпадение размера, mtime и mode с моментом открытия/последнего сохранения; иначе ошибка `remote file changed; reopen before saving`.
  3. Локальное сохранение клона в staged (вложенная операция `Preparing upload`, отменяется вместе с основной) — здесь же проверки кодировки/ReadOnly.
  4. `metadataQuestions`, затем `remotefs.Copy` (1 worker, права = права текущего удалённого файла, `ExpectedDestination = current`, конфликт → перезапись).
  5. Повторный `Lstat` → новый `RemoteInfo`.
  6. Применение: `*d = *copy` (локальный `Path` документа теперь — staged-файл), удаляется старый кэш-файл, статус `Remote file saved`, затем `done`.
- **Код:** `remote_document.go: saveRemoteDocument, sameRemoteInfo`.

#### Shift+F10 — сохранить и закрыть
- В редакторе: `saveDocumentThen(d, d.Path, false, finishCloseDocument)` — при ошибке сохранения документ остаётся открытым. В просмотрщике Shift+F10 = обычное закрытие (§10).
- **Код:** `document.go: documentKey`.

---

## 10. Закрытие, несколько документов, справка, терминал

#### Esc / F10 / F3 (просмотрщик) / F4 (редактор) — закрыть
- **Вызов:** Esc, F10, key bar `3Close`/`10Close` (просмотрщик) или `4Close`/`10Close` (редактор). Esc дополнительно отменяет запущенную задачу подсветки.
- **Поведение (`closeDocument`):** если `Dirty` → confirm `Save changes` / `<basename displayPath> has changed. Save it?` / кнопки `Save`, `Discard`, `Cancel` (Esc = Cancel). `Save` → сохранение по `d.Path` (локально или удалённо), затем закрытие; при ошибке сохранения документ остаётся.
- **`finishCloseDocument`:** удаляет `TempDir` (удалённые/архивные кэши), останавливает подсветку, закрывает `File`, восстанавливает предыдущий документ из стека `Documents` (LIFO) или панели; `refresh()` панелей.
- **Код:** `document.go: closeDocument, finishCloseDocument, documentKey`.

#### Cmd+Q — выход с проверкой всех документов
- **Поведение (`RequestQuit`):** `Busy` → `Wait for the file operation to finish`. Если любой открытый документ (включая скрытые в стеке) Dirty → confirm `Save changes` / `Save changes in open documents before quitting?` / `Save all`, `Discard`, `Cancel`. `Save all` сохраняет по очереди (`saveAllThen`; ошибка прерывает выход). Далее при выполняющейся команде в терминале — confirm `Quit` / `A command is running. Close the application?` / `Yes`, `No`. В нескольких окнах `WindowGroup` обходит окна по очереди; в других окнах статус `Finish the quit dialog in the other window`.
- **Код:** `app.go: RequestQuit, confirmQuitShell`, `document_tasks.go: saveAllThen`, `windows.go`.

#### F12 — список экранов
- **Вызов:** F12 в документе и в панелях.
- **Диалог:** choose `Screens`: `Panels`, затем для каждого документа (по `ScreenID`) `View ` или `Edit `, `* ` если Dirty, basename `displayPath`. Фокус — на текущем документе.
- **Поведение:** выбор делает документ текущим (`selectScreen`), остальные — в стек; `Panels` → все документы в стек, `Doc = nil`; терминал скрывается.
- **Код:** `document.go: screenMenu, screenDocuments, selectScreen`.

#### Ctrl+Tab / Ctrl+Shift+Tab, Cmd+Shift+] / Cmd+Shift+[ — циклическое переключение экранов
- Работает везде (кроме открытых форм/диалогов): порядок — панели, затем документы по `ScreenID`; `]`/Tab — вперёд, `[`/Shift+Tab — назад, по кругу.
- **Код:** `app.go: key`, `document.go: cycleScreen`.

#### F1 — справка
- В документе: открывает новый экран-просмотрщик `Viewer / editor help` с текстом `documentHelp` (`Help=true`); внутри справки F1 и F6 не действуют, подсветка отключена. В панелях F1 открывает `FarshMac — Help` с общим текстом `help` (тот же просмотрщик).
- **Текст `documentHelp` (дословно):**

```
FarshMac viewer / editor

Esc / F10   Close this document, return to previous screen / panels
Cmd+Q       Quit application (checks ALL unsaved documents)
F6          Switch viewer / editor, retaining text and cursor
F12         Switch open document screens
F7 / Cmd+F  Search: literal / regex, case, whole words, backwards, hex
Shift+F7    Next match      Alt+F7 Previous match
Cmd+F7 / Cmd+H  Search and replace; confirmation or Replace all
Alt+F8 / Cmd+G  Go to line:column, byte offset, or percentage
Shift+F8    Codepage chooser     F8 CP866 / Windows-1251
F9          Encoding-related options, line endings, BOM, read-only
Cmd+Option+0..9 Save bookmark    Cmd+0..9 Go to bookmark
Cmd+B       Toggle key bar      Cmd+Shift+B Toggle status
Cmd+O       Terminal / document

Viewer:
F2          Toggle wrap         Shift+F2 Word / character wrap
F4          Text / hex          Shift+F4 Text / hex / dump
Arrows      Scroll              Cmd+Left/Right Scroll 20 columns
Home/End    Start/end           PgUp/PgDn Page
Alt+Left/Right  Adjust hex bytes per row
Cmd+Z       Previous viewing position
+ / -       Next / previous file in active panel

Editor:
F2 / Cmd+S  Save                Shift+F2 / Cmd+Shift+S Save as
Shift+F4    Open/create another file
Shift+F10   Save and close this document
Cmd+Z / Cmd+Shift+Z Undo / redo
Insert      Insert / overwrite
Shift+arrows Stream selection   Alt+Shift+arrows Rectangular selection
Cmd+C/X/V   Copy / cut / paste  Cmd+A Select all
Cmd+Left/Right  Word movement   Cmd+Home/End File start/end
Cmd+Backspace/Delete Delete word   Cmd+Y Delete line
Cmd+K       Delete to line end  Alt+U / Alt+I Unindent / indent
Enter       Newline with auto-indent    Tab Insert tab

Limits: Go/RE2 regex, not Far's full regex dialect. No fuzzy/style-preserving
replacement, plugin APIs, macros or persistent bookmarks yet. Text editing
and text decoding are limited to 64 MiB; larger files use paged hex/dump.
Legacy encoding guesses are heuristic: choose an explicit codepage if needed.
```
- **Код:** `document.go: documentKey, documentHelp`, `app.go: function(1), help`.

#### Cmd+O — терминал поверх документа
- Cmd+O (Ctrl+O) без Shift/Alt при закрытых формах переключает `TermVisible`. Документ остаётся открытым; пока терминал виден, клавиши и клики идут в терминал. Возврат Cmd+O показывает документ.
- **Код:** `app.go: key, showTerminal`.

---

## 11. Подсветка синтаксиса (Chroma)

#### Планировщик
- **Условия:** `Settings.EditorSyntax` включён, документ не справка, не постраничный, `Mode == "text"` (работает и в просмотрщике, и в редакторе).
- **Поведение (`pollSyntax`, вызывается из `Poll`):** при смене документа/текста/пути — отмена старой задачи, сброс результата, дебаунс 200 мс. Текст > `highlight.MaxBytes` (1 MiB) — не отправляется, статус `syntax off: >1 MiB`. Иначе запускается `highlight.Run(ctx, displayPath, text)` в горутине; по завершении результат применяется только если поколение не изменилось (устаревшие результаты отбрасываются). Успех → статус = имя языка (`Result.Language`); ошибка/лимит → `syntax paused (limit/error)`. Esc в документе отменяет текущую задачу; закрытие документа/окна — `stopSyntax`.
- **`highlight.Run`:** глобально не более 2 одновременных helper-процессов (на все окна); таймаут 2 с; запуск `<исполняемый файл> --syntax-helper <basename>` с текстом на stdin; вывод ограничен 8 MiB; проверка валидности смещений; `MaxSpans = 100000`.
- **Helper (`HelperMain`/`Parse`):** лимит памяти Go 64 MiB; лексер — `lexers.Match(basename)` (для удалённых файлов — basename удалённого пути), иначе `lexers.Analyse(первые 8192 байт)`; нет лексера → пустой результат (без статуса). Ошибки: `syntax size limit`, `syntax token limit` (> 400 000 токенов), `lexer changed input`, `syntax span limit`, `incomplete lexer output`, паника лексера → `lexer: …`.
- **Цвета (`tokenColor`):** Comment `0xaaaaaa`; Keyword `0xffffff`; LiteralString `0xffff55`; LiteralNumber `0x55ff55`; NameFunction/NameClass/NameBuiltin `0x55ff55`; NameTag/NameAttribute `0xff88ff`; Operator `0xffffff`; Error `0xff8888`; остальное — `Plain = 0x55ffff` (не записывается в spans). Выделение рисуется поверх подсветки.
- **Отрисовка:** `documentSyntax(d)` возвращает результат только если он относится к текущему тексту и пути (иначе — без цветов), поэтому во время редактирования до пересчёта подсветка пропадает.
- **Код:** `document_syntax.go`, `highlight/highlight.go`, `cmd/farshmac/main.go` (диспетчеризация `--syntax-helper`).

---

## 12. Сводка сообщений статуса (документ)

`Choose a regular file` · `Editor limit is 64 MiB. Large files open in paged hex/dump viewer.` · `File grew while opening; reopen it` · `Encoding is not lossless; inspect in viewer and choose a codepage before editing` · `Binary data: open with F3 in hex/dump mode` · `Editor limit is 64 MiB` · `Binary data cannot be edited as text` · `Encoding is not lossless; choose a codepage before editing` · `Copy the file out with F5 before editing` · `Return to the archive panel to choose another file` · `No next file` / `No previous file` · `Large files currently support paged hex/dump only` · `Search in paged files larger than 64 MiB is not implemented yet` · `Hex replacement is not available in text editor` · `Enter pairs of hex digits, e.g. 48 65 6c 6c 6f` · `Not found` · `Match found` · `Replaced %d matches` · `enter search text` · `Save edited text before reinterpreting bytes` · `Tab width must be 1–16` · `Line endings must be LF, CRLF or CR` · `Enter a percentage from 0 to 100` · `Offset outside file` · `Invalid line / column` · `Bookmark %d saved` · `Read-only mode (⌘L to unlock)` · `editor is read-only` · `Saved` · `Remote file saved` · `file changed on disk; reopen it before saving` · `remote file changed; reopen before saving` · `remote file changed during download` · `viewer requires a regular remote file` · `text cannot be represented in %s: …` · `invalid UTF-8; choose another codepage` · `incomplete UTF-16 code unit` · `unsupported codepage %s` · `syntax off: >1 MiB` · `syntax paused (limit/error)` · `View/edit history is empty` · `Archive is read-only. Use F5 to copy files out.` · `Use F5 to copy the archive link` · `This command requires a local panel. Use F5 to copy remote files first.` · `Wait for the file operation to finish`.

---

## Расхождения между документацией (VIEWER_EDITOR_PARITY.md, README, F1-справка) и кодом

> **Закрыто на ветке `backlog-fixes`:** решения по каждому пункту — в разделе «Решения по расхождениям» `docs/IMPLEMENTATION_BACKLOG.md`.

1. **Alt+F3 / Alt+F4 (инверсия внутренний/внешний) и Cmd+Shift+F3 / Cmd+Shift+F4 (принудительно внутренний)** реализованы в `launch.go`/`app.go`, но не упомянуты ни в README, ни в справке F1, ни в `VIEWER_EDITOR_PARITY.md`, ни в меню Files.
2. **Меню Commands → `File view/edit history`** не показывает клавишу (пустая строка), хотя Alt+F11 работает и указан в справке панелей (`Alt+F11/F12 View/edit history / folder history`).
3. **Space в просмотрщике = следующее совпадение** — есть в коде, не описано нигде.
4. **Cmd+D (удалить выделение), Cmd+U (снять выделение), Cmd+L в просмотрщике, Shift+Enter (вставить имя файла панели), Ctrl+Tab / Cmd+Shift+[ ] (переключение экранов), двойной/тройной клик, перетаскивание полосы прокрутки, Shift+F10 в просмотрщике (просто закрывает)** — не описаны в справке/parity.
5. **Справка F1 описывает только Cmd-сочетания**, код принимает и Ctrl (`command := m.Meta || m.Ctrl`) для всех «Cmd+…» документа, включая закладки.
6. **Insert игнорирует модификаторы:** Ctrl+Insert в редакторе переключает insert/overwrite, а не копирует (в Far Ctrl+Insert — копирование; в панелях FarshMac Ctrl+Insert копирует имена). Shift+Delete в редакторе — удаление, а не вырезание (в панелях — permanent delete). Справка умалчивает.
7. **F9 в документе** — справка говорит «Encoding-related options, line endings, BOM, read-only», но форма также содержит `Scrollbar`, `Syntax highlighting (Chroma)`, `Auto indent`, `Tab width (1–16)`, `Expand tabs to spaces`, причём последние четыре — **глобальные** настройки, меняющие все документы (parity упоминает их как «F9: autoindent, tab width…»).
8. **Alt+Left/Right «Adjust hex bytes per row»** также ограничивает ширину dump-режима (`BytesPerLine` применяется и в dump, с hex-потолком `(Cols-14)/4`); не описано.
9. **Parity: «Shift+F2 selects word/character wrapping»** — в коде Shift+F2 переключает `WordWrap` и всегда включает `Wrap` (нет отдельного выбора), что соответствует справке «Word / character wrap», но не «selects».
10. **F8 «CP866 / Windows-1251»**: из любой другой кодировки первое нажатие даёт CP866 (не «toggle» между двумя); в просмотрщике для Dirty-документа и для постраничных файлов F8 молча ничего не делает (без сообщения), тогда как Shift+F8 даёт `Save edited text before reinterpreting bytes`.
11. **`+`/`-` открывают соседний файл всегда в просмотрщике** (даже из редактора) и всегда внутренним (`openDocument`), минуя внешние настройки — parity пишет только «Adjacent files: + / - in viewer».
12. **История просмотра (Alt+F11)** открывает файл напрямую `openDocument` — также минуя внешние программы и без возможности открыть удалённый/архивный файл; parity пишет «Persistent view/edit history … absent», тогда как `viewHistory` сохраняется в setup (`Viewed`) — т.е. история персистентна, персистентны лишь не позиции.
13. **Удалённое редактирование и сохранение на сервер** (`openRemoteDocument`, `saveRemoteDocument`, проверка `sameRemoteInfo`) полностью отсутствуют в `VIEWER_EDITOR_PARITY.md` и README (README лишь упоминает FTP/SFTP панели).
14. **Просмотр из архива** (`ReadOnly`, `ArchiveSource`, запрет F6) описан только в ARCHIVE_SUPPORT/README одной фразой («editing an archive member requires extracting it first»); сообщения не задокументированы.
15. **Parity «Highlighting: Chroma 2.20.0»** — совпадает с `go.mod` (`chroma/v2 v2.20.0`). Но parity не упоминает, что подсветка действует и в просмотрщике (text-режим), а статус языка показывается в заголовке.
16. **Светлая тема** подменяет `0xffffff` и `0xffff55` (ключевые слова/операторы и строки) на тёмные цвета, а `0x55ff55` (числа/функции), `0xaaaaaa`, `0xff88ff` оставляет яркими на светлом фоне — тема не описана в parity.
17. **Справка панелей:** «The text editor saves atomically, but does not preserve ACLs/xattrs/hardlinks» — соответствует `fsops.Save`; при этом новые файлы (`fsops.Create`) создаются с правами 0644, а не по umask — не описано.
18. **README «Keyboard essentials»** не содержит ни одной клавиши вьюера/редактора, кроме `F3 / F4`; все остальные только в F1-справке документа.
19. **Parity/справка: «Cmd+Shift+0..9 Save bookmark»** — закладки документа разбирают `m.Key` через `strconv.Atoi` (`document.go: documentKey`), а рендерер шлёт `Key: e.key` как есть (`app.js: keyMessage`), т.е. при Shift на US-раскладке придёт `!@#…`, и закладка не сохранится. Панельные folder-shortcuts (Ctrl+Shift+1..9) для того же случая используют `m.Code == "DigitN"` (`folder_shortcuts.go`). Тест `TestWrapGotoBookmarksAndRendering` шлёт `Key:"3", Shift:true` напрямую и этот путь не покрывает. Вероятная ошибка в коде, а не в документации.


<a id="часть-4"></a>
# Часть 4. Терминал, командная строка, окна, нативная оболочка, renderer

Источник истины — код репозитория `/Users/ikim/src/remote-codex/farshmac` (ветка `main`, коммит `b91313a`). Строки интерфейса приведены дословно (английский), как в коде. В конце — список расхождений между документацией/справкой и кодом.

Условные обозначения: «панельный режим» — `App.TermVisible == false`, `Update.Mode == "panels"`; «терминальный режим» — `TermVisible == true`, `Mode == "terminal"`. `PanelKeys` (поле `Update`) истинно, только когда нет документа, формы, диалога, меню и не идёт фоновая операция; нативный слой дополнительно сбрасывает его в терминальном режиме (`panelKeys = state.panelKeys && mode != "terminal"`).

---

## 1. Командная строка панелей

#### Ввод символов в командную строку
- **Вызов:** любая печатная клавиша (один rune) без Ctrl/Alt в панельном режиме; Space при непустой строке; вставка текста (⌘V / Shift+Insert / IME composition).
- **Где доступно:** панельный режим, нет меню/диалога/формы/документа, не `Busy`. Печатные символы сначала проверяются `fastFindKey` (Alt+символ — быстрый поиск; если уже есть активный запрос быстрого поиска, печатные символы идут в него), затем `selectionKey` (Numpad +/−/*), затем `folderShortcutKey` и `farPanelKey`.
- **Поведение:**
  1. `a.Command += m.Key` для одиночных rune без Ctrl и без Alt (Meta-комбинации обрабатываются отдельно).
  2. Space: если `a.Command == ""` — помечает файл (`mark`), иначе добавляет пробел.
  3. Backspace: удаляет последний rune; при пустой строке ничего не делает (не поднимается в родительский каталог).
  4. Escape: сначала `cancelReads()` (отмена чтения панели); если нечего отменять — очищает `a.Command` и `a.Status`.
  5. Вставка (`paste` сообщение): управляющие символы заменяются пробелом (`unicode.IsControl → ' '`), затем все `\n` удаляются; результат добавляется к `a.Command`. При активном fast-find вставка уходит в fast-find; при открытой форме — в поле формы; в редакторе — в документ; в диалоге — в поле ввода (если фокус на поле).
  6. `compositionend` (IME) в панельном режиме отправляется как `paste` с `e.data`.
- **Отрисовка:** предпоследняя строка экрана: `<путь активной панели>>` + команда, курсор `▌` в конце (`draw.go`). Для удалённой панели префикс `local:<локальный путь>`. Длинная строка усекается по хвосту (`tail(prompt, g.W-1)`). Строка статуса над ней: `a.Status` или по умолчанию `Tab Switch panel   Space Select   ⌘O Terminal   ⌘⌥0…9 Bookmark`.
- **Особые случаи:** Ctrl+U и ⌘U меняют панели местами, не трогая команду. Ctrl+Left/Right при пустой команде меняют ширину панелей (шаг 2 %, диапазон 20–80), Ctrl+Num5 сбрасывает.
- **Код:** `internal/ui/app.go` `key()` (ветка `default`, `Backspace`, `Escape`, `" "`), `Handle()` case `"paste"`, `internal/ui/draw.go`.

#### История команд: Ctrl+E / Ctrl+X, ⌘↑ / ⌘↓, Ctrl+↑ / Ctrl+↓
- **Вызов:** Ctrl+E (предыдущая), Ctrl+X (следующая); ⌘ArrowUp / ⌘ArrowDown; Ctrl+ArrowUp / Ctrl+ArrowDown.
- **Где доступно:** панельный режим без модальных слоёв.
- **Поведение:** `a.history` — срез строк, `a.historyAt` — индекс (равен `len(history)` после выполнения). «Назад» — `historyAt = max(0, historyAt-1)`, команда заменяется на `history[historyAt]`. «Вперёд» — `historyAt = min(len, historyAt+1)`; если индекс равен `len`, команда становится пустой. Ctrl+E/X: только Ctrl (не ⌘). ⌘↑/↓ и Ctrl+↑/↓ делают то же.
- **Лимит:** `Settings.HistoryLimit` (1–10000, по умолчанию 200); `appendUnique` удаляет дубликаты и хранит последние N.
- **Сохранение:** `savedSetup.Commands` в `~/Library/Application Support/FarshMac/setup.json` (через `os.UserConfigDir()`), записывается `saveSetup` (Shift+F9, Options → Save setup, или при выходе если `AutoSave`). При старте `LoadSetup` восстанавливает `a.history`, `historyAt = len`.
- **Код:** `internal/ui/far_keys.go` (`case "e", "x"`), `internal/ui/app.go` `key()` (`arrowup/arrowdown`, `ArrowUp/ArrowDown` c Ctrl), `internal/ui/settings.go`.

#### Очистка командной строки: Ctrl+Y / ⌘Y
- **Вызов:** Ctrl+Y или ⌘Y (`cmd && k == "y"`).
- **Поведение:** `a.Command = ""`. История не меняется.
- **Код:** `far_keys.go` `case "y"`.

#### Меню истории команд (Alt+F8 / Commands → Command history)
- **Вызов:** Alt+F8; F9 → Commands → `Command history` (подпись шортката `AltF8`).
- **Поведение:** список `a.history` в обратном порядке (новые сверху) в форме-списке с заголовком `Command history`; выбор элемента помещает текст в `a.Command` (не выполняет).
- **Сообщения:** при пустой истории — статус `Command history is empty`.
- **Код:** `internal/ui/commands.go` `historyMenu("Commands")`, `internal/ui/form.go` `choose`.

#### Автодополнение имени файла: ⌘Space
- **Вызов:** ⌘Space (`Meta` + `" "`). Tab НЕ дополняет — Tab переключает активную панель.
- **Где доступно:** панельный режим; только если `Settings.CompleteFiles` (по умолчанию true; выключается в Terminal settings / AutoComplete settings).
- **Поведение:**
  1. Дополняется только последнее слово команды (после последнего пробела/табуляции).
  2. Если токен содержит `' " \` $ \` — статус `Completion: use an unquoted path prefix`, ничего не делается.
  3. База — путь активной панели; `dir = filepath.Dir(token)`, `prefix = filepath.Base(token)`; если токен пуст или заканчивается `/` — префикс пустой, `dir = token`. Относительные пути и `~` раскрываются через `expand(dir, base)`.
  4. Каталог читается `fsops.ReadDirectory` (в async-приложении — как фоновая задача с подписью `Completing filename`, можно отменить Esc; в sync — синхронно). Учитывается `Settings.CompleteIgnoreCase`. Каталоги получают суффикс `/`. Результат сортируется.
  5. Одно совпадение — подставляется сразу; несколько — форма-список `Complete filename`, выбор подставляет. Подстановка: `a.Command[:start] + terminal.Quote(value)` (одинарные кавычки всегда).
  6. Нет совпадений — статус `No matching paths`.
- **Код:** `internal/ui/completion.go` `completeCommand`, `applyCompletion`.

#### Вставка имён/путей в командную строку
- **Вызов и поведение:**
  - **⌘Enter / Ctrl+Enter / Ctrl+J / ⌘J** → `insertFilename()`: имя файла под курсором активной панели. Если имя содержит управляющие символы — статус `Cannot insert a filename containing control characters`. Имя, начинающееся с `-`, получает префикс `./`. Если имя содержит что-либо кроме `[A-Za-z0-9_./-]` — обёртывается в `terminal.Quote` (одинарные кавычки, `'` → `'\''`). Перед вставкой при непустой команде без завершающего пробела/таба добавляется пробел; после имени — пробел. (Меню Commands → `Insert filename` `⌘Enter`.)
  - **Ctrl+Shift+Enter / ⌘Shift+Enter** → имя файла под курсором **неактивной** панели через `appendPanelValue` (всегда в кавычках).
  - **Ctrl+F / ⌘F** → полный путь текущего элемента активной панели (`p.Current()`), в кавычках.
  - **Ctrl+; / ⌘;** → полный путь текущего элемента неактивной панели.
  - **Ctrl+[ / ⌘[** → путь левой панели; **с Shift** — путь активной панели. **Ctrl+] / ⌘]** → путь правой панели; **с Shift** — путь неактивной.
  - `appendPanelValue`: пустое значение игнорируется; управляющие символы → статус `Cannot insert a path containing control characters`; при непустой команде без завершающего пробела добавляется пробел; значение в `terminal.Quote` + пробел.
- **Код:** `internal/ui/app.go` `insertFilename`, `internal/ui/far_keys.go` `appendPanelValue`, `farPanelKey` (`case "j"`, `"f"`, `";"`, `Enter`, `BracketLeft/Right`).

#### Копирование командной строки / имён в буфер: Ctrl+Insert, ⌘Insert, Alt+Shift+Insert, Ctrl+Alt+Insert
- **Вызов:** Insert с Ctrl или ⌘ (`cmd`), либо Alt+Shift.
- **Поведение:**
  - Без Shift/Alt и при непустой команде — копируется текст команды.
  - Если команда пуста, либо нажат Shift или Alt — копируются выбранные элементы (`p.Selected()`: помеченные, иначе текущий, кроме `..`): без Alt — базовые имена; с Alt — полные пути; с Alt+Ctrl — пути после `filepath.EvalSymlinks` (ошибка → `a.error`), для удалённой панели — `Remote.Label() + path`. Если ничего не выбрано — путь панели. Имена соединяются `\n`.
  - Значение попадает в `a.Clipboard` → `Update.clipboard` → renderer отправляет `{Type:'clipboard'}` → нативный слой пишет в `NSPasteboard` (`NSPasteboardTypeString`).
- **Код:** `far_keys.go` (`m.Key == "Insert" && (cmd || m.Alt && m.Shift)`), `app.js` (`state.clipboard`), `window_darwin.m` (`clipboard`).

#### ⌘C / ⌘X / ⌘V / ⌘A в панельном режиме
- **Вызов:** меню Edit (Copy ⌘C, Cut ⌘X, Paste ⌘V, Select All ⌘A). Renderer в панельном режиме не перехватывает ⌘C/⌘V/⌘X (`if(e.metaKey&&['c','v','x']...)return;`), они доходят до нативного меню, которое вызывает `window.farshmacCopy/Cut/Paste/SelectAll`.
- **Поведение:** `copy` → `copyDocument(false)`: в форме — значение поля; в диалоге — ввод; без документа — `copyPanelCommand`: команда, либо (если команда пуста) помеченные имена, либо имя под курсором (через `\n`). `cut` → то же, но очищает поле/команду (при пустой команде cut ничего не копирует). `selectAll` без документа → эквивалент `⌘A` в `key()` (в диалоге выделяет ввод). `paste` — см. «Ввод символов».
- **Код:** `app.go` `Handle` (`copy`, `cut`, `selectAll`, `paste`), `document.go` `copyDocument`, `mouse.go` `copyPanelCommand`.

---

## 2. Выполнение команды и shell-интеграция

#### Выполнение команды (Enter)
- **Вызов:** Enter при непустой `a.Command` в панельном режиме; также `Apply command…` (Files), `User menu` (F2), `runUserCommand` — они формируют `a.Command` и вызывают `execute()`.
- **Поведение (`App.execute`)**:
  1. `a.Term == nil` → статус `Shell unavailable`, выход.
  2. `!a.ShellReady` → статус `Shell is busy; ⌘O to return to it`, `TermVisible = true` (переключает на терминал), команда остаётся в строке.
  3. `path = активная панель.filesystemPath()` (для архивов/удалённых панелей — локальный путь); `a.Term.Run(path, command)`; ошибка → `a.error`.
  4. При успехе: команда добавляется в `a.history` (`appendUnique`, лимит `HistoryLimit`), `historyAt = len`, `Command = ""`, `TermVisible = true`, `ShellReady = false`.
- **Транспорт (`Session.Run` → `request`)**: проверка — `cwd` абсолютный, без NUL в cwd/command, `len(cwd)+len(command) <= 1 MiB`, иначе ошибка `invalid panel command or directory`. Файл `<sessiondir>/panel-command` создаётся `O_EXCL` 0600 (если уже существует — ошибка EEXIST), содержимое `cwd\0command\0run\0`. Затем в PTY записывается приватная клавиша `commandKey = ESC [777;<token>~` (token — 32 hex из 16 случайных байт на сессию). При ошибке файл удаляется.
- **На стороне shell:**
  - zsh: ZLE-виджет `_farshmac_panel_command` (привязан в keymap `main emacs viins vicmd` к commandKey, historyKey, directoryKey) читает три NUL-поля через `builtin read -d ''`, удаляет файл, при несовпадении `$PWD` делает `builtin cd -- target`; при неудаче `zle -M "Cannot enter directory: <target>"`, маркер статуса 1, команда не выполняется. Иначе `BUFFER=$line; CURSOR=${#BUFFER}; zle reset-prompt; zle .accept-line` — текст попадает в обычную историю zsh, незаконченный ввод пользователя целиком заменяется.
  - bash/sh (Bash 3.2): readline-макрос commandKey = `\C-x\C-k` (kill-whole-line) + `\C-x\C-g` (`bind -x` → `_farshmac_panel_command`). Функция печатает `\n<line>\n`, восстанавливает предыдущую запись истории (обход бага Bash 3.2 `history -s` из `bind -x`), `history -s -- line`, печатает busy-маркер, `builtin eval -- "$line"`, `history -a`, печатает маркер готовности со статусом. Неудачный `cd` → маркер статуса 1 без выполнения.
  - Многострочные команды (с `\n`) передаются как есть (Apply command соединяет команды `\n`).
- **Сообщения:** `Shell unavailable`, `Shell is busy; ⌘O to return to it`, после завершения — `Exit <N> · ⌘O panels / terminal`.
- **Особые случаи:** команда выполняется в каталоге активной панели, даже если shell в другом; для удалённой панели — в `RemoteLocalPath`. Интеграционные последовательности никогда не отправляются, пока `ShellReady == false`.
- **Код:** `internal/ui/app.go` `execute`, `internal/terminal/session.go` `Run`, `request`, zsh rc; `internal/terminal/bourne.go`.

#### Ввод в терминал с клавиатуры (`input` / `inputBinary`)
- **Вызов:** xterm `onData` → `{Type:'input',Text}`; `onBinary` (legacy mouse reports с байтами ≥128) → `{Type:'inputBinary',Text: base64}`.
- **Поведение:** `input`: если текст содержит `\r` или `\n`, `ShellReady = false` немедленно (резервирование shell до прихода preexec-маркера); затем `Term.Write`. `inputBinary` декодируется из base64 и пишется как есть. `Session.Write`: лимит 1 MiB (`paste exceeds 1 MiB`), неблокирующая очередь на 32 буфера (`terminal input queue is full`), закрытая сессия — `shell closed`. Эти сообщения — в `Status` через `a.error`.
- **Особые случаи:** `input`/`inputBinary`/`historyAppend`/`historyRead`/`commandHistory*`/`terminalError`/`mouseUp` обрабатываются даже при `Busy` (файловая операция).
- **Код:** `app.go` `Handle`, `session.go` `Write`.

#### Поддерживаемые shell и выбор
- **Настройка:** F9 → Options → `Terminal settings…` — форма: `Shell: bash / sh / zsh (new windows)` (текст), `Command history entries (1–10000)`, `Complete filenames`, `Completion ignores case`, `Use custom shell prompt / hooks (new windows, may be slow)`. Валидация: `Choose bash, sh or zsh`; `History entries must be 1–10000`. После применения статус `Shell applies to new windows; Save setup to keep settings after restart`. По умолчанию `Shell: "bash"`, `ShellCustomPrompt: false`. Некорректное значение в setup.json нормализуется в `bash`.
- **Запуск (`terminal.StartConfigured(cwd, cols, rows, shell, customPrompt)`)**:
  - Создаётся временный каталог `farshmac-shell-*` (0700) с приватными стартовыми файлами (0600).
  - **zsh:** `/bin/zsh -il`, `ZDOTDIR=<tempdir>`, `FARSHMAC_INTEGRATION_DIR=<tempdir>`, `FARSHMAC_USER_ZDOTDIR=<пользовательский ZDOTDIR или $HOME>`. Обёртки `.zshenv`/`.zprofile` сначала подключают пользовательские файлы с тем же именем, затем возвращают `ZDOTDIR` на интеграционный каталог; `.zshrc` подключает пользовательский `.zshrc`, регистрирует `_farshmac_precmd` (в `precmd_functions`) и `_farshmac_preexec` (в `preexec_functions`), виджет, `unsetopt PROMPT_SP`, и в конце `export ZDOTDIR="$FARSHMAC_USER_ZDOTDIR"` (чтобы дочерние zsh видели пользовательскую конфигурацию).
  - **bash:** `/bin/bash --noprofile --rcfile <tempdir>/shellrc -i`. `shellrc` подключает `/etc/profile`, затем первый из `~/.bash_profile`, `~/.bash_login`, `~/.profile`; если ни одного — `~/.bashrc`. Пользовательский `PROMPT_COMMAND` сохраняется в `_farshmac_user_prompt_command` и выполняется ПЕРЕД маркером готовности (`_farshmac_before_prompt`), затем `PROMPT_COMMAND=_farshmac_before_prompt`.
  - **sh:** `/bin/sh -i`, `ENV=<tempdir>/shellrc`, `FARSHMAC_USER_ENV=<старый $ENV>`; shellrc подключает пользовательский `$FARSHMAC_USER_ENV` и в конце восстанавливает `ENV` для дочерних shell (`export ENV="$FARSHMAC_USER_ENV"` или `unset ENV`).
  - Привязки bash/sh в keymap `emacs-standard vi-insert vi-command`: `\C-x\C-k` kill-whole-line, `\C-x\C-g` → функция, плюс макросы `\C-x\C-e` end-of-line, `\C-x\C-i` quoted-insert, `\C-x\C-b` backward-delete-char, `\C-x\C-j` accept-line, `\C-x\C-y` yank — используются directoryKey для смены каталога с сохранением редактируемой строки (Bash 3.2 кэширует PS1 во время `bind -x`).
  - **Simple prompt (customPrompt=false, по умолчанию):** zsh — `precmd_functions=()`, `preexec_functions=()`, `unfunction precmd preexec`, `unsetopt PROMPT_SUBST`, `PROMPT='%n@%m %~ %# '`, `RPROMPT=''` (после загрузки пользовательского .zshrc, т.е. пользовательские хуки промпта отключаются, но алиасы/переменные остаются); bash/sh — `unset PROMPT_COMMAND`, `PS1='\u@\h \w \$ '`.
  - Окружение: `TERM=xterm-256color`, `COLORTERM=truecolor`, `SHELL=/bin/<shell>`, `BASH_SILENCE_DEPRECATION_WARNING=1`, `CLICOLOR=1` (если не задан и `NO_COLOR` пуст). Размер PTY: `cols × (rows-1)` (одна строка под футер).
  - Пользовательские файлы не модифицируются.
- **Startup probe:** `TestLiveStartupProbe` — опциональный диагностический тест (`FARSHMAC_PROBE_STARTUP=1`, `FARSHMAC_PROBE_CUSTOM_PROMPT`) измеряет время до первого маркера готовности bash и до принятия команды, предел 20 с. Не часть `check.sh`.
- **Код:** `internal/terminal/session.go`, `bourne.go`, `startup_probe_test.go`, `internal/ui/settings.go` (`Terminal settings`), `app.go` `startTerminalAt`.

#### Протокол готовности (маркеры Ready / Running / Exit, cwd, exit status)
- **Поведение:** shell печатает OSC `ESC ] 777;<token>;<status|busy>;<PWD> BEL`. Go-парсер (`parser.feed`) вырезает только этот маркер из потока PTY, всё остальное (alternate screen, mouse, цвета, DSR) уходит в xterm. Маркер, разбитый на границе чтения, буферизуется (хранится хвост до `len(prefix)-1` байт; незавершённый маркер > 64 KiB сбрасывается как данные). `status != "busy"` → `Event{Ready:true, Status, Cwd}`; `busy` → `Event{Running:true, Cwd}`. На каждый Ready-событие `readCommands()` подхватывает файл `command-history`, если есть. EOF/EIO чтения → `Event{Exit:true, Err:nil}`, иная ошибка — `Err`. Канал `Events` ёмкостью 64; читатель PTY блокируется, если UI не дренирует.
- **UI (`App.Poll`)**: за один Poll читается до 8 событий и не более пока `len(pending) < 256 KiB`; дренаж только если `terminalCanDrain()`. `Running` → `ShellReady=false`. `Ready` → `ShellReady=true`, `shellStarted=true`, `ShellCwd=e.Cwd`, `finishPanelReturn()`, статус `Exit <N> · ⌘O panels / terminal`, `shellCommands` (если пришёл снимок истории). `Exit` → `scheduleShellRestart(e.Err)`.
- **Футер терминала (`Update.Footer` в режиме terminal):**
  - при запланированном перезапуске: `⌘O  Panels    ·    <Status>`;
  - готов: `⌘O  Panels    ·    Shell ready    ·    <ShellCwd>`;
  - ещё не было первого промпта: `⌘O  Panels    ·    Starting <shell> (loading shell configuration)…    ·    ⌘. Interrupt`;
  - команда выполняется: `⌘O  Panels    ·    ⌘.  Interrupt`.
  - Renderer дописывает ` · ⌘⇧H Commands` и, при ошибке истории, префикс `History: <error> · `.
- **Код:** `session.go` `parser`, `read`; `app.go` `Poll`.

#### Переключение панели ↔ терминал (⌘O / Ctrl+O)
- **Вызов:** ⌘O или Ctrl+O (без Alt/Shift); меню Commands → `Panels on/off / terminal` `⌘O` (вызывает только `showTerminal`). В терминале renderer перехватывает комбинацию и шлёт `key`.
- **Где доступно:** нет диалога и формы (`a.Dialog == nil && a.Form == nil`); работает при открытом документе (тогда терминал показывается поверх, возврат не синхронизирует панели) и во время `Busy`? — нет: при `Busy` `key()` сначала обрабатывает только Escape/Alt+C/форму и выходит.
- **Поведение:** закрывает меню и fast-find. Если терминал скрыт — `showTerminal()`; иначе `TermVisible=false`, и если документа нет — `returnFromTerminal()` + `refresh()` (перечитывание обеих панелей).
- **Код:** `app.go` `key()`.

#### Синхронизация каталога панель → shell (`showTerminal`, `syncShellCwd`, `pendingShellCwd`)
- **Поведение:**
  1. `showTerminal`: `TermVisible=true`. Если есть отложенный «возврат из терминала» `shellPanelReturn` для той же панели и того же пути — он сбрасывается и cwd НЕ отправляется (пользователь просто заглянул в панели и вернулся). Иначе, если активная панель локальная (`Remote == nil`): `pendingShellCwd = filesystemPath()`, `syncShellCwd()`.
  2. `syncShellCwd` (вызывается также в конце каждого Poll): ничего не делает, если `Term == nil`, `!ShellReady` или `pendingShellCwd == ""`. Если цель совпадает с `ShellCwd` — только очистка. Иначе `Term.ChangeDir(target)` → `ShellReady=false`.
  3. `Session.ChangeDir` пишет `cwd\0\0cd\0` в `panel-command` и шлёт `directoryKey`. zsh: виджет делает `cd`, `_farshmac_precmd`, `zle reset-prompt` — редактируемая строка сохраняется, запись в историю не добавляется. bash/sh: макрос directoryKey = `\C-x\C-g` (cd, `_farshmac_cd_status`) + принятие пустой строки для пересчёта PS1 + восстановление строки из kill ring.
  4. Если shell занят (программа читает stdin) — запрос остаётся в `pendingShellCwd` и уходит при следующем Ready (тест `TestPanelDirectorySyncWaitsForShell`: ключи не попадают в `read`).
  5. `pendingShellCwd` перезаписывается последним запросом; при перезапуске shell используется как стартовый cwd.
- **Код:** `app.go` `showTerminal`, `syncShellCwd`; `session.go` `ChangeDir`.

#### Синхронизация каталога shell → панель (`returnFromTerminal`, `finishPanelReturn`, `syncCwd`)
- **Поведение:**
  1. При ⌘O из терминала в панели (без документа): если `ShellReady` — `syncCwd()`: для локальной активной панели и `ShellCwd != ""` и `pendingShellCwd == ""` → `Panels[Active].Load(ShellCwd)`.
  2. Если shell не готов (команда выполняется, или `cd` только что набран и Ready ещё в пути): запоминается `shellPanelReturn{Active, Path}` (только для локальной панели и при пустом `pendingShellCwd`).
  3. При следующем Ready `finishPanelReturn`: если терминал всё ещё скрыт, активная панель та же и её путь не изменился пользователем, панель локальна и `ShellCwd != ""` → `Load(ShellCwd)`. Если пользователь успел перейти в другой каталог — его выбор не перезаписывается (`TestShellCDReturnsToActivePanelBeforePromptEvent`).
  4. Пассивная панель не меняется. Удалённые панели не синхронизируются ни в какую сторону.
- **Код:** `app.go` `returnFromTerminal`, `finishPanelReturn`, `syncCwd`.

#### Прерывание команды: ⌘. / Ctrl+C
- **Вызов:** ⌘. в терминальном режиме (renderer: `e.metaKey && e.key==='.'` → `{Type:'input',Text:'\x03'}`); Ctrl+C идёт в xterm как обычный ввод.
- **Поведение:** в PTY пишется байт 0x03 (SIGINT через драйвер терминала). Не завершает shell.
- **Код:** `app.js` `attachCustomKeyEventHandler`.

#### Перезапуск shell после выхода (`scheduleShellRestart`, `restartShellIfDue`)
- **Вызов:** событие `Exit` от сессии (`exit`, Ctrl+D/EOF, crash, `exec` другой программы и её завершение).
- **Поведение:**
  1. `scheduleShellRestart(err)`: cwd для перезапуска = `ShellCwd`, перекрывается `pendingShellCwd`, иначе путь активной панели. `Term.Close()` (SIGHUP группе процессов, закрытие PTY, удаление tempdir), `Term=nil`, `ShellReady=false`, `shellStarted=false`, `shellCommands=nil`. Если `closed` или `Quit` — выход (не перезапускается при завершении).
  2. `shellRestartAt = now + 1s` (защита от бесконечного цикла спауна при сломанном rc). Статус `Shell exited; restarting…` либо `Shell stopped: <err>; restarting…`.
  3. В поток терминала добавляется последовательность сброса: `ESC[?1049l ESC[0m ESC[?25h ESC[?1000l ESC[?1002l ESC[?1003l ESC[?1006l ESC[?2004l \r\n[Restarting shell]\r\n` (выход из alternate screen, сброс атрибутов, показ курсора, отключение mouse tracking и bracketed paste; scrollback сохраняется).
  4. `restartShellIfDue` (каждый Poll): по наступлении времени — если cwd удалён, берётся ближайший существующий родитель; если ничего нет — `$HOME`. `startTerminalAt(cwd)` с текущими `Settings.Shell`/`ShellCustomPrompt` этого окна. Ошибка → `shellRestartAt = now + 3s`, статус `Cannot restart shell: <err>; retrying…`; успех → `Shell restarting…`.
  5. Видимость терминала не меняется. Вложенный shell (`/bin/sh` → `exit`) не вызывает перезапуск (Exit только при закрытии PTY родителя).
- **Код:** `internal/ui/shell_restart.go`, тесты `shell_restart_test.go`.

#### Старт терминала при запуске/новом окне
- **Поведение:** `native.Run` после `EnableSession` вызывает `a.StartTerminal()` для первого окна, если `Term == nil`; `EnableSession`/`NewWindow` стартуют shell для остальных окон при `g.StartShell`. Ошибка → статус `Shell: <err>`. Стартовый cwd — `filesystemPath()` активной панели. Размер PTY `Cols × Rows-1` (Cols 60–300, Rows 15–120).
- **Код:** `native_darwin.go` `Run`, `windows.go`, `session.go`, `app.go` `StartTerminal`.

#### Изменение размера терминала
- **Поведение:** renderer измеряет ячейку (`measureCells` по `.xterm-screen` панели), вычисляет `cols = clamp(60..300, floor((innerWidth-8)/cellW))`, `rows = clamp(15..120, floor((innerHeight-8)/cellH))`; событие `resize` коалесцируется таймером 60 мс и шлётся только при изменении. Go: `resize` с тем же результатом игнорируется до любой другой обработки; иначе `Cols/Rows` обновляются и `Term.Resize(Cols, Rows-1)` (`pty.Setsize`). Renderer ресайзит панели до `cols×rows`, терминал до `cols×(rows-1)` через `archive.resize` (захват строк до и после). При смене шрифта перемер через 100 мс.
- **Код:** `app.js` `dimensions`, `farshmacUpdate`; `app.go` `Handle` `resize`; `history.js` `resize`; тест `resize.cjs`.

---

## 3. Терминал (renderer, xterm.js)

#### Эмуляция, цвета, мышь
- **Поведение:** xterm.js 5.5.0 + addon-unicode11 0.8.0 (vendored, offline, CSP `default-src 'none'`). Опции терминала: `fontFamily 'Menlo, Monaco, monospace'`, `fontSize 14` (из настроек), `lineHeight 1.15`, `cursorBlink`, `scrollback 4096`, `macOptionIsMeta: true` (Option+клавиша → ESC-префикс), `macOptionClickForcesSelection: true` (Option+drag выделяет даже при mouse tracking), `rightClickSelectsWord: true`, тема `FarshMacTerminalTheme` (чёрный фон `#000000`, текст `#c0c0c0`, курсор `#c0c0c0`, выделение `#465b73`, стандартная 16-цветная палитра xterm). Поддерживаются 16/256/RGB цвета, SGR, DSR, alternate screen, bracketed paste, mouse reports (SGR и legacy через `inputBinary`), Unicode 11 ширины (проверяется `terminal.cjs`).
- **Фокус:** при переходе в режим terminal `term.focus()`, при выходе `document.activeElement.blur()`. Закрываются оверлеи истории.
- **Код:** `app.js`, `history.js` (тема), `index.html`.

#### Клавиатура в терминале (что перехватывает renderer)
- **Поведение (`term.attachCustomKeyEventHandler`, только keydown):**
  - ⌘O / Ctrl+O → `key` в Go (переключение).
  - ⌘. → `\x03`.
  - ⌘C или Ctrl+Shift+C → `copyText()`.
  - ⌘V, Ctrl+Shift+V, Shift+Insert → `pasteRequest` (нативный слой читает `NSPasteboard` и вызывает `window.farshmacPaste`).
  - ⌘A → `term.selectAll()`.
  - Любая другая комбинация с ⌘ **не** передаётся xterm (`return !e.metaKey`) — уходит в нативные key equivalents (меню: ⌘N, ⌘W, ⌘Q, ⌘⇧H, ⌃⌘F, ⌘M, ⌘`, ⌘⇧`).
  - Все остальные клавиши (включая Fn/F1–F12, Option как Meta, Escape, Ctrl+*) обрабатывает xterm и отправляет в PTY.
  - Document-level capture-listener: ⌘⇧H в терминальном режиме → `toggleCommands()`; при видимом оверлее истории — свои клавиши (см. ниже); иначе в терминальном режиме listener ничего не делает (`if(mode==='terminal')return;`).
  - Нативно: `performKeyEquivalent` в `terminalMode` при ⌘⇧H (keyCode 4 или символ `h`) вызывает `terminalHistory:` напрямую. Option-перевод (`handlePanelOptionKey`) не применяется в терминале (`panelKeys` false).
- **Код:** `app.js`, `window_darwin.m`.

#### Мышь в терминале
- **Поведение:** xterm обрабатывает выделение и mouse tracking. Context menu (`contextmenu`): в терминальном режиме, если приложение включило mouse tracking (`term.modes.mouseTrackingMode !== 'none'`) и не зажаты Shift/Alt — меню не показывается (событие уходит приложению). Иначе показывается своё меню `.clipboard-menu` с кнопками `Copy`, `Paste`, `Select all` (позиция ограничена окном: `min(clientX, innerWidth-130)`, `min(clientY, innerHeight-100)`); закрывается кликом вне. В панельном режиме без документа контекстное меню не показывается (правая кнопка помечает файлы).
- **Колесо вверх** в терминале: если буфер normal, viewport на самом верху (`viewportY===0`), в архиве есть строки старше резидентного scrollback (`history.total > baseY`) и (нет mouse tracking или зажат Shift/Alt) → открывается оверлей истории вывода на позиции `max(first, total - baseY - rows)`.
- **Код:** `app.js` (`contextmenu`, `termEl wheel`).

#### Копирование / вставка / выделить всё в терминале
- **⌘C / Ctrl+Shift+C / Edit → Copy / контекстное меню Copy:** если открыт оверлей команд — копируется выделение в textarea; если открыт оверлей истории — выделенные строки страницы (переносы `\n` между строками, кроме soft-wrapped; `​` удаляется); иначе, если `term.hasSelection()` — `term.getSelection()`. Текст → `{Type:'clipboard'}` → `NSPasteboard`. Без выделения ничего не копируется.
- **⌘V / Ctrl+Shift+V / Shift+Insert / Edit → Paste:** `pasteRequest` → native читает строку из pasteboard → `window.farshmacPaste(text)`: в оверлее команд — вставка в textarea; в терминале — закрывает оверлей истории, `term.paste(data)` (с bracketed paste при `?2004h`, `\n`→`\r`), `term.focus()`. Go-лимит 1 MiB.
- **⌘A / Edit → Select All:** в оверлее команд — выделить textarea; в оверлее истории — выделить страницу; в терминале — `term.selectAll()` (весь резидентный буфер, до 4096 строк + экран).
- **⌘X / Edit → Cut:** в оверлее команд — вырезать из textarea; в терминале ничего; в панелях — `cut`.
- **Код:** `app.js` `copyText`, `farshmacPaste`, `farshmacSelectAll`, `farshmacCut`; `commands.js`; `history_view.js` `copy`, `selectAll`; `window_darwin.m` `pasteText:`, `copyText:` и обработчик `clipboard`.

#### Scrollback и спулинг истории вывода на диск
- **Поведение (renderer, `TerminalArchive`):** строки normal-буфера, уходящие выше верха экрана (между маркером и `baseY`), сериализуются (`{c: cols, w: isWrapped, r: [[text, fgRGB, bgRGB, flags], …]}`; flags: 1 bold, 2 dim, 4 italic, 8 underline, 16 blink, 32 inverse, 64 invisible, 128 strikethrough; хвостовые пробелы без фона/флагов обрезаются). Захват происходит после каждой записи куска ≤ `min(4094, scrollback-2)` байт, перед `ESC[3J`, `ESC c` и переходом в alternate screen (`?1049h/?1047h/?47h`), а также до/после resize. Alternate screen не архивируется. Рост окна не дублирует уже заархивированные строки. Архив отключается при `state.history.error`.
- **Передача:** после обработки кадра renderer шлёт `{Type:'historyAppend', History: lines}`; Go (`appendTerminalHistory`) ставит батч в очередь: не больше 8 батчей и не больше `MaxHistoryBatch = 8 MiB` в очереди, иначе `historyStatus.Error = "History writer is overloaded; live output continues"`. Запись идёт в горутине по одному батчу (`startHistoryWrite`).
- **Backpressure:** `terminalCanDrain()` = ошибка истории уже есть ИЛИ (`len(queue) < 2` и `queuedBytes < 4 MiB`). Пока false — события PTY не читаются и `Update.Terminal` не отправляется; кадр терминала ≤ 16 KiB base64-данных за Poll; буфер `pending` ограничен 256 KiB на цикл чтения. Renderer подтверждает кадр `frameDone{More}` только после парсинга xterm и отправки батча; native не вызывает следующий `goPoll`, пока `evaluating`; при `More` тик вызывается немедленно.
- **Хранилище (`terminal.History`):** temp-каталог `farshmac-history-*` (0700), сегменты `<total>.rows`/`.idx` (0600), сегмент ≈ `HistorySegmentBytes = 16 MiB`, общий бюджет `HistoryDiskBytes = 1 GiB` на окно — старые сегменты удаляются, `First` растёт («oldest rows expired»). Ограничения батча: строка ≤ 256 KiB, батч ≤ 8 MiB и ≤ 20000 строк, валидный JSON, иначе `terminal history batch exceeds limits or is invalid` и история переходит в состояние ошибки (ранее записанные страницы читаются). В RAM — только метаданные сегментов (тест: 200 000 строк, рост heap < 4 MiB).
- **Чтение:** `Page(start, count)`: `count` ограничен `HistoryPageLines = 256`; за раз в полёте не больше одного запроса (`historyRead`).
- **Закрытие окна:** `App.Close` закрывает историю в горутине и удаляет каталог; `WindowGroup.CloseAll` ждёт завершения всех очисток. Между запусками история не восстанавливается.
- **Код:** `internal/native/assets/history.js`, `internal/ui/terminal_history.go`, `internal/terminal/history.go`, тесты `terminal.cjs`, `throughput.cjs`, `terminal_throughput_test.go`, `history_test.go`.

#### Оверлей истории вывода (Window → Terminal Output History)
- **Вызов:** меню Window → `Terminal Output History` (без шортката; активен только в терминальном режиме — `validateMenuItem`); колесо вверх от верха scrollback (см. выше). Отдельного клавиатурного шортката нет (⌘⇧? не привязан).
- **Поведение:** `document.body` получает класс `archive`: терминал скрывается, показывается `#history` с тулбаром: кнопки `↑ Older`, `↓ Newer`, `Live · Esc` и подпись. Открывается на `max(first, total - rows)` (последняя страница), `rows = max(1, term.rows-2)`. Запрос `{Type:'historyRead', Start, Count: rows, Request}`; Go отвечает `historyPage` (в `Update`), renderer применяет только страницу с совпадающим `request`. Строки рендерятся span-ами с цветами/атрибутами (inverse меняет fg/bg, dim — opacity .5, invisible — hidden). Пустая строка — `​`.
- **Подпись:** `History <start+1>–<end> / <total>` + ` · oldest rows expired` если `first > 0`; `History is empty`; либо текст ошибки страницы.
- **Навигация:** ArrowUp/ArrowDown ±1, PageUp/PageDown ±rows, Home — самая старая страница, End/Escape — закрыть (`Live`), колесо — ±1..12 строк (по `deltaY/20`). Если ответ пришёл не на желаемую позицию — повторный запрос. ⌘O закрывает оверлей и переключает режим. ⌘C/⌘A/⌘V — копирование/выделение страницы/вставка (вставка закрывает оверлей и идёт в терминал).
- **Особые случаи:** поиск по истории не реализован. Выделение/копирование ограничены одной страницей. При смене режима оверлей закрывается.
- **Код:** `history_view.js`, `app.js` `toggleHistory`, `window_darwin.m` `terminalOutputHistory:`, `terminal_history.go` `readTerminalHistory`.

#### Оверлей истории команд shell (⌘⇧H / Window → Command History)
- **Вызов:** ⌘⇧H (renderer capture-listener и нативный `performKeyEquivalent` в terminalMode); меню Window → `Command History` `⇧⌘H` (активен только в терминальном режиме). Повторное нажатие закрывает.
- **Поведение:**
  1. Открывается диалог `#commands` (подпись `Command history`, список `<select size=12>`, поле `Edit command (Shift+Enter: new line)` — textarea 3 строки, кнопки `Run · Enter`, `Cancel · Esc`). Оверлей истории вывода закрывается.
  2. Подпись `Loading command history…`; если shell не готов — `Waiting for shell prompt… · Esc: cancel`; запрос `{Type:'commandHistory'}` отправляется, как только `shellReady` (один раз на открытие).
  3. Go: `Term.RequestCommands(ShellCwd)` — mode `history` через `historyKey`: zsh пишет до 500 последних записей `$history` (новые первыми) NUL-разделёнными в `command-history`; bash/sh — `history 500` с `HISTTIMEFORMAT='FARSHMAC_HISTORY_ENTRY:'`. Редактируемая строка сохраняется, запрос сам в историю не попадает. `ShellReady=false` до следующего Ready.
  4. При Ready `readCommands()`: файл читается (лимит 4 MiB + 1; при превышении последняя запись отбрасывается), парсится, bash-порядок разворачивается (новые первыми), пустые и дубликаты удаляются; файл удаляется. Результат → `Update.commands` (один раз).
  5. Renderer: список (многострочные команды показываются с ` ↵ `), выбран первый, его текст в textarea; подпись `Command history — Enter: run · F4: edit · Esc: cancel` или `Command history is empty`. Кнопка Run недоступна, пока `!shellReady`.
  6. Клавиши: Enter (без Shift) / двойной клик / кнопка — `run`: если shell готов и текст непустой → `{Type:'commandHistoryRun', Text}` и закрытие; F4 — фокус в textarea; Escape — закрыть; ⌘C/⌘X/⌘V/⌘A — буфер в textarea. Остальные клавиши идут в select/textarea (стрелки — выбор, `onchange` обновляет textarea).
  7. Go `commandHistoryRun`: `Term.Run(ShellCwd, text)` при `ShellReady` и непустом тексте → команда выполняется в текущем cwd shell, попадает в историю shell (не в `a.history` панелей).
- **Код:** `commands.js`, `app.js` `toggleCommands`, `app.go` `Handle` (`commandHistory`, `commandHistoryRun`), `internal/terminal/commands.go`, rc-скрипты в `session.go`/`bourne.go`, тест `commands.cjs`, `commands_test.go`.

#### «commands» в Update
- `Update.Commands *[]string` — снимок истории shell для оверлея; отправляется один раз после Ready с `Commands != nil`. Это не автодополнение команд: completion из shell не реализован.

#### Размер шрифта
- **Вызов:** F9 → Options → `General settings…` (`Font size (10–24)`) или `Interface settings` (`Font size (10–24)`, `Key bar`). Клавиатурных ⌘= / ⌘− / ⌘0 нет.
- **Поведение:** `Settings.FontSize` (10–24, по умолчанию 14; ошибка `Font size must be 10–24`) → `Update.fontSize` → renderer меняет `fontSize` обеих xterm и `#history-lines`, через 100 мс перемеряет ячейки и пересылает `resize`. Сохраняется через Save setup.
- **Код:** `settings.go`, `app.js`.

#### Полноэкранный режим и Zoom
- **Вызов:** Window → `Enter Full Screen` / `Exit Full Screen` (`⌃⌘F`, заголовок меняется по состоянию окна); F9 → Commands → `Full screen` `⌃⌘F` (ставит `a.Fullscreen = true` → `Update.fullscreen` → `[window toggleFullScreen:]`). Alt+F9 → `a.Zoom = true` → `[window zoom:]` (масштаб окна Cocoa, не шрифт).
- **Особые случаи:** во время fullscreen кадр окна не сохраняется в сессию (`captureFrame` пропускает при `NSWindowStyleMaskFullScreen` и во время перехода).
- **Код:** `window_darwin.m` (`toggleFullScreenActive:`, `validateMenuItem`, `tick:`), `menu.go`, `far_keys.go`.

---

## 4. Окна, сессия, выход

#### Новое окно (⌘N)
- **Вызов:** ⌘N (в панельном и терминальном режиме, если нет `Busy`), File → `New Window` `⌘N`, Commands → `New window` `⌘N`; Dock-клик при отсутствии видимых окон (`applicationShouldHandleReopen` → `newWindowFrom:0`).
- **Поведение:** `a.NewWindow=true` → `Update.newWindow` → native `newWindowFrom:session` → `goNewWindow(parent)` → `WindowGroup.NewWindow`: отказ `finish quitting before opening a new window` во время quit; левая/правая папки наследуются от родителя (`filesystemPath()`), иначе `$HOME`; настройки копируются через JSON (независимые копии), `panelOptions` обеих панелей копируются, `refresh()`; без родителя — `LoadSetup()`. Стартует собственный shell (`StartTerminal`) — PTY независимы (`TestIndependentPTYWindows`). Окно каскадируется от точки `(100+24*session, 850-24*session)`, размер 1120×740, минимальный контент 700×400, заголовок `FarshMac — <session>`; после первого Poll — `FarshMac — <session> — <путь активной панели | имя документа>`.
- **Код:** `windows.go` `NewWindow`, `window_darwin.m` `newWindowFrom:`, `open`.

#### Переключение окон
- Window → `Next Window` `⌘``, `Previous Window` `⇧⌘``, `Minimize` `⌘M`, `Bring All to Front`. Окна циклически по списку контроллеров.

#### Сохранение и восстановление сессии
- **Файл:** `<UserConfigDir>/FarshMac/session.json` (0600 через temp+rename), fallback чтения из `<UserConfigDir>/Far Go/session.json` если нового нет. Формат: `{"Version":1,"Windows":[{"Frame":{X,Y,Width,Height},"Panels":[{"Path","Options":panelOptions,"Cursor","Top"},…],"Active":0|1}],"Focused":N}`.
- **Когда сохраняется:** каждый `WindowGroup.Poll` (не чаще 500 мс и только при изменении байтов; неудачные записи тоже троттлятся), при закрытии окна (force), при начале завершения (`advanceQuit` → `SaveSession(true)`), `finishQuit` → `goSaveSession`, `CloseAll` (если ещё не Terminating). При нуле окон сохраняется последнее закрытое (`lastWindow`). Кадры окна приходят из Cocoa (`windowDidMove/Resize/EndLiveResize`), координаты в points; `Valid()` требует 100 ≤ W,H ≤ 20000, |X|,|Y| < 100000, без NaN/Inf.
- **Восстановление (`EnableSession(path, restore)`):** при старте, если не переданы `-left`/`-right` (`flag.Visit` → `restore=false`). Ошибки: `cannot read window session: …`, `unsupported window session version N`, `too many saved windows` (>100) — попадают в `a.Status`, приложение стартует с дефолтами. Для каждого окна: папки проверяются `accessibleFolder` (подъём к ближайшему читаемому родителю, затем fallback на папку первого окна, `$HOME`, `/`); `panelOptions` применяются; курсор/верх восстанавливаются по именам (`pendingPosition` для async-загрузки); `Active` ограничивается 0..1; невалидный кадр игнорируется (окно центрируется/каскадируется); фокус — `Focused`. Нативный слой при восстановлении подгоняет кадр под экран с наибольшим пересечением: ширина ≥700, высота ≥428, не больше `visibleFrame`, origin внутри экрана.
- **Особые случаи:** восстановление не зависит от `AutoSave`; архивная панель сохраняет `Cursor = имя архива`; команда, история терминала, содержимое shell не сохраняются.
- **Код:** `internal/ui/session.go`, `windows.go`, `native_darwin.go`, `window_darwin.m` `open`, `cmd/farshmac/main.go`, тесты `session_test.go`.

#### Закрытие окна (⌘W / красная кнопка)
- **Вызов:** File → `Close Window` `⌘W`; `windowShouldClose` → `{Type:"close"}` (окно не закрывается само).
- **Поведение:** `WindowGroup.Handle` → `a.RequestQuit()` для этого окна (та же последовательность подтверждений, что при выходе, см. ниже); при `a.Quit` → `Update.quit` → native `closeWindow:` → `goCloseWindow` → `CloseWindow`: снимок окна в `lastWindow`, `App.Close()` (отмена задач, закрытие документов/temp, `saveSetup` если `AutoSave`, `Term.Close()`), удаление из группы, сессия сохраняется. Остальные окна и их shell продолжают работать. Приложение не завершается после закрытия последнего окна (`applicationShouldTerminateAfterLastWindowClosed → NO`).
- **Код:** `windows.go` `Handle`, `CloseWindow`; `window_darwin.m`.

#### Выход из приложения (⌘Q) — групповая последовательность
- **Вызов:** ⌘Q (перехватывается `WindowGroup.Handle` по `key` Meta+q, либо нативное меню `Quit FarshMac` ⌘Q → `goQuitAll`), Commands → `Quit` `⌘Q`, `applicationShouldTerminate` (Dock/система) → `requestQuit:` (возвращает `NSTerminateCancel`, завершение идёт через группу). F10 приложение не закрывает.
- **Поведение (`BeginQuit`/`advanceQuit`):**
  1. Окна проходятся по порядку `Order`. Для каждого — `a.RequestQuit()`:
     - если `a.Busy` → статус `Wait for the file operation to finish`, quit прерывается;
     - если есть несохранённые документы → терминал скрывается и показывается подтверждение `Save changes` / `Save changes in open documents before quitting?` с кнопками `Save all`, `Discard`, `Cancel`; `Save all` сохраняет все и переходит к проверке shell; `Discard` — сразу к shell; `Cancel` — отмена всей последовательности (ранее одобренные окна остаются открытыми);
     - `confirmQuitShell`: если `Term != nil && !ShellReady` (команда выполняется или shell ещё стартует) → терминал скрывается, диалог `Quit` / `A command is running. Close the application?` с кнопками `Yes`, `No`; `Yes` → `a.Quit=true`;
     - иначе `a.Quit=true` сразу.
  2. Пока окно ждёт ответа, `Update.attentionWindow = id` заставляет native вывести это окно на передний план; ввод в другие окна отклоняется со статусом `Finish the quit dialog in the other window` (кроме `resize`).
  3. Когда все окна одобрили: `Terminating=true`, `SaveSession(true)` (ошибка — в статус всех окон), `Update.quitApplication` → native `finishQuit`: захват кадров, `goSaveSession`, закрытие всех окон, `[NSApp stop]`; `native.Run` → `CloseAll` (ждёт очистку историй).
- **Особые случаи:** в одном окне ⌘W при выполняющейся команде показывает тот же текст `Close the application?`. `NewWindow` во время quit невозможен.
- **Код:** `windows.go` `BeginQuit`, `advanceQuit`, `Handle`, `Poll`; `app.go` `RequestQuit`, `confirmQuitShell`; `window_darwin.m` `requestQuit:`, `finishQuit`; тест `TestIndependentWindowsAndQuitTransaction`.

#### Full Disk Access
- **Диалог при старте (`native.PrepareDiskAccess` → `farshmac_prepare_disk_access`):** показывается один раз (флаг `NSUserDefaults` `FarshMacDiskAccessIntroductionShown`), только если bundle id `local.farshmac.app` и запуск из `.app` (CLI/`go run`/тесты — пропуск). NSAlert: заголовок `Full Disk Access for FarshMac`, текст: `To browse protected folders without separate folder prompts, enable FarshMac in System Settings → Privacy & Security → Full Disk Access.\n\nIf it is missing, use '+' or drag the app selected in Finder:\n<bundlePath>\n\nOpen Settings closes this launch before folders are loaded. Enable access, then reopen FarshMac. If already enabled, choose Continue. You can also continue with limited access and use FarshMac → Full Disk Access later.` Кнопки: `Open Settings` (первая), `Continue` (Esc). Флаг ставится независимо от выбора. `Open Settings` → `farshmac_open_disk_access()`; при успехе процесс завершается (`main` возвращается до создания `App`), пользователь перезапускает вручную — автоматического relaunch нет. Доступ приложение не проверяет и не выдаёт.
- **`farshmac_open_disk_access`:** выделяет bundle в Finder (`activateFileViewerSelectingURLs`), открывает `x-apple.systempreferences:com.apple.preference.security?Privacy_AllFiles`, fallback — System Settings; если и это не удалось — NSAlert `Open Full Disk Access in System Settings` / `Open Privacy & Security → Full Disk Access, add this copy of FarshMac and enable it. Then quit and reopen FarshMac.` / `OK`.
- **Позже:** меню приложения `FarshMac → Full Disk Access…` (нативно, сразу) и F9 → Options → `Full Disk Access…` (ставит `DiskAccessSettings=true`, статус `Enable this copy of FarshMac in Full Disk Access (+ or drag from Finder), then quit and reopen.`).
- **Документация:** PUBLISHING.md — bundle id должен быть стабильным; после смены подписи (ad-hoc → self-signed) добавить копию в FDA заново; TCC не проверяется кодом.
- **Код:** `internal/native/disk_access_darwin.m`, `native_darwin.go`, `cmd/farshmac/main.go`, `menu.go`, `window_darwin.m` `diskAccessSettings:`.

---

## 5. Нативное меню macOS (window_darwin.m `startWindows`)

| Меню | Пункт | Шорткат | Действие |
| --- | --- | --- | --- |
| FarshMac | `About FarshMac` | — | стандартная панель About |
| FarshMac | `Full Disk Access…` | — | `farshmac_open_disk_access()` |
| FarshMac | `Quit FarshMac` | ⌘Q | `requestQuit:` → `goQuitAll` (групповой quit); без окон — `finishQuit` |
| File | `New Window` | ⌘N | `newWindowFrom:` активного окна |
| File | `Close Window` | ⌘W | шлёт `{Type:"close"}` активному окну |
| Edit | `Copy` | ⌘C | `window.farshmacCopy()` |
| Edit | `Cut` | ⌘X | `window.farshmacCut()` |
| Edit | `Paste` | ⌘V | читает `NSPasteboard` → `window.farshmacPaste(text)` |
| Edit | `Select All` | ⌘A | `window.farshmacSelectAll()` |
| Window | `Command History` | ⇧⌘H | `window.farshmacToggleHistory()`; активен только в terminal mode |
| Window | `Terminal Output History` | — | `window.farshmacToggleOutputHistory()`; только в terminal mode |
| Window | `Enter Full Screen` / `Exit Full Screen` | ⌃⌘F | `toggleFullScreen:` активного окна; заголовок по состоянию |
| Window | `Minimize` | ⌘M | `performMiniaturize:` |
| Window | `Next Window` | ⌘` | цикл по окнам +1 |
| Window | `Previous Window` | ⇧⌘` | цикл −1 |
| Window | `Bring All to Front` | — | `arrangeInFront:` |

«Активное окно» — `NSApp.keyWindow`, иначе последнее. Иконка приложения — `FarshMac.icns` из bundle. Окно: фон `#000080`, `NSWindowStyleMaskTitled|Closable|Resizable|Miniaturizable`, WKWebView с `nonPersistentDataStore`, навигация разрешена только на `index.html`; при крахе web-процесса — `reload`. Таймер 30 Гц (`NSRunLoopCommonModes`) вызывает `goPoll` на serial `engineQueue`, результат — `window.farshmacUpdate(json)`.

---

## 6. Клавиатура: нативный и renderer-уровень (панельный режим)

#### Option+буква (dead keys), Numpad, ⌘⇧.
- **Поведение (native, только `panelKeys`):**
  - Local event monitor (`NSEventMaskKeyDown`) перед AppKit и input methods: `FarshMacPanelOptionMessage` — для keyDown с Option и без ⌘/Ctrl символ берётся `charactersByApplyingModifiers:(Shift|CapsLock)` — т.е. базовый символ текущей раскладки без Option-акцентов/dead keys (Option+E → `e`, Option+- → `-`, Option+Shift+- → `_`). Управляющие (<32, 127) и функциональные (0xF700–0xF8FF) игнорируются. Отправляется `{Type:"key", Key, Code:(NumpadMultiply|NumpadAdd|NumpadSubtract|""), Alt:true, Shift}` — событие не доходит до WebKit (fast find по Alt+буква).
  - `performKeyEquivalent`: keyCode 46 (M) с ⌘/Ctrl → `Key:"m", Code:"KeyM"`; keyCode 67/69/78 (Numpad * + −) → `Key "*","+","-"` с кодами `NumpadMultiply/Add/Subtract` и флагами; keyCode 47 с ⌘⇧ (без Ctrl/Opt) → `Key ".", Code "Period", Meta, Shift` (показ скрытых). Остальное — Option-перевод, затем `super`.
  - В terminalMode и при модальных слоях (`panelKeys=false`) Option-перевод не выполняется: xterm получает Option как Meta, редактор — обычные символы.
- **Renderer (document keydown, capture) в панельном режиме:** игнорирует `isComposing`; Shift+Insert → `pasteRequest`; ⌘C/⌘V/⌘X пропускаются к нативному меню; всё остальное — `preventDefault` + `{Type:'key', Key:e.key, Code:e.code, Ctrl, Alt, Meta, Shift}`. Fn/F1–F12 приходят как `Key:"F1"…` (на клавиатурах с медиа-клавишами — удерживать Fn, см. README). Escape — обычная клавиша. Go в `key()`: Alt+Key* при открытой форме/диалоге/Busy маппится из `Code` (`KeyC` → `c`) для Alt-акселераторов.
- **Тест:** `panel_keys.m` (clang) — Option+a/e/./-/Shift+-, запрет при `panelKeys=NO` и при ⌘; `input.cjs` — `ç`/KeyC с Alt.
- **Код:** `panel_keys_darwin.h`, `window_darwin.m`, `app.js`.

#### Мышь в панелях (renderer → Go)
- **Поведение:** `pointerdown` (кнопки 0 и 2): `preventDefault`, снятие выделения xterm, `{Type:'click', X, Y (в ячейках), Clicks:1, Button, Buttons, Shift, Alt, Meta, Ctrl}`; **Control+левая = правая кнопка (`Button:2`)** — помечает файл как в Far. В документе правая/Ctrl-клик игнорируются. `dblclick` (левая без Ctrl) → `Clicks:2`; тройной клик в документе → `Clicks:3`. Drag: `pointermove` с зажатыми кнопками коалесцируется таймером 25 мс в `{Type:'drag'}`; автоповтор 60 мс у краёв (строки < 3 / ≥ rows-6 для панелей, < 1 / ≥ rows-1 для документа). `pointerup`/`pointercancel`/`blur` → `mouseUp` (обрабатывается при Busy). Колесо → `{Type:'wheel', Delta: sign·clamp(1..12, ceil(|deltaY|/20))}`. Pointer capture на `#panels`. Текст панелей `user-select:none`.
- **Go `click`:** клики по строке меню (Y=0) открывают разделы; при `TermVisible` клики игнорируются; клик по key bar (последняя строка, если `ShowKeys`) → `function(min(10, X*10/Cols+1))`.
- **Drag&drop файлов (из Finder/в Finder):** не реализован; «drag» — только выделение мышью внутри панелей/документа.
- **Код:** `app.js` (`pointer`, `flushDrag`), `app.go` `click`, `mouse.go`; тест `input.cjs`.

---

## 7. Справка F1 (фрагмент про терминал, дословно)
`Type a shell command directly while panels are visible, then press Enter. The panels disappear and the SAME window becomes an interactive terminal. Commands run in the active panel's directory. zsh keeps its environment, history, aliases and jobs. Your .zshrc is loaded at shell startup. The output stays visible after completion; ⌘O restores the panels. ⌘O also works during a command; returning does not stop the job. Use Cmd+. (or Ctrl+C) to interrupt, Cmd+C / Cmd+V to copy / paste. Scroll the terminal to review output. vim/less use the alternate screen.`

---

## Расхождения между документацией/справкой и кодом

> **Закрыто на ветке `backlog-fixes`:** решения по каждому пункту — в разделе «Решения по расхождениям» `docs/IMPLEMENTATION_BACKLOG.md`.

1. **Справка F1** (`app.go`, `help`): заголовок `persistent zsh terminal`, «zsh keeps its environment…», «Your .zshrc is loaded at shell startup» — по умолчанию shell `bash` (`defaultSettings`, `TestDefaultShellIsBash`); zsh — только при выборе в Terminal settings.
2. **Справка F1**: `Ctrl+E/X/Y` — в коде Ctrl+Y и ⌘Y очищают строку, но Ctrl+E/X работают только с Ctrl (не ⌘); дополнительно работают недокументированные Ctrl+↑/Ctrl+↓ и ⌘↑/⌘↓ (последние есть в справке).
3. **Справка F1**: `Ctrl+F / Ctrl+;` — в коде также ⌘F / ⌘; (любой `cmd`). `Ctrl+Insert … Alt+Shift+Insert full paths` — ⌘Insert тоже работает; недокументирован Ctrl+Alt+Insert (пути с разрешением симлинков / метка удалённого сервера). Ctrl+[ / Ctrl+] (пути левой/правой панели, с Shift — активной/неактивной) и Ctrl+Shift+Enter (имя из неактивной панели) в справке отсутствуют.
4. **Справка F1** упоминает `F10 closes only viewer/editor` — в панельном режиме `function(10)` ничего не делает (комментарий: выход только ⌘Q); закрытие документа F10 — в `document_keys.go` (вне этой области).
5. **TERMINAL_PARITY.md**: «xterm parses 1024-byte chunks before collecting history» — в `history.js` размер куска `min(4094, scrollback-2)` = 4094 байт.
6. **TERMINAL_PARITY.md**: «⌘⇧H Command history (output archive in the original 0.13 implementation)» и таблица навигации «в архиве» — актуально: ⌘⇧H = история команд, архив вывода — только через меню Window без шортката. Документ сам это оговаривает в преамбуле.
7. **Terminal settings**: подписи `Shell: … (new windows)` и `Use custom shell prompt / hooks (new windows…)`, статус `Shell applies to new windows` — но `restartShellIfDue` использует текущие `Settings.Shell`/`ShellCustomPrompt` этого же окна, т.е. после `exit` в текущем окне новый shell уже стартует с изменёнными настройками.
8. **Диалог закрытия окна**: ⌘W / кнопка закрытия при выполняющейся команде показывает `A command is running. Close the application?` — закрывается только окно, остальные продолжают работать.
9. **Задание упоминает ⌘= / ⌘− / ⌘0 (zoom шрифта) и ⌘⇧? (output history)** — таких привязок в коде нет: размер шрифта только через настройки (10–24), история вывода — только через меню Window / колесо вверх от края scrollback.
10. **README «Keyboard essentials»**: `Cmd+Shift+H Shell command history: Enter to run, F4 to edit` — верно, но пункт доступен только в терминальном режиме (меню деактивировано в панелях); в README это не указано.
11. **CLAUDE.md**: «Session wraps a PTY running bash/sh/zsh» и «Directory sync … only when the shell is at a prompt» — соответствует коду. CLAUDE.md описывает `Message` типы «key, click, drag, wheel, resize, input, historyRead» — фактический набор шире (`mouseUp`, `terminalError`, `commandHistory`, `commandHistoryRun`, `historyAppend`, `inputBinary`, `copy`, `cut`, `selectAll`, `scroll`, `save`, `paste`, `quit`, `close`, `redraw`).
12. **Справка F1**: «Shift+Insert Paste text» — в панельном режиме реализовано в renderer (`pasteRequest`); в Go нет отдельной обработки Shift+Insert — зависит от renderer, что верно для приложения, но не для headless-тестов.
13. **TERMINAL_PARITY.md**: «Pages … the UI asks for at most the window height» — запрашивается `term.rows-2` строк (минимум 1), не высота окна.


<a id="часть-5"></a>
# Часть 5. Меню F9, диалоги, настройки, сетевые подключения, справка

Источник истины — код в `internal/ui` и `internal/remotefs` (версия 0.20.0, строка бренда `FarshMac 0.20.0` в `draw.go`). Строки интерфейса приведены дословно на английском. Отсылки «см. раздел файловых операций / поиска / user menu» относятся к спецификациям других разделов.

---

## 1. Общая механика модальных окон

Движок UI хранит ровно четыре вида модального состояния в `App` (`internal/ui/app.go`): `Menu *MenuState`, `Dialog *Dialog`, `Form *Form`, а также «занятость» `Busy` с прогресс-окном (`Operation`). Пока открыт документ, форма, диалог, меню или идёт фоновая операция, `Update.PanelKeys == false` и рендерер маршрутизирует клавиши иначе (`App.Poll`).

Порядок обработки клавиш в `App.key` (упрощённо): `Alt+буква` при открытой форме/диалоге/Busy нормализуется к латинской букве по `m.Code` → если `Busy`: `Escape`/`Alt+C` отменяют операцию, иначе клавиши идут в форму (вопросы операции) → `⌘N` новое окно → `⌘Q` quit → `Ctrl+Tab`/`⌘⇧[ ]` переключение экранов → `⌘O`/`Ctrl+O` (только без диалога/формы) закрывает меню и переключает терминал → если терминал виден — выход → `Form` → `Dialog` → `Doc` → … → `Menu`.

Отрисовка (`Draw`, `draw.go`): панели → строка статуса → командная строка → key bar → документ → fast find → меню → прогресс → Dialog → Form → применение цветовой темы.

### Общие примитивы (`internal/ui/modal.go`)

- `modalBounds(cols, rows, width, height)` — окно по центру; ширина `min(width, max(12, cols-4))`, высота `min(height, max(8, rows-2))`.
- `drawModal` — тень (чёрная заливка со сдвигом +2/+1), серое тело (`Gray`), рамка `Box` с отступом 2 по X и 1 по Y, заголовок по центру в рамке, обрезанный `tail(title, W-10)` и обёрнутый пробелами.
- `modalRule` — горизонтальный разделитель `─` с концами `╟`/`╢`.
- `mnemonicKeys(labels)` — для каждого ярлыка первая ещё не занятая буква (в нижнем регистре) становится акселератором; `modalLabel` рисует эту букву жёлтым (`Yellow`).
- `modalInput` — поле ввода: фон `Cyan`, текст `LightCyan`; при `selected` (выделено всё) — `White` на `Blue`; каретка — инверсия ячейки (`White` на `Black`) только у сфокусированного поля; управляющие символы заменяются пробелом; показывается окно текста вокруг каретки (`editWindow`).
- `buttonLayout(labels, width)` — кнопки в ряд с шагом 1, ширина `len+4`, перенос на новую строку при нехватке места, каждая строка центрируется.
- `drawModalButtons` — первая кнопка рисуется как `{ OK }`, остальные `[ Cancel ]`; сфокусированная — фон `Cyan`, остальные `Gray`; акселераторы подсвечены.

### Dialog (однострочный prompt, `App.prompt`, `app.go`)

#### Dialog (prompt с одним полем)
- **Вызов:** `a.prompt(title, msg, value, fn)` — используют: Make folder (F7), Change directory (⌘L), Select/Deselect group, File panel filter, Create archive, Extract archive, Tree settings, Command line settings, File descriptions, Sort groups, Folder description files, Describe files, Apply command, Save folder shortcut, Remote move / rename, Make remote folder, Move / rename (F6).
- **Поведение:**
  - Если `value == ""` и включено `RememberDialogValues`, подставляется последнее введённое значение для этого заголовка (`a.dialogValues[title]`, только в памяти процесса).
  - `Selected = Settings.SelectDialogInput` — при включённой настройке начальный текст выделен целиком; первый печатный символ/Backspace заменяет его.
  - Фокус: `0` поле, `1` OK, `2` Cancel. `Tab`/`Shift+Tab` циклически по трём; `ArrowLeft/Right/Home/End` в поле снимают выделение (курсор всегда в конце строки — отдельного курсора у Dialog нет), на кнопках переключают OK↔Cancel.
  - `Enter`: закрывает; если фокус не на Cancel — вызывает `Confirm(Input)` (и запоминает значение при `RememberDialogValues`). `Space` на кнопке = Enter. `Escape` — закрыть без вызова.
  - `Alt+O` → OK, `Alt+C` → Cancel. `⌘A` — выделить всё. Backspace удаляет последний символ (или всё выделение). Печатные символы (без ⌘/Ctrl) добавляются в конец.
  - Вставка (`paste`): только при фокусе на поле; управляющие символы → пробел, переводы строк удаляются.
  - Мышь (`promptClick`): клик по строке поля ставит фокус 0; клик по кнопке активирует её.
- **Отрисовка** (`drawDialog`): ширина `min(88, Cols-4)`, сообщение до 3 строк (перенос по ширине `W-8`), поле ввода, разделитель, кнопки `OK`/`Cancel` (акселераторы `o`/`c`).
- **Код:** `app.go` (`Dialog`, `prompt`, `key`), `draw.go` (`promptBounds`, `drawDialog`, `promptClick`).

### Form (многопольная форма / список выбора / подтверждение, `internal/ui/form.go`)

Одна структура `Form` обслуживает три режима:
1. **Форма с полями** (`a.form(title, fields, submit)`): `Fields []Field{Label, Value, Toggle, Checked, Secret}`, кнопки `Primary`/`Cancel`.
2. **Список выбора** (`a.choose(title, choices, select)`): `Choice` непустой, `Buttons=false`. Если список пуст — форма не открывается, статус `"<title>: empty"`.
3. **Подтверждение с кнопками** (`a.confirm(title, message, buttons, select)`): `Buttons=true`, `Choice` = ярлыки кнопок, `Cancel` вызывает `select(len(buttons)-1)` (т.е. Esc = последняя кнопка). Доп. поле `Remember` — чекбокс `Remember choice` (используется вопросами операций).

#### Форма с полями
- **Поведение:**
  - Начальный фокус — поле 0, курсор в конце его значения. `Selected` (выделить всё) включает вызывающий код (например, диалог копирования ставит `Form.Selected = Settings.SelectDialogInput`).
  - Навигация: `Tab`/`ArrowDown` → следующий элемент, `Shift+Tab`/`ArrowUp` → предыдущий; элементы = поля + кнопка Primary + кнопка Cancel (`count = len(Fields)+2`), переход циклический. `ArrowLeft/Right` внутри текстового поля двигают курсор, а не фокус; на toggle-поле переключают флажок.
  - Текстовое поле: `Home/End`, `Backspace`, `Delete`, печатные символы (без ⌘/Ctrl); `⌘A` выделяет всё; при выделении Backspace/Delete/символ очищают поле. Вставка `formPaste` — в позицию курсора.
  - Toggle: `Space`, `ArrowLeft`, `ArrowRight` переключают `[ ]`/`[x]`.
  - `Secret`: значение отображается как `•` на каждый символ (пароли). Тест `TestRemoteUnknownHostConfirmationAndMasking` проверяет, что пароль не попадает в сетку.
  - `Enter`: закрывает форму; если фокус на Cancel (`len(Fields)+1`) — вызывает `Cancel` (если задан), иначе `Submit(Fields)` (в т.ч. при фокусе на любом поле). `Space` на кнопке = Enter.
  - `Escape`: закрывает, вызывает `Cancel` если задан.
  - Акселераторы `Alt+буква`: мнемоники вычисляются как `mnemonicKeys(buttons ++ labels)` — т.е. сначала буквы кнопок (`OK`/`Primary` и `Cancel`), затем ярлыки полей. `Alt+буква` поля: фокус на поле; toggle переключается, текстовое — курсор в конец. `Alt+буква` кнопки — нажатие.
  - Разделитель рисуется между группой текстовых и группой toggle-полей (`Rules`). При `CheckColumns == 2` toggle-поля раскладываются в две колонки с вертикальной линией `│` (`┬`/`┴`).
  - Геометрия: ширина 88 (или меньше), высота `rows+8`; текстовое поле занимает 2 строки (ярлык с добавленным `:` + поле), toggle — 1. Прокрутка (`Scroll`) следует за фокусом; индикатор `▲` справа вверху, если список прокручен.
  - Кнопки: `[Primary|OK]` и `Cancel` (`Form.buttons()`); у списка выбора — `Select`/`Cancel` без фокуса.
  - Мышь (`formClick`): клик по кнопке — Primary: фокус на кнопку + Enter (submit), Cancel → Escape. Клик по полю — фокус, курсор в конец, toggle переключается.
- **Код:** `form.go` (`form`, `formKey`, `formPaste`, `formGeometry`, `drawForm`, `formClick`).

#### Список выбора (`choose`)
- **Поведение:** `ArrowUp/Down/Tab` двигают фокус; при `Settings.MenuWrap` — циклически, иначе с ограничением; `Home`/`End`; `Enter` → `Select(Focus)`; `Escape` закрывает (без колбэка, `Cancel` не задаётся). Акселераторы по первым буквам пунктов (`Alt+буква` ставит фокус, не выбирает). Клик по строке выбирает сразу. Видимые строки = `rows+8` ограничено высотой окна, прокрутка.
- **Используют:** Sort mode, Volumes and home, Folder shortcuts, Network connections, Advanced settings, Colors, Code pages, Groups of file masks, Command/Folder/View-edit history, Screens, Archive commands, User menu, Edit user menu, File associations, результаты Find, Folder tree.

#### Подтверждение (`confirm`)
- **Поведение:** сообщение переносится по ширине `W-8` (многострочное через `\n`); `PageUp/PageDown` прокручивают длинный текст на 3 строки; кнопки по `ArrowLeft/Right/Tab`, `Enter`/`Space`, `Escape` = последняя кнопка; `Alt+буква` нажимает кнопку; при `Remember` — чекбокс `Remember choice` (`Space`/`Enter` на нём, `Alt+M`). Клик по кнопке нажимает её; клик по строке чекбокса переключает.
- **Используют:** Save changes (quit), Quit (shell busy), Unknown SSH host key, Delete remote files permanently, Move to Trash / Delete permanently, Error (Make folder retry), `<label> failed` (ошибки фоновых задач с `reportError`), вопросы операций (overwrite/skip/retry; описаны в разделе файловых операций).

### Окно прогресса (`internal/ui/progress.go`)
- Показывается, пока `a.Operation != nil` (Busy). Размер 88×15. Заголовок = метка операции; строки: путь текущего элемента, `Working / calculating… ▰▱▱▱  N items` (сканирование/стриминг) или `Total  xx.x%  n / m items` + полоса, `File  ` + полоса, при `Workers > 1` — `Parallel transfers: A active / W workers`, статистика `done / total · rate/s · Ns  ETA Ns` или `… processed · rate/s · Ns`. Кнопка `Cancel` (акселератор `c`); `Escape`/`Alt+C` или клик по кнопке → `cancelOperation`.
- При завершении (`pollTask`): `Status = ""`, ошибка → `a.error(err)` (в статус), а если у задачи `reportError` и ошибка не отмена — дополнительно `confirm("<Label> failed", err, ["OK"])`.

### Строка статуса и key bar (`draw.go`, `Poll`)
- Строка `Rows-3`: `a.Status`; если пусто — подсказка `Tab Switch panel   Space Select   ⌘O Terminal   ⌘⌥0…9 Bookmark`.
- Строка `Rows-2`: `<path>>` + командная строка; для удалённой панели путь показывается как `local:<локальный путь панели>` (команды выполняются локально).
- Строка `Rows-1` (если `ShowKeys`): 10 ячеек `1Help 2UserMenu 3View 4Edit 5Copy 6RenMov 7MkFold 8Trash 9Menu 10` (пусто); для удалённой панели 8 → `Delete`. Клик по ячейке вызывает `function(n)`.
- В режиме терминала `Footer` заменяется: `⌘O  Panels    ·    Shell ready    ·    <cwd>` / `⌘O  Panels    ·    Starting <shell> (loading shell configuration)…    ·    ⌘. Interrupt` / `⌘O  Panels    ·    ⌘.  Interrupt` / при перезапуске `⌘O  Panels    ·    <Status>`.
- Очистка статуса: `Escape` в панелях (если нет активных чтений: `Command = ""; Status = ""`), завершение фоновой задачи (`task.go`), а также каждое событие готовности shell перезаписывает статус `Exit N · ⌘O panels / terminal`. Остальные сообщения живут до следующей перезаписи.
- `a.error(err)` — если `err != nil`, кладёт `err.Error()` в статус и помечает `dirty`. Отдельного окна ошибок нет (кроме `reportError` задач и `Error` при создании папки).

---

## 2. Меню F9

### Открытие и навигация

#### Главное меню F9
- **Вызов:** `F9`, `⌘9` (`function(9)`); `Shift+F10` — открыть меню на последнем выполненном пункте (`a.lastMenu`, запоминается при `Enter`); мышь — клик по имени раздела в строке 0.
- **Где доступно:** панели (не терминал, не документ, не форма/диалог, не Busy). При `Active == 0` открывается раздел `Left` (column 0), при `Active == 1` — `Right` (column 4).
- **Поведение (`menuKey`):**
  - `Escape`, `F9`, `⌘9` — закрыть.
  - `ArrowLeft`/`ArrowRight` — соседний раздел по кругу, строка 0.
  - `Tab` — из `Left` в `Right`, из `Right` в `Left`, из любого другого — в `Right` (или в `Left`, если активна правая панель); строка 0.
  - `ArrowDown`/`ArrowUp` — при `Settings.MenuWrap` циклически, иначе с упором; `Home`/`End`; `PageDown`/`PageUp` — шаг `Rows-5`.
  - `Enter` — запоминает `lastMenu`, закрывает меню, затем выполняет `Run` пункта.
  - После любой клавиши курсор «перепрыгивает» разделители (пустые `menuItem`) в направлении движения.
  - Буквенных акселераторов в меню **нет**.
  - `⌘O` при открытом меню закрывает его и переключает терминал. Клавиши `Alt+F1/F2/F7/F8/F10`, `Shift+F10`, `Shift+F4`, `F12`, `⌘N`, `⌘Q` перехватываются раньше `menuKey` и срабатывают даже при открытом меню (меню при этом остаётся открытым, кроме явного закрытия).
- **Мышь (`menuClick`):** клик внутри рамки по пункту (не разделителю) — выполнить; по разделителю — ничего; вне рамки — закрыть меню. Клик по заголовку другого раздела в строке 0 переключает раздел.
- **Отрисовка (`drawMenu`):** строка 0 — имена разделов (`Left` x=2, `Files` x=11, `Commands` x=21, `Options` x=34, `Right` x=46), активный раздел инвертирован (`White` на `Black`); справа бренд `FarshMac 0.20.0` (при ширине ≥ 70). Выпадающий блок: ширина `min(44, Cols-2)`, высота `min(items+2, Rows-3)`, прокрутка через `Menu.Top`, фон `Cyan`, рамка белая, текущая строка — фон `Black`; разделители `╟───╢`; текст шортката жёлтым в правых 9 колонках; префикс `✓ ` рисуется как галочка в первой колонке (текущий вид панели).
- **Код:** `menu.go` (`menuSections`, `menuKey`, `menuBounds`, `menuClick`, `drawMenu`), `app.go` (`function(9)`, `click`).

### Раздел Left / Right (одинаковы, параметр — индекс панели)

| Пункт | Шорткат в меню | Действие |
| --- | --- | --- |
| `Brief` | — | `setView(i, "Brief")` |
| `Medium` | — | `setView(i, "Medium")` |
| `Full` | — | `setView(i, "Full")` (галочка также при пустом `View`) |
| `Wide` | — | `setView(i, "Wide")` |
| `Detailed` | — | `setView(i, "Detailed")` |
| *(разделитель)* | | |
| `Info panel` | — | `setView(i, "Info panel")` |
| `Tree panel` | — | `setView(i, "Tree panel")` |
| `Quick view` | — | `setView(i, "Quick view")` |
| *(разделитель)* | | |
| `Sort modes…` | — | `sortMenu(i)` |
| `Panel on/off` | `⌘F1` / `⌘F2` | `togglePanel(i)` |
| `Re-read` | `⌘R` | `rereadPanel(i)` |
| `Change volume…` | `AltF1` / `AltF2` | `volumes(i)` |

Текущий вид помечен `✓ `. Тест `TestCleanMenuOrderAndSeparators` фиксирует именно этот порядок.

#### Смена вида панели (setView)
- **Поведение:** для удалённой панели `Tree panel`/`Info panel`/`Quick view` запрещены → статус `Choose a file view for remote panels`; внутри архива `Tree panel` → `Tree view is unavailable inside archives`. Иначе `View = view`, `Hidden = false`, панель перезагружается, кэш quick view сбрасывается. При загрузке старых setup виды `Descriptions`, `Long descriptions`, `File owners`, `File links`, `Alternative full` нормализуются в `Full` (`configurePanel`).
- **Код:** `panel_options.go` (`setView`), `settings.go` (`configurePanel`).

#### Sort modes…
- **Диалог:** список `Sort mode`: `Name`, `Extension`, `Size`, `Modified`, `Reverse order`, `Case sensitive`, `Directories first`, `Selected first`, `Unsorted`, `Created`, `Accessed`, `Description`, `Owner`.
- **Поведение:** пункты-режимы устанавливают `panelOptions.Sort`; `Reverse order`/`Case sensitive`/`Directories first`/`Selected first` переключают флаги `Reverse`/`CaseSensitive`/`MixDirectories`/`SelectedFirst` (пункт `Directories first` инвертирует `MixDirectories`; в списке текущие значения не показываются). После выбора панель перезагружается.
- **Клавиши вне меню:** `Ctrl/⌘+F12` — тот же список для активной панели; `Ctrl/⌘+F3…F11` — прямой выбор режима (`Name, Extension, Modified, Size, Unsorted, Created, Accessed, Description, Owner`; повтор — инверсия порядка); на удалённой панели `Created/Accessed/Owner/Description` → `This sort mode requires local file metadata`; `Shift+F11` — выкл/вкл sort groups (`DisableSortGroups`), `Shift+F12` — `Selected first`.
- **Код:** `panel_options.go` (`sortMenu`, `sortEntries`), `far_keys.go`.

#### Panel on/off
- **Вызов:** меню, `⌘F1`/`⌘F2` (`Ctrl+F1/F2` тоже), `Ctrl/⌘+P` — скрыть неактивную панель.
- **Поведение:** `Hidden` инвертируется; если скрывается активная панель, а другая видима, активной становится другая.
- **Код:** `archive_panel.go` (`togglePanel`).

#### Re-read
- **Вызов:** меню (конкретная панель), `⌘R`/`Ctrl+R` (активная панель).
- **Поведение:** сбрасывает ключи инфо-панели, для архива — переоткрывает архив, иначе `Load(Path)`; ошибка в статус.
- **Код:** `archive_panel.go` (`rereadPanel`).

#### Change volume… / Mounted volumes
- **Вызов:** меню Left/Right (`AltF1`/`AltF2`), Commands → `Mounted volumes` (активная панель).
- **Диалог:** список `Volumes and home`: `/`, домашняя папка, все записи `/Volumes/*`.
- **Поведение:** выбранная панель становится активной, `Remote = nil` (удалённое подключение панели сбрасывается без сообщения), загружается путь.
- **Код:** `panel_options.go` (`volumes`).

### Раздел Files

| Пункт | Шорткат | Действие / условия |
| --- | --- | --- |
| `Open with macOS` | `⇧Enter` | `openSystemSelected`: на удалённой панели → статус `Copy remote files to a local panel before opening them with macOS`; элемент внутри архива → `Use F5 to copy this archive item out before opening it with macOS`; иначе `open` через Finder-ассоциацию |
| `View / folder size` | `F3` | `function(3)`: на папке — подсчёт размеров; удалённый файл — скачивание и просмотр; архив — просмотр члена |
| `Calculate all folder sizes` | `⌘⌥F3` | `calculateFolderSizes(true)` (работает и на SFTP, см. тест) |
| `Edit` | `F4` | `function(4)`; в архиве → `Archive is read-only. Use F5 to copy files out.`; на удалённой панели — скачать и редактировать |
| `New file…` | `ShiftF4` | `newFile` (на удалённой панели/в архиве блокируется) |
| `Copy…` | `F5` | `transferDialog(false)` |
| `Rename or move…` | `F6` | `transferDialog(true)` → для удалённых панелей `remoteMoveDialog` |
| `Link…` | — (в коде `Alt+F6`) | `createLink` |
| `Make folder…` | `F7` | `function(7)` → локально prompt `Make folder` / удалённо `Make remote folder` |
| `Move to Trash…` | `F8` | `deleteSelected(false)`; на удалённой панели — `remoteDelete` |
| `Delete permanently…` | `⇧F8` | `deleteSelected(true)` (также `Shift+Delete`; `Delete` без Shift = Trash) |
| `Create archive…` | — (в коде `Shift+F1`) | `archiveCreate` |
| `Extract archive…` | — (в коде `Shift+F2`) | `archiveExtract` |
| `Archive commands…` | — (в коде `Shift+F3`) | `archiveCommands` |
| `File attributes…` | — | `fileAttributes` |
| `Apply command…` | — | `applyCommand` |
| `Describe files…` | — | `describeFiles` |
| `Select group…` | `Num +` | `selectGroup("Select")` |
| `Deselect group…` | `Num −` | `selectGroup("Deselect")` |
| `Invert selection` | `Num *` | `selectGroup("Invert")` |
| `Restore selection` | `⌘M` | `selectGroup("Restore")` |

Пункты `Link…`, `File attributes…`, `Apply command…`, `Describe files…`, `New file…`, `Create archive…` на удалённой панели пишут в статус `This command requires a local panel. Use F5 to copy remote files first.` и ничего не открывают (тест `TestRemoteCommandsDoNotModifyLocalPaths`); внутри архива — `Archive is read-only. Use F5 to copy files out.`.

#### Link…
- **Диалог:** `Create link`: `Link name` (по умолчанию `<другая панель>/<имя>`), toggle `Symbolic link` (вкл.). OK → `os.Symlink` или `os.Link`, ошибки в статус, панели обновляются.

#### File attributes…
- **Диалог:** `File attributes`: `Permissions (octal)` (текущие права `%03o` первого выбранного), `Modified (local)` (`2006-01-02 15:04:05`). Проверки: `Permissions must be 000–777`; время парсится в локальной зоне. Применяется ко всем выбранным через `workProgress("Updating attributes")`; символические ссылки → ошибка `attributes of symlinks are not changed: <path>`.

#### Apply command…
- **Диалог:** prompt `Apply command` / `Use {} for a shell-quoted filename`. Для каждого выбранного файла формируется команда (`{}` заменяется на экранированный путь, иначе путь добавляется в конец), команды объединяются `\n` и выполняются в терминале (`execute`).

#### Describe files…
- **Диалог:** prompt `Describe files` / `Description (empty removes it)`, начальное значение — описание первого выбранного. Пишет файл `Settings.DescriptionFile` (по умолчанию `descript.ion`) в текущей папке атомарно; имена с пробелами в кавычках; имена с `\n\r"` → ошибка `this filename cannot be stored in descript.ion`.

#### Select group… / Deselect group… / Invert / Restore
- **Диалог:** prompt `Select group` / `Deselect group`, подсказка `Masks: *.go;*.md | *_test.go (exclude)`, значение `*`. Маски валидируются (`validateMasks`, группы `<name>` из `MaskGroups`); папки включаются при `Settings.SelectFolders`. `Restore` обменивает `Marked` и `RestoreMarked`.

### Раздел Commands

| Пункт | Шорткат | Действие |
| --- | --- | --- |
| `Network connections…` | — | `networkMenu` (раздел 5) |
| `Quick view on/off` | `Ctrl+Q` | `toggleOtherView("Quick view")` для неактивной панели; возврат к прежнему виду при повторе |
| `New window` | `⌘N` | `NewWindow = true` → Cocoa открывает окно, копируя настройки |
| `Find file…` | `AltF7` | `findFiles(false)` — см. раздел поиска |
| `Command history` | `AltF8` | `historyMenu("Commands")` |
| `Find folder…` | — | `findFiles(true)` |
| `Folder tree` | `AltF10` | `treeMenu` — фоновое сканирование, список `Folder tree` (`(partial)` при лимитах) |
| `File view/edit history` | — (в коде `Alt+F11`) | `historyMenu("Files")` |
| `Folder history` | — (в коде `Alt+F12`) | `historyMenu("Folders")` |
| `Same folder in other panel` | `⌘=` | `matchOtherPanel` (также `⌘⇧\`) |
| `Swap panels` | `⌘U` | обмен `Panels[0]`/`Panels[1]` (также `Ctrl+U`) |
| `Panels on/off / terminal` | `⌘O` | `showTerminal` |
| `Compare folders` | — | `compareFolders` |
| `User menu` | `F2` | `userMenu` — см. раздел user menu |
| `Edit user menu…` | — | `editUserMenu` |
| `File associations…` | — | `fileAssociations` |
| `Bookmarks…` | ⌘⌥0…9 | `bookmarksMenu` |
| `Edit sort groups…` | — | `settingsMenu("Sort groups")` |
| `File panel filter…` | — | `filterPanel(Active)` |
| `Screens list` | `F12` | `screenMenu` |
| `Full screen` | `⌃⌘F` | `Fullscreen = true` (Cocoa toggleFullScreen) |
| `Task list` | — | `taskList` |
| `Mounted volumes` | — | `volumes(Active)` |
| `Insert filename` | `⌘Enter` | `insertFilename` |
| `Quit` | `⌘Q` | `RequestQuit` |

#### Command history / View-edit history / Folder history
- **Диалоги:** списки `Command history` (новые сверху; выбор подставляет команду в командную строку без выполнения), `View/edit history` (`View <path>` / `Edit <path>`; выбор открывает документ), `Folder history` (объединение `folderHistory` и истории обеих панелей, лимит `HistoryLimit`; выбор загружает в активную панель).
- **Сообщения:** `Command history is empty`, `View/edit history is empty`; для пустой папочной истории — `Folder history: empty` (через `choose`).
- Истории сохраняются в `setup.json` только по Save setup.

#### Compare folders
- Снимает выделение (сохраняя в `RestoreMarked`) и помечает в обеих панелях файлы, отсутствующие напротив, отличающиеся размером или более новые. Статус: `Marked files missing, different in size, or newer than the opposite panel (non-recursive)`.

#### Edit user menu… / File associations…
- `Edit user menu`: список `Add command…` + имена команд; форма `User command — {} inserts filename` с полями `Label`, `Command (empty deletes)`; после OK — `saveSetup()` (немедленная запись setup.json).
- `File associations`: список `Add association…`, `Open current file with macOS default app`, затем `<mask> → <command>`; форма `File association — {} inserts filename`: `Masks`, `Run command`, `Viewer command`, `Editor command`; маски валидируются; после OK — `saveSetup()`. Viewer/Editor-команды ассоциаций переопределяют внешний viewer/editor из настроек (`viewPlan`).

#### Bookmarks…
- **Диалог:** список `Bookmarks`: `Save current folder…`, затем `0  <метка>` … `9  <метка>` (папка — `<путь>/<файл>`, для удалённых с префиксом `<label>`; документ — `Edit|View <путь>:<строка>`). Выбор слота → `goBookmark`; `Save current folder…` → prompt `Save bookmark` / `Slot 0–9` (значение `1`), ошибка `Choose 0–9`.
- **Клавиши:** `⌘⌥0…9` — сохранить (в панелях папку и файл под курсором, в документе позицию), `⌘0…9` — перейти (не Numpad). Вариантов с Ctrl или Shift нет.
- **Хранение:** `App.Bookmarks map[int]bookmark` — только в памяти окна; не сохраняются и другим окнам не видны. Подробности — раздел 9.
- **Код:** `folder_shortcuts.go`, `panel_options.go` (`folderShortcuts`).

#### File panel filter…
- prompt `File panel filter` / `Include masks | exclude masks; empty shows all`, текущее значение `panelOptions.Filter`; валидация масок; фильтр применяется к файлам (не папкам), сохраняется в `panelOptions` (и в setup/session).

#### Screens list
- Список `Screens`: `Panels`, затем `View <name>`/`Edit <name>` (`* ` при несохранённых изменениях); фокус на текущем документе. `Ctrl+Tab`/`⌘⇧[`/`]` — циклическое переключение.

#### Task list
- Фоновая задача `Reading processes` (`/bin/ps -axo pid,ppid,%cpu,%mem,comm`), результат — документ-отчёт `Processes` (режим Help, только чтение).

#### Insert filename
- `⌘Enter` (и `Ctrl+J`): имя текущего файла добавляется в командную строку с экранированием (`terminal.Quote`), имена с `-` получают `./`; имена с управляющими символами → `Cannot insert a filename containing control characters`.

#### Quit
- `⌘Q` обрабатывается `WindowGroup.BeginQuit` для всех окон. `RequestQuit`: при Busy → `Wait for the file operation to finish`; при несохранённых документах — confirm `Save changes` / `Save changes in open documents before quitting?` с кнопками `Save all`, `Discard`, `Cancel`; при занятом shell — confirm `Quit` / `A command is running. Close the application?` (`Yes`/`No`). Другие окна во время группового выхода получают статус `Finish the quit dialog in the other window`. Перед завершением сохраняется session.json; `AutoSave` → `saveSetup()` в `App.Close`.

### Раздел Options

| Пункт | Шорткат | Действие |
| --- | --- | --- |
| `General settings…` | — | форма `General settings` |
| `Panel settings…` | — | форма `Panel settings` |
| `Terminal settings…` | — | форма `Terminal settings` |
| *(разделитель)* | | |
| `Viewer settings…` | — | форма `Viewer settings` |
| `Editor settings…` | — | форма `Editor settings` |
| `Advanced…` | — | список `Advanced settings` |
| *(разделитель)* | | |
| `Full Disk Access…` | — | `DiskAccessSettings = true` + статус |
| *(разделитель)* | | |
| `Save setup` | — (в коде `Shift+F9`) | `saveSetup` |
| `Keyboard reference` | `F1` | `function(1)` — справка |

---

## 3. Диалоги настроек (`internal/ui/settings.go`, `settingsMenu`)

Все формы настроек применяют значения в памяти сразу после OK; на диск они попадают только через `Save setup` (Shift+F9 / меню), `AutoSave` при закрытии окна или косвенно через действия, вызывающие `saveSetup()` (user menu, associations, folder shortcuts). Ошибка валидации пишется в статус, форма при этом уже закрыта (значения не применены).

#### General settings
- **Диалог:** `General settings`
  - `Font size (10–24)` — текст, default `14` → `FontSize` (передаётся рендереру в каждом `Update.FontSize`).
  - `Key bar` — toggle, default вкл → `ShowKeys` (также `Ctrl/⌘+B`).
  - `Wrap menus` — toggle, default вкл → `MenuWrap` (циклическая навигация в меню и списках).
  - `Select initial dialog text` — toggle, default вкл → `SelectDialogInput`.
  - `Remember dialog values` — toggle, default выкл → `RememberDialogValues`.
  - `Confirm deletion` — toggle, default вкл → `ConfirmDelete`.
  - `Confirm overwrite` — toggle, default вкл → `ConfirmOverwrite`.
  - `Save setup on exit` — toggle, default выкл → `AutoSave`.
- **Сообщения:** `Font size must be 10–24`.

#### Panel settings
- **Диалог:** `Panel settings`
  - `Show hidden files (Cmd+Shift+.)` — default вкл → `ShowHidden` (панели получают `HideHidden = !ShowHidden`); клавиши `⌘⇧.`, `Ctrl+H`.
  - `Highlight files` — default вкл → `Highlight` (раскраска по типу в панелях).
  - `Human-readable sizes (off = bytes)` — default выкл → `HumanReadableSizes` (также `Ctrl/⌘+Shift+S`).
  - `Hide and ignore .DS_Store` — default выкл → `IgnoreDSStore` (скрывает в панелях, в т.ч. удалённых, и пропускает при копировании/архивации: `Operation.IgnoreDSStore`).
  - `Select folders with file masks` — default выкл → `SelectFolders`.
- **Эффект:** сброс `ForcedVisiblePath`, `configurePanel` для обеих панелей, `refresh()`.

#### Terminal settings
- **Диалог:** `Terminal settings`
  - `Shell: bash / sh / zsh (new windows)` — текст, default `bash` → `Shell`.
  - `Command history entries (1–10000)` — default `200` → `HistoryLimit`.
  - `Complete filenames` — default вкл → `CompleteFiles` (⌘Space).
  - `Completion ignores case` — default выкл → `CompleteIgnoreCase`.
  - `Use custom shell prompt / hooks (new windows, may be slow)` — default выкл → `ShellCustomPrompt`.
- **Сообщения:** `Choose bash, sh or zsh` (валидация `terminal.ValidShell`, защищает от инъекций вроде `zsh; echo bad`), `History entries must be 1–10000`, после успеха: `Shell applies to new windows; Save setup to keep settings after restart`.

#### Viewer settings
- **Диалог:** `Viewer settings`: `Wrap lines` (default выкл → `ViewerWrap`), `External by default` (выкл → `ExternalViewerDefault`), `Command (blank=system)` (текст → `ExternalViewer`).

#### Editor settings
- **Диалог:** `Editor settings`: `Auto indent` (вкл → `EditorAutoIndent`), `Syntax highlighting (Chroma)` (вкл → `EditorSyntax`), `Tab width (1–16)` (`4` → `EditorTabSize`), `Expand tabs to spaces` (выкл → `EditorExpandTabs`), `External by default` (выкл → `ExternalEditorDefault`), `Command (blank=system)` (→ `ExternalEditor`).
- **Сообщения:** `Tab width must be 1–16`.

#### Advanced…
- **Диалог:** список `Advanced settings`: `Colors`, `Info panel settings`, `Tree settings`, `Folder description files`, `File descriptions`, `Groups of file masks`, `Sort groups`, `Code pages`.

#### Colors
- Список `Colors`: `Classic Far`, `Dark`, `Light` → `ColorTheme` (default `""` = классическая палитра). `Dark`: `Blue→#151b24`, `LightCyan→#c7d7e9`, `Cyan→#486987`; `Light`: `Blue→#f1f3f5`, `LightCyan→#182b49`, `White→#000000`, `Yellow→#885000`, `Cyan→#80b8dd`. Применяется ко всей сетке при отрисовке (`Draw`). Отдельной настройки шрифта (гарнитуры) нет — только размер.

#### Info panel settings
- **Вызов:** Advanced → `Info panel settings`, либо `Ctrl/⌘+F12` при активной инфо-панели.
- **Диалог:** `Info panel settings`: `Computer and user` (вкл → `InfoHost`), `Drive information` (вкл → `InfoVolume`), `Memory information` (вкл → `InfoMemory`), `Folder description` (вкл → `InfoDescription`), `Power status` (вкл → `InfoPower`). После OK кэш инфо-панели сбрасывается.

#### Tree settings
- prompt `Tree settings` / `Maximum levels for folder tree (1–12)`, default `4` → `TreeDepth`; ошибка `Enter a number from 1 to 12`; панели перенастраиваются и обновляются.

#### Folder description files
- prompt `Folder description files` / `Names separated by commas`, default `file_id.diz,README.md,README,readme.txt` → `FolderDescriptions` (показ описания папки в инфо-панели); имена с путём → `Use filenames without paths`.

#### File descriptions
- prompt `File descriptions` / `Description file name`, default `descript.ion` → `DescriptionFile`; `Enter a file name without a path`.

#### Groups of file masks
- Список `Groups of file masks`: `Add group…`, затем `<name> = <masks>` (ключи отсортированы). Форма `Mask group`: `Name`, `Masks (empty deletes)`. Ошибки: `Invalid group name` (пусто или содержит `<>,;|`), `Use literal masks in groups` (вложенные `<>`), ошибки `filepath.Match`. Переименование удаляет старый ключ. Default-группы: `arc` (`*.zip,*.tar,*.gz,*.bz2,*.xz,*.tgz,*.tbz,*.tbz2,*.txz,*.7z,*.rar,*.jar,*.war,*.cpio`), `source` (`*.go,*.c,*.h,*.js,*.ts,*.py`), `exec` (`*.sh,*.command`). Группы подставляются как `<name>` в масках фильтра, выделения, поиска, ассоциаций, sort groups.

#### Sort groups (Advanced → Sort groups; Commands → Edit sort groups…)
- Два входа в один prompt — намеренное дублирование (как в Far: Commands → «Edit sort groups», Options → «Sorting»).
- prompt `Sort groups` / `Masks in priority order, separated by semicolons`, default пусто → `SortGroups`. Валидация с заменой `;`→`,`; статус с текстом ошибки (`unknown mask group` и т.п.). Файлы, подходящие под более ранние маски, сортируются выше (кроме режима `Unsorted` и при `DisableSortGroups`/Shift+F11).

#### Code pages
- Список `Default encoding for new files`: `UTF-8`, `UTF-16LE`, `UTF-16BE`, `Windows-1251`, `CP866`, `Windows-1252`, `ISO-8859-1`, `KOI8-R` → `DefaultCodepage` (default `UTF-8`).

#### Full Disk Access…
- **Вызов:** меню Options, а также нативное меню приложения `FarshMac → Full Disk Access…`.
- **Поведение:** `DiskAccessSettings = true` (одноразовый флаг в `Update`, тест `TestDiskAccessMenuEmitsOneNativeRequest`), статус `Enable this copy of FarshMac in Full Disk Access (+ or drag from Finder), then quit and reopen.`. Нативная сторона выделяет бандл в Finder и открывает `x-apple.systempreferences:com.apple.preference.security?Privacy_AllFiles` (fallback — System Settings, затем NSAlert `Open Full Disk Access in System Settings`). При первом запуске `native.PrepareDiskAccess` показывает алерт `Full Disk Access for FarshMac` с кнопками `Open Settings`/`Continue` (флаг `FarshMacDiskAccessIntroductionShown` в NSUserDefaults). Подробности — в разделе платформы.

#### Save setup
- **Вызов:** меню Options → `Save setup`; `Shift+F9`; косвенно из user menu/associations/folder shortcuts.
- **Поведение (`saveSetup`):** `normalizeSettings()`; для удалённых панелей обновляет `Directory` соответствующего профиля в `Settings.RemoteConnections` (в памяти); истории папок обеих панелей добавляются в `folderHistory`; собирается `savedSetup{Settings, Panels[2] panelOptions, Paths[2] (filesystemPath), Commands, Folders, Viewed}`; JSON с отступами пишется атомарно (temp-файл `.setup-*` + rename, каталог `0700`). Статус `Setup saved`; ошибка записи — поверх через `a.error`.
- **Код:** `settings.go`.

#### Keyboard reference (F1)
- **Вызов:** `F1`, `⌘1`, меню Options.
- **Поведение:** открывает документ `FarshMac — Help` с константой `help` в режиме `Help` (только чтение, `markSaved`). Внутри документа `F1` повторно ничего не делает (для Help-документа), для обычных документов `F1` открывает `Viewer / editor help`. Закрытие: `Escape`, `F10`, `F3` (в viewer). `F12` — список экранов. `F6` (переключение viewer/editor) для Help игнорируется.
- **Содержание (дословно):**

```
FarshMac — native macOS window, Go file manager, persistent zsh terminal

Tab                 Switch active panel
Arrows / PgUp/PgDn   Navigate files
Ctrl+Q             Toggle inactive Quick view (restore previous view)
Ctrl+L / Ctrl+T    Toggle inactive Info / Tree
Ctrl+P / Ctrl+B    Toggle inactive panel / key bar
Ctrl+F3..F11       Sort (name, ext, modified, size, unsorted, created, accessed, description, owner)
Ctrl+F12           Sort menu; Shift+F12 selected first
Shift+F5/F6        Copy/move only the current item
Shift+F1/F2/F3     Archive create/extract/commands
Shift+F9           Save setup
Alt+F11/F12        View/edit history / folder history
Ctrl+PgUp/PgDown   Parent / enter folder or archive
Ctrl+Left/Right    Panel width (empty command); Ctrl+Num5 reset
Ctrl+E/X/Y         Previous / next command / clear
Ctrl+F / Ctrl+;    Insert full path from active / inactive panel
Ctrl+Insert        Copy command or selected names; Alt+Shift+Insert full paths
Shift+Insert       Paste text
Num + / Num -      Select / deselect by masks
Num *              Invert files; Cmd/Ctrl+Num * includes folders
Cmd/Ctrl+Num +/-   Select / deselect same extension
Alt+Num +/-        Select / deselect same base name
Shift+Num +/-      Select / deselect all files
Cmd/Ctrl+M         Restore previous selection
Ctrl+Shift+1..9     Save folder shortcut (panels)
Ctrl+1..9           Go to folder shortcut (panels)
Network connections: F9 > Commands > Network connections
FTP / FTPS / SFTP    Remote panel; terminal stays local
F5 Parallel transfers: 1-8 workers, Esc cancels

Enter               Open directory / view file / execute command
Shift+Enter         Open selected item with macOS (folders in Finder)
Cmd+Shift+.         Show / hide hidden files (display only)
Backspace           Delete command-line character; never leave directory
Space / Insert      Mark file; operations apply to marked files
F3 on a folder      Calculate marked/current folder sizes; participates in Size sort
⌘Option+F3         Calculate all folder sizes (~ = cached, possibly outdated)
Info: Ctrl+F12      Configure sections; PgUp/PgDn/wheel scroll; F3/F4 description
F3 / F4             Viewer / editor (F1 inside for full key reference)
F5 / F6             Copy / move or rename (no overwriting)
F7                  New directory
F8 / Delete         Move selected items to Trash
Shift+F8 / Shift+Del Delete permanently
F9 / Cmd+9          Main menu (arrows, Enter, Esc)
Cmd+L              Change directory
Cmd+Enter          Insert current filename in command line
F2 / Cmd+2          User menu
Cmd+R              Refresh panels
Cmd+Space          Complete filename in command line
⌘U              Swap panels
⌘F1 / ⌘F2     Hide/show left / right panel
Alt+F1 / Alt+F2    Choose left / right volume
⌘Shift+\ or ⌘=  Open active folder in the other panel
Enter on archive  Browse archive; .. returns to parent
F5 in archive     Copy selected files/folders out; F3 previews
File links view   Names, sizes and hard-link counts (names sharing one inode)
⌘Up / Down      Command history
⌘O              Toggle panels and persistent terminal
Cmd+1 … Cmd+9       Alternatives to F1 … F9 on Mac keyboards
Cmd+N / Cmd+W      New / close native window
Cmd+Q              Quit ALL windows; F10 closes only viewer/editor
Alt+characters     Fast find (Cmd+Enter next; Cmd+Shift+Enter previous)
F12                Panels and document screens
Esc during operation  Cancel

Type a shell command directly while panels are visible, then press Enter.
The panels disappear and the SAME window becomes an interactive terminal.
Commands run in the active panel's directory. zsh keeps its environment,
history, aliases and jobs. Your .zshrc is loaded at shell startup.
The output stays visible after completion; ⌘O restores the panels.
⌘O also works during a command; returning does not stop the job.
Use Cmd+. (or Ctrl+C) to interrupt, Cmd+C / Cmd+V to copy / paste.
Scroll the terminal to review output. vim/less use the alternate screen.

This is an initial independent implementation, not full Far compatibility.
Read-only archive panels are supported. No Windows DLL plugins, macros or network VFS yet.
Cross-volume move is refused. Copy, verify, then delete explicitly.
Copies preserve permissions, timestamps and links, not ACLs/xattrs.
The text editor saves atomically, but does not preserve ACLs/xattrs/hardlinks.
Text editing/decoding: 64 MiB limit. Larger files: paged hex/dump viewer.
Esc returns to the file panels.
```
- **Код:** `app.go` (`help`, `function(1)`), `document.go` (`documentKey`), `commands.go` (`report`).

#### Неиспользуемые из меню ветки settingsMenu (мёртвый код)
В `settingsMenu` есть формы, до которых нельзя добраться из текущего меню: `Dialog settings` (`Select initial input`, `Remember values`), `AutoComplete settings` (`Files (⌘Space)`, `Ignore case`), `System settings` (`Save setup on exit`), `Interface settings` (`Font size (10–24)`, `Key bar`), `Menu settings` (`Wrap at list ends`), `Confirmations` (`Delete files/folders`, `Overwrite files`), `Command line settings` (prompt `Maximum history entries (1–10000)`). Неизвестный `kind` → статус `Settings: <kind>`.

#### Настройки, редактируемые вне Options
- `CopyWorkers` (default `4`, диапазон 1–8), `CopyPermissions` (вкл), `CopyAttributes` (вкл) — поля диалога копирования F5: `Preserve permissions (mode, owner, group, ACL)`, `Preserve attributes (xattrs, flags, timestamps)`, `Parallel transfers (1–8)` (ошибка `parallel transfers must be between 1 and 8`). Значения запоминаются в `Settings` при каждом копировании.
- `EditorExpandTabs`/`EditorTabSize` — также из диалога настроек редактора внутри документа (`document.go`).
- `ShowHidden`, `ShowKeys`, `HumanReadableSizes` — горячими клавишами (см. выше).
- `UserMenu`, `Associations`, `Shortcuts*` — соответствующими диалогами (с немедленным `saveSetup`).
- `RemoteConnections` — `connections.json` (раздел 5).

---

## 4. Структура Settings, значения по умолчанию, хранение и совместимость

### Поля `Settings` (`settings.go`) и defaults (`defaultSettings`)

| Поле | Default | Где меняется |
| --- | --- | --- |
| `InfoMemory`, `InfoPower`, `InfoDescription` | true | Info panel settings |
| `ShellCustomPrompt` | false | Terminal settings |
| `SelectFolders` | false | Panel settings |
| `CopyWorkers` | 4 (норм. 1–8) | диалог Copy |
| `RemoteConnections []remotefs.Config` | nil | connections.json / connectRemote |
| `CopyPermissions`, `CopyAttributes` | true | диалог Copy |
| `Shell` | `bash` (норм. `ValidShell`) | Terminal settings |
| `HumanReadableSizes` | false | Panel settings, Ctrl+Shift+S |
| `ExternalViewer`, `ExternalEditor` | "" | Viewer/Editor settings |
| `ExternalViewerDefault`, `ExternalEditorDefault` | false | Viewer/Editor settings |
| `CompleteFiles` | true | Terminal settings |
| `CompleteIgnoreCase` | false | Terminal settings |
| `SelectDialogInput` | true | General settings |
| `RememberDialogValues` | false | General settings |
| `InfoHost`, `InfoVolume` | true | Info panel settings |
| `FolderDescriptions` | `file_id.diz,README.md,README,readme.txt` | Folder description files |
| `AutoSave` | false | General settings |
| `RestoreFolders` | false | **нигде не используется** |
| `MenuWrap` | true | General settings |
| `ShowKeys` | true | General settings, Ctrl+B |
| `FontSize` | 14 (норм. 10–24) | General settings |
| `ColorTheme` | "" (Classic Far) | Colors |
| `ConfirmDelete` | true | General settings |
| `ConfirmOverwrite` | true | General settings |
| `ShowHidden` | true | Panel settings, ⌘⇧., Ctrl+H |
| `IgnoreDSStore` | false | Panel settings |
| `Highlight` | true | Panel settings |
| `ViewerWrap` | false | Viewer settings |
| `EditorAutoIndent` | true | Editor settings |
| `EditorSyntax` | true | Editor settings |
| `EditorExpandTabs` | false | Editor settings / документ |
| `EditorTabSize` | 4 (норм. 1–16) | Editor settings / документ |
| `DefaultCodepage` | `UTF-8` | Code pages |
| `HistoryLimit` | 200 (норм. 1–10000) | Terminal settings |
| `TreeDepth` | 4 (норм. 1–12) | Tree settings |
| `UserMenu []UserCommand{Name, Command}` | nil | Edit user menu |
| `Associations []Association{Mask, Command, Viewer, Editor}` | nil | File associations |
| `Shortcuts [10]string`, `ShortcutRemotes [10]*remotefs.Config`, `ShortcutArchives [10]string` | пусто | Устаревшие folder shortcuts; читаются для совместимости, не используются (закладки живут в окне) |
| `SortGroups` | "" | Sort groups |
| `MaskGroups map[string]string` | `arc`, `source`, `exec` (см. выше) | Groups of file masks |
| `DescriptionFile` | `descript.ion` (норм. — только базовое имя) | File descriptions |

Также в `savedSetup` сохраняются `panelOptions` обеих панелей (`DisableSortGroups`, `View`, `Sort`, `Filter`, `Reverse`, `CaseSensitive`, `MixDirectories`, `SelectedFirst`, `Hidden`), `Paths[2]`, `Commands` (история команд), `Folders` (история папок), `Viewed []viewRecord{Path, Edit}`.

#### Расположение файлов
- Корень: `os.UserConfigDir()` → на macOS `~/Library/Application Support`. Каталог `FarshMac/`:
  - `setup.json` — настройки + истории (`setupPath`).
  - `connections.json` — профили подключений (`profilesPath`).
  - `session.json` — окна/папки/курсоры (`SessionPath`, `WindowGroup.SaveSession`).
- Секреты (`Password`, `KeyPassphrase`, `HostKey`) имеют тег `json:"-"` и никогда не попадают на диск (тесты `TestHostKeysAndSecrets`, `TestConnectionProfilesPersistWithoutSaveSetup`).

#### Загрузка setup (`LoadSetup`)
- Вызывается в `main` после создания `App`. Читает `setup.json` через `readConfigFile`; при ошибке JSON — настройки по умолчанию. Применяет `normalizeSettings`, `panelOptions` обеих панелей и `configurePanel`; **пути панелей из setup не восстанавливаются** (комментарий в коде: восстановление папок — через session.json); истории команд/папок/просмотра — из setup. В конце всегда `loadConnectionProfiles()` (defer), поэтому профили доступны даже без setup.json.

#### Автосохранение
- `AutoSave` (`Save setup on exit`) — `saveSetup()` в `App.Close()` для каждого окна (при закрытии окна и при выходе). По умолчанию выключено.
- Session (`session.json`) сохраняется независимо от Save setup: в каждом `WindowGroup.Poll` не чаще раза в 500 мс и только при изменении содержимого; принудительно при закрытии окна, при завершении (`BeginQuit`), `CloseAll`. Формат: `{Version: 1, Windows: [{Frame{X,Y,Width,Height}, Panels: [{Path, Options, Cursor, Top}], Active}], Focused}`. При запуске с флагами `-left/-right` восстановление сессии отключено. При восстановлении недоступные папки заменяются ближайшим читаемым родителем, затем папкой по умолчанию, домом, `/` (`accessibleFolder`); версия ≠ 1 → `unsupported window session version N`; > 100 окон → `too many saved windows`.

#### Совместимость конфигурации (`config_compat.go`)
- `readConfigFile(path)`: если файл в `<UserConfigDir>/FarshMac/` отсутствует, читается одноимённый файл из каталога прежнего названия проекта `<UserConfigDir>/Far Go/` (setup.json, session.json, connections.json). Запись всегда идёт в `FarshMac/`; старые файлы не изменяются и не удаляются. Для путей вне стандартного каталога (например, явный путь сессии в тестах) fallback не применяется. Тесты: `TestRenamedConfigFallsBackWithoutOverwriting`, `TestRenamedConfigDoesNotFallbackForCustomPath`.
- Миграция старых видов панелей (`Descriptions`, `Long descriptions`, `File owners`, `File links`, `Alternative full` → `Full`) в `configurePanel`.
- `normalizeSettings` чинит выходящие за диапазон значения (`CopyWorkers`, `Shell`, `EditorTabSize`, `FontSize`, `HistoryLimit`, `TreeDepth`, `MaskGroups == nil`, `DescriptionFile`).
- Тест `TestSettingsRoundtrip` проверяет round-trip FontSize/MaskGroups/View/Shortcuts.

---

## 5. Сетевые подключения (FTP / FTPS / SFTP)

#### Network connections…
- **Вызов:** меню Commands → `Network connections…` (горячей клавиши нет; справка F1: `Network connections: F9 > Commands > Network connections`).
- **Диалог:** перед показом перечитывается `connections.json` (`loadConnectionProfiles`). Список `Network connections`: `New FTP / SFTP connection…`, затем каждый профиль как `<Label><Directory>` (Label = `sftp://user@host:port`), последний пункт `Disconnect active panel`.
- **Поведение:** `New…` → форма с заготовкой `{Protocol: sftp, Port: 22, Directory: "/", User: $USER}`; профиль → форма с его значениями; `Disconnect active panel` → `disconnectRemote(Active)` (если панель не удалённая — ничего).
- **После backlog (P5-13 и др.):** F8/Delete на профиле удаляет его после подтверждения `Delete connection profile`; несколько профилей на один сервер различаются парой сервер + папка; повреждённый `connections.json` переименовывается в `.broken-<время>`. Неизвестный ключ SFTP: `Trust once` / `Trust and save` (дописывает в `~/.ssh/known_hosts`) / `Cancel`; изменённый или отозванный ключ — `SSH host key changed` / `revoked` с отпечатками и `file:line`, только OK; прочие ошибки — `Connection failed` с `Edit` / `Retry` / `Cancel`. Тест с тремя пунктами меню при одном профиле соответствует коду и обновляется вместе с удалением профилей.
- **Код:** `remote.go` (`networkMenu`), `connection_profiles.go`.

#### Форма «FTP / SFTP connection»
- **Поля (`connectionDialog`):**
  1. `Server URL (sftp://, ftp://, ftps://)` — текст; для нового подключения `sftp://`, для профиля `c.ServerURL()` (схема://host:port/путь, без пользователя).
  2. `User` — текст (из профиля / `$USER`).
  3. `Password (memory only)` — secret, всегда пустое.
  4. `SSH private key (blank = default keys + agent)` — текст (`KeyFile`).
  5. `Key passphrase (memory only)` — secret, всегда пустое.
  - Кнопки: `Connect` (Primary) / `Cancel`.
- **Разбор URL (`remotefs.Parse`):** `strings.TrimSpace`; схема `ssh` → `sftp`; допустимы только `sftp`/`ftp`/`ftps` (иначе `use sftp://host, ftp://host or ftps://host`); обязателен host, запрещены query/fragment (`invalid server address`); пользователь в URL (`sftp://user@host`) переопределяет поле `User`; пароль в URL → `enter passwords in the password field, not the URL`; порт по умолчанию 22 для sftp, 21 для ftp/ftps, диапазон 1–65535 (`invalid port`); путь пустой → `/`, иначе `path.Clean`; управляющие символы `\x00\r\n` → `invalid control character in address`.
- **После OK:** `Password`, `KeyPassphrase` берутся из полей; `KeyFile` раскрывается через `expand` (поддержка `~`, относительный путь — от локального пути активной панели). Вызывается `connectRemote(Active, config)`.

#### Процесс подключения (`connectRemote`)
- **Поведение:** фоновая задача `Connecting to <label>` (окно прогресса, Esc отменяет; отмена закрывает все сокеты через `socketSet`): `Dial` + `ReadDir(Directory)` как проверка; соединение закрывается.
  - Ошибка `remotefs.UnknownHost` → confirm `Unknown SSH host key` с текстом `<host> <SHA256:fingerprint>. Trust for this connection?`, кнопки `Trust once` / `Cancel`. `Trust once` кладёт ключ в `c.HostKey` (только память, только это подключение) и повторяет `connectRemote`.
  - Другие ошибки — в статус (`a.error`).
  - Успех (`apply`): панель перенастраивается (`configurePanel`), отменяются фоновые загрузки, запоминается локальный путь панели `RemoteLocalPath` (или `$HOME`), `Remote = &c`, `Archive = nil`, `Async = true`, `View = "Full"`, сброс состояний/выделения, `Path = Directory`, курсор 0. Профиль сохраняется (`saveConnectionProfile`), панель загружается. Статус `Connected: <label> (terminal is local)`; при ошибке сохранения профиля добавляется `; profile not saved: <err>`.
- **Код:** `remote.go` (`connectRemote`), `remotefs/fs.go`, `sftp.go`, `ftp.go`.

#### Проверка host key и аутентификация SFTP (`remotefs/sftp.go`, `ssh_defaults.go`)
- known_hosts: `~/.ssh/known_hosts`, `~/.ssh/known_hosts2`, `/etc/ssh/ssh_known_hosts`, `/etc/ssh/ssh_known_hosts2` (только существующие). Неизвестный хост (нет записей) → `UnknownHost`; **несовпадение с известным ключом или отозванный ключ никогда не принимаются автоматически** (возвращается ошибка knownhosts). Если задан `HostKey` (после `Trust once`) — `ssh.FixedHostKey`. Предпочтение алгоритмов host key — как в OpenSSH: сначала типы, уже известные для хоста (`hostKeyAlgorithms`, тест `TestKnownHostAlgorithmPreference`).
- Ключи: если указан `KeyFile` — только он (ошибка `cannot read SSH private key <path>: …`); иначе `~/.ssh/id_rsa`, `id_ecdsa`, `id_ed25519` (отсутствующие пропускаются; зашифрованные без passphrase дают подсказку `<name> requires a passphrase or ssh-agent`); введённая passphrase применяется и к default-ключам. Файл ключа > 1 MiB → `SSH key file is too large`. Плюс все identities из `SSH_AUTH_SOCK` (ssh-agent). Пароль добавляется как `ssh.Password`, если непустой. Нет ни одного метода → `no usable SSH identity: provide a password, private key or ssh-agent. <hint>`. При ошибке handshake с hint — `<err> (<hint>)`.
- Таймауты: dial 15 с, SSH handshake 15 с, дедлайн чтения/записи каждого сокета 30 с. SFTP без concurrent reads/writes. `Rename` использует `posix-rename@openssh.com` при наличии. `Create` — `O_CREATE|O_EXCL|O_WRONLY`.

#### FTP / FTPS (`remotefs/ftp.go`)
- Пользователь пустой → `anonymous`. Credentials с `\x00\r\n` → `FTP credentials contain a protocol control character`.
- FTPS = только explicit TLS (`AUTH TLS`) с `MinVersion TLS1.2`, `ServerName = host`, системные корни (самоподписанный сертификат отвергается — тест `TestExplicitFTPSProtectsDataConnection`); data-соединения тоже оборачиваются в TLS. Implicit FTPS не поддерживается.
- Таймаут dial 15 с; дедлайны 30 с. `Lstat`: `GetEntry` (MLST), при 550 с текстом `no such file`/`not found`/`does not exist` → `ErrNotExist`, иначе листинг родителя; `/` считается папкой. `Chmod` → `FTP does not provide portable permission preservation`; `Readlink`/`Symlink` → `FTP symbolic links are not supported`. Размер > int64 → `invalid FTP size`.

#### Удалённая панель: загрузка и отображение (`loadRemote`)
- Каждая загрузка открывает новую сессию (`Dial`) и закрывает её; > 100000 записей → `remote folder exceeds 100000 entries`; `.`/`..` пропускаются; имя с `/\\\x00\r\n` или пустое → `server returned an unsafe filename`; применяются `IgnoreDSStore` (`.DS_Store`), `HideHidden` (точка-файлы), фильтр панели (файлы). Сортировка стандартная. `InfoText = "<label>\nRemote filesystem; terminal remains local"` (показывается в инфо-панели напротив).
- В заголовке командной строки путь выводится как `local:<RemoteLocalPath>`; key bar F8 → `Delete`. Quick view напротив удалённой панели: `Remote file: use F3 to download and view`.
- Виды `Tree panel`/`Info panel`/`Quick view` для удалённой панели недоступны (`Choose a file view for remote panels`; `Ctrl+L/T` → `This panel view requires a local folder`).

#### Операции в удалённой панели (кратко; подробности — в разделе файловых операций)
- `Enter` на папке — переход; на файле / `F3` / `F4` — фоновая задача `Downloading remote file` во временный каталог `farshmac-remote-view-*`, затем просмотр/редактирование; заголовок документа — `<label><remote path>`; не регулярный файл → `viewer requires a regular remote file`; изменился в процессе → `remote file changed during download`. Сохранение (`Saving remote file`): проверка `remote file changed; reopen before saving`, загрузка через временный файл `.farshmac-part-*` + rename; статус `Remote file saved`.
- `F5` между локальной и удалённой панелями, а также между двумя серверами (через локальный поток) — `remotefs.Copy` с воркерами 1–8, у каждого воркера своя сессия; лимиты 1 000 000 элементов / глубина 256 (`transfer tree limit exceeded`), 100000 записей на каталог (`remote directory exceeds 100000 entries`); метаданные переносятся «портативно» (chmod/chtimes), с вопросами `Cannot preserve metadata`.
- `F6`: только в пределах одного сервера (`Label` совпадает) — prompt `Remote move / rename` / `Destination on the same server`; иначе статус `Remote move is available within the same server. Between servers use F5 Copy, then Delete.`; несколько элементов в не-папку → `destination must be an existing directory`; вложенность → `destination is inside source`.
- `F7`: prompt `Make remote folder` / `Folder name`; имя с `/` и т.п. → `enter one folder name`; ошибки показываются и в окне `Creating remote folder failed` (`reportError`); `Folder creation skipped` при skip.
- `F8`/`Shift+F8`/`Delete`: confirm `Delete remote files permanently` / `Delete N selected item(s) on <label>? No remote Trash is available.` кнопки `Delete`/`Cancel`; удаление рекурсивное (`RemoveTree`, глубина ≤ 256: `remote tree depth exceeds 256`). Корзины нет.
- `Shift+Enter`, `New file`, `Link`, `File attributes`, `Apply command`, `Describe files`, `Create/Extract archive`, `Find`, `Folder tree`, user menu — заблокированы со статусом `This command requires a local panel. Use F5 to copy remote files first.` (или `Copy remote files to a local panel before opening them with macOS`).
- Терминал всегда локальный: `showTerminal`/`syncCwd` не синхронизируют cwd для удалённой панели; команды выполняются в `filesystemPath()` (локальный путь панели).
- `Ctrl+Alt+Insert` копирует полные пути в виде `<label><path>`.

#### Disconnect active panel
- **Поведение (`disconnectRemote`):** отменяет загрузки, `Remote = nil`, загружает `RemoteLocalPath` (локальная папка до подключения), статус `Disconnected`. Также любое действие, ставящее `Remote = nil` (Change volume, локальный folder shortcut, Same folder in other panel с локальной панели-источника).

#### Профили подключений (`connection_profiles.go`)
- Файл `connections.json` — JSON-массив `remotefs.Config` (`Protocol, Host, User, Directory, KeyFile, Port`; без пароля/passphrase/host key). Записывается после каждого успешного подключения (`saveConnectionProfile`): перечитывает файл, заменяет профиль с тем же `Label()` (`scheme://user@host:port`) или добавляет новый, пишет атомарно (`.connections-*` + rename, `0700`), обновляет `Settings.RemoteConnections`. Доступ под `profilesMu` (общий для окон). Не зависит от Save setup (тест `TestConnectionProfilesPersistWithoutSaveSetup`).
- `Save setup` дополнительно обновляет `Directory` профилей подключённых панелей, но только в памяти (`setup.json` содержит копию `RemoteConnections` в `Settings`; при старте приоритет у `connections.json`, т.к. `loadConnectionProfiles` вызывается после чтения setup).
- Удаления/редактирования профиля из UI нет (только повторное подключение с новыми параметрами под тем же label перезаписывает `Directory`/`KeyFile`).

---

## 6. Нативное меню macOS (для полноты)

Создаётся в `window_darwin.m`: `FarshMac` → `About FarshMac`, `Full Disk Access…`, `Quit FarshMac ⌘Q`; `File` → `New Window ⌘N`, `Close Window ⌘W`; `Edit` → `Copy ⌘C`, `Cut ⌘X`, `Paste ⌘V`, `Select All ⌘A`; `Window` → `Command History ⌘⇧H` и `Terminal Output History` (активны только в режиме терминала), `Enter Full Screen ⌃⌘F`, `Minimize ⌘M`, `Next Window ⌘\``, `Previous Window ⌘⇧\``, `Bring All to Front`.

Примечание: `⌘M` в панелях обрабатывается Go-кодом как «Restore selection» (пункт меню Files), но на уровне Cocoa тот же `⌘M` назначен `Minimize` — какой обработчик победит, определяется нативным монитором клавиш; в коде Go нет исключения.

---

## 7. Расхождения между кодом, MENU_PARITY.md, README и справкой F1

> **Закрыто на ветке `backlog-fixes`:** решения по каждому пункту — в разделе «Решения по расхождениям» `docs/IMPLEMENTATION_BACKLOG.md`.

1. **`Save setup` без шортката в меню**, хотя `Shift+F9` реализован (`far_keys.go`) и указан в справке F1. В README не упомянут.
2. **Пункты без шортката в меню, но с горячими клавишами в коде и справке:** `Link…` (`Alt+F6` — не упомянут и в справке), `Create archive…`/`Extract archive…`/`Archive commands…` (`Shift+F1/F2/F3` в справке), `File view/edit history`/`Folder history` (`Alt+F11/F12` в справке), `Sort modes…` (`Ctrl+F12` в справке), `Find folder…` нет клавиши нигде.
3. **Справка F1 называет `Ctrl+L / Ctrl+T` переключением Info/Tree, а `Cmd+L` — Change directory**: код подтверждает (`toggleOtherView` только при `Ctrl` без `Meta`; `⌘L` → `gotoPrompt`). README пишет `Ctrl+Q / Ctrl+L` — согласовано. Но в меню Commands есть только `Quick view on/off`, пункта для `Info panel on/off`/`Tree` нет (они в Left/Right как виды).
4. **Меню показывает `Ctrl+Q`**, тогда как остальные шорткаты в меню записаны символами (`⌘`, `⇧`) — смешанная нотация (`AltF1`, `ShiftF4`, `⇧F8`, `Ctrl+Q`, `⌃⌘F`).
5. **MENU_PARITY.md: «Folder shortcuts provide ten slots»** — в коде слоты 1–9 (`Shortcuts[10]`, индекс 0 не используется; диалог `Slot 1–9`).
6. **MENU_PARITY.md: «Options → General: … dialogs, confirmations»** и перечень Advanced «colors, Info panel, tree depth, descriptions, masks, sorting and encodings» — соответствует; но отдельные диалоги `Dialog settings`, `Confirmations`, `System/Interface/Menu/AutoComplete/Command line settings` существуют в коде и недостижимы из меню.
7. **MENU_PARITY.md: «Settings are stored in `~/Library/Application Support/FarshMac/setup.json`»** — верно; про `connections.json` и fallback на `Far Go/` документ не упоминает (описано только в коде/тестах).
8. **Справка F1 первая строка: «persistent zsh terminal»** и абзац «zsh keeps its environment… Your .zshrc is loaded» — shell по умолчанию в коде `bash` (`defaultSettings`, `normalizeSettings`), README говорит «bash, sh, or zsh».
9. **Справка F1: «Cross-volume move is refused»** — MENU_PARITY.md утверждает «Cross-volume moves copy before removing the source»; за код отвечает раздел файловых операций, здесь фиксируется противоречие документов.
10. **Справка F1: «F5 / F6 Copy / move or rename (no overwriting)»** — при этом существует `Confirm overwrite` и кнопки `Overwrite / All / Skip…` (MENU_PARITY.md, тесты). Формулировка справки устарела.
11. **Справка F1: «Copies preserve permissions, timestamps and links, not ACLs/xattrs»** — диалог копирования предлагает `Preserve permissions (mode, owner, group, ACL)` и `Preserve attributes (xattrs, flags, timestamps)`.
12. **Справка F1 упоминает `File links view`** (`Names, sizes and hard-link counts`), но вид `File links` удалён из меню и нормализуется в `Full` (`configurePanel`, MENU_PARITY.md).
13. **README «Ctrl+Shift+1…9 / Ctrl+1…9 Save / open folder bookmarks»** — верно; меню `Folder shortcuts…` шортката не показывает.
14. **Справка: «Cmd+1 … Cmd+9 Alternatives to F1 … F9»** — верно (`function(n)` при `Meta`+цифра); `⌘9` открывает меню (также закрывает его в `menuKey`).
15. **`Full screen ⌃⌘F`** в меню Commands дублирует нативный пункт Window → Enter Full Screen; в справке F1 не упомянут.
16. **Нотация `AltF1`/`AltF2`, `ShiftF4`** в меню против `Alt+F1 / Alt+F2` в справке — только косметика.
17. **Тест `TestConnectionProfilesPersistWithoutSaveSetup` ожидает 3 пункта** в `Network connections` при одном профиле — соответствует коду (`New…`, профиль, `Disconnect active panel`).
18. **`RestoreFolders`** в `Settings` нигде не читается и не редактируется (мёртвое поле; попадает в setup.json).
19. **`Describe files…`, `Edit sort groups…`** — в MENU_PARITY.md «Edit sort groups» описан в Commands; в коде пункт ведёт в ту же prompt-форму, что Advanced → `Sort groups` (дублирование).


<a id="сводка-расхождений"></a>
# Сводка расхождений

> **Закрыто.** Все расхождения ниже разрешены на ветке `backlog-fixes` (см. «Решения по расхождениям» в `docs/IMPLEMENTATION_BACKLOG.md`). Раздел сохранён как история.

Списки расхождений между кодом и README / справкой F1 / parity-документами приведены в конце каждой части:

- Часть 1. Файловые панели и навигация: 13 пунктов.
- Часть 2. Файловые операции, архивы, запуск программ: 13 пунктов.
- Часть 3. Внутренний просмотрщик и редактор: 19 пунктов.
- Часть 4. Терминал, командная строка, окна, нативная оболочка, renderer: 13 пунктов.
- Часть 5. Меню F9, диалоги, настройки, сетевые подключения, справка: 19 пунктов.

Самые важные для пользователя (встречаются в нескольких частях):

- Справка F1 называет терминал «persistent zsh» и обещает загрузку `.zshrc`; shell по умолчанию — bash, выбор в настройках.
- Справка F1 утверждает «Cross-volume move is refused» и «not ACLs/xattrs»; код перемещает между томами через копирование с удалением и сохраняет ACL и xattr.
- Ряд работающих клавиш не задокументирован: Alt+F3/Alt+F4, Cmd+Shift+F3/F4, Ctrl+J, Ctrl+[ ], Ctrl+Shift+Enter, Shift+F11, Alt+F6, Alt+F11/F12, Cmd+Shift+S.
- Ряд пунктов меню показан без шортката, хотя шорткат есть в коде: Save setup (Shift+F9), Create link (Alt+F6), архивные команды (Shift+F1–F3), истории (Alt+F11/F12).
- Семь диалогов из `settingsMenu` (Dialog, Confirmations, System, Interface, Menu, AutoComplete, Command line settings) недостижимы из меню.
- Cmd+=/−/0 и Cmd+Shift+? в коде отсутствуют: размер шрифта меняется только через настройки, история вывода открывается через меню Window или колесо.
- Вероятный дефект: закладки документа Cmd+Shift+0..9 разбирают `m.Key` через `Atoi`, тогда как renderer при Shift шлёт `!@#…`; панельные закладки используют `m.Code`.
- Ctrl+PgDn считает `.gz/.bz2/.xz` архивами по списку `isArchiveName`, но `ArchiveFormat` их не открывает.


<a id="приложения"></a>
# Приложения. Дополнения по результатам gap analysis

Варианты поведения, которые код обрабатывает, но основной текст частей не описывал. Таблицы сгруппированы по частям; колонка «Код» указывает, где проверить. Случаи, которые код не обрабатывает, вынесены в `docs/IMPLEMENTATION_BACKLOG.md`.

## Приложение 1. Файловые панели и навигация

Код обрабатывает вариант, спецификация его не описывает.

| Функция | Вариант | Что делает код | Код (файл:функция) |
|---|---|---|---|
| Panel.Load | Esc во время асинхронного чтения | После отмены панель откатывается к предыдущему состоянию, в футере остаётся `operation cancelled` (текст `fsops.ErrCancelled`) до следующей загрузки; «Stopping folder scan…» показывается только до прихода результата | `internal/ui/panel_load.go:pollPanelLoad`, `internal/fsops/progress.go:Check` |
| Panel.Load | Ошибка самой первой загрузки (стартовая папка `-left`/`-right` недоступна) | `job.previous.Path == ""` → отката нет: панель остаётся на недоступном пути с единственной записью `..` и текстом ошибки в футере | `internal/ui/panel_load.go:pollPanelLoad` |
| Panel.Load | Нет прав на чтение (EACCES/EPERM) | `readDirectory` идёт через `o.Attempt("read folder", …)`; без обработчика вопросов ошибка возвращается как есть: `open <path>: permission denied` в футере, курсор остаётся в прежней папке | `internal/fsops/walk.go:readDirectory` |
| Panel.Load | Ошибка `e.Info()` для отдельной записи (файл исчез между ReadDir и stat) | Запись молча пропускается | `internal/ui/app.go:Load` (`if info, err := e.Info(); err != nil { return nil }`) |
| Panel.Load | Esc при непустой командной строке во время чтения | `cancelReads()` возвращает true → командная строка не очищается, статус не сбрасывается | `internal/ui/app.go:key` (`case "Escape"`) |
| Сессия | Папки из session.json не существуют / не читаются | `accessibleFolder` поднимается к ближайшему читаемому родителю; затем fallback на папку первого окна, домашний каталог, `/` | `internal/ui/session.go:accessibleFolder` |
| Save setup | Битый setup.json | `LoadSetup` молча возвращается при ошибке `json.Unmarshal`; используются значения по умолчанию, пользователю ничего не сообщается | `internal/ui/settings.go:LoadSetup` |
| Save setup | Запись не удалась (нет прав, диск полон) | `a.Status = "Setup saved"` затем `a.error(err)` переопределяет статус текстом ошибки; значения в памяти сохраняются | `internal/ui/settings.go:saveSetup` |
| Закладки | saveSetup не удался при Ctrl+Shift+N | Слот всё равно заполнен в памяти; статус показывает ошибку записи, а не `Folder shortcut N saved` | `internal/ui/folder_shortcuts.go:saveFolderShortcut` |
| Закладки | Ctrl+Shift+N при раскладке, где Shift+цифра даёт символ (`!`, `@`) | Номер берётся из `m.Code` (`Digit1`…`Digit9`) | `internal/ui/folder_shortcuts.go:folderShortcutKey` |
| Закладки | Удалённая закладка, когда какая-либо панель уже подключена к тому же серверу | Переподключается **активная** панель с живой конфигурацией другой панели (включая пароль в памяти) | `internal/ui/folder_shortcuts.go:goFolderShortcut` |
| Folder history | История пуста | `choose` выводит статус `Folder history: empty` (общий механизм `<title>: empty`) | `internal/ui/form.go:choose` |
| F3 на папке | Курсор на `..` без пометок | Считается размер **текущей** папки (`name == ".." → root = base`), результат показывается в строке `..` вместо `<DIR>`; спецификация ошибочно пишет «не `..`» | `internal/ui/folder_sizes.go:calculateFolderSizes, measureFolders` |
| F3 на папке | Панель сменила путь/источник до конца подсчёта | Результат молча отбрасывается, статус остаётся пустым | `internal/ui/folder_sizes.go:calculateFolderSizes` (apply: `if p.Path != base … return`) |
| F3 на папке | Tree panel активна | Имена узлов — относительные пути; `filepath.Join(base, "a/b")` считается корректно; пометки в дереве учитываются | `internal/ui/folder_sizes.go:calculateFolderSizes` |
| Human-readable sizes | Переключение ⌘Shift+S | Футер панели пересчитывается **сразу** (`drawPanel` берёт `a.Settings.HumanReadableSizes`); устаревает только `InfoText`, который нигде не отображается. Спецификация утверждает обратное | `internal/ui/panel_draw.go:drawPanel` |
| Quick view | Курсор источника на `..` | Показывается листинг **родительской** папки (`Current()` = `Join(Path, "..")`) | `internal/ui/commands.go:updatePreview` |
| Quick view | Битая/циклическая символическая ссылка | `os.Stat` следует ссылке → текст ошибки (`too many levels of symbolic links` и т.п.); ссылка на папку → листинг папки | `internal/ui/commands.go:updatePreview` |
| Quick view | Helper-процесс отсутствует/упал/превысил 4 с | `preview.Run` возвращает ошибку → `Preview unavailable (unsupported, damaged or limit exceeded)` | `internal/preview/preview.go:Run` |
| Quick view | Esc, пока превью грузится | `cancelReads` отменяет превью и пишет `Preview cancelled`; команда/статус не очищаются; текст остаётся до смены курсора | `internal/ui/panel_load.go:cancelReads`, `commands.go:updatePreview` |
| Quick view | Содержимое с управляющими символами/бинарное | `Grid.Text` заменяет control-символы пробелом; `\r` тоже становится пробелом; одна длинная строка обрезается по ширине | `internal/ui/grid.go:Text` |
| Quick view / Info | Ctrl+Q/Ctrl+L, когда режим восстановлен из setup.json и `viewReturns` пуст | Возврат к нулевой паре `{View:"", Hidden:false}`; пустой View трактуется как Full | `internal/ui/far_keys.go:toggleOtherView` |
| Info panel | Источник — удалённая панель | `sysinfo.Read` вызывается для `RemoteLocalPath` (локальная папка терминала); секция `Filesystem panel` показывает `Label()+Path` | `internal/ui/info_panel.go:updateInfoPanels` |
| Info panel | Папка источника исчезла | `statfs` падает молча: секция `Disk  ()` с пустыми `Mount`/`Filesystem`, нулевыми размерами | `internal/sysinfo/info_darwin.go:Read` |
| Info panel | Файла описания нет | `infoDescriptionPaths` = первый кандидат; F4 создаёт его, F3 даёт ошибку открытия в статус | `internal/ui/info_panel.go:readInfoPanel, infoPanelKey` |
| Info panel | Описание > 4096 байт / обрезанная UTF-8 последовательность | Читается `LimitReader(4096)`; недопустимые байты рисуются как U+FFFD | `internal/ui/info_panel.go:readInfoPanel`, `grid.go:Text` |
| Tree panel | Корень недоступен/не папка/не существует | `result.err` → футер с текстом ошибки, список только `..`, путь панели = недоступный (отката нет, см. P1-08) | `internal/ui/tree.go:scanTree, pollTree` |
| Tree panel | Пометки | Футер в режиме дерева **никогда** не показывает `N selected`/`[filter]`: поле `Err` занято строкой статуса дерева | `internal/ui/tree.go:pollTree`, `panel_draw.go:drawPanel` |
| Tree panel | matchOtherPanel из дерева | Другая панель грузит корень дерева; курсор не совпадает (имя узла — относительный путь `a/b`) | `internal/ui/archive_panel.go:matchOtherPanel` |
| Enter | Файл/папка исчезли | `os.Stat` → ошибка в статус, курсор не двигается, панель не перечитывается | `internal/ui/launch.go:enterFile` |
| Enter | Исполняемый файл, shell занят | `execute()` → `Shell is busy; ⌘O to return to it` и терминал показывается | `internal/ui/app.go:execute` |
| Enter | Символическая ссылка на папку | Загружается путь ссылки (не цель); `..` возвращает в папку ссылки | `internal/ui/launch.go:enterFile`, `app.go:beginLoad` |
| Ctrl+PgDn | Курсор на `..` | `Dir == true` → `enterFile` → переход в родителя | `internal/ui/far_keys.go:farPanelKey` |
| Ctrl+PgUp | В `/` или в корне remote | `filepath.Dir("/") == "/"` → перечитывание той же папки | `internal/ui/far_keys.go:farPanelKey` |
| Cmd+L | Удалённая панель, ввод `~` или относительного пути | `expand` подставляет **локальный** домашний каталог / относительно удалённого пути; результат идёт на сервер | `internal/ui/app.go:gotoPrompt, expand` |
| Cmd+L | Архивная панель | Путь внутри архива → каталог архива; вне → выход из архива в локальную папку | `internal/ui/app.go:Load` |
| Remote | Каждая навигация | На каждый `Load` открывается новое соединение (`Remote.Dial`) и закрывается после чтения | `internal/ui/remote.go:loadRemote` |
| Remote | Сервер вернул небезопасное имя / > 100000 записей | Вся загрузка завершается ошибкой `server returned an unsafe filename` / `remote folder exceeds 100000 entries`, панель откатывается | `internal/ui/remote.go:loadRemote` |
| Remote | Разрыв соединения при навигации | Асинхронный откат к предыдущему листингу, ошибка в футере; ⌘R повторяет попытку | `internal/ui/panel_load.go:pollPanelLoad` |
| Архив | Файл архива изменён/удалён во время просмотра | `ArchiveIndex.Check` → `archive changed on disk; leave and reopen it` при следующей навигации/F3 | `internal/fsops/archive_index.go:Check` |
| Архив | Файл с архивным расширением повреждён | Задача `Opening archive` завершается ошибкой в статусе (без диалога: `reportError` false), панель не меняется | `internal/ui/task.go:pollTask`, `archive_panel.go:openArchiveAt` |
| Архив | Расширение в верхнем регистре (`.ZIP`) | `ArchiveFormat` приводит к нижнему регистру | `internal/fsops/formats.go:ArchiveFormat` |
| Архив | Открытие в панели в режиме Tree/Info/Quick view | Режим принудительно `Full` | `internal/ui/archive_panel.go:openArchiveAt` |
| Alt+F1/F2 | `/Volumes` не читается | Ошибка `os.ReadDir` игнорируется: список содержит только `/` и домашний каталог | `internal/ui/panel_options.go:volumes` |
| Мышь | Клик по Quick view | `panelItem` возвращает false → панель **не** активируется (в отличие от Info panel) | `internal/ui/mouse.go:panelItem`, `info_panel.go:infoPanelClick` |
| Мышь | Shift+клик | Всегда помечает диапазон (`markRange(…, true)`), снять пометку Shift+кликом нельзя | `internal/ui/mouse.go:panelMouse` |
| Мышь | Колесо над Form/Menu | Перемещает фокус/выбор на `delta` | `internal/ui/mouse.go:scrollMouse` |
| Статусная строка | Длинное сообщение | Обрезается справа по `Cols` без переноса; длинные пути в ошибках недоступны целиком | `internal/ui/draw.go:Draw` |
| Имена | Длиннее колонки / CJK / комбинирующие знаки | Обрезка без `…`; широкие руны занимают 2 ячейки (`runewidth`); руны нулевой ширины приклеиваются к предыдущей ячейке | `internal/ui/grid.go:Text` |
| Имена | Управляющие символы (`\n`) | В файловых панелях заменяются пробелом; в Tree panel экранируются (`\n`, `\uXXXX`) — разное поведение режимов | `internal/ui/grid.go:Text`, `tree.go:treeDisplayName` |
| Геометрия | Ширина колонки < 12 (узкое окно + `panelSplit` 20%) | Колонка метаданных не рисуется, только имена; Brief при w=12 даёт 1–2 символа имени | `internal/ui/panel_draw.go:drawPanel` |
| Заголовок окна | Удалённая панель | `Update.Title` = удалённый путь без имени сервера | `internal/ui/app.go:Poll` |
| Выделение | Quick view/Info/Tree активна, нажаты `+`/`-`/`*` (в т.ч. Numpad) | `selectionKey` возвращает false → символ добавляется в командную строку | `internal/ui/selection_keys.go:selectionKey`, `app.go:key` |
| Сортировка | Битая маска в `SortGroups` (из setup.json) | `matchesMask` игнорирует ошибку `filepath.Match` → группа никогда не совпадает, сообщения нет | `internal/ui/panel_options.go:matchesMask, sortEntries` |
| Панели скрыты | ⌘F1 при уже скрытой правой | Обе панели скрыты, `Active` не меняется; клавиши продолжают действовать на невидимую панель (см. P1-01) | `internal/ui/archive_panel.go:togglePanel` |
| Panel settings | Форма `Show hidden files` | Тоже сбрасывает `ForcedVisiblePath` обеих панелей | `internal/ui/settings.go:settingsMenu("Panel settings")` |
| Несколько окон | Cmd+N | Новое окно получает JSON-копию `Settings` и `panelOptions` родителя; `LoadSetup` вызывается только для окна без родителя | `internal/ui/windows.go:NewWindow` |
| Закрытие окна/Cmd+Q во время чтения | — | `Close()` отменяет загрузки панелей, дерево, превью, операцию; подтверждения нет (чтение панели не считается `Busy`) | `internal/ui/app.go:Close, RequestQuit` |
| Busy | Клавиши во время фоновой задачи | Принимаются только Esc / Alt+C (отмена) и клавиши формы вопросов; клик левой по `Cancel` прогресса | `internal/ui/app.go:key, click` |
| Сессия | Снимок во время загрузки/в архиве | Курсор берётся из ожидающего состояния; для архива сохраняется `Cursor = Base(Archive.Path)`, `Top = ""` | `internal/ui/session.go:windowSnapshot` |
| Fast find | Пустая панель | Первый символ принимается, окно `Find` появляется, курсор не меняется | `internal/ui/fastfind.go:fastFindText, findPrefix` |
| ⌘C (copyPanelCommand) | Курсор на `..`, пометок нет | Копируется строка `..`; пустая панель — буфер не меняется (в отличие от Ctrl+Insert, который копирует путь панели) | `internal/ui/mouse.go:copyPanelCommand` |

## Приложение 2. Файловые операции, архивы, запуск программ

| Функция | Вариант | Что делает код | Код (файл:функция) |
|---|---|---|---|
| F5 Copy | Диск полон (ENOSPC), файл исчез, нет прав на чтение — на каждом файле | Для local→local каждый файл идёт через отдельный `fsops.Transfer` → `Attempt("copy")` → диалог `Error` (`Cannot copy\n<src>\nTo: <dst>\n\n<err>`) Retry/Skip/Skip all/Cancel; временный `.farshmac-copy-*` удаляется `defer os.Remove`; при Skip остальные файлы продолжаются | `remotefs/transfer.go:Copy` (worker loop), `fsops/files.go:copyRegularStream` |
| F5 Copy | `fclonefileat` вернул ошибку, не входящую в список ENOTSUP/EXDEV/EINVAL/ENOSYS/EPERM (например ENOSPC, EACCES) | Ошибка возвращается как есть без попытки потокового копирования → диалог `Error` | `fsops/files.go:copyEntry` |
| F5 Copy | Источник исчез между обходом и копированием (`read attributes`) | `Attempt("read attributes")` → Skip пропускает элемент, обход продолжается (`IgnoreSkipped`) | `remotefs/transfer.go:Copy.walk` |
| F5 Copy | Ошибка в одном воркере при `workers > 1` | После ответа `Cancel` первый error через `fail()` отменяет `ctx` всех воркеров; остальные воркеры завершают текущий файл (temp удаляется) и выходят; в статусе — первая ошибка с суффиксом ` (copy <src> to <dst>)` | `remotefs/transfer.go:Copy.fail` |
| F5 Copy | Файл на себя (оба панели в одной папке, предложенный путь) | `remotefs.Copy` для одинакового endpoint сравнивает `path.Clean` и возвращает `destination is inside source` даже для файла (локальный `fsops.transfer` вернул бы `source and destination are the same file`) | `remotefs/transfer.go:Copy` (цикл по `pairs`) |
| F5 Copy | Жёсткие ссылки внутри копируемого дерева (не move) | Копируются как независимые файлы (`copyHardlink` работает только при `o.moving`) | `fsops/hardlinks.go:copyHardlink` |
| F5 Copy | remote → local, флажок Permissions выключен | Файл создаётся `O_EXCL 0666` минус umask; `Chmod` не вызывается; локальный ACL родителя применяется | `remotefs/local.go:Local.Create`, `transfer.go:portableMetadata` |
| F5 Copy | Прогресс: проценты/ETA | `o.Streaming()` до конца обхода, `o.Ready()` сразу после `close(queue)` — для большого дерева проценты появляются, когда обход закончен, а файлы ещё копируются; для одного большого файла — почти сразу | `remotefs/transfer.go:Copy` |
| F5 из архива | Прогресс | Счётчики извлечения (`o.Add/Complete` по именам членов) и последующего копирования складываются → `N items` в окне примерно вдвое больше реального | `fsops/formats.go:extractArchive`, `transfers.go:transferDialog` |
| F5 Copy | Enter на архиве в **удалённой** панели | Не открывает архивную панель: файл скачивается во временный каталог и открывается внутренним viewer как бинарный документ | `launch.go:enterFile` → `remote_document.go:openRemoteDocument` |
| F6 Move | Каталог перемещается на себя | Сообщение `destination is inside source` (а не «same file») — проверка `rel == "."` срабатывает раньше `SameFile` | `fsops/transfer.go:transfer` |
| F6 Move | Esc при слиянии каталога (same volume) | Уже перемещённые элементы остаются в назначении, источник сохраняет остальные; `delete source` не выполняется | `fsops/transfer.go:transfer` (ветка `si.IsDir() && di.IsDir()`) |
| F6 Move | Esc на стадии копирования cross-volume | Незавершённый каталог назначения удаляется целиком (`RemoveAll(dst)` в defer `copyEntry`), источник не тронут | `fsops/files.go:copyEntry` |
| F6 Move | Esc на стадии `delete source` cross-volume | Копия в назначении полная, источник удалён частично (прерванный `Remove`); сообщения об этом нет, только `operation cancelled` | `fsops/transfer.go:moveAcrossVolumes` |
| F6 Move | Источник — симлинк | Переименовывается сама ссылка; симлинк → существующий каталог даёт диалог коллизии (`Existing: Folder`) и после `Overwrite` ошибку `cannot replace a file with a directory or a directory with a file` | `fsops/transfer.go:transfer` |
| Коллизия | Esc/пустое имя в форме `Rename` | Отменяет **всю** операцию (`Resolution{Cancel}`), а не возвращает к диалогу коллизии | `operation_questions.go:collisionQuestions` |
| Коллизия | Overwrite при изменившемся назначении | `destination changed during copy/move/append` → диалог `Error`; Retry запускает `transfer` заново → новый диалог коллизии с актуальными данными | `fsops/transfer.go`, `append.go` |
| F7 | Имя `.`/`..`/`~`, абсолютный путь | `expand` → `MkdirAll` на существующий каталог не ошибка; панель переходит к родителю этого каталога и ставит курсор; имя, совпадающее с файлом → `Cannot create folder … not a directory` Retry/Cancel | `app.go:function case 7` |
| F8/Shift+F8 | Симлинк на каталог | Удаляется/отправляется в Trash сама ссылка (`Lstat`, без обхода) | `fsops/progress.go:removeTreeStep`, `trash_darwin.go` |
| Shift+F8 | Файл с флагом `uchg`, файл на read-only томе | `os.Remove` → EPERM/EROFS → диалог `Error` Retry/Skip; обхода флагов нет (политика) | `fsops/progress.go:removeTreeStep` |
| Shift+F8 | Esc в середине | Уже удалённые элементы не восстанавливаются; родительский каталог остаётся | `fsops/progress.go:removeTreeStep` |
| Remote delete | Сервер вернул небезопасное имя в подкаталоге | `unsafe server filename` прерывает всю операцию (не Skip) | `remotefs/transfer.go:removeTreeStep` |
| Общее | `Cmd+Q` / закрытие окна во время операции (в т. ч. из другого окна) | `WindowGroup.advanceQuit`: `RequestQuit` ставит статус `Wait for the file operation to finish`, групповой quit прерывается; другие окна получают `Finish the quit dialog in the other window`; реальное `Close()` отменяет операцию и дожидается `cleanup` в goroutine | `windows.go:advanceQuit`, `app.go:Close` |
| Общее | Вопрос воркера после отмены | `operationChoice` проверяет `o.Check()` внутри queued closure — диалог не открывается, если операция уже отменена | `operation_questions.go:operationChoice` |
| Shift+Enter / Enter | Esc во время `Opening <name>` | `exec.CommandContext` убивает `/usr/bin/open`, но уже запущенное приложение не закрывается | `launch.go:launch` |
| Enter | Ассоциация с несуществующей программой; внешний редактор завершился с ошибкой | Команда уходит в терминал; «command not found»/код возврата виден только в терминале, диалога нет | `launch.go:launch` (`plan.Command`) |
| Enter | Файл без расширения и без exec-бита | `/usr/bin/open` → при отсутствии приложения `open` возвращает код 1 → диалог `Opening <name> failed` | `launch.go:enterFile` |
| Shift+F1 | Расширение в верхнем регистре (`.ZIP`) | `ArchiveFormat` приводит к нижнему регистру — создаётся zip | `fsops/formats.go:ArchiveFormat` |
| Shift+F1 | Отмена | Временный `.farshmac-zip-*`/`.farshmac-archive-*` удаляется, bsdtar убивается `SIGKILL` по группе | `fsops/formats.go:CreateArchive`, `archive.go:CreateZIP` |
| Shift+F2 | Назначение — несуществующий родитель | `MkdirTemp` ошибка → `Error` Retry/Skip (Skip → `Done — 1 item(s) skipped`) | `fsops/formats.go:extractArchive` |
| Archive F3 | ZIP с нестандартным методом сжатия (bzip2/lzma) | `withArchiveSelection` падает обратно на полный проход bsdtar | `fsops/archive_zip_selection.go` |
| Apply command | Пустая/пробельная команда | Ничего не выполняется, диалог закрывается | `commands.go:applyCommand` |

## Приложение 3. Внутренний просмотрщик и редактор

| Функция | Вариант | Что делает код | Код (файл:функция) |
|---|---|---|---|
| F3/F4 открытие | Файл ровно 64 MiB / 64 MiB + 1 | `info.Size() > maxTextBytes` — ровно 64 MiB читается в память (`LimitReader(maxTextBytes+1)`, прочитано = лимиту — ОК); на 1 байт больше — постраничный hex | `document.go: openDocument` |
| F3/F4 открытие | FIFO, сокет, устройство, папка через `Alt+F11` | `!info.Mode().IsRegular()` → `Choose a regular file`; битая символическая ссылка → `lstat …: no such file or directory` из `EvalSymlinks` | `document.go: openDocument` |
| F3/F4 открытие | Файл 0 байт | `Detect` → `UTF-8` (пустой срез валиден), буфер из одной пустой строки; hex/dump рисуют пусто, заголовок `Offset 0 / 0`; Go to `0` допустим (`v >= max(1, Size)`), End = 0 | `document.go: openDocument, gotoDocument`; `document_keys.go: viewerKey` |
| F3/F4 открытие | UTF-16 без BOM | NUL валиден для `utf8.Valid` → `Codepage = UTF-8`, `binary = true` → просмотрщик в hex, редактор `Binary data: open with F3 in hex/dump mode`. Текст виден только через Shift+F8 → `UTF-16LE/BE` | `textfile/encoding.go: Detect`; `document.go: openDocument` |
| F3/F4 открытие | Невалидный UTF-8 (один «битый» байт в UTF-8 файле) | `Detect` → `Windows-1251` всегда; charmap-декодер не ошибается (неопределённые байты → U+FFFD), файл показывается как кириллическая «каша»; для F4 roundtrip не совпадает → `Encoding is not lossless; …` | `textfile/encoding.go: Detect, Decode`; `document.go: openDocument` |
| Сохранение | Смешанные EOL после правки | Байт-точное сохранение (`SavedRaw`) только если ничего не менялось; после любой правки `Endings(Text, EOL)` унифицирует все окончания в определённый при открытии EOL (CRLF, если был хоть один CRLF) | `document.go: saveDocument`; `textfile/encoding.go: Normalize, Endings, DetectEOL` |
| Сохранение | Файл без завершающего `\n` | Сохраняется как есть (Normalize ничего не добавляет); `Lines()` даёт последнюю строку без `\n` | `textfile/buffer.go: New, Lines` |
| Сохранение | Файл стал 0444 после открытия (папка записываема) | `fsops.Save` сравнивает только inode/размер/mtime, не mode → временный файл + `chmod 0444` + `rename` — **сохранение проходит**, права сохраняются. Файл другого владельца в каталоге со sticky-bit → `rename …: operation not permitted` | `fsops/files.go: Save` |
| Сохранение | Диск полон | `copyProgress` → `write …: no space left on device`, временный файл удалён `defer os.Remove`, целевой файл не тронут, буфер/Dirty сохранены | `fsops/files.go: Save`; `document_tasks.go: saveDocumentThen` |
| Сохранение | Файл с `uchg` / quarantine / hardlink / xattr | `uchg` → `rename …: operation not permitted` (буфер цел); quarantine-xattr **теряется** (новый inode), т.е. сохранение снимает карантин; hardlink разрывается; xattr/ACL теряются (см. B P3-10) | `fsops/files.go: Save` |
| Сохранение (Async) | Esc после фактического `rename`, но до применения | `applyCompleted = true` → apply всё равно выполняется (`Saved`, `markSaved`), но `done` (закрытие/выход) пропускается при `o.Check() != nil` | `document_tasks.go: saveDocumentThen`; `task.go: pollTask`; тест `cancel_test.go: TestAsyncDocumentReadReplaceSaveAndCancel` |
| Сохранение | Paste/selectAll во время фоновой задачи | `Handle` отбрасывает все сообщения кроме key/click/resize/quit/input при `Busy`; клавиши при `Busy` — только Esc/Alt+C; клики — `click` при `Busy` → return. Поэтому гонки «правка во время `*d = *copy`» нет | `app.go: Handle (строка 546), key (632), click (1136)` |
| Save as | Активная панель удалённая | `base = p.filesystemPath()` = `RemoteLocalPath` (последний локальный путь панели); сохранение на сервер через Save as невозможно (подпись `Local file name`) | `document.go: saveAs`; `archive_panel.go: filesystemPath` |
| Save as | Тот же файл под другим написанием (относительный путь, `..`) | `path != d.Path` → `Lstat` → confirm `Overwrite` → `fsops.Save` с `SameFile` → проходит; `d.Path` становится новым написанием | `document.go: saveAs, saveDocument` |
| Shift+F4 | Введён путь существующей папки | `os.Stat` OK → `openDocument(path, true)` → `Choose a regular file` | `document.go: newFile, openDocument` |
| Shift+F4 | Новый файл, папка без прав на запись | Ошибка только при сохранении (`fsops.Create` → `open <dir>/.farshmac-new-…: permission denied`) | `document.go: newFile`; `fsops/files.go: Create` |
| Удалённый документ | Обрыв соединения при скачивании | Ошибка задачи → статус; `cleanup` удаляет временный каталог; документ не открывается | `remote_document.go: openRemoteDocument` |
| Удалённый документ | Обрыв/ошибка сервера при сохранении | `Dial`/`Lstat`/`remotefs.Copy` → ошибка → статус; клон отбрасывается, буфер и Dirty целы, F2 повторяет. Загрузка идёт в `.farshmac-part-<random>` + `Rename` — на сервере замена атомарна; `ExpectedDestination` повторно сверяет цель | `remote_document.go: saveRemoteDocument`; `remotefs/transfer.go: copyFile` |
| Удалённый документ | Права на запись на сервере отсутствуют | Предварительной проверки нет; ошибка приходит от сервера при `Create(tmp)`/`Rename` (`550 …`, `permission denied`) и уходит в усечённый статус | `remote_document.go: saveRemoteDocument` |
| Alt+F11 история | Удалённые и архивные документы | В историю попадают только локальные открытия (`openDocument`/`openDocumentAsync`); `openRemoteDocument`/`viewArchiveMember` имеют собственный apply без записи | `document_tasks.go: openDocumentAsync`; `remote_document.go`; `archive_panel.go` |
| Закрытие окна | Красная кнопка окна / Cmd+W с несохранёнными документами | `windowShouldClose` → `{"Type":"close"}` → `WindowGroup.Handle` → `RequestQuit()` → тот же диалог `Save changes` / `Save all`, `Discard`, `Cancel`; `App.Close` закрывает `File`, удаляет `TempDir` всех документов | `window_darwin.m:150`; `windows.go:150`; `app.go: Close` |
| Cmd+Q | `Save all` завершилось ошибкой | `saveAllThen` прерывает цепочку, выход отменяется, причина — только усечённый статус | `document_tasks.go: saveAllThen`; `app.go: RequestQuit` |
| Ввод текста | Вставка больших блоков | Верхнего предела размера буфера в редакторе нет (можно вырастить > 64 MiB и сохранить); правка > 32 MiB очищает весь undo и сама не откатывается; > 1 MiB отключает подсветку | `textfile/buffer.go: Replace`; `document_syntax.go: pollSyntax` |
| Ввод текста | Ввод «за концом строки» | Виртуального пространства нет: `At(row, col)` зажимает колонку длиной строки; в Overwrite в конце строки символы вставляются (не затирают `\n`) | `textfile/buffer.go: At, Insert` |
| Undo | Undo после сохранения | Стек не очищается при `markSaved`; undo делает документ Dirty, а возврат к тексту, равному `SavedText`, снимает Dirty (`modified()`) | `textfile/buffer.go: Undo`; `document.go: documentKey ("z")` |
| Закладки / Cmd+Z позиции | После удаления/вставки строк | Смещения не корректируются; `restore` зажимает только `Cursor` по `len(Text)`; `Offset` зажимается при отрисовке, `ViewStart` — в `visualRows`. Закладка указывает «куда-то», но не падает | `document.go: restore`; `document_draw.go: drawDoc (157), visualRows` |
| Поиск | Regex с пустым совпадением (`^`, `a*`) | `Matches` возвращает нулевые совпадения; `Find` при пустом выделении стартует с `Next(start)` → Shift+F7 продвигается по позициям; `Replace all` вставляет замену в каждой позиции (`Replaced N matches`) | `textfile/buffer.go: Find, ReplaceAll` |
| Поиск | Пустая строка; `Hex bytes` в dump | `enter search text`; в dump-режиме `Hex bytes` по умолчанию выключен (только `Mode == "hex"`), hex-поиск из dump переключает в hex | `textfile/buffer.go: Compile`; `document.go: searchDocument, findDocument` |
| Hex-поиск | Нечётная/не-hex строка, `0x`-префикс | `hex.DecodeString` после удаления пробелов → `Enter pairs of hex digits, e.g. 48 65 6c 6c 6f` (префикс `0x` не поддерживается — та же ошибка) | `document.go: searchDocument` |
| Кодировки | BOM при однобайтовой кодировке | `Encode` пишет BOM только для UTF-8/UTF-16; F9 `Write BOM` для `Windows-1251` и т.п. молча не действует; при смене UTF-8+BOM → 1251 BOM теряется без сообщения | `textfile/encoding.go: Encode` |
| Кодировки | Сохранение в UTF-16 без BOM | Файл при следующем открытии определится как UTF-8 с NUL → «бинарный», hex-режим | `textfile/encoding.go: Detect` |
| Кодировки | F8 в редакторе vs Shift+F8 | F8 проверяет `Encode(d.Text, …)` без `Endings`, Shift+F8 — с `Endings`; на результат не влияет (`\r` кодируется везде) | `document.go: documentKey (F8), chooseCodepage` |
| Внешние программы | `/usr/bin/open` вернул ошибку | `workProgress("Opening <name>")` → статус `<err>: <вывод open>` (например `exit status 1: Unable to …`) | `launch.go: launch` |
| Внешние программы | Команда не найдена / shell занят / нет shell | Непустая команда уходит в shell: «не найдена» видна только в терминале, приложение об этом не знает; shell занят → `Shell is busy; ⌘O to return to it` + показ терминала; `Term == nil` → `Shell unavailable`. После внешнего редактирования файл в панели не перечитывается автоматически | `launch.go: launch, submitCommand`; `app.go: execute` |
| Подсветка | Helper упал / ненулевой exit / завис | `cmd.Run` error (в т.ч. kill по таймауту 2 с через `CommandContext`) → `syntax paused (limit/error)`; повторная попытка не делается, пока текст/путь не изменятся (`attempted = true`) | `highlight/highlight.go: Run`; `document_syntax.go: pollSyntax` |
| Подсветка | Нет свободного слота (2 helper'а на все окна) | `Run` блокируется в `select` на `slots` без таймаута, пока не освободится слот или не отменится ctx (Esc/смена текста/закрытие) | `highlight/highlight.go: Run` |
| Подсветка | Язык не определён | `lexers.Match`/`Analyse` → nil → пустой `Result`, статус пустой (без `[…]`) | `highlight/highlight.go: Parse` |
| Подсветка | Смена темы во время задачи | Цвета подменяются на каждом `Draw` по всей сетке; задача подсветки не перезапускается | `draw.go: Draw` |
| UI | Узкое окно | `Cols` зажат 60..300, `Rows` 15..120; ячейки key bar `W/10` → при 60 колонках подписи усечены (`9Optio`, `8Codep`); заголовок обрезается `g.Text(…, g.W)`; статус-сообщение — до `min(32, W/3)` рун | `app.go: Handle (resize)`; `document_draw.go: drawDoc` |
| UI | Полоса прокрутки при 1 строке / клик в колонку W-1 без полосы | Полоса рисуется только при `lines > height`; но клик в `X == Cols-1` при `Scrollbar == true` всегда трактуется как drag полосы (`Offset = 0`, `ManualScroll = true`), даже если полосы нет | `document_draw.go: drawDoc, documentClick` |
| UI | Клик ниже последней строки / в hex / правой кнопкой | `row >= len(rows)` → игнор (курсор в конец не ставится); hex/dump — игнор (кроме key bar); правая кнопка при открытом документе — игнор | `document_draw.go: documentClick`; `app.go: click (1128)` |
| Help (F1) | `+`/`-`, Cmd+L, F12 в справке | `+`/`-` заменяют документ справки соседним файлом панели; Cmd+L ставит `RO` (бессмысленно, но безвредно); в `Screens` справка видна как `View Viewer / editor help` | `document_keys.go: adjacentFile`; `document.go: screenMenu` |
| Несколько документов | Тот же файл открыт дважды | Дубликаты не объединяются; у второго экрана `Info` устаревает после сохранения первого → `file changed on disk; reopen it before saving` (см. B P3-04) | `document.go: showDocument, saveDocument` |

## Приложение 4. Терминал, командная строка, окна, нативная оболочка, renderer

| Функция | Вариант | Что делает код | Код (файл:функция) |
| --- | --- | --- | --- |
| Ввод в командную строку (paste) | Вставка многострочного текста | Управляющие символы (включая `\n`) заменяются пробелом ДО `ReplaceAll("\n","")`, т.е. строки склеиваются через пробел, а не удаляются переводы строк; второй шаг — мёртвый код. | `app.go:Handle` case `"paste"` |
| Ввод в командную строку (paste) | Вставка во время `Busy` | Сообщение `paste` отбрасывается молча (гейт `a.Busy && m.Type != key/click/resize/quit/input`). | `app.go:Handle` |
| История команд Ctrl+E/X | Пустая история | Ctrl+E — no-op; Ctrl+X очищает `Command` (`a.Command = ""` до проверки индекса). ⌘↑/Ctrl+↑ при пустой истории ничего не меняют. | `far_keys.go` case `"e","x"`; `app.go:key` |
| История команд | Изменение `HistoryLimit` в настройках | Существующая история не усекается до следующего `appendUnique` (при следующем выполнении команды). | `settings.go` Terminal settings; `commands.go:appendUnique` |
| История команд | Ошибка записи setup.json (диск/права) | `saveSetup` выставляет `Setup saved`, затем `a.error(err)` перезаписывает статус текстом ошибки; история остаётся только в памяти. | `settings.go:saveSetup` |
| Автодополнение ⌘Space | Очень много совпадений | Ограничения нет: форма-список `Complete filename` получает все совпадения (тысячи записей в большом каталоге). | `completion.go:applyCompletion`, `form.go:choose` |
| Вставка имени ⌘Enter | Курсор на `..` | Вставляется литерал `.. ` (имя проходит фильтр `[A-Za-z0-9_./-]`, без кавычек). | `app.go:insertFilename` |
| Выполнение команды | Команда длиннее 1 MiB | `Session.request` возвращает `invalid panel command or directory` (то же сообщение, что и для NUL/не-абсолютного cwd); команда остаётся в строке. | `session.go:request` |
| Выполнение команды | Enter в терминале при незавершённой строке (PS2: `echo "` + Enter) | `input` с `\r` ставит `ShellReady=false`; preexec/precmd не срабатывают, поэтому до завершения/Ctrl+C панельный Enter даёт `Shell is busy; ⌘O to return to it`, футер — `⌘.  Interrupt`. Ctrl+C возвращает prompt → Ready. | `app.go:Handle` case `"input"` |
| Выполнение команды | Ввод во время паузы перезапуска (`Term == nil`) | `input`/`inputBinary` молча игнорируются (нажатые клавиши теряются); статус в футере `Shell exited; restarting…`. | `app.go:Handle` |
| Протокол готовности | `$PWD` содержит `;` | `SplitN(…, ";", 2)` — cwd с `;` разбирается верно. Символ BEL (0x07) в имени каталога обрежет маркер (cwd усечётся, хвост попадёт в вывод) — не обрабатывается, но практически не встречается. | `session.go:parser.feed` |
| Протокол готовности | cwd — симлинк (`cd /tmp` → `$PWD=/tmp`, панель `/private/tmp`) | Сравнение строк `target == a.ShellCwd` даёт промах → лишний `ChangeDir` при каждом ⌘O (безвредно; PWD становится `/private/tmp`). | `app.go:syncShellCwd` |
| Протокол готовности | Shell убит извне (`kill -9`) | Чтение PTY → `EIO` → `Event{Exit:true, Err:nil}` → `Shell exited; restarting…` (как обычный `exit`). | `session.go:read` |
| Протокол готовности | Shell не читает stdin (SIGSTOP, XOFF/Ctrl+S) | Очередь `input` (32 буфера) переполняется → статус `terminal input queue is full`; writer-горутина блокируется в `f.Write` до закрытия PTY. | `session.go:Write`, `StartConfigured` writer goroutine |
| Протокол готовности | `Session.Write` после `Close` | `select` между `s.input <-` (буферизованный канал) и `<-s.done` выбирается случайно — запись может «успешно» попасть в буфер и быть потеряна; в `App` после `Close` всегда `Term=nil`, поэтому не проявляется. | `session.go:Write` |
| Старт терминала | `/bin/<shell>` не существует/не исполняемый | Путь жёстко `/bin/bash|sh|zsh`; ошибка `pty.StartWithSize` → `Shell: fork/exec …`, temp-каталог удаляется. Повтора нет (см. P4-03). | `session.go:StartConfigured`; `native_darwin.go:Run`; `windows.go:NewWindow`; `session.go:EnableSession` |
| Старт терминала | Хуки других инструментов (starship, direnv, nvm) в custom prompt | bash: пользовательский `PROMPT_COMMAND` выполняется внутри `_farshmac_before_prompt` до маркера, `$?` сохраняется в `farshmac_status` до eval; zsh: `_farshmac_precmd` добавляется в конец `precmd_functions` после `.zshrc`, т.е. выполняется после пользовательских хуков. Многострочный PS1 в bash при directoryKey печатается дважды (принятие пустой строки для пересчёта PS1). | `bourne.go`, `session.go` rc |
| Старт терминала | cwd длиннее `PATH_MAX` / недоступный для `cd` | Панель не может находиться в таком каталоге; при `Run` shell делает `builtin cd` → неудача → `Cannot enter directory: …` (zle -M) / bash: только маркер статуса 1, без сообщения в терминале. | `session.go` rc `_farshmac_panel_command`; `bourne.go` |
| Перезапуск shell | Во время `Busy` (файловая операция) или при открытом документе/диалоге | `Poll` не зависит от `Busy`/документа: Exit обрабатывается, `[Restarting shell]` дописывается, `a.Status` перезаписывается (`Shell exited; restarting…`) даже при открытой форме. | `app.go:Poll`, `shell_restart.go` |
| Перезапуск shell | Exit при открытом диалоге `Quit` / `A command is running. Close the application?` | Диалог остаётся, хотя команды уже нет; `No` → shell перезапускается, `Yes` → `Quit=true`, `restartShellIfDue` не запускает shell. | `app.go:confirmQuitShell`, `shell_restart.go` |
| Синхронизация shell → панель | Shell перешёл в каталог, недоступный панели (`x` без `r`) | `Panel.Load` падает в `fsops.ReadDirectory` до `beginLoad` → панель не меняется, ошибка в `a.Status`. | `app.go:syncCwd`, `finishPanelReturn`; `app.go:Panel.Load` |
| Scrollback/спулинг | Temp-каталог недоступен/переполнен | `NewHistory` или `Append` → `historyStatus.Error` навсегда для этого окна (футер `History: <err> · …`, `archive.enabled=false`); восстановления нет, уже записанные страницы читаются. | `terminal_history.go:appendTerminalHistory`, `history.go:Append` |
| Scrollback/спулинг | Сегмент `.rows/.idx` удалён извне | `Append` продолжает писать в открытый (unlinked) fd — байты учитываются, строки недоступны; `Page` возвращает `open …: no such file or directory` в подписи оверлея. Детекции нет. | `history.go:Append`, `Page`; `history_view.js:update` |
| Scrollback/спулинг | `History writer is overloaded` | Практически недостижимо: кадры отправляются только при `len(queue) < 2`, поэтому `len(queue) >= 8` невозможен; но состояние ошибки — постоянное. | `terminal_history.go:terminalCanDrain` |
| Клавиатура в терминале | Option+буква (dead keys, `€`, `ß`, `~` на некоторых раскладках) | `macOptionIsMeta: true` — Option всегда ESC-префикс; ввести символы через Option в терминале невозможно (только через IME/вставку). | `app.js` опции `term` |
| Клавиатура в терминале | Fn/медиа-клавиши при выключенном «Use F1, F2… as standard function keys» | Системные медиа-функции перехватывают клавишу; xterm получает F1–F12 только с зажатым Fn (README). Кода нет. | — |
| Клавиатура в терминале | Caps Lock | Go сравнивает `EqualFold`/`ToLower` для ⌘O/N/Q/E/X/Y; renderer — `e.key.toLowerCase()`; ввод символов в командную строку — как есть. | `app.go:key`, `far_keys.go`, `app.js` |
| Копирование/вставка | ⌘V при не-текстовом содержимом буфера (файл, картинка) | `stringForType:NSPasteboardTypeString` → nil → `return` без сообщения. | `window_darwin.m:pasteText:` |
| Копирование/вставка | Вставка > 1 MiB в терминал | xterm `paste` (bracketed) → один `input` → `Session.Write` → статус `paste exceeds 1 MiB`; текст не вставляется целиком (без частичной вставки). | `app.js:farshmacPaste`; `session.go:Write` |
| Мышь в терминале | Control+клик | Генерирует `contextmenu` → показывается `.clipboard-menu` (если нет mouse tracking); xterm при `rightClickSelectsWord` выделяет слово. | `app.js` `contextmenu` |
| Renderer | Крах web-процесса WebKit | `webViewWebContentProcessDidTerminate` → `reload`; shell продолжает работать, но содержимое экрана xterm и резидентный scrollback теряются (на экране пусто до следующего вывода/Ctrl+L); дисковый архив цел, `history.total` приходит из `Update`. | `window_darwin.m` |
| Новое окно | ⌘N при 10+ окнах | Лимита нет (ограничение 100 — только при восстановлении сессии). Точка каскада `(100+24*n, 850-24*n)` при n > 35 уходит за экран — Cocoa `cascadeTopLeftFromPoint` сама подгоняет. | `windows.go:NewWindow`, `window_darwin.m:open` |
| Сессия | Панель на удалённом сервере | В `session.json` пишется `filesystemPath()` = `RemoteLocalPath`; после перезапуска окно открывается на локальном пути, соединение не восстанавливается. | `session.go:windowSnapshot` |
| Сессия | Битый `session.json` | Помимо статуса `cannot read window session: …` файл перезаписывается значениями по умолчанию на первом же `Poll` (≤ 500 мс), резервной копии не делается. | `windows.go:Poll` → `SaveSession` |
| Выход ⌘Q | Отмена на любом шаге | `quitting=false`, `approved` не очищается до следующего `BeginQuit`, где создаётся заново → при повторном ⌘Q все окна спрашиваются снова (включая ранее одобренные). | `windows.go:BeginQuit`, `advanceQuit` |
| Нативное меню | Edit → Copy/Cut/Paste/Select All при диалоге/форме | Всегда enabled (`validateMenuItem` возвращает YES); действие уходит в `copyDocument`/`formPaste` и т.д. Services, Zoom в меню отсутствуют; `Minimize` — стандартный `performMiniaturize:` по цепочке respondent'ов. | `window_darwin.m:validateMenuItem`, `startWindows` |
| Full Disk Access | Выдача без перезапуска | TCC применяется к новым процессам; код доступ не проверяет и не перечитывает панели; в тексте диалога — «quit and reopen». | `disk_access_darwin.m` |

## Приложение 5. Меню F9, диалоги, настройки, сетевые подключения, справка

| Функция | Вариант | Что делает код | Код (файл:функция) |
| --- | --- | --- | --- |
| Главное меню F9 | `Shift+F10`, если ни один пункт ещё не выполнялся | `lastMenu` — нулевой `MenuState` → открывается раздел `Left`, строка 0 | `app.go:key` (`F10 && Shift`) |
| Главное меню F9 | `F12`, `Alt+F1/F2/F7/F8/F10`, `Shift+F4` при открытом меню | Открывают список/форму поверх меню; форма получает клавиши первой; после её закрытия меню остаётся открытым | `app.go:key` (проверки идут до `if a.Menu != nil`) |
| Главное меню F9 | `F1`, `⌘1…9`, печатные клавиши при открытом меню | Попадают в `menuKey` и игнорируются (нет в `switch`); справка по `F1` из меню недоступна | `menu.go:menuKey` |
| Главное меню F9 | Длинная подпись пункта | Обрезается до `w-4-len(shortcut)` колонок; шорткат рисуется в фиксированных 9 правых колонках | `menu.go:drawMenu` |
| Главное меню F9 | Клик вне рамки / по разделителю / по заголовку другого раздела | Вне рамки — закрыть; по разделителю — ничего; по заголовку в строке 0 — переключить раздел (обрабатывается в `click` до `menuClick`) | `menu.go:menuClick`, `app.go:click` |
| Главное меню F9 | `PageUp/PageDown` | Шаг `Rows-5`, курсор потом «перепрыгивает» разделитель | `menu.go:menuKey` |
| Главное меню F9 | Открытие при документе/форме/диалоге/Busy/терминале | Недостижимо: `key` маршрутизирует в `Form`/`Dialog`/`Doc`/`Busy` раньше; `click` по строке 0 блокируется `a.Doc != nil`; клик мышью при `Busy`/`Doc` отвергается | `app.go:key`, `app.go:click` |
| Form (поля) | `Enter` на toggle-поле или в любом текстовом поле | Отправляет форму (`Submit`), не переключает флажок | `form.go:formKey` |
| Form (поля) | `Esc` при изменённых полях | Закрывает без вопроса, изменения теряются | `form.go:formKey` |
| Form / Dialog | Клик мышью вне окна | Игнорируется, окно остаётся (нет закрытия по клику вне) | `form.go:formClick`, `draw.go:promptClick` |
| Form (поля) | `Alt+буква` без совпадения с мнемоникой | `App.key` нормализует `m.Key` к латинской букве по `m.Code` → в поле вставляется латинская буква; Option-символы (`ø`, `ß`, `€`) ввести в поле формы/диалога нельзя | `app.go:key` (первая строка), `form.go:formKey` |
| Form (поля) | `⌘C`/`⌘X` в текстовом поле | Копирует/вырезает **всё** значение поля в системный буфер (без учёта выделения); в Dialog — весь `Input` | `document.go:copyDocument` |
| Form (поля) | Очень длинное значение | Поле показывает окно вокруг каретки (`editWindow`); прокрутки по словам нет | `modal.go:editWindow`, `modalInput` |
| Form (поля) | Поля не помещаются по высоте | Прокрутка следует за фокусом, индикатор `▲`; разделители вне видимой области не рисуются | `form.go:formGeometry`, `drawForm` |
| Form (список) | Больше 26 пунктов / повторяющиеся первые буквы | Акселераторы назначаются первой ещё не занятой букве; когда буквы кончились — пункт без акселератора (`key == 0`) | `modal.go:mnemonicKeys` |
| Form (список) | Клик по строке | Одинарный клик сразу выбирает (`Enter`), двойной не нужен | `form.go:formClick` |
| Form (confirm) | Перенос текста | По количеству рун, не по словам и не по ширине CJK | `form.go:confirmationLayout` |
| Dialog (prompt) | Сообщение длиннее 3 строк | Обрезается молча; перенос по символам | `draw.go:drawDialog`, `promptBounds` |
| Dialog (prompt) | Вставка многострочного текста | Управляющие символы → пробел (переводы строк тоже становятся пробелами) | `app.go:Handle` (`paste`) |
| Make folder (F7) | Пустое/пробельное имя | Молча игнорируется, без сообщения | `app.go:function(7)` |
| General settings | `Font size` = `+14`, ` 14` | `strconv.Atoi` принимает `+14`, отвергает пробелы (`Font size must be 10–24`) | `settings.go:settingsMenu` |
| Terminal settings | `Shell` с пробелами; уменьшение `HistoryLimit` | `TrimSpace`; история не обрезается немедленно, только при следующем `appendUnique` | `settings.go`, `commands.go:appendUnique` |
| Editor settings | `Tab width` вне 1–16 в setup.json | `normalizeSettings` сбрасывает на 4 (не clamp, в отличие от FontSize/HistoryLimit/TreeDepth, которые clamp-ятся) | `settings.go:normalizeSettings` |
| Все настройки | Отсутствующие поля в setup.json | `Unmarshal` поверх `defaultSettings()` → отсутствующие поля получают defaults, а не нули | `settings.go:LoadSetup` |
| Все настройки | Строка вместо числа / массив >10 для `Shortcuts` | `json.Unmarshal` возвращает ошибку → **все** настройки и истории заменяются defaults, молча (см. P5-04) | `settings.go:LoadSetup` |
| Colors / Code pages | Неизвестное значение `ColorTheme`/`DefaultCodepage` в setup.json | Не нормализуется: `Draw` применяет палитру только для `Dark`/`Light` (иначе классика); `textfile.codec` возвращает `nil` для неизвестного имени | `draw.go:Draw`, `textfile/encoding.go:codec` |
| General settings | Применение `FontSize`/`ColorTheme`/`ShowKeys` | Немедленно в этом окне (`Update.FontSize` каждый `Poll`, палитра — в каждом `Draw`); другие окна не затрагиваются | `app.go:Poll`, `draw.go:Draw` |
| New window (⌘N) | Копирование настроек | `Settings` сериализуются в JSON и десериализуются в новое окно → секреты (`json:"-"`) не копируются; `dialogValues`, истории — не копируются | `windows.go:NewWindow` |
| Save setup | Каталог конфигурации не создаётся / временный файл не пишется | Статус = текст ошибки ОС (`a.error` поверх `Setup saved`); `saveFolderShortcut` проверяет `Status == "Setup saved"` | `settings.go:saveSetup`, `folder_shortcuts.go` |
| Save setup | `DescriptionFile` = `.`/`..` | Проходят `filepath.Base(s) == s`, сохраняются; запись `descript.ion` потом завершится ошибкой rename | `settings.go` (`File descriptions`, `normalizeSettings`) |
| Network connections | URL без схемы, `host:22`, `user@host` | `url.Parse` даёт пустую схему → `use sftp://host, ftp://host or ftps://host` | `remotefs/fs.go:Parse` |
| Network connections | Пробел внутри URL, `@` в имени пользователя внутри URL, `sftp://::1` без скобок | `url.Parse` → `invalid server address`; IPv6 в скобках (`sftp://[::1]:22/`) поддерживается через `JoinHostPort` | `remotefs/fs.go:Parse`, `Address` |
| Network connections | Ключ без прав 0600 | Не проверяется (в отличие от OpenSSH); читается любой файл ≤ 1 MiB | `remotefs/ssh_defaults.go:readPrivateKey` |
| Network connections | `SSH_AUTH_SOCK` не задан / агент недоступен | Молча пропускается; при отсутствии других методов — `no usable SSH identity…` | `remotefs/sftp.go:dialSFTP` |
| Network connections | known_hosts без прав чтения | `os.Stat` успешен → `knownhosts.New` возвращает ошибку открытия → в статус, без диалога | `remotefs/sftp.go:HostKeyCallback` |
| Network connections | Несколько ключей хоста в known_hosts | Предпочтение алгоритмов уже известных типов (`hostKeyAlgorithms`), RSA расширяется до `rsa-sha2-256/512` | `remotefs/ssh_defaults.go` |
| Удалённая панель | Сервер упал после подключения | Каждая загрузка — новый `Dial`; при `Async` ошибка попадает в `p.Err` и рисуется в строке статуса панели, содержимое панели откатывается к предыдущему снимку | `panel_load.go:pollPanelLoad`, `panel_draw.go` |
| FTP | Режим передачи | Только пассивный (EPSV/PASV библиотеки); активного режима нет | `remotefs/ftp.go:dialFTPWithTLS` |
| FTP | Пароль с пробелами/не-ASCII | Передаётся как есть; отвергаются только `\x00\r\n` | `remotefs/ftp.go` |
| Change volume… | Панель удалённая/архивная | `Remote = nil` без `cancelPanelLoad`/`cancelTree`/сброса `states`/`RemoteLocalPath`; гонки нет, т.к. `loadPanel` сам отменяет предыдущую загрузку | `panel_options.go:volumes` |
| Disconnect active panel | Панель не удалённая | Молча ничего | `remote.go:disconnectRemote` |
| Профили | connections.json повреждён | `loadConnectionProfiles` молча оставляет профили из setup.json; `saveConnectionProfile` возвращает ошибку → `; profile not saved: <err>` при каждом подключении | `connection_profiles.go` |
| Справка F1 | Внутри документа | `F1` в Help-документе — no-op; в обычном документе — `Viewer / editor help`; `F9` в документе открывает `documentSettings`, а не главное меню | `document.go:documentKey` |
| Строка статуса | Обрезка / перезапись | Обрезается по ширине без «…»; перезаписывается `backgroundTask` (label), `pollTask` (`""`), событием готовности shell (`Exit N · ⌘O panels / terminal`) даже в режиме панелей | `draw.go:Draw`, `task.go`, `app.go:Poll` |
