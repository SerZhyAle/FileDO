# Спецификация: попадание FileDO в GitHub-Store

Цель: сделать FileDO видимым и устанавливаемым в каталоге [OpenHub-Store / GitHub-Store](https://github.com/OpenHub-Store/GitHub-Store) — open-source app store для Android и Desktop (Linux/macOS/Windows) на Kotlin + Compose Multiplatform.

## 1. Как работает GitHub-Store (ресерч)

Источники: [README](https://github.com/OpenHub-Store/GitHub-Store/blob/main/README.md), [docs/README-RU](https://github.com/OpenHub-Store/GitHub-Store/blob/main/docs/README-RU.md), [roadmap/E1_BACKEND_HANDOFF](https://github.com/OpenHub-Store/GitHub-Store/blob/main/roadmap/E1_BACKEND_HANDOFF.md).

### 1.1 Модель каталога
- **Нет ручной модерации, нет манифеста, нет PR в репо стора.** Приложение появляется автоматически.
- Бэкенд `api.github-store.org` (open-source, self-hostable) ходит в публичный **GitHub Search API**, фильтрует репозитории по критериям и кэширует результат.
- Клиент (Android / Desktop) тянет данные с бэкенда, скачивает релиз напрямую из GitHub (с опциональным mirror-fallback и SHA-256 верификацией против `digest` ассета).

### 1.2 Жёсткие критерии включения
1. Репозиторий **публичный**.
2. **Последний релиз** содержит хотя бы один «устанавливаемый» ассет с поддерживаемым расширением:
   - **Windows**: `.exe`, `.msi`
   - **macOS**: `.dmg`, `.pkg`
   - **Linux**: `.deb`, `.rpm`, `.AppImage`, `.pkg.tar.zst`
   - **Android**: `.apk`
3. Авто-сгенерированные source-архивы GitHub (`*.tar.gz`, `*.zip` для исходников) **игнорируются**.
4. Репозиторий должен находиться через GitHub Search (т.е. не закрыт от индексации).

### 1.3 Сигналы ранжирования (Trending / Hot Release / Most Popular)
- GitHub topics, соответствующие платформе:
  - Desktop/Windows: `desktop`, `windows`, `linux`, `macos`, `compose-desktop`, `electron`
  - Android: `android`, `mobile`, `apk`
- `language` репозитория.
- `description` репозитория (попадает в полнотекстовый поиск).
- Звёзды (`stargazers_count`) — попадание в bucket'ы ранжирования.
- Сигнал «has APK assets in last 5 releases» (по аналогии для desktop — наличие совместимых ассетов в последних релизах).
- Свежесть `published_at`.

## 2. Аудит текущего состояния FileDO

Источник: GitHub API `/repos/SerZhyAle/FileDO` и `/releases` на 2026-05-15.

| Критерий | Требование | FileDO сейчас | Статус |
|---|---|---|---|
| Видимость | public | public | OK |
| Лицензия | любая совместимая | MIT | OK |
| Поддерживаемое расширение в latest release | `.exe` / `.msi` | только `FileDO-*-windows-x64.zip` | **BLOCKER** |
| `description` репо | непустая | `null` | **BLOCKER (ранжирование)** |
| `topics` репо | релевантные платформе | `[]` (пусто) | **BLOCKER (ранжирование)** |
| SHA-256 ассета | в release.body или `digest` | в `body` + `*.sha256` файл, плюс заполнен `digest` ассета | OK |
| Звёзды | желательно «a few» | 2 | низко, но не блокер |
| Стабильные релизы | `prerelease: false` | OK | OK |
| Имя ассета содержит платформу | поможет матчингу | да (`windows-x64`) | OK |

**Главный блокер**: ZIP-портабл не распознаётся сторoм как установочный артефакт. Без `.exe` или `.msi` репо не попадёт даже в поиск по платформе Windows.

## 3. Требования к реализации

Изменения сгруппированы по приоритету.

### P0 — обязательные для попадания в каталог

1. **Публиковать MSI рядом с ZIP.** В `.github/workflows/release.yml` добавить шаг, который собирает MSI из стейджа `dist/FileDO/` (WiX или `dotnet tool install -g wix` / готовый action). Файл должен называться по схеме `FileDO-<version>-windows-x64.msi`. Прицепить к релизу. Соответствует уже существующему winget-флоу (см. [docs/how-i-posted-this-project-to-winget.md](docs/how-i-posted-this-project-to-winget.md)) — пакет идентичный, переиспользуется.
2. **Добавить repo description** через GitHub UI / API. Один абзац, ≤ 200 символов: что делает (file/disk operations, capacity test, fill, check, duplicates), под какую платформу, что CLI. Описание попадает в поиск.
3. **Проставить repo topics** через GitHub UI (Settings → About → Topics) или API:
   - `windows`, `desktop`, `cli` — для платформенного матчинга в сторе
   - `file-management`, `disk-utility`, `capacity-test`, `duplicate-finder`, `go`, `golang` — для тем/поиска
   - Минимум: `windows`, `desktop`, `cli`.

### P1 — улучшают ранжирование и UX в каталоге

4. **Гарантировать SHA-256 в `digest` ассета.** Уже есть (`"digest": "sha256:..."` приходит из GitHub автоматически для крупных ассетов). Проверять, что MSI тоже его получает.
5. **Не помечать релизы `prerelease`.** Сейчас `false` — оставить так.
6. **README с иконкой и скриншотами.** GitHub-Store не парсит manifest, но при отображении карточки приложения может тянуть README/OpenGraph. Добавить:
   - PNG-иконку 256×256 в `assets/icon.png` (если её нет в репо) и ссылку из README.
   - Один-два скриншота интерфейса (для CLI — скрин терминала с типичной командой) в `docs/screenshots/`.
   - В верх README — одну строку с описанием (тот же текст, что и repo description), это часто используется агрегаторами как fallback.
7. **GitHub social preview image** (Settings → Social preview) — карточка 1280×640. Некоторые сторы используют её как hero для карточки приложения.

### P2 — опциональные ускорения попадания

8. **Подать репо вручную через бэкенд `/v1/external-match`** (если у стора есть форма submission на сайте `github-store.org`) — это сократит время до индексации с цикла кэша до минут. Если формы нет — пропустить, индексер найдёт через 1–2 цикла кэша.
9. **Кода-сигнатура MSI** (sigstore / signtool с самоподписью) — для будущей политики стора по доверию.

## 4. План реализации и статус

| # | Шаг | Статус |
|---|---|---|
| 1 | Repo description + topics (`windows`, `desktop`, `cli`, `file-management`, `disk-utility`, `capacity-test`, `duplicate-finder`, `sd-card`, `usb`, `golang`) | **Сделано** через `gh repo edit` |
| 2 | WiX-манифест [packaging/wix/FileDO.wxs](../packaging/wix/FileDO.wxs) — 4 exe, 5 bat, LICENSE, README; добавляет `INSTALLFOLDER` в системный `PATH`; per-machine install в `C:\Program Files\FileDO\`; ARP-метаданные | **Сделано** |
| 3 | Шаги `Install WiX` + `Build MSI` в [.github/workflows/release.yml](../.github/workflows/release.yml) после `Package zip`; MSI прицепляется к релизу вместе с `.sha256` файлом, SHA-256 пишется в release body | **Сделано** |
| 4 | Иконка (`assets/icon.png` 256×256) и скриншоты в README | TODO |
| 5 | GitHub social preview (1280×640) | TODO (только через UI: Settings → Social preview) |
| 6 | Submission через `/v1/external-match` или issue в стор, если индексер не подхватит | TODO (после первого MSI-релиза) |

### Детали реализации MSI

- **Версия MSI** — `vMaj.vMin.vPat.vBld`, например `26.4.27.2228` (парсится из тега `v2604272228` существующим шагом `Resolve version`).
- **UpgradeCode** — фиксированный GUID `4d6b3b1f-7c8e-4a25-9f1d-8e3b2c5a7d91`, не меняется между релизами; ProductCode авто-генерируется WiX каждой сборкой → `MajorUpgrade` работает.
- **Caveat:** MSI учитывает только `Major.Minor.Build` для апгрейда; две сборки в один день (одинаковый `vPat=dd`) различаются только Revision и `MajorUpgrade` их не отличит. Релизы обычно реже — приемлемо. Если станет проблемой, поменять маппинг версии в шаге `Resolve version` (например `vMin = MM*100+dd`).
- **PATH** — компонент `EnvPath` добавляет `[INSTALLFOLDER]` в системный `PATH` (`Part="last" Action="set" System="yes"`). Деинсталляция возвращает PATH в исходное состояние.
- **Зависимости рантайма** — нет, Go-бинари статичны (`CGO_ENABLED=0`).
- **WiX** — версия 5.x, ставится `dotnet tool install --global wix --version 5.*` в свежем шаге workflow.

### Как проверить локально

```pwsh
dotnet tool install --global wix --version 5.*
# Сначала прогнать сборку (`Build binaries` + `Package zip` руками), чтобы dist\FileDO\ был заполнен.
wix build packaging\wix\FileDO.wxs -arch x64 -d "ProductVersion=26.4.27.2228" -d "StageDir=$PWD\dist\FileDO" -o dist\FileDO-test.msi
```

## 5. Acceptance criteria (как понять, что готово)

- [ ] `gh release view --json assets` для последнего релиза показывает файл с расширением `.msi`.
- [ ] `GET https://api.github.com/repos/SerZhyAle/FileDO` возвращает непустые `description` и `topics`, содержащие хотя бы `windows` и `desktop`.
- [ ] MSI ставится двойным кликом на чистой Windows 11, добавляет `filedo` в `PATH` (или хотя бы кладёт в Program Files).
- [ ] SHA-256 MSI совпадает между release body, `*.sha256` файлом и `digest` поля ассета GitHub API.
- [ ] Через 24–48 часов после релиза репо находится в клиенте GitHub-Store по запросу «filedo» или «disk capacity».

## 6. Открытые вопросы

- Поддерживает ли GitHub-Store портативные `.zip` через какой-то отдельный канал? По текущей доке — **нет**, но может появиться (см. их roadmap). Если появится — `.zip` можно оставить как есть, а MSI всё равно сделать ради winget/обычных пользователей.
- Нужна ли подпись MSI Authenticode для попадания? В текущей доке требований к подписи ассетов приложений нет (подписан только сам клиент стора).
