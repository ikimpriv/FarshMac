# Kubernetes-панель: требования, gap analysis, спецификация реализации

Статус: фазы 0–2 реализованы на ветке `kubernetes-panel` (2026-11-13), фаза 3 (apply при копировании в кластер, scale/restart/port-forward, восстановление из сессии, Info panel, Quick view, `du`) не начата. Отличия реализации от текста ниже: `Panel` сохраняет поля `Remote`/`Archive` и получает `Kube`, а `panelBackend` вычисляется из них (`panel_backend.go`); `remotefs.Endpoint{Opener}` реализован; `documentSource` реализован для файлов серверов и контейнеров (`remoteSource`) и манифестов (`manifestSource`); вместо `terminal.Stream` в `App` есть `Foreign foreignStream` (`foreign.go`) для exec и follow-логов; папка типа, имя которой есть в нескольких группах, называется `plural.group`; Shift+F4 внутри ФС контейнера остаётся «новый файл»; объекты без дочерних объектов показываются файлами (Enter = манифест), а не пустыми папками; колонки панели — Kind / Status / Age (Detailed: время создания); после листинга namespace его типы наблюдаются через Table-watch (`kube.WatchFolder`, `Snapshot.Apply`), снимок обновляется в памяти и панель перечитывается сама, TTL остаётся запасным вариантом; после завершения команды в контейнере терминал ждёт клавишу и возвращает панели; Quick view (Ctrl+Q) рядом с Kubernetes-панелью показывает describe текущего объекта с подсветкой (из фазы 3 перенесено вперёд). Тесты: `internal/kube` (fake API в `internal/kube/kubetest`, локальный `sh` в `internal/kube/localexec`), `internal/ui/kube_*_test.go`.

Принятые направления (ревизия 3):
- Backend — **client-go**, без `kubectl`.
- Режимы панели переводятся на **strategy pattern** (интерфейс `panelBackend`) с enum `backendKind` для дешёвых проверок. Это отдельная подготовительная фаза без изменения поведения.
- Key bar **реагирует на удержание Shift/Ctrl/Alt/Cmd**, как в Far Manager, для всех режимов панели.
- Назначение — **dev tool для быстрого доступа к объектам**, не универсальный инструмент обслуживания кластера. Cluster-scoped объекты (Node, PV, ClusterRole, CRD) в панели не показываются.
- Дерево: `namespace > <тип ресурса> > объект > <тип дочернего ресурса> > объект > … > pod > container`. Промежуточные владельцы (ReplicaSet, ControllerRevision) **схлопываются** по жёстко заданным правилам, чтобы дерево было неглубоким.
- Describe — через `k8s.io/kubectl/pkg/describe`.

## 1. Требования (как сформулированы)

1. В **Commands → Network connections** появляется пункт **New Kubernetes cluster…**: указывается путь к kubeconfig и выбирается контекст. Профиль сохраняется и показывается в списке подключений.
2. Kubernetes показывается в панели как файловая система:
   - корень — namespaces;
   - внутри namespace — папки по типу ресурса (`deployments`, `services`, `configmaps`, CRD-типы, …);
   - внутри папки типа — объекты этого типа;
   - внутри объекта — папки дочерних типов с объектами, которыми он владеет, причём неудобные промежуточные уровни (ReplicaSet) пропускаются: `deployments > app > pods > app-7c9d-x > container`.
3. При перемещении по уровням меняется нижняя панель клавиш (key bar) и доступные действия.
4. F5 на объекте копирует его манифест. F5 манифеста (локальный файл) в namespace создаёт объект.
5. Вход в Pod показывает список контейнеров (если их несколько). Вход в контейнер (или в одноконтейнерный Pod) показывает файловую систему контейнера.
6. Команда меню «Логи» для Pod или контейнера.
7. Копирование файлов из контейнера и в контейнер.
8. Внутри контейнера команда из командной строки выполняется в Pod (если есть права).

## 2. Проверка идеи на целостность

