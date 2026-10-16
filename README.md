<div align="center">

<img alt="3X-UI KIT" src="manuals/assets/banner.svg" width="820">

**Установка 3X-UI одной командой: все протоколы, один порт 443, одна подписка на всё**

[![Протоколов](https://img.shields.io/badge/протоколов-11-93E06F?labelColor=221B17)](manuals/3x-ui.md#протоколы)
[![3X-UI](https://img.shields.io/badge/3X--UI-v3.8.5-93E06F?labelColor=221B17)](https://github.com/MHSanaei/3x-ui)
[![Обновлено](https://img.shields.io/github/last-commit/itsnotkubrick/3X-UI_KIT?label=обновлено&color=93E06F&labelColor=221B17)](https://github.com/itsnotkubrick/3X-UI_KIT/commits)

[Возможности](#возможности) · [Установка](#установка) · [Команды](#команды-kit) · [Роутер](#vpn-на-роутере) · [Генератор](#генератор-конфигов-для-роутера) · [Полезное](#полезное) · [Поддержать](#поддержать-проект)

</div>

---

> [!TIP]
> **Что нового в форке (CDN-маскировка):** панель, личный кабинет и подписка теперь работают
> **через Cloudflare** (по CDN-домену), а по голому IP и по основному домену отдаётся
> безобидный сайт-заглушка. Так реальный IP сервера не светится и реже попадает в блок.

> [!NOTE]
> **Проект – набор скриптов с открытым кодом.** Он не оказывает услуг связи и не предоставляет доступ
> к серверам. Вы ставите его на собственный сервер и сами отвечаете за соблюдение законодательства
> своей страны.

**3X-UI KIT** превращает чистый VPS в готовый VPN-сервер за несколько минут. Скрипт ставит
официальную панель [3X-UI](https://github.com/MHSanaei/3x-ui), настраивает все популярные
протоколы, сертификат и файрвол и выдаёт одну ссылку-подписку. Её можно вставить в любое
приложение – оно само получит подходящие ему протоколы. Домен не обязателен.

<div align="center">
<img alt="Конец установки 3X-UI KIT" src="manuals/assets/script-3x-ui.svg" width="760">
</div>

## Возможности

- 🧩 **10 протоколов сразу + MTProto по запросу** – VLESS REALITY, XHTTP, WebSocket, Trojan gRPC, VMess,
  Shadowsocks 2022, Hysteria2, TUIC и AmneziaWG (классика и 3.1). MTProto для Telegram **не ставится
  по умолчанию** (прямой и легко распознаётся ТСПУ, а ссылку `tg://` легко расшарить) — включается
  флагом `--protocols all,mtproto`; ссылку можно посмотреть `kit user link имя --all`.
- 🚪 **Всё TCP – через порт 443.** Панель, подписка и протоколы спрятаны за одним портом.
- 🕵️ **Маскировка под обычный сайт.** По голому IP и по основному домену отдаётся сайт-заглушка
  (кофейня, блог, портфолио) — активное сканирование не видит VPN-кабинет.
- ☁️ **Панель, личный кабинет и подписка — через Cloudflare.** По CDN-домену, реальный IP сервера
  не попадает в клиентские ссылки и в DNS клиентских имён.
- 🌐 **Личный кабинет пользователя** – отдельный защищенный веб-портал с авторизацией,
  просмотром расхода трафика, срока действия, сменой пароля и инструкциями по подключению.
- 📦 **Sing-box подписка с умной маршрутизацией** – автоматический DoH 1.1.1.1, прямой доступ к РФ сайтам
  и банкам, блокировка рекламы, глушение QUIC для YouTube и автовыбор самого быстрого сервера (URLTest).
- 🔗 **Одна подписка на все приложения.** Hiddify, Happ, v2rayN, Karing, Clash Verge, FlClash и Sing-box
  получают свой формат и только те протоколы, которые умеют.
- 👥 **Дополнительные пользователи одной командой** – `kit user add` добавляет пользователя
  сразу во все протоколы, автоматически заводит ему аккаунт в личном кабинете с надежным паролем.
- 🔄 **Обновляется сам, но только подписанными релизами**, а `kit backup` сохраняет
  копию сервера с пользователями и ключами.
- 🔒 **Сертификат Let's Encrypt** выпускается и продлевается сам, админ-панель 3X-UI полностью изолирована
  от публичного доступа.
- ✅ **Проверено настоящими клиентами** – каждый протокол на ядрах Xray, Mihomo и sing-box,
  в том числе через интернет к удалённому серверу: [tests/matrix](tests/matrix/).

## Что понадобится

- VPS с **Ubuntu 22.04/24.04** или **Debian 12/13** и доступом root по SSH.
- Два имени (поддомена) и управление DNS на **Cloudflare** — см.
  [подготовку ниже](#подготовка-cloudflare).
- A-записи обоих имён ведут на IP этого сервера.
- Свободные порты **443** и **80** – на свежем сервере они свободны.
- **Чистый (не заблокированный) IP.** Лучше свежий и отдельный под VPN — «засвеченные»
  ранее адреса переиспользовать не стоит.

Нужен VPS? Я сам использую **[IS Hosting](https://ishosting.io/affiliate/NTg4MiM4)** и рекомендую его
(реферальная ссылка).

## Подготовка сервера

1. **Свежий VPS** с Ubuntu 22.04/24.04 или Debian 12/13, root по SSH.
2. Убедитесь, что на нём **нет своего nginx/apache** и порты **80** и **443** свободны
   (установщик сам их освободит, но чище, когда ничего не мешает):
   ```bash
   ss -ltnp '( sport = :80 or sport = :443 )'
   ```
3. Обновите пакеты (по желанию — установщик подождёт автообновления системы):
   ```bash
   apt-get update && apt-get -y upgrade
   ```
4. Больше ничего готовить не нужно: панель, сертификат, файрвол и подписку ставит скрипт.

## Подготовка Cloudflare

Домен переводите на Cloudflare (меняете NS на выданные Cloudflare). Затем в разделе **DNS → Records**:

| Тип | Имя | Значение | Проксирование |
|---|---|---|---|
| A | `example.com` | IP сервера | **DNS only** (серое облако) |
| A | `ws.example.com` | IP сервера | **Proxied** (оранжевое облако) |
| A (опционально) | `wc.example.com` | IP сервера | **Proxied** (если делаете отдельный `--web-domain`) |

- **Основной домен** (`example.com`) — **серое облако**. На него подключаются прямые протоколы
  (REALITY, Trojan, Hysteria2 и т.д.) и он же отдаёт сайт-заглушку.
- **CDN-поддомен** (`ws.example.com`) — **оранжевое облако**. Через него идут WebSocket
  (VMess/VLESS), а также панель, кабинет и подписка.
- В **SSL/TLS → Overview** выберите режим **Full** (не Flexible и не Full strict: сертификат на
  сервере выпущен на основной домен, а не на поддомен).
- В **Network** WebSockets должны быть **включены** (по умолчанию включены).

> [!IMPORTANT]
> Cloudflare проксирует только порты HTTPS **443, 2053, 2083, 2087, 2096, 8443**. Установщик
> использует 443, поэтому ничего менять в портах не нужно.

## Установка

Подключитесь к серверу по SSH и выполните:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/Qislord/3X-UI_KIT_SING_BOX/main/scripts/3x-ui.sh) \
  --domain example.com \
  --ws-domain ws.example.com \
  --protocols reality,ws,vmess
```

Это **рекомендуемая** команда. Она ставит только три самых устойчивых к блокировкам протокола —
**REALITY + VLESS-WS + VMess-WS**, кабинет/панель/подписка уходят за Cloudflare, а по IP и по
`example.com` отдаётся сайт-заглушка.

> [!WARNING]
> Без `--protocols` установщик поставит почти все протоколы (кроме WireGuard и MTProto) и **откроет
> их порты**, даже если никто ими не пользуется — а это лишние «зацепки» для блокировки IP. Для
> тихого сервера **всегда указывайте список**. Если сомневаетесь — берите `--protocols reality,ws,vmess`.

Хотите разные домены для WebSocket и для панели/подписки — добавьте `--web-domain`:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/Qislord/3X-UI_KIT_SING_BOX/main/scripts/3x-ui.sh) \
  --domain example.com \
  --ws-domain ws.example.com \
  --web-domain wc.example.com \
  --protocols reality,ws,vmess \
  -y
```

### Что означают параметры доменов

- **`--domain example.com`** — основной домен (DNS only, смотрит на IP сервера):
  - на нём **сайт-заглушка**, и на этот адрес подключаются прямые протоколы (REALITY, Trojan-gRPC);
  - сертификат Let's Encrypt выпускается и продлевается сам.
- **`--ws-domain ws.example.com`** — CDN-поддомен (Proxied, SSL Full):
  - VMess-WS и VLESS-WS автоматически переключаются на него (адрес, SNI, заголовок `Host`);
  - трафик идёт через сеть Cloudflare — ТСПУ/DPI видят только адреса Cloudflare.
- **`--web-domain wc.example.com`** (необязательный) — отдельный CDN-домен для панели,
  кабинета и подписки. Если не указан, используется `--ws-domain`.

> [!NOTE]
> **Прямые протоколы (REALITY/Hysteria2/TUIC/…) по-прежнему подключаются к реальному IP** — их
> Cloudflare проксировать не умеет. Поэтому основной домен держите в режиме **DNS only**, а
> «прятать» узел помогает сайт-заглушка. Полностью скрыть origin позволяет только
> CDN-путь (WebSocket), см. `FixVulnerability.md`.

### Что попадает в подписку

Чтобы узел реже уходил в блок, в публичную подписку по умолчанию входят **только самые устойчивые
протоколы**:

- **VLESS REALITY** — прямой, самый живучий к активному зондированию;
- **VLESS WebSocket** и **VMess WebSocket** — через Cloudflare.

Остальные протоколы (XHTTP, Trojan-gRPC, Hysteria2, TUIC, Shadowsocks, AmneziaWG, MTProto) в
рекомендуемой команде **на сервер не ставятся вообще**. Если же вы их поставили, они всё равно
**пользователю не выдаются**, пока вы не подключите их вручную:

```bash
kit user attach sasha hy2   # выдать Саше Hysteria2
kit user attach sasha awg   # AmneziaWG (классика и 3.1)
kit user attach sasha ss    # Shadowsocks
```

> [!TIP]
> Не нужны какие-то протоколы совсем — не устанавливайте их: `--protocols reality,ws,vmess`.
> Так их порты (например UDP 51821/51822 у AmneziaWG и 8388 у Shadowsocks) вообще не откроются,
> и сканировать будет нечего.

### Как добавить протокол на сервер позже

Это два разных действия — не путайте:

**1. Протокола нет на сервере** (его не было в `--protocols`). Сначала добавьте его на сервер —
повторный запуск установщика с нужным списком и `--force` (подключения, пользователи и ключи
не теряются):

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/Qislord/3X-UI_KIT_SING_BOX/main/scripts/3x-ui.sh) \
  --domain example.com \
  --ws-domain ws.example.com \
  --protocols reality,ws,vmess,hy2 \
  --force
```

Появится подключение Hysteria2 и откроется его порт. Пользователям оно пока не выдаётся.

**2. Выдать протокол конкретному пользователю.** Даже когда протокол уже есть на сервере, в
подписку он попадает только после явной привязки:

```bash
kit user attach sasha hy2      # у Саши в подписке появится Hysteria2
```

Клиенту нужно обновить подписку в приложении. Так протокол получает один человек, а не все.

> [!NOTE]
> Убрать протокол у пользователя можно в панели 3X-UI (снять клиента с нужного подключения).
> Полностью **удалить** протокол с сервера автоматически нельзя — удалите его в панели
> (`x-ui` → Подключения) или пересоздайте сервер.

> Если домена пока нет или ставите на чистый IP, можно запустить без флагов:
> `bash <(curl -fsSL https://raw.githubusercontent.com/Qislord/3X-UI_KIT_SING_BOX/main/scripts/3x-ui.sh)`

Через пару минут скрипт покажет адрес панели, логин, пароль и подписку с QR-кодом.

Повторный запуск для смены доменов:
```bash
bash <(curl -fsSL https://raw.githubusercontent.com/Qislord/3X-UI_KIT_SING_BOX/main/scripts/3x-ui.sh) \
  --domain example.com --ws-domain ws.example.com --force
```

Подробно – подключение приложений, дополнительные пользователи и параметры –
в **[инструкции](manuals/3x-ui.md)**. Нужен только Hysteria2 – есть
[отдельный скрипт](manuals/hysteria2.md).

> [!WARNING]
> Проект создан в образовательных целях. Убедитесь, что ваши действия
> соответствуют законодательству вашей страны.

## Команды kit

После установки на сервере появляется команда `kit`. Подробности – в [инструкции](manuals/3x-ui.md).

| Команда | Что делает |
|---|---|
| `kit user add имя [--gb 50] [--days 30] [--devices 3] [--all]` | добавить пользователя и подписку (по умолчанию — только REALITY + WebSocket; `--all` — сразу все протоколы) |
| `kit user attach имя hy2` | выдать пользователю дополнительный протокол по запросу (`hy2, tuic, ss, awg, awg3, xhttp, trojan, mtproto, wg`) |
| `kit user list` | кто сколько израсходовал, до какого числа и когда был в сети |
| `kit user link имя [--all]` | показать подписку ещё раз; `--all` добавляет ссылки `vpn://` и `tg://` |
| `kit user limit имя [--gb N] [--days N] [--devices N]` | изменить лимиты (`0` – без ограничений) |
| `kit user passwd имя [пароль]` | изменить пароль пользователя от личного кабинета |
| `kit user token имя` | сбросить и обновить ссылку на подписку Sing-box |
| `kit user off имя` / `kit user on имя` | временно выключить и включить пользователя |
| `kit user del имя` | удалить пользователя и закрыть доступ в кабинет |
| `kit check` | проверить сервер: службы, сертификат, подписку, сайт маскировки, права на файлы (ничего не меняет) |
| `kit fix [--dry-run]` | исправить безопасное: перезапустить службы, вернуть права, включить автообновление |
| `kit update [--manual \| --auto]` | обновить сейчас; `--manual` выключает автообновление, `--auto` включает |
| `kit backup` | сохранить копию сервера (подключения, ключи, пользователи) |
| `kit version` | версии `kit`, панели и ядра, состояние автообновления |

Для отдельного сервера Hysteria2 есть команда `hy2` (`hy2 add`, `hy2 del`, `hy2 list`, `hy2 link`, `hy2 status`,
`hy2 restart`, `hy2 update`, `hy2 uninstall`), подробнее – в [инструкции по Hysteria2](manuals/hysteria2.md).
Панелью 3X-UI управляет команда `x-ui`.

## VPN на роутере

Через прокси идут только нужные сервисы и выбранные устройства, остальное работает напрямую.
Порядок: **1.** установить программу на роутер → **2.** собрать конфиг в [генераторе](#генератор-конфигов-для-роутера) →
**3.** вставить команду из генератора.

> [!NOTE]
> **🙏 Спасибо автору XKeen.** На роутере работает не моя программа: я только собрал инструкцию
> и генератор конфигов, а всю сложную работу сделал **jameszeroX**.
>
> **[XKeen](https://github.com/jameszeroX/XKeen)** – прокси на Keenetic (Xray и Mihomo, политики для устройств).
> [Репозиторий](https://github.com/jameszeroX/XKeen) · [Вики](https://github.com/jameszeroX/XKeen/wiki) · ⭐ [Поставить звезду](https://github.com/jameszeroX/XKeen)
>
> Вопросы по самой программе задавайте в её репозитории: так автор узнаёт об ошибках. Если что-то не так
> с моей инструкцией или генератором, пишите мне.

<details>
<summary><b>📶 Keenetic: XKeen (jameszeroX) · 8 шагов, 20–30 минут</b></summary>

Нужны роутер **Keenetic** или **Netcraze** с USB-портом, USB-флешка в формате **ext4** и свой сервер с VLESS
(например, из [этой инструкции](manuals/3x-ui.md)).

<details>
<summary><b>1. Компоненты KeeneticOS</b></summary>

В веб-интерфейсе: **Управление → Общие настройки → Изменить набор компонентов**. Отметьте компоненты
с галочками на картинке и установите. Роутер обновится и перезагрузится.

![Нужные компоненты KeeneticOS](manuals/assets/keenetic-components.svg)

</details>

<details>
<summary><b>2. Entware на флешку</b></summary>

1. Отформатируйте флешку в **ext4** и вставьте её в роутер.
2. Откройте её по сети (`\\192.168.x.x\`), создайте папку `install` и положите туда установщик под процессор роутера:
   [mipsel](https://bin.entware.net/mipselsf-k3.4/installer/mipsel-installer.tar.gz),
   [mips](https://bin.entware.net/mipssf-k3.4/installer/mips-installer.tar.gz) или
   [aarch64](https://bin.entware.net/aarch64-k3.10/installer/aarch64-installer.tar.gz).
   Не знаете, какой нужен? Модель роутера – в характеристиках на сайте Keenetic.

![Установщик Entware на флешке](manuals/assets/entware-installer.svg)

3. **Управление → OPKG**: выберите флешку и сохраните. Установка займёт несколько минут.

![Выбор накопителя для OPKG](manuals/assets/keenetic-opkg.svg)

</details>

<details>
<summary><b>3. Подключение по SSH</b></summary>

```bash
ssh root@192.168.x.x -p 222
```

Пароль по умолчанию `keenetic`. **Сразу смените его** командой `passwd`.

</details>

<details>
<summary><b>4. Шифрованный DNS</b></summary>

Пропишите шифрованные DNS-серверы (DoT или DoH) по
[инструкции Keenetic](https://support.keenetic.ru/ultra/kn-1811/ru/31543-dot-and-doh-proxy-servers-for-dns-requests-encryption.html):
без этого XKeen работает неправильно.

</details>

<details>
<summary><b>5. Токен для KeeneticOS 5.2 и новее</b></summary>

XKeen нужен токен доступа к роутеру. Создайте его в разделе **Пользователи и доступ**, вставьте в
[шаблон xkeen.json](https://github.com/jameszeroX/XKeen/releases/download/2.0.1_Beta/xkeen.json) и положите файл на роутер
по пути `/opt/etc/xkeen/xkeen.json`. Подробнее – в [вики XKeen](https://github.com/jameszeroX/XKeen/wiki/Порядок-установки).

</details>

<details>
<summary><b>6. Установка XKeen</b></summary>

Подключитесь по SSH и выполните:

```bash
opkg update && opkg upgrade && opkg install curl tar && cd /tmp
sh -c "$(curl -sSL https://raw.githubusercontent.com/jameszeroX/XKeen/main/install.sh)"
```

Если GitHub недоступен, замените адрес на `https://cdn.jsdelivr.net/gh/jameszeroX/XKeen@main/install.sh`.
Установщик спросит ядро (Xray или Mihomo), геобазы и автозагрузку.

Затем соберите конфиг в [генераторе](#генератор-конфигов-для-роутера) (роутер Keenetic, нужное ядро), вставьте команду в SSH-консоль
и нажмите Enter: файлы запишутся, XKeen перезапустится.

![Конфигурационные файлы Xray](manuals/assets/xkeen-configs.svg)

</details>

<details>
<summary><b>7. Какие устройства через прокси</b></summary>

В веб-интерфейсе откройте **Приоритеты подключений → Политики доступа в Интернет**, создайте политику
с именем **`xkeen`** и перенесите в неё нужные устройства.

> [!WARNING]
> Без политики `xkeen` через прокси пойдёт трафик **всех** устройств в сети.

</details>

<details>
<summary><b>8. Проверка</b></summary>

- На роутере: `xkeen -status` – XKeen должен быть запущен.
- На устройстве из политики `xkeen` откройте сайт, показывающий IP-адрес: он должен совпасть с адресом вашего сервера,
  а на остальных устройствах остаться прежним.
- Удобнее управлять настройками из браузера через [XKeen UI](https://github.com/zxc-rv/XKeen-UI).

</details>

Подробная версия с теми же шагами: [инструкция для Keenetic](manuals/xkeen-keenetic.md).

</details>

## Генератор конфигов для роутера

Вставьте ссылку на сервер или подписку, выберите роутер и ядро, отметьте нужные сервисы –
и получите готовый конфиг и одну команду, которая сама положит его на роутер.
Всё считается в браузере, ссылки никуда не отправляются.
Как поставить XKeen на роутер – в разделе [VPN на роутере](#vpn-на-роутере).

| Роутер | Ядро | Что получится | Генератор |
|---|---|---|---|
| Keenetic (XKeen) | Xray | `04_outbounds.json` и `05_routing.json`: серверы, выбор сервисов, реклама, свои сайты | [Открыть →](https://itsnotkubrick.github.io/3X-UI_KIT/tools/?core=xray) |
| Keenetic (XKeen) | Mihomo | `config.yaml` с автовыбором сервера, подпиской, Hysteria2, AmneziaWG и веб-панелью | [Открыть →](https://itsnotkubrick.github.io/3X-UI_KIT/tools/?core=mihomo) |
| OpenWrt (Nikki), бета | Mihomo | профиль для Nikki и команда, которая его включит | [Открыть →](https://itsnotkubrick.github.io/3X-UI_KIT/tools/?router=openwrt) |

## Полезное

- [XKeen](https://github.com/jameszeroX/XKeen) и его [вики](https://github.com/jameszeroX/XKeen/wiki) – документация по маршрутизации на Keenetic
- [XKeen UI](https://github.com/zxc-rv/XKeen-UI) – веб-интерфейс для XKeen
- [IP-адреса для AmneziaWG](https://github.com/RockBlack-VPN/ip-address) – актуальные списки от RockBlack

## Поддержать проект

Скрипты и инструкции бесплатные. Донат добровольный – он помогает оплачивать
тестовые серверы и держать скрипты в актуальном состоянии. Спасибо! 💜

| Способ | |
|---|---|
| Российской картой, СБП, Tinkoff Pay | [CloudTips](https://pay.cloudtips.ru/p/d4f9e3d1) |
| Зарубежной картой, Apple Pay, Google Pay | [Buy Me a Coffee](https://buymeacoffee.com/relo.cate) |
| USDT (TRC-20) | `TS83ViXrdezUpp1eFadqj1rBhGLZaba1c1` |
| TON | `UQBchO4XFPwF9MMa_tjXpwqTo8IL2FhUDyllhYuFo8WM-Qbf` |
| Ethereum (ERC-20) | `0xC06F6B3A029d7Ea00705B7028490744e2BC16799` |

## Благодарности

3X-UI KIT построен на работе авторов этих проектов:
[3X-UI](https://github.com/MHSanaei/3x-ui) ·
[Xray-core](https://github.com/XTLS/Xray-core) ·
[Mihomo](https://github.com/MetaCubeX/mihomo) ·
[Hysteria](https://github.com/apernet/hysteria) ·
[AmneziaWG](https://github.com/amnezia-vpn) ·
[XKeen](https://github.com/jameszeroX/XKeen)