| # | Проблема | Решение |
|---|----------|---------|
| C1 | Объект может иметь несколько владельцев, владельца в другом namespace или cluster-scoped владельца (Node). | Папка типа на уровне namespace показывает **все** объекты типа, независимо от владения (как `kubectl get <type>`). Внутри объекта показываются только дочерние объекты по правилам 5.2. Один Pod виден и в `ns/pods/`, и в `ns/deployments/app/pods/`: это представление, а не хранилище. |
| C2 | Имена записей в папке должны быть уникальны и не содержать `/`. | В папке типа все объекты одного GVR, имена уникальны по определению, поэтому запись = имя объекта без суффикса. Папка типа называется по множественному имени ресурса (`deployments`); если одно имя ресурса есть в нескольких группах, папка называется `deployments.<group>`. Разбор имён назад не требуется: снимок хранит карту «путь → объект». |
| C3 | Одноконтейнерный Pod: при входе сразу видна ФС, но тогда негде показать действия уровня Pod. | Правило «сразу ФС» применяется только когда в Pod ровно один обычный контейнер и нет init/ephemeral-контейнеров. Иначе показывается список контейнеров с пометками ` (init)`, ` (ephemeral)`. Логи и манифест доступны на записи Pod в родительской папке. Виртуальный путь однозначен: после сегмента Pod либо всегда контейнер, либо всегда путь ФС (решает снимок). |
| C4 | F5 имеет три смысла: манифест объекта, файлы контейнера, применение манифеста. | Смысл определяется **уровнем источника** и **уровнем назначения** (таблица в 5.3). Запись-контейнер копировать нельзя. Локальный файл в список контейнеров копировать нельзя. |
| C5 | Pod не в состоянии Running не имеет доступной ФС. | Вход показывает ошибку в панели, остальные действия (манифест, логи, describe, удаление) работают. |
| C6 | В контейнере может не быть `sh`/`tar` (distroless). | При первом входе выполняется probe (`/bin/sh`, `/bin/bash`, наличие `tar`, вариант `find`/`stat`/`ls`). Результат кэшируется по (pod uid, container). Без shell: ФС и команды недоступны с явной ошибкой. Без `tar`: деревья недоступны, одиночные файлы через `cat`. Ephemeral debug-контейнер — фаза 3. |
| C7 | F6 (переименование) не имеет смысла для объектов Kubernetes. | На уровне объектов F6 отключён. В ФС контейнера F6 = `mv` внутри того же контейнера. |
| C8 | F8 на namespace уничтожает всё содержимое. | Удаление namespace требует ввода его имени. Удаление объектов показывает kind/name и каскад. Shift+F8 на объекте = `PropagationPolicy: Orphan`. |
| C9 | Синхронизация папки панели и терминала (Cmd+O) не применима. | Kubernetes-панель ведёт себя как Remote: локальный shell остаётся в последней локальной папке. |
| C10 | Команда «в Pod» и терминал. | С client-go интерактивный exec — это поток SPDY/WebSocket с TTY. Терминальная область получает абстракцию `terminal.Stream`: пока выполняется команда в контейнере, вывод идёт в xterm, ввод и resize — в поток exec; локальный shell простаивает. Это даёт и «Open shell in container» как настоящий интерактивный shell. Постоянного shell между отдельными командами нет: cwd берётся из панели, окружение не сохраняется. |
| C11 | Редактирование манифеста (F4) и конкурентные изменения. | Сохранение = `Update` с `resourceVersion` из загруженного объекта. `errors.IsConflict` показывается как ошибка, буфер и флаг изменений сохраняются (согласуется с TODO.md про ошибки сохранения). |
| C12 | Копирование манифестов Secret на диск записывает секреты в открытом виде (base64). | Если выделение содержит Secret, дополнительное подтверждение. |
| C13 | Exec-плагины аутентификации (`gke-gcloud-auth-plugin`, `aws`, `kubelogin`) запускаются client-go по PATH процесса, а PATH GUI-приложения, запущенного из Finder, минимален. | При первом обращении к Kubernetes один раз запрашивается PATH login-shell (`$SHELL -lc 'printf %s "$PATH"'`, таймаут 5 с) и применяется к процессу через `os.Setenv`. В профиле есть необязательное поле «Extra PATH». Ошибка плагина показывается с его stderr. |
| C14 | Cluster-scoped объекты (Node, PV, ClusterRole, CRD) не входят в «корень = namespaces». | Не показываются. Инструмент ориентирован на объекты приложения, а не на обслуживание кластера. Node нужен разработчику редко; для Pod имя ноды показывается в Info panel и в Describe. |
| C15 | Папка типа внутри объекта и имя контейнера внутри Pod — разные уровни, но оба идут после имени объекта. | Внутри Pod папки типов не показываются (у Pod нет интересных дочерних объектов), поэтому сегмент после Pod — всегда контейнер или путь ФС (C3). Внутри остальных объектов — только папки типов. |
| C16 | Глубина дерева. Цепочка `Deployment → ReplicaSet → Pod` и `StatefulSet → ControllerRevision` делают дерево на уровень глубже без пользы. | Жёстко заданный список **прозрачных** типов, через которые владение проходит насквозь: `replicasets`, `controllerrevisions`. Они остаются доступными в `ns/replicasets/`, но внутри Deployment их нет. Список расширяем в коде, не в настройках (5.2). |

Вывод: идея целостна при соблюдении правил C2–C4. Основная работа — не UI Kubernetes, а подготовительный рефакторинг режимов панели (раздел 4) и key bar (раздел 6).

## 3. Gap analysis относительно текущего кода

| # | Что есть | Чего не хватает | Решение |
|---|----------|-----------------|---------|
| G1 | Зависимостей Kubernetes нет; `go.mod` содержит 7 прямых модулей. | Клиент API, exec/stream, YAML. | **client-go** (`k8s.io/client-go`, `k8s.io/apimachinery`, `k8s.io/api`) одной согласованной версии `v0.3x` под текущие кластеры, пин точной версии. YAML — `sigs.k8s.io/yaml` и `k8s.io/apimachinery/pkg/util/yaml` (идут транзитивно). Цена: `go.sum` вырастает примерно на 150 модулей, бинарник примерно на 25–35 МБ, первая сборка дольше; `THIRD_PARTY_NOTICES.txt` дополняется Apache-2.0 (Kubernetes, gogo/protobuf, gnostic, …) и BSD/MIT остальных. Это осознанная плата за отсутствие внешнего бинарника. |
| G2 | `Panel` имеет два режима через поля `Remote *remotefs.Config` и `Archive *fsops.ArchiveIndex`; 37 мест в `internal/ui` проверяют `Remote != nil`/`== nil`, ещё ряд мест — `Archive != nil`. | Третий режим без размножения условий. | Strategy pattern: интерфейс `panelBackend` с четырьмя реализациями (local, archive, remote, kube) и enum `backendKind`. Подробно в разделе 4. Сначала рефакторинг существующих режимов без изменения поведения, затем Kubernetes. |
| G3 | `remotefs.FS` — интерфейс удалённой ФС; `remotefs.Copy`/`Move`/`RemoveTree` работают через него с прогрессом, коллизиями и отменой. `Endpoint{Remote *Config}`. | Открытие FS контейнера. | `kube.ContainerFS` реализует `remotefs.FS` поверх `remotecommand` exec. `Endpoint` обобщается до `{Opener}` (4.4). |
| G4 | Листинг — `[]Entry{Name, Info os.FileInfo, Dir}`; отрисовка берёт размер и дату из `Info`, спец-колонки через `Info.Sys()`. | Статус объекта и возраст. | `kube.ObjectInfo` реализует `os.FileInfo`; `Sys()` возвращает `*kube.Object`. Колонка статуса берётся из серверного Table-представления (5.2), что даёт те же колонки, что у `kubectl get`, включая CRD с `additionalPrinterColumns`. |
| G5 | Key bar статичен (`draw.go:39`), меняет только F8 для Remote; не реагирует на удержание модификаторов. Renderer не сообщает об удержании. | Контекстные подписи по режиму, уровню и модификаторам. | Раздел 6. |
| G6 | `menuSections()` динамична. | Раздел «Kubernetes» в F9. | Раздел берётся из `panelBackend.MenuSection()`. |
| G7 | Профили — `[]remotefs.Config` в `connections.json`; старые версии не переживут записи другого типа. | Хранение профилей кластеров. | Отдельный файл `kubernetes.json` (`[]kube.Profile`), поле `Settings.KubeProfiles`. Секретов нет. |
| G8 | Форма `a.form` без выпадающих списков. | Выбор контекста. | Два шага: форма (kubeconfig, name) → `a.choose` со списком контекстов из `clientcmd.LoadFromFile`. Текущий контекст предвыбран. |
| G9 | `Document` статичен. | Логи с follow. | Статичные логи (tail N) — в viewer. Follow — через `terminal.Stream` в терминальной области (как exec, C10): `GetLogs(...).Stream(ctx)` с `Follow: true`, Ctrl+C отменяет контекст. |
| G10 | `execute()` всегда отправляет команду локальному shell. | Выполнение в контейнере. | Ветка через `panelBackend.Execute(a, cmd) bool`: kube-backend на уровне ФС контейнера запускает `terminal.Stream` exec (C10). В историю попадает введённый текст. Приглашение `pod:<ns>/<pod>/<ctr>:<dir>>`. |
| G11 | Сессия сохраняет только локальный путь; Remote-панели не восстанавливаются. | Восстановление Kubernetes-панели. | MVP как Remote. Восстановление по профилю — фаза 3, секретов не требует. |
| G12 | `openRemoteDocument`/`saveRemoteDocument` привязаны к `remotefs.Config.Dial`. | Просмотр/правка файлов контейнера и манифестов. | `documentSource` с `Fetch(ctx, o) (localPath, info)` и `Store(ctx, data)`; реализации remote, container, manifest. Выдаётся через `panelBackend.DocumentSource`. |
| G13 | Терминальная область жёстко связана с `terminal.Session` (локальный PTY): `App.Term`, `a.pending`, `Handle("input")`. | Второй источник байтов и приёмник ввода. | `terminal.Stream` интерфейс `{Write([]byte) error; Resize(cols, rows) error; Events() <-chan Event; Close()}`; `Session` реализует его; `kube.ExecStream` реализует поверх `remotecommand.StreamWithContext` с `TerminalSizeQueue`. `App.Foreign terminal.Stream` при активном удалённом потоке перехватывает ввод и resize, по завершении возвращает управление локальному shell. |
| G14 | Отмена через `fsops.Operation.Context()`. | — | Все вызовы client-go принимают `o.Context()`. |
| G15 | Тесты используют loopback-серверы (`internal/testnet`). | Тестовый API-сервер. | `httptest.Server` с ручным роутером для discovery, LIST (Table и JSON), GET/UPDATE/DELETE/PATCH, logs; exec — тестовый SPDY/WebSocket-сервер поверх `k8s.io/apimachinery/pkg/util/httpstream` либо инжекция `Executor` интерфейса с локальным `sh`. Реальный кластер — только opt-in `FARSHMAC_KUBE_CHECK` (по аналогии с `FARSHMAC_SFTP_CHECK`). |

## 4. Рефакторинг режимов панели (фаза 0)

### 4.1 Цель

Убрать разрозненные проверки `Remote != nil` и `Archive != nil` и дать Kubernetes-режиму одно место подключения. Поведение local, archive и remote после фазы 0 не меняется; существующие тесты проходят без правок ожиданий.

### 4.2 Интерфейс

```go
type backendKind uint8
const (
    backendLocal backendKind = iota
    backendArchive
    backendRemote
    backendKube
)

type panelCaps uint32
const (
    capEdit panelCaps = 1 << iota   // F4 и сохранение
    capMkdir                         // F7
    capDelete                        // F8
    capTrash                         // F8 = Trash (иначе Delete)
    capRename                        // F6
    capCopyIn                        // панель может быть назначением F5
    capFolderSize                    // F3 на папке, Cmd+Option+F3
    capQuickView                     // Ctrl+Q на текущем элементе
    capArchiveEnter                  // Enter на архиве
    capShellSync                     // Cmd+O синхронизирует cwd
    capDescriptions                  // files.bbs и т.п.
    capExecute                       // команда из строки выполняется backend-ом
)

type panelBackend interface {
    Kind() backendKind
    Label() string                                   // префикс заголовка панели ("", "archive:", "sftp://…", "k8s:ctx")
    Load(p *Panel, path string, o *fsops.Operation) ([]Entry, loadMeta, error)
    FilesystemPath(p *Panel) string                  // локальная папка для терминала и относительных путей
    Caps(p *Panel, path string) panelCaps            // может зависеть от уровня внутри backend-а
    KeyBar(p *Panel, mods modifiers) [10]string      // раздел 6
    Endpoint(p *Panel) remotefs.Endpoint             // для F5/F6 через remotefs.Copy / Move
    DocumentSource(p *Panel, path string) documentSource
    Delete(a *App, paths []string, permanent bool)
    Mkdir(a *App)
    Execute(a *App, cmd string) bool                 // true = обработано; false = локальный shell
    MenuSection() *menuSection                       // nil, если раздела нет
    SessionState(p *Panel) SessionPanel              // что сохранять в сессии
    Close()                                          // отключение
}
```

`loadMeta` переносит то, что сейчас `loadRemote`/`loadArchiveDirectory` пишут в поля панели напрямую: `InfoText`, `TotalBytes`, `ItemCount`, `Descriptions`.

### 4.3 Что переезжает

| Сейчас | Куда |
|--------|------|
| `Panel.Remote`, `Panel.RemoteLocalPath`, `Panel.Archive` | `Panel.backend panelBackend` (`localBackend{}` по умолчанию), `remoteBackend{config, localPath}`, `archiveBackend{index, localPath}` |
| `Panel.Load` ветвление | `p.backend.Load(...)` плюс общий код `beginLoad/sortEntries/installEntries` |
| `filesystemPath()` | `p.backend.FilesystemPath(p)` |
| `function(n)` ветки по Remote/Archive | `caps` и методы backend-а |
| `transferDialog` условия | `Caps(...)&capCopyIn`, `Endpoint()` |
| `deleteSelected`, `remoteDelete`, `archiveReadOnly` | `Delete`, `capDelete`, `capTrash` |
| `openConfigured` ветки | `DocumentSource` |
| `draw.go` подписи F8 и префикс `local:` | `KeyBar`, `Label` |
| `syncCwd`, `showTerminal`, `finishPanelReturn` проверки `Remote == nil` | `capShellSync` |
| `image_preview.go`, `folder_size_cache.go`, `info_panel.go`, `panel_options.go`, `folder_shortcuts.go` | `capQuickView`, `capFolderSize`, `Kind()` там, где важен именно тип |
| `session.go` сохранение | `SessionState` |

Enum `Kind()` допускается использовать только там, где нужна именно идентичность типа (например, тесты и сообщения), а не поведение. Правило для ревью: новое условие вида `Kind() == backendX` в коде действий считается запахом; вместо него добавляется cap или метод.

### 4.4 `remotefs.Endpoint`

```go
type Opener interface {
    Open(ctx context.Context) (FS, error)
    Identity() string      // для Same(): один и тот же сервер/контейнер
}
type Endpoint struct{ Opener Opener } // nil = локальная ФС
```

`*Config` реализует `Opener`. Kube-контейнер реализует его в пакете `kube`, импортируя `remotefs` (не наоборот).

### 4.5 Проверка фазы 0

- `grep -n "Remote != nil\|Remote == nil\|Archive != nil" internal/ui/*.go` вне `*_test.go` и файлов backend-ов даёт ноль.
- `./scripts/check.sh --race` зелёный, `remote_test.go`, `archive_panel_test.go`, `transfer_collision_test.go`, `far_behavior_test.go` не меняют ожиданий.
- Ручная проверка: SFTP-панель, архивная панель, копирование в обе стороны, сессия.

## 5. Kubernetes-режим

### 5.1 Пакет `internal/kube`

```
internal/kube/
  profile.go      Profile{Name, Kubeconfig, Context, ExtraPath}; Label() = "k8s:"+Context
  client.go       Client{rest *rest.Config; dyn dynamic.Interface; core kubernetes.Interface (CoreV1 для logs/exec);
                         disc discovery.CachedDiscoveryInterface; mapper meta.RESTMapper}
                  Connect(ctx, Profile) — clientcmd.NewNonInteractiveDeferredLoadingClientConfig с
                  ConfigOverrides{CurrentContext}; импорт _ "k8s.io/client-go/plugin/pkg/client/auth" для auth providers;
                  QPS/Burst подняты (kubectl-подобно: 50/300); Timeout из настроек
  contexts.go     Contexts(kubeconfig) ([]string, current string, error) через clientcmd.LoadFromFile
  discovery.go    Resources(ctx) ([]Resource, error) — ServerPreferredNamespacedResources, кэш на сессию;
                  фильтр по глаголам list/get; Resource{GVR, Kind, Singular, Namespaced, Verbs}
  snapshot.go     Snapshot{Namespace, Objects map[types.UID]*Object, ByType map[GVR][]*Object, children map[UID][]*Object,
                           Taken time.Time, Skipped []SkippedResource}
  tree.go         transparent = {replicasets.apps, controllerrevisions.apps}; selectorChildren = {services → pods};
                  Children(obj) map[GVR][]*Object по правилам 5.2; TypeFolders(ns) с приоритетным порядком
                  LoadSnapshot(ctx, c, ns, set ResourceSet) — параллельные LIST (семафор 8) в формате Table
                  (Accept: application/json;as=Table;v=v1;g=meta.k8s.io, includeObject=Metadata):
                  строки содержат printer-колонки и PartialObjectMetadata с ownerReferences и labels; forbidden → Skipped;
                  services дополнительно LIST полными объектами ради spec.selector
  object.go       Object{GVR, Kind, Name, Namespace, UID, Created, Owners []UID, Labels, Columns []string, Status string}
                  ShortStatus() выбирает колонку STATUS/READY/TYPE по kind, иначе первую не-NAME/AGE колонку
  pod.go          PodSummary(ctx, ns, name) — контейнеры (regular/init/ephemeral), состояния, phase
  manifest.go     Get(ctx, ref) (*unstructured.Unstructured, error); ExportYAML(obj, clean bool) ([]byte, error)
                  (clean: managedFields, status, uid, resourceVersion, creationTimestamp, generation,
                  ownerReferences при копировании в другой namespace/кластер);
                  Decode(data) ([]*unstructured.Unstructured, error) — YAMLOrJSONDecoder, многодокументный;
                  Apply(ctx, obj, ns) — server-side apply, FieldManager "farshmac", Force=false;
                  Update(ctx, obj) — с resourceVersion; Delete(ctx, refs, propagation)
  describe.go     Describe(ctx, ref) (string, error) — k8s.io/kubectl/pkg/describe (модуль k8s.io/kubectl, решение принято)
  logs.go         Logs(ctx, ref, opts) (io.ReadCloser, error) — corev1 GetLogs(...).Stream
  exec.go         Executor interface { Stream(ctx, ExecOptions) error }; реализация remotecommand
                  (NewWebSocketExecutor с FallbackExecutor на SPDY, как в kubectl)
  containerfs.go  ContainerRef{Profile, Namespace, Pod, Container}; ContainerFS implements remotefs.FS;
                  probe (shell, tar, lister); ReadDir через `find -maxdepth 1 -printf` → `stat -c` → `ls -lan`;
                  Open = `cat`, Create = `cat > path` через stdin; Remove = rm/rmdir; Rename = mv; Chmod/Chtimes
  execstream.go   ExecStream implements terminal.Stream: TTY, stdin из xterm, resize через TerminalSizeQueue
```

Все методы принимают `context.Context`. Данные с сервера никогда не интерпретируются как команды.

### 5.2 Виртуальный путь и снимок

`Panel.Path` — абсолютный путь:

```
/                                        namespaces
/<ns>                                    папки типов ресурсов
/<ns>/<type>                             объекты типа (все, как kubectl get)
/<ns>/<type>/<name>                      папки дочерних типов (см. правила владения)
/<ns>/<type>/<name>/<childtype>/<name>/… рекурсивно
/<ns>/…/pods/<pod>/<container>           только если контейнеров > 1 (C3)
/<ns>/…/pods/<pod>[/<container>]/<abs path in container>
```

Уровни (`kubeLevel`): `root`, `namespace`, `typeFolder`, `object`, `containers`, `containerFS`. Папка дочернего типа внутри объекта — тот же уровень `typeFolder`, но с родителем.

**Правила владения (жёстко в коде, `internal/kube/tree.go`):**

1. Дочерние объекты объекта X — это объекты снимка, у которых X в `ownerReferences`.
2. Если дочерний объект имеет **прозрачный** тип (`replicasets`, `controllerrevisions`), он не показывается; вместо него показываются его дочерние объекты (рекурсивно). Так `deployments/app/pods/` содержит Pod всех ReplicaSet-ов, включая старые после rollout; старые Pod отличаются колонкой статуса.
3. **Селекторные дети**: `services/<svc>/pods/` — Pod, чьи labels удовлетворяют `spec.selector` сервиса. Это не владение, помечается в InfoText панели как «matched by selector». Для этого Service загружаются с `spec` (один LIST полных объектов, они небольшие), Pod — только метаданные (labels есть в `PartialObjectMetadata`).
4. Всё остальное, включая CRD, использует только правила 1–2. Пример: Argo `rollouts/app/pods/` получается автоматически через прозрачный ReplicaSet; `cronjobs/backup/jobs/<job>/pods/` — два уровня, оба полезны.
5. Внутри Pod папок типов нет (C15): контейнеры или ФС.
6. Объект без детей — пустая папка (вход показывает пустой список, не ошибку).

Расширение правил 2–3 — изменение кода с тестом, не настройка: это осознанно, чтобы дерево было одинаковым у всех.

**Папки типов на уровне namespace.** Показываются типы, у которых в снимке есть хотя бы один объект; настройка `KubeShowEmptyTypes` (выкл.) показывает все. Колонка «размер» папки типа = число объектов, дата пуста. Порядок «Unsorted» (Ctrl+F7) — приоритетный список: `deployments, statefulsets, daemonsets, cronjobs, jobs, pods, services, ingresses, configmaps, secrets, persistentvolumeclaims, serviceaccounts`, затем остальные по алфавиту; сортировка по имени — чистый алфавит.

**Снимок.** Снимок namespace кэшируется в backend-е (`snapshots[ns]`), обновляется по Ctrl+R, при повторном входе в namespace и по TTL (`KubeSnapshotTTL`, 30 с). Загрузка — через асинхронный `loadPanel`, `Esc` отменяет контекст всех LIST. Набор ресурсов: все namespaced-ресурсы с глаголом `list`, кроме подресурсов и `*.metrics.k8s.io` (это не объекты). `events` — обычная папка типа: в структуре с папками по типам события никому не мешают, а разработчику бывают нужны. Настройка `KubeHiddenTypes` (по умолчанию пуста) позволяет скрыть шумные типы конкретного кластера. Типы с Forbidden молча пропускаются; их число показывается в InfoText («3 resource types skipped: forbidden»).

Записи:

| Уровень | Имя | Dir | Дата | Поле размера |
|---------|-----|-----|------|--------------|
| root | имя namespace | да | creation | STATUS (Active/Terminating) |
| namespace | имя типа (`deployments`) | да | — | число объектов |
| typeFolder | имя объекта | да | creation | Table-колонка: Deployment `READY 2/3`, Pod `Running`, Job `Complete`, Service `ClusterIP`, CRD — первая printer-колонка после NAME |
| object | имя дочернего типа (`pods`) | да | — | число объектов |
| containers | имя [` (init)`/` (ephemeral)`] | да | start time | `running` / `waiting:CrashLoopBackOff` / `terminated:0` |
| containerFS | как у Remote | по режиму | mtime | размер |

Маски панели применяются к именам записей на любом уровне.

### 5.3 Действия по уровням

| Клавиша | root | namespace | typeFolder (объекты) | object (папки дочерних типов) | containers | containerFS |
|---------|------|-----------|----------------------|-------------------------------|------------|-------------|
| Enter / Ctrl+PgDn | войти | войти | войти (Pod по C3) | войти | войти в ФС | войти |
| F3 | манифест namespace | — | манифест объекта (yaml, read-only) | — | манифест Pod | просмотр файла |
| F4 | править манифест | — | править манифест (C11) | — | — | править файл |
| F5 | экспорт namespace (все папки типов → yaml) | экспорт папки типа (все манифесты) | манифест объекта → local/k8s | экспорт дочерней папки типа | запрещено | файлы → local/container |
| F6 | — | — | — (C7) | — | — | `mv` |
| F7 | новый namespace | — | — | — | — | `mkdir` |
| F8 | удалить namespace (ввод имени) | — («select objects inside») | delete (подтверждение, каскад Background) | — | — | `rm -r` |
| Shift+F8 | — | — | delete с Orphan | — | — | как F8 |
| Shift+F3 | — | — | логи (Pod; для владельца Pod — выбор Pod из `pods/`) | — | логи контейнера | — |
| Shift+F4 | describe | — | describe | — | describe Pod | — |
| Alt+F3 | — | — | follow-логи | — | follow-логи | — |
| Ctrl+R | обновить | обновить снимок | обновить | обновить | обновить | обновить |

Правило экспорта (F5 из Kube в local): папка = рекурсивный экспорт её содержимого как `yaml`-файлов (`namespace/` → `deployments/app.yaml`, `services/web.yaml`, …); **объект** экспортируется только как свой манифест `<name>.yaml`, без дочерних объектов, потому что дочерние объекты — производное состояние контроллеров. Имя файла `<name>.yaml` однозначно: выделение всегда внутри одной папки типа. Типы из `KubeHiddenTypes` и `events` в рекурсивный экспорт не входят.

Копирование в Kube-панель (назначение):

| Назначение | Источник local-файлы | Источник объекты k8s | Источник файлы контейнера |
|------------|----------------------|----------------------|---------------------------|
| root | запрещено | запрещено | запрещено |
| namespace | server-side apply в этот namespace; namespace в файле ≠ целевому → Override/Skip/Cancel; объект существует → Apply/Skip/Cancel (через `fsops.Operation.Collision`) | то же, чистый экспорт, `ownerReferences` снимаются с предупреждением | запрещено |
| typeFolder | как namespace; kind манифеста ≠ типу папки → «Apply anyway / Skip» | как namespace | запрещено |
| object / дочерняя typeFolder | как typeFolder (namespace берётся из пути) | как namespace | запрещено |
| containers | запрещено | запрещено | запрещено |
| containerFS | загрузка через `remotefs.Copy` | запрещено | `remotefs.Copy` |

Диалог копирования манифестов: флажок «Clean export (strip status and server fields)», включён по умолчанию. Многодокументные YAML и JSON при apply поддерживаются.

### 5.4 Меню F9 → Kubernetes (при активной Kube-панели)

- Logs… (`Shift+F3`): для владельца Pod — выбор Pod из его `pods/`; контейнер (choose, если > 1), Tail lines (`KubeLogsTail` = 1000), Previous instance, Since.
- Follow logs in terminal (`Alt+F3`).
- Describe (`Shift+F4`).
- Open shell in container: `ExecStream` с TTY и `sh`/`bash` из probe.
- Refresh snapshot (`Ctrl+R`).
- Switch context…
- Фаза 3: Scale…, Restart rollout, Port-forward…, Copy object reference в буфер.

### 5.5 Команда в контейнере

`kubeBackend.Execute(a, cmd)`: на уровне ФС контейнера создаётся `ExecStream` с TTY и командой `[shell, "-c", "cd " + Quote(dir) + " && " + cmd]`, `a.Foreign = stream`, терминал показывается. Ввод с клавиатуры и resize идут в поток, вывод в xterm. По завершении потока `a.Foreign = nil`, статус показывает код завершения, панель контейнера обновляется. Ctrl+C отправляется в поток как байт 0x03 (TTY); Cmd+. отменяет контекст. На других уровнях Kube-панели команда выполняется локально в `FilesystemPath()` с приглашением `local:<path>>`.

### 5.6 Подключение

Форма «Kubernetes cluster»: Kubeconfig path (по умолчанию первый путь из `$KUBECONFIG` или `~/.kube/config`), Profile name (blank = context). Затем `a.choose("Context", contexts)` с предвыбранным current-context. `backgroundTask`: Connect, `ServerVersion`, discovery, LIST namespaces. Ошибка показывает текст client-go (включая stderr exec-плагина). Успех: панель переходит на `/`, профиль сохраняется, статус «Connected: k8s:<context> (terminal is local)».

### 5.7 Ограничения, сообщаемые пользователю

- Размер папки в Kube-панели не считается (фаза 3 через `du`).
- Quick view на объекте — фаза 3 (манифест).
- Info panel: контекст, сервер, версия, число namespaces — фаза 2.
- Архивы внутри ФС контейнера не открываются как папки (как у Remote).

## 6. Key bar с модификаторами

### 6.1 Протокол renderer → Go

Новое сообщение `{Type: "modifiers", Shift, Ctrl, Alt, Meta bool}`. Renderer (`app.js`) отправляет его при `keydown`/`keyup` клавиш Shift, Control, Alt, Meta, только если состояние изменилось, и при `blur` окна/`visibilitychange` — сброс в false. Сообщение отправляется в обоих режимах (панели и терминал), но Go использует его только для отрисовки панелей. Нажатие с модификатором (`key` с флагами) не меняет протокол.

Go: `App.mods modifiers` (битовая маска), обновляется в `Handle`, помечает `dirty`. Draw вызывает `p.backend.KeyBar(p, a.mods)`; документ (viewer/editor) и меню имеют свои таблицы в `document_draw.go` и тоже получают `mods`.

Защита от залипания: если в течение 2 с не пришло ни одного сообщения при удерживаемом модификаторе и приходит `key` без этого флага, маска сбрасывается по флагам этого `key`.

### 6.2 Таблицы

Local (совпадает с F1-справкой и Far Manager):

| Mods | 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 | 9 | 10 |
|------|---|---|---|---|---|---|---|---|---|----|
| — | Help | UserMn | View | Edit | Copy | RenMov | MkFold | Trash | Menu | |
| Shift | AddArc | ExtrAr | ArcCmd | | Copy | Rename | | Delete | Save | |
| Ctrl | | | Name | Ext | Modif | Size | Unsort | Creat | Access | Descr |
| Alt | | | | | | | Find | Histry | | |
| Alt+Shift | | | | | | | | | | |

Remote: как Local, но F8 `Delete`, Shift+F1..F3 пусто, Ctrl+F8..F10 пусто, если сортировка по этим полям не поддерживается сервером.
Archive: F4, F6, F7, F8 пусто; Shift+F2 `Extract`.
Kube — по уровням:

| Уровень | Mods | 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 | 9 |
|---------|------|---|---|---|---|---|---|---|---|---|
| root | — | Help | UserMn | Manif | Edit | Export | | NewNS | Delete | Menu |
| root | Shift | | | | Descr | | | | | |
| namespace | — | Help | UserMn | | | Export | | | | Menu |
| typeFolder (объекты) | — | Help | UserMn | Manif | Edit | Copy | | | Delete | Menu |
| typeFolder (объекты) | Shift | | | Logs | Descr | | | | Orphan | |
| typeFolder (объекты) | Alt | | | Follow | | | | | | |
| object (папки типов) | — | Help | UserMn | | | Export | | | | Menu |
| containers | — | Help | UserMn | Manif | | | | | | Menu |
| containers | Shift | | | Logs | Descr | | | | | |
| containers | Alt | | | Follow | | | | | | |
| containerFS | — | Help | UserMn | View | Edit | Copy | RenMov | MkFold | Delete | Menu |
| containerFS | Shift | | | | | Copy | Rename | | Delete | |

Подписи `Logs`/`Follow` показываются на уровне typeFolder только если текущий объект — Pod или имеет Pod среди дочерних (Deployment, StatefulSet, Job, Service и т.п.); key bar пересчитывается при движении курсора.

Ctrl-ряд (сортировки) одинаков для всех режимов там, где поле есть.

### 6.3 Тесты

- `internal/native/tests/input.cjs`: `keydown Shift` → сообщение `modifiers{Shift:true}`, повторный `keydown` не дублирует, `keyup` → `Shift:false`, `blur` → все false.
- Go: `draw_test` на подписи key bar для каждой таблицы, включая сброс залипания.
- Проверка согласованности: тест, что каждая непустая подпись соответствует обработчику в `far_keys.go`/`function()` (таблица «подпись → действие» единая, из неё строятся и key bar, и часть F1-справки).

## 7. План реализации

### Фаза 0 — рефакторинг (без новых функций)

1. `panelBackend`, `backendKind`, `panelCaps`; `localBackend`, `archiveBackend`, `remoteBackend`.
2. Перенос по таблице 4.3, удаление полей `Remote`, `RemoteLocalPath`, `Archive` из `Panel`.
3. `remotefs.Endpoint{Opener}`.
4. `documentSource` и перевод `openRemoteDocument`/`saveRemoteDocument`/архивного просмотра на него.
5. `terminal.Stream` и `App.Foreign` (без реализаций, кроме `Session`).
6. Key bar с модификаторами (раздел 6) для local/archive/remote.
7. Проверка 4.5.

### Фаза 1 — кластер, namespaces, объекты, манифесты

1. Зависимости client-go, `THIRD_PARTY_NOTICES.txt`, обновление CI-кэша.
2. `internal/kube`: profile, client, contexts, discovery, snapshot (Table), object, manifest (Get, ExportYAML, Decode, Delete), describe, logs. Тесты на `httptest`.
3. `Settings.KubeProfiles`, `kubernetes.json`, пункт в Network connections, диалог подключения, disconnect.
4. `kubeBackend`: уровни root/namespace/typeFolder/object, `tree.go` (прозрачные типы, селекторные дети), `ObjectInfo`, статус в `panel_draw.go`, key bar, меню.
5. F3/F4 манифестов через `documentSource` (F4 с Update и конфликтом), F5 объект → local и рекурсивный экспорт папок, F8, F7 namespace, Shift+F4, Shift+F3.
6. Документация: README, F1-справка, `MENU_PARITY.md`, CHANGELOG.

### Фаза 2 — контейнеры

1. `exec.go`, `containerfs.go` с probe; тесты на локальном `sh` через инжектированный `Executor`.
2. `Opener` для контейнера, маршрутизация F5/F6/F7/F8 в ФС контейнера.
3. Уровни containers/containerFS, правило C3.
4. `ExecStream`, `Execute` в контейнере, «Open shell in container», follow-логи, приглашение `pod:`.
5. F3/F4 файлов контейнера.

### Фаза 3 — apply и расширения

1. Apply при копировании в namespace (local → k8s, k8s → k8s), вопросы Override/Apply/Skip.
2. Scale, Restart rollout, Port-forward (`k8s.io/client-go/tools/portforward`), восстановление панели из сессии, Info panel, Quick view, `du`, потоковое копирование деревьев одним `tar`, ephemeral debug-контейнер, объединённые логи всех Pod владельца.

## 8. Тестирование

- `internal/kube`: `httptest`-сервер с discovery, Table-LIST, GET/PUT/PATCH/DELETE, logs; `tree.go`: схлопывание ReplicaSet (включая старые ReplicaSet после rollout), ControllerRevision под StatefulSet, Service → Pod по селектору (в том числе пустой селектор = нет детей), CronJob → Job → Pod, объект с несколькими владельцами, CRD без правил; имена папок типов с конфликтом групп; чистый экспорт; рекурсивный экспорт папок; многодокументный Decode; конфликт Update; forbidden → Skipped.
- `ContainerFS`: все три варианта листинга, symlink на каталог, имена с пробелами и не-ASCII, отсутствие shell/tar.
- `internal/ui`: навигация по уровням с кэшем, C3, key bar по уровням и модификаторам, маршрутизация F5 по таблице C4, удаление namespace с вводом имени, `Execute` формирует команду с корректным quoting, `Foreign`-поток перехватывает ввод и возвращает shell, профили не трогают `connections.json`.
- `./scripts/check.sh --race` без обращений к реальному кластеру; `FARSHMAC_KUBE_CHECK` — opt-in.

## 9. Принятые решения по открытым вопросам

| Вопрос | Решение | Обоснование |
|--------|---------|-------------|
| Cluster-scoped объекты (`_cluster`) | Не делаем. | Инструмент — dev tool для быстрого доступа к объектам приложения, не средство обслуживания кластера. |
| Раскладка namespace и набор ресурсов | Папки по типу ресурса; внутри объекта — папки дочерних типов; ReplicaSet и ControllerRevision схлопываются; Service показывает Pod по селектору. Чёрный список типов не нужен: `events` — обычная папка. | Папка типа изолирует шум любого типа и повторяет привычное `kubectl get <type>`; схлопывание задано в коде (`tree.go`), чтобы дерево было одинаковым и неглубоким. |
| Describe | `k8s.io/kubectl/pkg/describe`. | Покрывает все встроенные типы и события; рост бинарника умеренный. |

Открытых вопросов нет. Следующий шаг — фаза 0 (раздел 7).
