#!/usr/bin/env bash
# kit – управление сервером 3X-UI KIT: пользователи, обновление, резервная копия.
# https://github.com/itsnotkubrick/3X-UI_KIT
#
#   kit user add имя [--gb 50] [--days 30] [--devices 3] [--all]
#   kit user attach имя протокол | list | link имя | limit имя [--gb N] [--days N] | off имя | on имя | del имя
#   kit update [--auto | --manual] | kit backup | kit check | kit fix | kit version

# Запуск через sh (dash) ломается на непонятной ошибке синтаксиса – подскажем сразу.
[ -n "${BASH_VERSION:-}" ] || { echo "Запустите через bash, а не через sh." >&2; exit 1; }

set -Eeuo pipefail
export LC_ALL=C.UTF-8  # ширина колонок по символам, а не байтам

KIT_VERSION="1.1.2"
KIT_REPO="${KIT_REPO:-Qislord/3X-UI_KIT_SING_BOX}"
KIT_BRANCH="${KIT_BRANCH:-main}"
KIT_RAW="${KIT_RAW:-https://raw.githubusercontent.com/$KIT_REPO/$KIT_BRANCH}"
kit_ref_raw() { echo "https://raw.githubusercontent.com/$KIT_REPO/v$1"; }

XUI_ENV=/etc/x-ui/install-result.env
KIT_ENV=/etc/kit/kit.env
KIT_LATEST=/etc/kit/latest-version
PORTAL_DB=/etc/kit/portal.db

if [[ -t 1 ]]; then
  G=$'\e[32m'; Y=$'\e[33m'; R=$'\e[31m'; B=$'\e[1m'; D=$'\e[2m'; N=$'\e[0m'
else
  G=; Y=; R=; B=; D=; N=
fi
say()  { printf '%s\n' "${G}==>${N} $*"; }
warn() { printf '%s\n' "${Y}!${N}  $*" >&2; }
die()  { printf '%s\n' "${R}✗${N}  $*" >&2; exit 1; }

[[ $EUID -eq 0 ]] || die "Запустите от root: sudo -i, затем команду ещё раз."
[[ -f $XUI_ENV && -f $KIT_ENV ]] || die "Не найдена установка – сначала поставьте сервер скриптом 3x-ui.sh."
# shellcheck disable=SC1090
. "$XUI_ENV"; . "$KIT_ENV"

API=""
for scheme in https http; do
  API="$scheme://127.0.0.1:$XUI_PANEL_PORT/$XUI_WEB_BASE_PATH/panel/api"
  curl -fsk -m 5 -o /dev/null -H "Authorization: Bearer $XUI_API_TOKEN" "$API/server/getNewUUID" 2>/dev/null && break
done

api() { # METHOD path [json]
  local out
  if [[ $1 == GET ]]; then
    out=$(curl -sSk -m 20 -H "Authorization: Bearer $XUI_API_TOKEN" "$API/$2")
  else
    out=$(curl -sSk -m 20 -H "Authorization: Bearer $XUI_API_TOKEN" -H 'Content-Type: application/json' -X "$1" -d "${3:-{\}}" "$API/$2")
  fi
  [[ $(jq -r '.success' <<<"$out" 2>/dev/null) == true ]] || die "Панель ответила ошибкой: $(jq -r '.msg // .' <<<"$out" 2>/dev/null | head -c 300)"
  jq -c '.obj' <<<"$out"
}

portal_py() {
  if [[ -x /usr/local/bin/kit-portal ]]; then
    /usr/local/bin/kit-portal "$@"
    return
  fi
  local p="/usr/local/lib/kit-portal/kit_portal.py"
  [[ -f $p ]] || p="$(dirname "${BASH_SOURCE[0]}")/kit-portal.py"
  if [[ -f $p ]]; then
    python3 "$p" "$@"
  fi
}

# В 3X-UI 3.x у клиента одна запись и в ней одна пара ключей WireGuard и один адрес. Если
# клиент подключён к двум AmneziaWG, в подписку для обоих уходят ключ и адрес одного из них,
# и второй сервер клиента не узнаёт (проверено 2026-09-27). Поэтому к первому AmneziaWG
# подключаем основную запись, а ко второму – запись-«двойник» «имя-awg» с подпиской «<id>-awg»
# (subId в 3X-UI обязан быть уникальным) и теми же лимитами; kit-sub подмешивает её в Clash.
awg_ids() { api GET inbounds/list | jq -r '[.[] | select(.protocol == "amneziawg") | .id] | sort | .[]'; }
non_awg_ids() { api GET inbounds/list | jq -c '[.[] | select(.protocol != "amneziawg") | .id]'; }
# Публичная подписка по умолчанию — только устойчивые протоколы: REALITY и WebSocket.
core_ids() { api GET inbounds/list | jq -c '[.[] | select(.remark == "REALITY" or .remark == "VLESS-WS" or .remark == "VMess-WS") | .id]'; }
# Протокол из команды → remark подключения в панели.
proto_remark() {
  case ${1,,} in
    reality) echo "REALITY" ;;
    xhttp) echo "XHTTP" ;;
    ws | vless-ws) echo "VLESS-WS" ;;
    vmess) echo "VMess-WS" ;;
    trojan) echo "Trojan-gRPC" ;;
    ss | shadowsocks) echo "Shadowsocks" ;;
    hy2 | hysteria2) echo "Hysteria2" ;;
    tuic) echo "TUIC" ;;
    wg | wireguard) echo "WireGuard" ;;
    mtproto) echo "MTProto" ;;
    *) return 1 ;;
  esac
}

awg_attach() { # имя subId [лимит-байт] [срок-мс] [устройств]
  local name=$1 sid=$2 total=${3:-0} exp=${4:-0} lim=${5:-0} n=1 id email have
  have=$(api GET clients/list | jq -c 'if type == "array" then . else .clients end')
  for id in $(awg_ids); do
    local esid=$sid
    if ((n == 1)); then email=$name
    elif ((n == 2)); then email="$name-awg"; esid="$sid-awg"
    else email="$name-awg$n"; esid="$sid-awg$n"; fi
    n=$((n + 1))
    if jq -e --arg e "$email" --argjson i "$id" 'any(.[]; .email == $e and ((.inboundIds // []) | index($i)))' <<<"$have" >/dev/null; then
      continue
    elif jq -e --arg e "$email" 'any(.[]; .email == $e)' <<<"$have" >/dev/null; then
      api POST "clients/$email/attach" "$(jq -nc --argjson i "$id" '{inboundIds: [$i]}')" >/dev/null
    else
      api POST clients/add "$(jq -nc --arg e "$email" --arg s "$esid" --argjson t "$total" --argjson x "$exp" --argjson l "$lim" --argjson i "$id" \
        '{client: {email: $e, subId: $s, totalGB: $t, expiryTime: $x, limitIp: $l, enable: true, comment: "kit"}, inboundIds: [$i]}')" >/dev/null
    fi
  done
}

clients() { api GET clients/list | jq -c 'if type == "array" then . else .clients end'; }
client() { clients | jq -c --arg e "$1" 'map(select(.email == $e))[0] // empty'; }
# Все записи пользователя: основная и «двойники» для AmneziaWG («имя-awgN»).
emails_of() { clients | jq -r --arg e "$1" '.[] | select(.email == $e or ((.email | startswith($e + "-awg")) and (.email[($e | length) + 4:] | test("^[0-9]*$")))) | .email'; }
valid_name() { [[ $1 =~ ^[A-Za-z0-9_.-]{1,32}$ ]] || die "Имя: латиница, цифры, _ . - (до 32 символов)."; }
rand_id() { openssl rand -base64 48 | tr -dc 'a-z0-9' | head -c 16; }

# Числа из командной строки: «08» не должно читаться как восьмеричное, а 30 цифр – переполнять счёт.
gb_bytes() { [[ ${1-} =~ ^[0-9]{1,6}$ ]] || die "--gb: целое число гигабайт (до 999999), например --gb 50"; echo $((10#$1 * 1073741824)); }
days_ms() { [[ ${1-} =~ ^[0-9]{1,5}$ ]] || die "--days: целое число дней (до 99999), например --days 30"; ((10#$1 == 0)) && { echo 0; return; }; echo $((($(date +%s) + 10#$1 * 86400) * 1000)); }
# Флаги вида --gb=50 превращаем в «--gb 50»; у флага без значения – понятная ошибка.
eq_args() { # печатает аргументы по одному
  local a
  for a in "$@"; do if [[ $a == --*=* ]]; then printf '%s\n%s\n' "${a%%=*}" "${a#*=}"; else printf '%s\n' "$a"; fi; done
}
need_val() { [[ -n ${2-} ]] || die "У параметра $1 нет значения."; }

human() { # байты → «1.2 ГБ»
  awk -v b="$1" 'BEGIN { split("Б КБ МБ ГБ ТБ", u, " "); i = 1; while (b >= 1024 && i < 5) { b /= 1024; i++ }
    printf (i == 1 ? "%d %s" : "%.1f %s"), b, u[i] }'
}

# Публичный IP этого сервера (для правки HOST при восстановлении).
public_ip() {
  local ip u
  for u in https://api.ipify.org https://ifconfig.me/ip https://ipv4.icanhazip.com; do
    ip=$(curl -4 -fsS -m 6 "$u" 2>/dev/null | tr -d '[:space:]') || true
    [[ $ip =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]] && { echo "$ip"; return; }
  done
  ip -4 route get 1.1.1.1 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="src") print $(i+1)}'
}

# Снимок SQLite через backup API: база может работать (WAL), копия остаётся целой.
sqlite_snapshot() { # файл-источник файл-назначение
  python3 - "$1" "$2" <<'PY' || return 1
import sqlite3, sys
src = sqlite3.connect("file:%s?mode=ro" % sys.argv[1], uri=True)
dst = sqlite3.connect(sys.argv[2])
src.backup(dst)
dst.close(); src.close()
PY
}

sub_url() { echo "${SUB_BASE}$1"; }

show_link() { # имя subId
  local url
  url=$(sub_url "$2")
  echo
  echo "Подписка ${B}$1${N} – все протоколы одной ссылкой. Вставьте в Happ, Hiddify, Karing,"
  echo "v2rayN, Clash Verge или FlClash:"
  echo
  echo "$url"
  echo
  command -v qrencode >/dev/null && qrencode -t ANSIUTF8 -m 1 "$url"
  echo "${D}AmneziaVPN и Telegram: kit user link $1 --all – отдельные ссылки vpn:// и tg://${N}"
}

cmd_add() {
  local name=${1:-} gb=0 days=0 devices=0 all=no
  valid_name "$name"; shift
  mapfile -t _args < <(eq_args "$@"); set -- ${_args[@]+"${_args[@]}"}
  while [[ $# -gt 0 ]]; do
    case $1 in
      --gb) need_val "$@"; gb=$2; shift 2 ;;
      --days) need_val "$@"; days=$2; shift 2 ;;
      --devices) need_val "$@"; devices=$2; shift 2 ;;
      --all) all=yes; shift ;;
      *) die "Неизвестный параметр: $1 (kit user add имя [--gb N] [--days N] [--devices N] [--all])" ;;
    esac
  done
  [[ $devices =~ ^[0-9]{1,4}$ ]] || die "--devices: целое число"
  devices=$((10#$devices))
  [[ -z $(client "$name") ]] || die "Пользователь $name уже есть. Ссылка: kit user link $name"
  local ids sid body
  # По умолчанию — только устойчивые протоколы (REALITY + WS). --all включает остальные.
  if [[ $all == yes ]]; then ids=$(non_awg_ids); else ids=$(core_ids); fi
  [[ $ids != "[]" ]] || die "На сервере нет устойчивых подключений (REALITY/WebSocket). Добавьте их при установке или выдайте все: kit user add $name --all"
  gb_bytes "$gb" >/dev/null; days_ms "$days" >/dev/null
  gb=$((10#$gb)); days=$((10#$days))
  sid=$(rand_id)
  body=$(jq -nc --arg e "$name" --arg s "$sid" --argjson t "$(gb_bytes "$gb")" --argjson x "$(days_ms "$days")" \
    --argjson ip "$devices" --argjson ids "$ids" '{client: {email: $e, subId: $s, totalGB: $t, expiryTime: $x,
    limitIp: $ip, enable: true, comment: "kit"}, inboundIds: $ids}')
  api POST clients/add "$body" >/dev/null
  [[ $all == yes ]] && awg_attach "$name" "$sid" "$(gb_bytes "$gb")" "$(days_ms "$days")" "$devices"

  local portal_out pwd purl sing_sub
  portal_out=$(portal_py user add "$name" --sub-id "$sid" 2>/dev/null || true)
  if [[ -n $portal_out ]] && jq -e '.username' <<<"$portal_out" >/dev/null 2>&1; then
    pwd=$(jq -r '.password // empty' <<<"$portal_out")
    purl=$(jq -r '.portal_url // empty' <<<"$portal_out")
    sing_sub=$(jq -r '.singbox_sub_url // empty' <<<"$portal_out")
  fi

  if [[ $all == yes ]]; then
    say "Пользователь $name добавлен во все протоколы ($(api GET inbounds/list | jq length))$( ((gb)) && echo ", лимит $gb ГБ")$( ((days)) && echo ", на $days дн")."
  else
    say "Пользователь $name добавлен (устойчивые протоколы: REALITY + WebSocket)$( ((gb)) && echo ", лимит $gb ГБ")$( ((days)) && echo ", на $days дн")."
    echo "${D}Нужен ещё протокол? kit user attach $name hy2|tuic|ss|awg|xhttp|trojan|mtproto${N}"
  fi

  if [[ -n ${pwd:-} ]]; then
    echo
    echo "${B}Личный кабинет пользователя:${N}"
    echo "  Адрес:   ${G}$purl${N}"
    echo "  Логин:   ${B}$name${N}"
    echo "  Пароль:  ${Y}$pwd${N}"
    echo
    echo "${B}Подписка Sing-box (DoH 1.1.1.1 + обход РФ сайтов):${N}"
    echo "  $sing_sub"
  fi
  show_link "$name" "$sid"
}

# Подключить пользователю дополнительный протокол (по умолчанию выдаются только
# устойчивые: REALITY + WebSocket). Так клиент получает протокол по запросу.
cmd_attach() { # имя протокол
  local name=${1:-} proto=${2:-} c sid ids remark total exp lim
  valid_name "$name"
  [[ -n $proto ]] || die "Укажите протокол: kit user attach $name reality|ws|vmess|xhttp|trojan|hy2|tuic|ss|wg|awg|awg3|mtproto"
  c=$(client "$name"); [[ -n $c ]] || die "Нет пользователя $name"
  sid=$(jq -r '.subId' <<<"$c")
  proto=${proto,,}
  if [[ $proto == awg || $proto == awg3 ]]; then
    total=$(jq -r '.totalGB // 0' <<<"$c"); exp=$(jq -r '.expiryTime // 0' <<<"$c"); lim=$(jq -r '.limitIp // 0' <<<"$c")
    awg_attach "$name" "$sid" "$total" "$exp" "$lim"
    say "AmneziaWG подключён пользователю $name (классика и 3.1)."
    echo "${D}Клиенту нужно обновить подписку. Ссылки vpn://: kit user link $name --all${N}"
    return
  fi
  remark=$(proto_remark "$proto") || die "Неизвестный протокол: $proto (reality, ws, vmess, xhttp, trojan, hy2, tuic, ss, wg, awg, awg3, mtproto)"
  ids=$(api GET inbounds/list | jq -c --arg r "$remark" '[.[] | select(.remark == $r) | .id]')
  [[ $ids != "[]" ]] || die "Протокол $proto ($remark) не установлен на этом сервере."
  api POST "clients/$name/attach" "$(jq -nc --argjson i "$ids" '{inboundIds: $i}')" >/dev/null
  say "Протокол $proto ($remark) подключён пользователю $name."
  echo "${D}Клиенту нужно обновить подписку в приложении.${N}"
  [[ $proto == mtproto ]] && echo "${D}Ссылка для Telegram: kit user link $name --all${N}"
  return 0
}

cmd_link() {
  local name=${1:-} all=${2:-} c
  valid_name "$name"
  c=$(client "$name"); [[ -n $c ]] || die "Нет пользователя $name"
  show_link "$name" "$(jq -r '.subId' <<<"$c")"
  if [[ $all == --all ]]; then
    local sid raw out="" suffix
    sid=$(jq -r '.subId' <<<"$c")
    for suffix in "" -awg; do
      raw=$(curl -fsSk -m 10 -A "v2rayN/7" -H "Host: ${DOMAIN:-$HOST}" "http://127.0.0.1:$SUB_INTERNAL$SUB_PATH$sid$suffix" 2>/dev/null || true)
      grep -q '://' <<<"$raw" || raw=$(base64 -d <<<"$raw" 2>/dev/null || true)
      out+=$(grep -E '^(vpn|tg)://' <<<"$raw" || true)$'\n'
    done
    [[ ${SINGLE:-no} == yes ]] && out=$(sed "s/^\(tg:\/\/proxy?\)\(.*\)port=${MTPROTO_INNER:-10445}/\1\2port=443/" <<<"$out")
    echo; grep . <<<"$out" || echo "Отдельных ссылок нет."
  fi
}

cmd_list() {
  local now
  now=$(($(date +%s) * 1000))
  clients | jq -r --argjson now "$now" '
    map(select(.subId != null)) | group_by(.subId | sub("-awg[0-9]*$"; "")) | map(sort_by(.email | length) as $g | $g[0] + {used: ([$g[] | (.traffic.up // 0) + (.traffic.down // 0)] | add),
      seen: ([$g[] | .traffic.lastOnline // 0] | max)}) | sort_by(.email)[]
    | [.email, .used, (.totalGB // 0), (.expiryTime // 0), .enable, .seen] | @tsv' |
  {
    printf "${B}%-18s %-22s %-14s %-10s %s${N}\n" "Пользователь" "Трафик" "До" "Статус" "Был в сети"
    while IFS=$'\t' read -r email used total exp en last; do
      local tr till st seen
      tr="$(human "$used")"; ((total > 0)) && tr="$tr / $(human "$total")"
      if ((exp > 0)); then till=$(date -d "@$((exp / 1000))" +%d.%m.%Y); else till="бессрочно"; fi
      if [[ $en != true ]]; then st="${R}выключен${N}"
      elif ((exp > 0 && exp < $(date +%s) * 1000)); then st="${Y}истёк${N}"
      elif ((total > 0 && used >= total)); then st="${Y}лимит${N}"
      else st="${G}активен${N}"; fi
      if ((last > 0)); then seen=$(date -d "@$((last / 1000))" '+%d.%m %H:%M'); else seen="–"; fi
      printf "%-18s %-22s %-14s %-19s %s\n" "$email" "$tr" "$till" "$st" "$seen"
    done
  }
}

# Меняет все записи пользователя (основную и «двойников» AmneziaWG), каждую – от её собственных данных.
update_user() { # имя jq-фильтр [аргументы jq...]
  local name=$1 filter=$2 e rec body
  shift 2
  for e in $(emails_of "$name"); do
    rec=$(client "$e")
    body=$(jq -c "$@" "{email, subId, totalGB, expiryTime, limitIp, enable, comment} | $filter" <<<"$rec")
    api POST "clients/update/$e" "$body" >/dev/null
  done
}

cmd_limit() {
  local name=${1:-} f="." gb="" days="" dev=""
  valid_name "$name"; shift
  [[ -n $(client "$name") ]] || die "Нет пользователя $name"
  mapfile -t _args < <(eq_args "$@"); set -- ${_args[@]+"${_args[@]}"}
  while [[ $# -gt 0 ]]; do
    case $1 in
      --gb) need_val "$@"; gb=$(gb_bytes "$2"); f+=" | .totalGB = \$gb"; shift 2 ;;
      --days) need_val "$@"; days=$(days_ms "$2"); f+=" | .expiryTime = \$days"; shift 2 ;;
      --devices) need_val "$@"; [[ $2 =~ ^[0-9]{1,4}$ ]] || die "--devices: целое число"; dev=$((10#$2)); f+=" | .limitIp = \$dev"; shift 2 ;;
      *) die "Неизвестный параметр: $1" ;;
    esac
  done
  update_user "$name" "$f" --argjson gb "${gb:-0}" --argjson days "${days:-0}" --argjson dev "${dev:-0}"
  say "Лимиты $name обновлены (0 – без ограничений)."
}

cmd_toggle() { # имя true|false
  valid_name "$1"
  [[ -n $(client "$1") ]] || die "Нет пользователя $1"
  update_user "$1" ".enable = \$v" --argjson v "$2"
  portal_py user toggle "$1" "$([ "$2" == true ] && echo 1 || echo 0)" >/dev/null 2>&1 || true
  if [[ $2 == true ]]; then say "Пользователь $1 включён."; else say "Пользователь $1 выключен – подписка и подключения не работают."; fi
}

cmd_del() {
  local name=${1:-} ans=""
  valid_name "$name"
  [[ -n $(client "$name") ]] || die "Нет пользователя $name"
  if [[ ${2:-} != -y && -t 0 ]]; then
    read -rp "Удалить $name со всех протоколов? [y/N] " ans
    [[ $ans =~ ^[yYдД]$ ]] || { echo "Отменено."; return; }
  fi
  local e
  for e in $(emails_of "$name"); do api POST "clients/del/$e" >/dev/null; done
  portal_py user del "$name" >/dev/null 2>&1 || true
  say "Пользователь $name удалён, его подписка и доступ в кабинет больше не работают."
}

cmd_passwd() { # имя [новый_пароль]
  local name=${1:-} pwd=${2:-} out
  valid_name "$name"
  [[ -n $(client "$name") ]] || die "Нет пользователя $name"
  if [[ -z $pwd ]]; then
    pwd=$(openssl rand -base64 12 | tr -dc 'A-Za-z0-9!@#$%' | head -c 16)
  fi
  out=$(portal_py user passwd "$name" "$pwd" 2>/dev/null || true)
  say "Пароль для пользователя $name в личном кабинете изменен: ${Y}$pwd${N}"
}

cmd_token() { # имя
  local name=${1:-} out
  valid_name "$name"
  [[ -n $(client "$name") ]] || die "Нет пользователя $name"
  out=$(portal_py user token "$name" 2>/dev/null || true)
  say "Токен Sing-box для $name обновлен: $(jq -r '.singbox_sub_url // empty' <<<"$out")"
}

# ---------- версия и обновление ----------

# Открытый ключ, которым автор подписывает релизы (ssh-keygen -Y sign). Закрытая часть
# есть только у автора, поэтому подменить обновление, взломав один GitHub, не выйдет.
# Новый ключ приходит только в релизе, подписанном старым.
KIT_SIGNERS=(
  # SHA256:VDuGgJ8dOeCNXB4nBZf3+kRlWthw3+vh8rfMxzMSRIM (itsnotkubrick, 2026-09-30)
  "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGrTGCDhhnm8XO1ekpPJuSWRVCJiFiupEspfQxcbEBmz 3X-UI KIT releases"
)
KIT_SIG_NS="3x-ui-kit-release"
KIT_SIG_ID="releases@3x-ui-kit"
KIT_MANUAL=/etc/kit/manual-update
KIT_UPDATE_LOG=/var/log/kit-update.log

xray_version() {
  local b
  for b in /usr/local/x-ui/bin/xray-linux-*; do [[ -x $b ]] && "$b" version 2>/dev/null | awk 'NR==1 {print "v" $2}'; return; done
}
remote_version() { curl -fsS -m "${1:-5}" "$KIT_RAW/VERSION" 2>/dev/null | tr -d '[:space:]'; }
newer() { [[ $1 != "$2" && $(printf '%s\n%s\n' "$1" "$2" | sort -V | tail -1) == "$1" ]]; }  # $1 новее $2?
auto_enabled() { systemctl is-enabled -q kit-update.timer 2>/dev/null; }

# Раз в сутки узнаём последнюю версию (не дольше 3 секунд) и подсказываем обновиться.
update_hint() {
  local latest=""
  if [[ ! -f $KIT_LATEST ]] || (($(date +%s) - $(stat -c %Y "$KIT_LATEST") > 86400)); then
    latest=$(remote_version 3) || true
    [[ $latest =~ ^[0-9]+(\.[0-9]+)+$ ]] && echo "$latest" >"$KIT_LATEST" || touch "$KIT_LATEST"
  fi
  latest=$(cat "$KIT_LATEST" 2>/dev/null || true)
  if [[ -n $latest ]] && newer "$latest" "$KIT_VERSION"; then
    echo
    if auto_enabled; then
      echo "${Y}↑ Вышла версия $latest${N} (у вас $KIT_VERSION). Встанет сама этой ночью или сейчас: ${B}kit update${N}"
    else
      echo "${Y}↑ Доступна версия $latest${N} (у вас $KIT_VERSION). Обновить: ${B}kit update${N}"
    fi
  fi
}

cmd_version() {
  echo "3X-UI KIT $KIT_VERSION"
  echo "${D}панель 3X-UI $(/usr/local/x-ui/x-ui -v 2>/dev/null | head -1 || echo '?'), ядро Xray $(xray_version)${N}"
  if auto_enabled; then echo "${D}автообновление: включено (только подписанные релизы), журнал $KIT_UPDATE_LOG${N}"
  else echo "${D}автообновление: выключено, включить: kit update --auto${N}"; fi
  update_hint
}

# Что нового в версии $1 – из CHANGELOG.md, без разметки.
changelog_of() {
  curl -fsS -m 5 "$KIT_RAW/CHANGELOG.md" 2>/dev/null | awk -v v="## v$1" '
    index($0, v) == 1 { on = 1; t = substr($0, length(v) + 1); sub(/^[: ]+/, "", t); if (t != "") print t; next } on && /^## / { exit } on' | sed -E 's/ ?Спасибо .*$//; s/\[([^]]*)\]\([^)]*\)/\1/g; s/\*\*//g; s/`//g' | grep -v '^[[:space:]]*$' | head -40 || true
}

# Скачивает релиз $1 в каталог $2 и проверяет: подпись SHA256SUMS нашим ключом, версию
# внутри подписанного файла и SHA256 каждого файла. Любое несовпадение – отказ.
fetch_release() { # версия каталог
  local v=$1 d=$2 raw f sum k
  ((${#KIT_SIGNERS[@]})) || { warn "В этой сборке kit нет ключа подписи – проверить обновление нечем."; return 1; }
  command -v ssh-keygen >/dev/null || { warn "Нет ssh-keygen (пакет openssh-client) – подпись не проверить."; return 1; }
  raw=$(kit_ref_raw "$v")
  curl -fsSL --retry 3 -o "$d/SHA256SUMS" "$raw/SHA256SUMS" && curl -fsSL --retry 3 -o "$d/SHA256SUMS.sig" "$raw/SHA256SUMS.sig" \
    || { warn "Не удалось скачать подпись релиза $v."; return 1; }
  : >"$d/allowed_signers"
  for k in "${KIT_SIGNERS[@]}"; do printf '%s namespaces="%s" %s\n' "$KIT_SIG_ID" "$KIT_SIG_NS" "$k" >>"$d/allowed_signers"; done
  if ! ssh-keygen -Y verify -f "$d/allowed_signers" -I "$KIT_SIG_ID" -n "$KIT_SIG_NS" -s "$d/SHA256SUMS.sig" <"$d/SHA256SUMS" >/dev/null 2>&1; then
    warn "Подпись релиза $v не сошлась с ключом автора – это не наш релиз. Ничего не ставлю."
    return 1
  fi
  # Версия записана внутри подписанного файла: старый подписанный релиз под видом нового не пройдёт.
  grep -qx "# 3X-UI KIT $v" "$d/SHA256SUMS" || { warn "Подписанный релиз не той версии – ничего не ставлю."; return 1; }
  for f in scripts/kit.sh scripts/kit-sub.py; do
    sum=$(awk -v f="$f" '$2 == f {print $1}' "$d/SHA256SUMS")
    [[ $sum =~ ^[0-9a-f]{64}$ ]] || { warn "В подписанном списке нет $f."; return 1; }
    curl -fsSL --retry 3 -o "$d/${f##*/}" "$raw/$f" || { warn "Не удалось скачать $f."; return 1; }
    [[ $(sha256sum "$d/${f##*/}" | awk '{print $1}') == "$sum" ]] || { warn "$f не совпал с подписанным SHA256 – ничего не ставлю."; return 1; }
  done
}

# Юнит kit-sub: без root (DynamicUser), конфиг и сертификат – через LoadCredential.
# Сертификат Let's Encrypt на IP продлевается раз в несколько дней, поэтому при отдельном
# порте kit-sub перезапускается раз в сутки и берёт свежий.
kit_sub_unit() { # путь-к-сертификату путь-к-ключу (пусто – за nginx)
  cat <<UNIT
[Unit]
Description=kit-sub: подписка с учётом приложения (3X-UI KIT)
After=network-online.target x-ui.service
Wants=network-online.target

[Service]
ExecStart=/usr/bin/python3 /usr/local/lib/kit-sub/kit_sub.py
Restart=on-failure
RestartSec=5
DynamicUser=yes
LoadCredential=config.json:/etc/kit-sub/config.json
${1:+LoadCredential=cert.pem:$1}
${2:+LoadCredential=key.pem:$2}
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=true
PrivateDevices=true
ProtectProc=invisible
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
AmbientCapabilities=CAP_NET_BIND_SERVICE
MemoryMax=64M

[Install]
WantedBy=multi-user.target
UNIT
}

# Подписка отвечает? Берём настоящего пользователя и спрашиваем kit-sub изнутри сервера.
sub_ok() {
  local cfg=/etc/kit-sub/config.json port path scheme=http sid i
  port=$(jq -r '.port' "$cfg"); path=$(jq -r '.path' "$cfg")
  [[ -n $(jq -r '.cert // empty' "$cfg") ]] && scheme=https
  sid=$(clients 2>/dev/null | jq -r '[.[] | .subId // empty | select(. != "")][0] // empty' 2>/dev/null || true)
  for i in $(seq 1 10); do
    if [[ -n $sid ]]; then
      curl -fsSk -m 5 -o /dev/null -A "Happ/1.0" "$scheme://127.0.0.1:$port$path$sid" 2>/dev/null && return 0
    else
      # Пользователей нет – достаточно, что служба жива и слушает порт.
      systemctl is-active -q kit-sub && ss -Hltn "sport = :$port" | grep -q . && return 0
    fi
    sleep 1
  done
  return 1
}

# Сервер ещё не получил исправления 1.1 (например, kit обновили вручную из 1.0)?
needs_migration() {
  [[ -f /etc/cron.d/kit-xui-menu ]] && return 0
  [[ -f /etc/systemd/system/kit-sub.service ]] && ! grep -q '^DynamicUser=yes' /etc/systemd/system/kit-sub.service && return 0
  [[ ! -f $KIT_MANUAL ]] && ! auto_enabled && return 0
  return 1
}

# Автообновление: раз в сутки ночью со случайной задержкой, чтобы тысячи серверов не шли
# на GitHub в одну минуту. Ставит только подписанные релизы и только kit и kit-sub.
auto_on() {
  cat >/etc/systemd/system/kit-update.service <<UNIT
[Unit]
Description=3X-UI KIT: автообновление kit и kit-sub (только подписанные релизы)
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=/usr/local/bin/kit update --unattended
StandardOutput=append:$KIT_UPDATE_LOG
StandardError=append:$KIT_UPDATE_LOG
UNIT
  cat >/etc/systemd/system/kit-update.timer <<'UNIT'
[Unit]
Description=3X-UI KIT: проверка обновлений раз в сутки

[Timer]
OnCalendar=*-*-* 03:00:00
RandomizedDelaySec=3h
Persistent=true

[Install]
WantedBy=timers.target
UNIT
  rm -f "$KIT_MANUAL"
  systemctl daemon-reload
  systemctl enable --now kit-update.timer >/dev/null 2>&1
}

auto_off() {
  systemctl disable --now kit-update.timer >/dev/null 2>&1 || true
  install -d -m 700 /etc/kit
  touch "$KIT_MANUAL"
}

cmd_update() {
  local force="" unattended=no latest tmp
  while [[ $# -gt 0 ]]; do
    case $1 in
      --force) force=yes ;;
      --auto) auto_on; say "Автообновление включено: раз в сутки ночью, только подписанные релизы. Журнал: $KIT_UPDATE_LOG"; return ;;
      --manual) auto_off; say "Автообновление выключено. Обновляться вручную: kit update, включить снова: kit update --auto"; return ;;
      --unattended) unattended=yes ;;
      *) die "Неизвестный параметр: $1 (kit update [--force | --auto | --manual])" ;;
    esac
    shift
  done
  # Ночной запуск и ручной не должны встретиться.
  exec 9>/run/kit-update.lock
  flock -n 9 || die "Обновление уже идёт."
  [[ $unattended == yes ]] && echo "--- $(date '+%F %T') kit $KIT_VERSION: проверяю обновления"

  latest=$(remote_version) || true
  [[ $latest =~ ^[0-9]+(\.[0-9]+)+$ ]] || die "Не удалось узнать последнюю версию: GitHub недоступен с сервера. Попробуйте позже."
  echo "$latest" >"$KIT_LATEST"
  # Откатить на старую версию нельзя даже с подписью: только вперёд или та же.
  newer "$KIT_VERSION" "$latest" && die "На GitHub версия $latest старше вашей $KIT_VERSION – ничего не делаю."
  if ! newer "$latest" "$KIT_VERSION" && [[ $force != yes ]] && ! needs_migration; then
    say "У вас последняя версия: $KIT_VERSION."
    [[ $unattended == yes ]] || echo "${D}Переустановить файлы kit той же версии: kit update --force${N}"
    return
  fi
  if [[ $latest == "$KIT_VERSION" ]]; then say "Применяю исправления версии $latest"; else say "3X-UI KIT $KIT_VERSION → $latest"; fi
  tmp=$(mktemp -d)
  # shellcheck disable=SC2064 # путь подставляем сразу: при выходе локальной переменной уже нет
  trap "rm -rf -- '$tmp'" EXIT
  fetch_release "$latest" "$tmp" || die "Сервер не тронут."
  bash -n "$tmp/kit.sh" && python3 -c "import ast,sys; ast.parse(open(sys.argv[1]).read())" "$tmp/kit-sub.py" \
    || die "Файлы релиза не прошли проверку синтаксиса – сервер не тронут."

  # Подписка kit-sub: ставим новую, проверяем, что отвечает, иначе возвращаем старую.
  if [[ -f /usr/local/lib/kit-sub/kit_sub.py ]]; then
    cp /usr/local/lib/kit-sub/kit_sub.py "$tmp/kit_sub.old"
    cp /etc/systemd/system/kit-sub.service "$tmp/kit-sub.service.old"
    install -m 644 "$tmp/kit-sub.py" /usr/local/lib/kit-sub/kit_sub.py
    # С 1.1 kit-sub работает без root: переписываем юнит под DynamicUser и LoadCredential.
    local c k
    c=$(jq -r '.cert // empty' /etc/kit-sub/config.json); k=$(jq -r '.key // empty' /etc/kit-sub/config.json)
    kit_sub_unit "$c" "$k" >/etc/systemd/system/kit-sub.service
    [[ -n $c ]] && echo '19 4 * * * root systemctl restart kit-sub >/dev/null 2>&1' >/etc/cron.d/kit-sub-cert
    systemctl daemon-reload
    if systemctl restart kit-sub && sleep 2 && sub_ok; then
      say "Подписка kit-sub обновлена и отвечает"
    else
      install -m 644 "$tmp/kit_sub.old" /usr/local/lib/kit-sub/kit_sub.py
      install -m 644 "$tmp/kit-sub.service.old" /etc/systemd/system/kit-sub.service
      grep -q '^LoadCredential=cert.pem' "$tmp/kit-sub.service.old" || rm -f /etc/cron.d/kit-sub-cert
      systemctl daemon-reload
      systemctl restart kit-sub || true
      die "Новая подписка не ответила – вернул прежнюю, kit остался версии $KIT_VERSION. Лог: journalctl -u kit-sub -n 30"
    fi
  fi

  # Исправления для установок 1.0.
  local all uri
  all=$(api POST setting/all)
  uri=${SUB_BASE:-}
  if [[ $uri == https://* && $(jq -r '.subURI // ""' <<<"$all") != "$uri" ]]; then
    api POST setting/update "$(jq -c --arg u "$uri" '.subURI = $u' <<<"$all")" >/dev/null
    say "Ссылка подписки в панели: $uri…"
  fi
  # 1.0 ставил cron, который каждый день правил файлы x-ui, – убираем.
  rm -f /etc/cron.d/kit-xui-menu
  # Автообновление включено по умолчанию, пока его не выключили командой kit update --manual.
  [[ -f $KIT_MANUAL ]] || auto_enabled || { auto_on; say "Включил автообновление: kit update --manual, чтобы выключить"; }

  # Через rename: bash дочитывает текущий kit по ходу работы, его файл трогать нельзя.
  install -m 755 "$tmp/kit.sh" /usr/local/bin/kit.new && mv -f /usr/local/bin/kit.new /usr/local/bin/kit
  echo
  echo "${G}✓ Готово: 3X-UI KIT $latest.${N} Пользователи, ссылки и подписки не менялись."
  [[ $unattended == yes ]] && return
  local news
  news=$(changelog_of "$latest")
  if [[ -n $news ]]; then echo; echo "${B}Что нового в $latest${N}"; echo "$news"; fi
}

# ---------- проверка и починка ----------

# kit check только читает и показывает, что с сервером. kit fix чинит безопасное: перезапускает
# упавшие службы, возвращает права на файлы, включает автообновление, перечитывает сертификат.
CHECK_BAD=0; CHECK_WARN=0; CHECK_FIX=(); CHECK_QUIET=no
c_ok()   { [[ $CHECK_QUIET == yes ]] || printf '%s\n' "${G}✅${N} $*"; }
c_info() { [[ $CHECK_QUIET == yes ]] || printf '%s\n' "${D}ℹ  $*${N}"; }
c_warn() { printf '%s\n' "${Y}⚠${N}  $*"; CHECK_WARN=$((CHECK_WARN + 1)); }
c_bad()  { printf '%s\n' "${R}❌${N} ${*:2}"; CHECK_BAD=$((CHECK_BAD + 1)); [[ -z $1 ]] || CHECK_FIX+=("$1"); } # код-починки сообщение

# Сайт маскировки отвечает по TLS 1.3 и HTTP/2 (как требует REALITY)? Проверка с самого сервера.
sni_alive() { echo | timeout 8 openssl s_client -connect "$1:443" -servername "$1" -tls1_3 -alpn h2 2>/dev/null | grep -q 'ALPN protocol: h2'; }

check_services() {
  local pid u
  if systemctl is-active -q x-ui; then c_ok "служба x-ui работает"; else c_bad svc:x-ui "служба x-ui не работает (journalctl -u x-ui -n 50)"; fi
  if [[ ${SINGLE:-no} == yes ]]; then
    if systemctl is-active -q nginx; then c_ok "nginx работает"; else c_bad svc:nginx "nginx не работает (journalctl -u nginx -n 50)"; fi
  fi
  if [[ -f /usr/local/lib/kit-sub/kit_sub.py ]]; then
    if systemctl is-active -q kit-sub; then
      c_ok "служба kit-sub работает"
      pid=$(systemctl show -p MainPID --value kit-sub 2>/dev/null || true)
      u=$(ps -o user= -p "$pid" 2>/dev/null | tr -d ' ' || true)
      if [[ -n $u && $u != root ]]; then c_ok "kit-sub работает не от root (пользователь $u)"; else c_bad "" "kit-sub работает от root – это небезопасно, обновите kit: kit update --force"; fi
    else
      c_bad svc:kit-sub "служба kit-sub не работает (journalctl -u kit-sub -n 50)"
    fi
  fi
  if systemctl is-active -q kit-portal 2>/dev/null; then
    c_ok "служба kit-portal работает"
    local p_port=${PORTAL_PORT:-10465}
    if ! ss -ltnH "sport = :$p_port" 2>/dev/null | grep -q "$p_port"; then
      c_bad svc:kit-portal "служба kit-portal не слушает внутренний порт $p_port"
    fi
  elif systemctl list-unit-files kit-portal.service 2>/dev/null | grep -q kit-portal; then
    c_bad svc:kit-portal "служба kit-portal не работает (journalctl -u kit-portal -n 50)"
  fi
  if auto_enabled; then c_ok "автообновление включено"
  elif [[ -f $KIT_MANUAL ]]; then c_info "автообновление выключено вами (включить: kit update --auto)"
  else c_bad timer "автообновление не включено"; fi
}

check_versions() {
  local xv; xv=$(xray_version || true)
  c_info "kit $KIT_VERSION, панель 3X-UI $(/usr/local/x-ui/x-ui -v 2>/dev/null | head -1 || echo '?'), ядро Xray ${xv:-?}"
  if [[ -n $xv ]] && newer "$xv" v26.6.27; then
    c_warn "ядро Xray $xv новее проверенного (26.6.27): клиенты на Mihomo и sing-box могут не подключаться к REALITY"
  fi
  if [[ -s $KIT_LATEST ]] && newer "$(cat "$KIT_LATEST")" "$KIT_VERSION"; then
    c_info "вышла версия $(cat "$KIT_LATEST"): kit update"
  fi
}

check_cert() {
  local cert end left
  cert=$(awk '$1 == "ssl_certificate" {sub(/;$/, "", $2); print $2; exit}' /etc/nginx/conf.d/kit.conf 2>/dev/null || true)
  [[ -n $cert ]] || cert=$(jq -r '.cert // empty' /etc/kit-sub/config.json 2>/dev/null || true)
  [[ -n $cert ]] || { c_info "сертификат не настроен (режим без TLS)"; return 0; }
  [[ -f $cert ]] || { c_bad cert "файл сертификата $cert из настроек не найден"; return 0; }
  if ! openssl x509 -in "$cert" -noout -checkend 0 >/dev/null 2>&1; then
    c_bad cert "сертификат $cert просрочен"
  else
    end=$(openssl x509 -in "$cert" -noout -enddate 2>/dev/null | cut -d= -f2)
    left=$((($(date -d "$end" +%s) - $(date +%s)) / 86400))
    if openssl x509 -in "$cert" -noout -checkend 86400 >/dev/null 2>&1; then c_ok "сертификат действителен ещё ~$left дн."
    else c_warn "сертификат истекает меньше чем через сутки: проверьте продление (acme.sh --cron)"; fi
  fi
}

check_exposure() {
  local bind f m bad=0
  # Панель наружу не торчит: за nginx или в режиме «только SSH-туннель».
  if [[ ${SINGLE:-no} == yes || ! -f /usr/local/lib/kit-sub/kit_sub.py ]]; then
    bind=$(ss -ltnH "sport = :$XUI_PANEL_PORT" 2>/dev/null | awk '{print $4}' || true)
    if [[ -z $bind ]]; then c_bad "" "панель не слушает порт $XUI_PANEL_PORT"
    elif grep -qv '^127\.0\.0\.1:' <<<"$bind"; then c_bad "" "панель слушает наружу ($(tr '\n' ' ' <<<"$bind")): в настройках 3X-UI укажите адрес 127.0.0.1"
    else c_ok "панель слушает только localhost"; fi
  fi
  if [[ ${SINGLE:-no} == yes ]]; then
    if ss -ltnH 'sport = :443' 2>/dev/null | grep -q .; then c_ok "порт 443 слушается"; else c_bad svc:nginx "порт 443 никто не слушает"; fi
    if command -v ufw >/dev/null && ufw status 2>/dev/null | grep -q '^Status: active'; then
      if ufw status | grep -Eq '^443/tcp +ALLOW'; then c_ok "ufw пропускает 443/tcp"; else c_bad "" "ufw не пропускает 443/tcp: ufw allow 443/tcp"; fi
    fi
  fi
  for f in /etc/x-ui/install-result.env /etc/kit/kit.env /etc/kit-sub/config.json /etc/kit/portal.db /root/3x-ui.txt /root/cert/*/privkey.pem; do
    [[ -f $f ]] || continue
    m=$(stat -c %a "$f")
    [[ $m == 600 ]] || { c_bad perms "права на $f: $m (нужно 600)"; bad=1; }
  done
  ((bad)) || c_ok "файлы с паролями и ключами доступны только root"

  if [[ -f /etc/nginx/nginx.conf ]]; then
    local wc
    wc=$(grep -oE 'worker_connections[[:space:]]+[0-9]+' /etc/nginx/nginx.conf 2>/dev/null | awk '{print $2}' | head -1 || true)
    if [[ -n $wc && $wc -lt 2048 ]]; then
      c_bad nginx_limits "лимит соединений nginx ($wc) мал для мультиплексирования – kit fix увеличит до 4096"
    fi
  fi
}

check_subscription() {
  local cfg=/etc/kit-sub/config.json port path scheme=http sid ua code size body up sub_cert
  [[ -f $cfg ]] || { c_info "подписка не установлена (режим без сертификата: используйте отдельные ссылки из /root/3x-ui.txt)"; return 0; }
  port=$(jq -r '.port' "$cfg"); path=$(jq -r '.path' "$cfg")
  # Частая поломка: команда «x-ui cert» или меню панели включает TLS у встроенной подписки 3X-UI,
  # а kit-sub ходит к ней по http – подписка перестаёт отвечать.
  up=$(jq -r '.upstream // empty' "$cfg")
  sub_cert=$(api POST setting/all '{}' 2>/dev/null | jq -r '.subCertFile // empty' 2>/dev/null || true)
  if [[ $up == http://* && -n $sub_cert ]]; then
    c_bad subtls "у встроенной подписки 3X-UI включён TLS (сертификат $sub_cert), а kit-sub ходит к ней по http – подписка не отвечает"
  fi
  body=$(mktemp)
  [[ -n $(jq -r '.cert // empty' "$cfg") ]] && scheme=https
  sid=$(clients 2>/dev/null | jq -r '[.[] | .subId // empty | select(. != "")][0] // empty' 2>/dev/null || true)
  [[ -n $sid ]] || { c_info "пользователей нет, подписку проверить нечем"; return 0; }
  for ua in "Happ/1.0" "HiddifyNext/2.0" "v2rayN/7.0" "clash-verge/v2"; do
    : >"$body"
    code=$(curl -sgk -m 10 -A "$ua" -o "$body" -w '%{http_code}' "$scheme://127.0.0.1:$port$path$sid" 2>/dev/null || true)
    size=$(wc -c <"$body" | tr -d ' ')
    if [[ $code == 200 && ${size:-0} -gt 0 ]]; then c_ok "подписка отвечает для $ua ($size байт)"; else c_bad svc:kit-sub "подписка для $ua: HTTP ${code:-нет ответа}"; fi
  done
  rm -f "$body"
  if [[ ${SINGLE:-no} == yes ]]; then
    # Подписка теперь живёт на CDN-домене (server_name), а не на голом IP.
    local sub_host=${WEB_DOMAIN:-127.0.0.1}
    code=$(curl -sk -m 10 -A "Happ/1.0" --resolve "$sub_host:443:127.0.0.1" -o /dev/null -w '%{http_code}' "https://$sub_host$path$sid" 2>/dev/null || true)
    if [[ $code == 200 ]]; then c_ok "подписка отвечает и через nginx (443)"; else c_bad svc:nginx "подписка через nginx: HTTP ${code:-нет ответа} (ищите причину выше)"; fi
  fi
}

check_masking() {
  local list line sni target host ips
  list=$(api GET inbounds/list 2>/dev/null) || { c_bad "" "не удалось получить подключения из панели (панель не отвечает?)"; return 0; }
  while IFS=$'\t' read -r name sni target; do
    [[ -n $sni ]] || continue
    if [[ $target == 127.0.0.1:* ]]; then
      ips=$(getent ahostsv4 "$sni" 2>/dev/null | awk '{print $1}' | sort -u || true); host=${HOST:-}
      if [[ -n $ips ]] && grep -qx "$host" <<<"$ips"; then c_ok "$name: свой домен $sni указывает на этот сервер"
      else c_bad "" "$name: свой домен $sni не указывает на этот сервер (A-запись: ${ips:-нет}, нужен $host)"; fi
    elif sni_alive "$sni"; then
      c_ok "$name: сайт маскировки $sni отвечает по TLS 1.3 и HTTP/2"
    else
      c_bad "" "$name: сайт маскировки $sni не отвечает по TLS 1.3 и HTTP/2 – смените его в панели 3X-UI"
    fi
  done < <(jq -r '.[] | select(.protocol == "vless") | (.streamSettings | if type == "string" then fromjson else . end) as $s
    | select($s.security == "reality") | [.remark, ($s.realitySettings.serverNames[0] // ""), ($s.realitySettings.target // "")] | @tsv' <<<"$list" 2>/dev/null || true)
  if [[ -n ${DOMAIN:-} ]]; then
    local d_ips me=${HOST:-}
    d_ips=$(getent ahostsv4 "$DOMAIN" 2>/dev/null | awk '{print $1}' | sort -u || true)
    if [[ -z $d_ips ]]; then
      d_ips=$(curl -4 -fsS -m 5 "https://1.1.1.1/dns-query?name=$DOMAIN&type=A" -H "accept: application/dns-json" 2>/dev/null \
        | jq -r '.Answer[]? | select(.type == 1) | .data' 2>/dev/null | sort -u || true)
    fi
    if [[ -n $d_ips ]] && grep -qx "$me" <<<"$d_ips"; then
      c_ok "домен $DOMAIN указывает на этот сервер ($me)"
    elif [[ -n $d_ips ]]; then
      c_warn "домен $DOMAIN указывает на $(tr '\n' ' ' <<<"$d_ips"), а IP этого сервера: $me"
    else
      c_warn "не удалось определить IP для домена $DOMAIN"
    fi
  fi

  # Веб-домен должен быть за Cloudflare (Proxied), иначе он раскрывает IP сервера.
  if [[ -n ${WEB_DOMAIN:-} && -n ${HOST:-} ]]; then
    local w_ips
    w_ips=$(getent ahostsv4 "$WEB_DOMAIN" 2>/dev/null | awk '{print $1}' | sort -u || true)
    if [[ -z $w_ips ]]; then
      w_ips=$(curl -4 -fsS -m 5 "https://1.1.1.1/dns-query?name=$WEB_DOMAIN&type=A" -H "accept: application/dns-json" 2>/dev/null \
        | jq -r '.Answer[]? | select(.type == 1) | .data' 2>/dev/null | sort -u || true)
    fi
    if grep -qx "$HOST" <<<"$w_ips"; then
      c_warn "веб-домен $WEB_DOMAIN указывает прямо на IP сервера (DNS Only) – Cloudflare не скрывает узел. Включите Proxied (оранжевое облако)"
    elif [[ -n $w_ips ]]; then
      c_ok "веб-домен $WEB_DOMAIN за прокси (не раскрывает IP сервера)"
    else
      c_warn "не удалось определить IP веб-домена $WEB_DOMAIN"
    fi
  fi

  # По IP и посторонним именам должен отдаваться сайт-заглушка, а не VPN-кабинет.
  if [[ ${SINGLE:-no} == yes && -n ${WEB_DOMAIN:-} && ${WEB_DOMAIN} != "${HOST:-}" && ${WEB_DOMAIN} != "${DOMAIN:-}" ]]; then
    local decoy
    decoy=$(curl -sk -m 8 "https://127.0.0.1/" 2>/dev/null || true)
    if [[ $decoy == *"Личный кабинет"* ]]; then
      c_bad "" "по IP/неизвестному имени отдаётся портал, а не заглушка (nginx kit.conf) – переустановите: kit update --force"
    elif [[ -n $decoy ]]; then
      c_ok "по IP и посторонним именам отдаётся сайт-заглушка"
    fi
  fi
  c_info "Проверка идёт с самого сервера: доступность из вашей сети она не покажет."
}

check_system() {
  local used
  if command -v timedatectl >/dev/null; then
    if [[ $(timedatectl show -p NTPSynchronized --value 2>/dev/null || true) == yes ]]; then c_ok "время синхронизировано"
    else c_warn "время не синхронизировано: при сильном сдвиге TLS и REALITY не работают (kit fix включит синхронизацию)"; CHECK_FIX+=(ntp); fi
  fi
  used=$(df -P / | awk 'NR==2 {gsub("%", "", $5); print $5}')
  if ((${used:-0} >= 95)); then c_warn "диск заполнен на ${used}%"; else c_ok "место на диске: занято ${used:-?}%"; fi

  # Проверка таблицы conntrack (при переполнении ядро молча сбрасывает пакеты)
  if [[ -f /proc/sys/net/netfilter/nf_conntrack_max ]]; then
    local ct_max ct_cur
    ct_max=$(cat /proc/sys/net/netfilter/nf_conntrack_max 2>/dev/null || echo 0)
    ct_cur=$(cat /proc/sys/net/netfilter/nf_conntrack_count 2>/dev/null || echo 0)
    if ((ct_max < 32768)); then
      c_bad conntrack "лимит conntrack слишком мал ($ct_max, занято $ct_cur) – возможны обрывы VPN (kit fix настроит sysctl)"
    else
      c_ok "таблица conntrack: $ct_cur / $ct_max записей"
    fi
  fi
}

# Согласованность: аккаунты кабинета ссылаются на subId, которые есть в панели;
# настройки панели совпадают с install-result.env (после кривого restore бывает рассинхрон).
check_panel() {
  local pdb=${PORTAL_DB:-/etc/kit/portal.db} psubs panel_subs missing
  if [[ -f $pdb ]]; then
    psubs=$(python3 - "$pdb" <<'PY' 2>/dev/null || true
import sqlite3, sys
con = sqlite3.connect("file:%s?mode=ro" % sys.argv[1], uri=True)
for (s,) in con.execute("select sub_id from users where sub_id is not null and sub_id != ''"):
    print(s)
PY
)
    if [[ -n $psubs ]]; then
      panel_subs=$(clients 2>/dev/null | jq -r '.[].subId // empty' 2>/dev/null | sort -u || true)
      if [[ -z $panel_subs ]]; then
        c_info "аккаунты кабинета есть, но список из панели не получен — проверка связей пропущена"
      else
        missing=$(comm -23 <(sort -u <<<"$psubs") <(printf '%s\n' "$panel_subs") 2>/dev/null | tr '\n' ' ' || true)
        if [[ -n ${missing// /} ]]; then
          c_bad "" "в кабинете есть подписки, которых нет в панели:$missing — аккаунты не получат ссылки (перенос: kit restore --import)"
        else
          c_ok "аккаунты кабинета связаны с панелью"
        fi
      fi
    fi
  fi

  local all wp wbp
  all=$(api POST setting/all 2>/dev/null || true)
  [[ -n $all ]] || return 0
  wp=$(jq -r '.webPort // empty' <<<"$all" 2>/dev/null || true)
  wbp=$(jq -r '.webBasePath // empty' <<<"$all" 2>/dev/null || true)
  if [[ -n $wp && -n ${XUI_PANEL_PORT:-} && $wp != "$XUI_PANEL_PORT" ]]; then
    c_warn "порт панели в настройках ($wp) не совпадает с install-result.env ($XUI_PANEL_PORT) — kit/портал могут не видеть панель"
  fi
  if [[ -n $wbp && -n ${XUI_WEB_BASE_PATH:-} && ${wbp#/} != "${XUI_WEB_BASE_PATH#/}" ]]; then
    c_warn "путь панели в настройках ($wbp) не совпадает с install-result.env (/$XUI_WEB_BASE_PATH)"
  fi
}

run_checks() {
  CHECK_BAD=0; CHECK_WARN=0; CHECK_FIX=()
  check_services; check_versions; check_cert; check_exposure; check_subscription; check_masking; check_panel; check_system
}

cmd_check() {
  echo "${B}kit check${N} – проверка сервера (ничего не меняет)"
  echo
  run_checks
  echo
  if ((CHECK_BAD == 0)); then
    echo "${G}${B}Всё в порядке.${N}$( ((CHECK_WARN)) && echo " Предупреждений: $CHECK_WARN." || true)"
  else
    echo "${R}${B}Проблем: $CHECK_BAD.${N} Безопасные исправления: ${B}kit fix${N} (сначала можно посмотреть: kit fix --dry-run)."
    return 1
  fi
}

fix_action() { # код
  case $1 in
    svc:x-ui) say "Перезапускаю x-ui"; systemctl restart x-ui; sleep 3 ;;
    svc:nginx)
      if nginx -t >/dev/null 2>&1; then say "Перезапускаю nginx"; systemctl restart nginx
      else warn "Конфиг nginx не проходит проверку (nginx -t) – не трогаю, чтобы не сломать сервер."; fi ;;
    svc:kit-sub) say "Перезапускаю kit-sub"; systemctl restart kit-sub; sleep 2 ;;
    svc:kit-portal) say "Перезапускаю kit-portal"; systemctl restart kit-portal; sleep 2 ;;
    subtls)
      say "Убираю сертификат у встроенной подписки 3X-UI (TLS снимает nginx, kit-sub ходит по http)"
      local all upd
      all=$(api POST setting/all '{}')
      upd=$(jq -c '.subCertFile = "" | .subKeyFile = ""' <<<"$all")
      api POST setting/update "$upd" >/dev/null
      systemctl restart x-ui; sleep 4 ;;
    timer) say "Включаю автообновление"; auto_on ;;
    perms)
      say "Возвращаю права 600 на файлы с паролями и ключами"
      local f
      for f in /etc/x-ui/install-result.env /etc/kit/kit.env /etc/kit-sub/config.json /etc/kit/portal.db /root/3x-ui.txt /root/cert/*/privkey.pem; do
        if [[ -f $f ]]; then chmod 600 "$f"; fi
      done ;;
    cert)
      if nginx -t >/dev/null 2>&1; then say "Перечитываю сертификат в nginx"; systemctl reload nginx
      else warn "Конфиг nginx не проходит проверку – сертификат не перечитываю."; fi
      warn "Если сертификат всё ещё просрочен, запустите продление: ~/.acme.sh/acme.sh --cron" ;;
    ntp) say "Включаю синхронизацию времени"; timedatectl set-ntp true ;;
    conntrack)
      say "Оптимизирую параметры conntrack и сетевого стека"
      modprobe nf_conntrack 2>/dev/null || true
      cat >/etc/sysctl.d/99-vpn-conntrack.conf <<'EOF'
# Оптимизация таблицы conntrack и сетевого стека (3X-UI KIT)
net.netfilter.nf_conntrack_max = 65536
net.netfilter.nf_conntrack_tcp_timeout_established = 600
net.netfilter.nf_conntrack_tcp_timeout_time_wait = 30
net.netfilter.nf_conntrack_tcp_timeout_close_wait = 30
net.core.somaxconn = 32768
net.ipv4.tcp_max_syn_backlog = 16384
EOF
      sysctl -p /etc/sysctl.d/99-vpn-conntrack.conf >/dev/null 2>&1 || sysctl --system >/dev/null 2>&1 || true ;;
    nginx_limits)
      say "Оптимизирую лимиты соединений и дескрипторов nginx"
      if ! grep -q 'worker_rlimit_nofile' /etc/nginx/nginx.conf 2>/dev/null; then
        sed -i '1i worker_rlimit_nofile 16384;' /etc/nginx/nginx.conf
      fi
      sed -i -E 's/worker_connections[[:space:]]+[0-9]+;/worker_connections 4096;/' /etc/nginx/nginx.conf
      if ! grep -q 'multi_accept' /etc/nginx/nginx.conf 2>/dev/null; then
        sed -i -E 's/(worker_connections[[:space:]]+4096;)/\1\n\tmulti_accept on;/' /etc/nginx/nginx.conf
      fi
      mkdir -p /etc/systemd/system/nginx.service.d
      cat >/etc/systemd/system/nginx.service.d/override.conf <<'EOF'
[Service]
LimitNOFILE=65535
EOF
      systemctl daemon-reload
      if nginx -t >/dev/null 2>&1; then systemctl reload nginx; fi ;;
  esac
}

# Что чинить сейчас: если найдена причина (TLS у подписки), перезапуски kit-sub и nginx – лишь
# следствия: сначала лечим причину, потом проверяем заново.
fix_plan() {
  local c
  while IFS= read -r c; do
    if [[ $c == svc:kit-sub || $c == svc:nginx ]] && printf '%s\n' "${CHECK_FIX[@]}" | grep -qx subtls; then continue; fi
    echo "$c"
  done < <(printf '%s\n' "${CHECK_FIX[@]}" | sort -u)
}

cmd_fix() {
  local dry=no c pass plan
  case ${1:-} in --dry-run) dry=yes ;; "") ;; *) die "kit fix [--dry-run]" ;; esac
  echo "${B}kit fix${N} – безопасные исправления (службы, права, автообновление, сертификат, время)"
  echo
  CHECK_QUIET=yes; run_checks; CHECK_QUIET=no
  if ((${#CHECK_FIX[@]} == 0)); then
    if ((CHECK_BAD == 0)); then echo "${G}Чинить нечего: всё в порядке.${N}"; return 0; fi
    echo; echo "${Y}Автоматически эти проблемы не исправить, они описаны выше. Подробности: kit check${N}"; return 1
  fi
  if [[ $dry == yes ]]; then
    while IFS= read -r c; do echo "  будет сделано: $c"; done < <(fix_plan)
    return 0
  fi
  for pass in 1 2; do
    plan=$(fix_plan)
    [[ -n $plan ]] || break
    while IFS= read -r c; do fix_action "$c"; done <<<"$plan"
    CHECK_QUIET=yes; run_checks; CHECK_QUIET=no
    ((${#CHECK_FIX[@]})) || break
  done
  echo; echo "${B}Повторная проверка${N}"; echo
  run_checks
  echo
  if ((CHECK_BAD == 0)); then echo "${G}${B}Готово: всё в порядке.${N}"; else echo "${R}Осталось проблем: $CHECK_BAD.${N} Часть из них нужно решать вручную (см. выше)."; return 1; fi
}

# ---------- резервная копия ----------

# Что входит в копию. Базы берём снимком SQLite, остальное — как есть.
# Для ПЕРЕНОСА на новый сервер важны backup/users.json и backup/portal_accounts.json
# (+ portal.db): домены, IP, сертификаты и nginx не переносятся — их создаёт установщик.
# Сертификаты Let's Encrypt (на IP и на домен) не берём: на новом сервере выпускаются заново.
BACKUP_PATHS=(/etc/x-ui/install-result.env /etc/kit/kit.env /etc/kit-sub/config.json
  /etc/nginx/kit-stream.conf /etc/nginx/conf.d/kit.conf /var/www/kit /root/cert/self /root/cert/custom /root/3x-ui.txt)

# Портативный список пользователей (email, subId, лимиты, срок, вкл/выкл, протоколы).
backup_users_json() { # путь-куда
  local inb cl
  inb=$(api GET inbounds/list 2>/dev/null || echo '[]')
  cl=$(clients 2>/dev/null || echo '[]')
  [[ -n $cl ]] || cl='[]'
  jq -c --argjson ib "${inb:-[]}" '
    map(select(.subId != null and ((.email // "") | test("-awg[0-9]*$|-mtproto$") | not)))
    | map({email, subId,
           totalGB: (.totalGB // 0),
           expiryTime: (.expiryTime // 0),
           limitIp: (.limitIp // 0),
           enable: (.enable // false),
           protocols: [ (.inboundIds // [])[] as $id | ($ib[] | select(.id == $id) | .remark) ]})' <<<"$cl" >"$1"
}

# Портативные аккаунты кабинета (логин + хеш пароля, sub_id, токен).
backup_portal_accounts() { # путь-куда
  PORTAL_DB=$PORTAL_DB python3 - "$1" <<'PY' || echo '[]' >"$1"
import json, os, sqlite3, sys
db = os.environ.get("PORTAL_DB", "/etc/kit/portal.db")
rows = []
if os.path.exists(db):
    con = sqlite3.connect("file:%s?mode=ro" % db, uri=True)
    con.row_factory = sqlite3.Row
    for r in con.execute("select username, password_hash, salt, sub_id, sub_token, is_active, created_at, updated_at from users"):
        rows.append({k: r[k] for k in r.keys()})
    con.close()
json.dump(rows, open(sys.argv[1], "w"), ensure_ascii=False)
PY
}

cmd_backup() {
  local out tmp p ssl=none c users_n acct_n
  out=/root/kit-backup-$(date +%Y%m%d-%H%M).tar.gz
  tmp=$(mktemp -d)
  # shellcheck disable=SC2064 # путь подставляем сразу: при выходе локальной переменной уже нет
  trap "rm -rf -- '$tmp'" EXIT
  install -d -m 700 "$tmp/etc/x-ui" "$tmp/etc/kit" "$tmp/backup"

  # Базы — снимком SQLite (могут быть в WAL и работать).
  if [[ -f /etc/x-ui/x-ui.db ]]; then sqlite_snapshot /etc/x-ui/x-ui.db "$tmp/etc/x-ui/x-ui.db" || die "Не удалось снять копию базы панели."; fi
  [[ -f $PORTAL_DB ]] && sqlite_snapshot "$PORTAL_DB" "$tmp/etc/kit/portal.db" || true

  # Окружение (для режима --full на тот же сервер).
  for p in "${BACKUP_PATHS[@]}"; do if [[ -e $p ]]; then cp -a --parents "$p" "$tmp"; fi; done

  # Портативные данные для переноса (--import).
  backup_users_json "$tmp/backup/users.json" || echo '[]' >"$tmp/backup/users.json"
  backup_portal_accounts "$tmp/backup/portal_accounts.json"
  users_n=$(jq 'length' "$tmp/backup/users.json" 2>/dev/null || echo 0)
  acct_n=$(jq 'length' "$tmp/backup/portal_accounts.json" 2>/dev/null || echo 0)

  # Какой сертификат был у панели и подписки.
  c=$(jq -r '.cert // empty' /etc/kit-sub/config.json 2>/dev/null || true)
  [[ -z $c && -f /etc/nginx/conf.d/kit.conf ]] && c=$(awk '$1 == "ssl_certificate" {sub(/;$/, "", $2); print $2; exit}' /etc/nginx/conf.d/kit.conf)
  case $c in
    /root/cert/ip/*) ssl=ip ;;
    /root/cert/custom/*) ssl=custom ;;
  esac
  {
    printf 'BACKUP_KIT_VERSION=%q\n' "$KIT_VERSION"
    printf 'BACKUP_HOST=%q\n' "${HOST:-}"
    printf 'BACKUP_DOMAIN=%q\n' "${DOMAIN:-}"
    printf 'BACKUP_WS_DOMAIN=%q\n' "${WS_DOMAIN:-}"
    printf 'BACKUP_WEB_DOMAIN=%q\n' "${WEB_DOMAIN:-}"
    printf 'BACKUP_SSL=%q\n' "$ssl"
    printf 'BACKUP_DATE=%q\n' "$(date +%F)"
    printf 'BACKUP_USERS=%q\n' "$users_n"
  } >"$tmp/backup/manifest.env"

  (umask 077; tar -czf "$out" -C "$tmp" .)
  chmod 600 "$out"
  say "Резервная копия: ${B}$out${N} ($(du -h "$out" | cut -f1))"
  echo "  для переноса: пользователей ${B}$users_n${N}, аккаунтов кабинета ${B}$acct_n${N}"
  echo
  echo "В ней ключи и пароли от сервера, храните её как пароль. Скачать к себе (на компьютере):"
  echo "  ${B}scp root@$HOST:$out .${N}"
  echo "  ${D}Перенос на новый сервер: kit restore --import <архив>${N}"
  echo
}

cmd_restore() {
  local mode=import archive="" host="" domain="" wsdomain=""
  while [[ $# -gt 0 ]]; do
    case $1 in
      --import) mode=import; shift ;;
      --full) mode=full; shift ;;
      --check | --dry-run | -n) mode=check; shift ;;
      --host) need_val "$@"; host=$2; shift 2 ;;
      --domain) need_val "$@"; domain=$2; shift 2 ;;
      --ws-domain) need_val "$@"; wsdomain=$2; shift 2 ;;
      -*) die "kit restore [--import|--full|--check] <архив> [--host IP] [--domain d] [--ws-domain w]" ;;
      *) archive=$1; shift ;;
    esac
  done
  [[ -n $archive ]] || die "Укажите путь к архиву: kit restore [--import|--full|--check] /root/kit-backup-XXXX.tar.gz"
  [[ -f $archive ]] || die "Файл бэкапа не найден: $archive"
  if [[ $mode == full && ( -n $domain || -n $wsdomain ) ]]; then
    die "Смена домена несовместима с --full (kit.env и nginx разъедутся). Для нового домена используйте --import."
  fi

  local tmp
  tmp=$(mktemp -d)
  # shellcheck disable=SC2064
  trap "rm -rf -- '$tmp'" EXIT
  tar -xzf "$archive" -C "$tmp" || die "Не удалось распаковать архив (файл повреждён?)."

  case $mode in
    check) restore_check "$tmp" ;;
    full) restore_full "$tmp" "$host" ;;
    import)
      [[ -f "$tmp/backup/users.json" ]] || die "В архиве нет backup/users.json (старый формат). Сделайте свежий 'kit backup' на старом сервере; восстановить на тот же сервер: kit restore --full."
      restore_import "$tmp"
      ;;
  esac
}

# Показать содержимое архива, ничего не меняя.
restore_check() {
  local tmp=$1
  local man="$tmp/backup/manifest.env" users="$tmp/backup/users.json" accts="$tmp/backup/portal_accounts.json"
  echo "${B}Содержимое архива${N}"
  if [[ -f $man ]]; then
    # shellcheck disable=SC1090
    ( . "$man"
      echo "  версия kit:    ${BACKUP_KIT_VERSION:-?}"
      echo "  дата:          ${BACKUP_DATE:-?}"
      echo "  старый HOST:   ${BACKUP_HOST:-?}"
      echo "  домены:        ${BACKUP_DOMAIN:-–} / ${BACKUP_WS_DOMAIN:-–} / ${BACKUP_WEB_DOMAIN:-–}"
      echo "  сертификат:    ${BACKUP_SSL:-none}"
      echo "  пользователей: ${BACKUP_USERS:-0}" )
  else
    echo "  manifest.env нет — архив старого формата"
  fi
  [[ -f $users ]] && echo "  users.json:    $(jq 'length' "$users" 2>/dev/null || echo 0) записей" || echo "  users.json:    нет"
  [[ -f $accts ]] && echo "  аккаунты:      $(jq 'length' "$accts" 2>/dev/null || echo 0)" || echo "  аккаунты:      нет"
  echo
  echo "${D}--import (по умолчанию): переносит только пользователей и аккаунты кабинета."
  echo "--full: восстанавливает и окружение (только тот же сервер/домен).${N}"
}

# Перенос данных на НОВЫЙ сервер: пользователи + аккаунты кабинета. Окружение не трогаем.
restore_import() {
  local tmp=$1
  local users="$tmp/backup/users.json" acct="$tmp/backup/portal_accounts.json"
  api GET inbounds/list >/dev/null || die "Панель недоступна. Сначала установите сервер этим же скриптом, затем повторите."
  local inb; inb=$(api GET inbounds/list)

  local created=0 updated=0 failed=0
  local u email sid total exp lim enable prots ids has_awg rec have_ids missing_ids
  while IFS= read -r u; do
    email=$(jq -r '.email // empty' <<<"$u")
    [[ -n $email ]] || continue
    sid=$(jq -r '.subId // empty' <<<"$u")
    total=$(jq -r '.totalGB // 0' <<<"$u")
    exp=$(jq -r '.expiryTime // 0' <<<"$u")
    lim=$(jq -r '.limitIp // 0' <<<"$u")
    enable=$(jq -r '.enable // false' <<<"$u")
    prots=$(jq -c '.protocols // []' <<<"$u")
    has_awg=$(jq -r '([.protocols[]?] | any(. == "AmneziaWG" or . == "AmneziaWG-3.1"))' <<<"$u")

    ids=$(jq -nc --argjson ib "$inb" --argjson p "$prots" \
      '[ $ib[] | select(.protocol != "amneziawg") | select(.remark as $r | $p | index($r)) | .id ]')
    [[ $ids == "[]" ]] && ids=$(core_ids)
    [[ $ids == "[]" ]] && ids=$(non_awg_ids)

    if rec=$(client "$email" 2>/dev/null) && [[ -n $rec ]]; then
      update_user "$email" ".totalGB = \$t | .expiryTime = \$x | .limitIp = \$l | .enable = \$e" \
        --argjson t "$total" --argjson x "$exp" --argjson l "$lim" --argjson e "$enable" >/dev/null 2>&1 || true
      sid=$(jq -r '.subId // empty' <<<"$rec")
      # До-привязать протоколы, которых у пользователя ещё нет.
      have_ids=$(jq -c '.inboundIds // []' <<<"$rec")
      missing_ids=$(jq -nc --argjson all "$ids" --argjson have "$have_ids" '$all - $have')
      [[ $missing_ids != "[]" ]] && api POST "clients/$email/attach" "$(jq -nc --argjson i "$missing_ids" '{inboundIds: $i}')" >/dev/null 2>&1 || true
      updated=$((updated + 1))
    else
      [[ -n $sid ]] || sid=$(rand_id)
      if api POST clients/add "$(jq -nc --arg e "$email" --arg s "$sid" --argjson t "$total" --argjson x "$exp" \
            --argjson l "$lim" --argjson en "$enable" --argjson i "$ids" \
            '{client: {email: $e, subId: $s, totalGB: $t, expiryTime: $x, limitIp: $l, enable: $en, comment: "kit"}, inboundIds: $i}')" >/dev/null; then
        created=$((created + 1))
      else
        failed=$((failed + 1)); warn "не удалось создать пользователя $email"; continue
      fi
    fi
    [[ $has_awg == true ]] && awg_attach "$email" "$sid" "$total" "$exp" "$lim"
  done < <(jq -c '.[]' "$users")

  # Аккаунты кабинета: логин/пароль прежние, sub_id — от нового клиента.
  local map_sub
  map_sub=$(clients 2>/dev/null | jq -c '[.[] | select(.subId != null) | {(.email): .subId}] | add // {}' 2>/dev/null || true)
  [[ -n $map_sub ]] || map_sub='{}'
  import_portal_accounts "$acct" "$map_sub"

  # Чего не хватает на новом сервере.
  local want have missing
  want=$(jq -r '[.[].protocols[]?] | unique | .[]' "$users" 2>/dev/null | sort -u || true)
  have=$(jq -r '[.[].remark] | unique | .[]' <<<"$inb" 2>/dev/null | sort -u || true)
  missing=$(comm -23 <(printf '%s\n' "$want") <(printf '%s\n' "$have") 2>/dev/null | tr '\n' ' ' || true)

  echo
  say "Перенос завершён: ${G}создано $created${N}, обновлено $updated$( ((failed)) && echo ", ошибок $failed" )."
  echo "  Пароли кабинета и лимиты сохранены. Домены/сертификаты/nginx не затронуты."
  echo "  ${Y}Ссылки изменились (новый домен) — раздайте пользователям новую подписку.${N}"
  [[ -n ${missing// /} ]] && warn "на новом сервере нет протоколов:$missing — перенесённым пользователям они не выданы. Добавьте их (см. README) и повторите 'kit restore --import'."
  echo
}

# Влить аккаунты кабинета (хеш+соль пароля сохраняются — старый пароль работает).
import_portal_accounts() { # json-аккаунты json-map(email→subId)
  local acct=$1 map=$2
  [[ -f $acct ]] || { warn "нет backup/portal_accounts.json — аккаунты кабинета не перенесены"; return; }
  PORTAL_DB=$PORTAL_DB python3 - "$acct" "$map" <<'PY' || warn "не удалось перенести аккаунты кабинета"
import json, os, sqlite3, sys
db = os.environ.get("PORTAL_DB", "/etc/kit/portal.db")
accounts = json.load(open(sys.argv[1]))
m = json.loads(sys.argv[2]) if sys.argv[2].strip() else {}
con = sqlite3.connect(db, timeout=10)
con.execute("PRAGMA busy_timeout=5000")
for a in accounts:
    u = a.get("username")
    if not u:
        continue
    sub = m.get(u) or a.get("sub_id") or ""
    vals = (u, a["password_hash"], a["salt"], sub, a["sub_token"], a.get("is_active", 1),
            a.get("created_at", 0), a.get("updated_at", 0))
    try:
        con.execute(
            "INSERT INTO users (username,password_hash,salt,sub_id,sub_token,is_active,created_at,updated_at)"
            " VALUES (?,?,?,?,?,?,?,?)"
            " ON CONFLICT(username) DO UPDATE SET password_hash=excluded.password_hash, salt=excluded.salt,"
            " sub_id=excluded.sub_id, sub_token=excluded.sub_token, is_active=excluded.is_active, updated_at=excluded.updated_at", vals)
    except sqlite3.IntegrityError:
        cur = con.execute("UPDATE users SET password_hash=?, salt=?, sub_id=?, is_active=? WHERE username=?",
                          (a["password_hash"], a["salt"], sub, a.get("is_active", 1), u))
        if cur.rowcount == 0:  # конфликт по sub_token у нового логина — выдаём новый токен
            import secrets, string
            tok = "".join(secrets.choice(string.ascii_letters + string.digits) for _ in range(32))
            con.execute("INSERT INTO users (username,password_hash,salt,sub_id,sub_token,is_active,created_at,updated_at)"
                        " VALUES (?,?,?,?,?,?,?,?)",
                        (u, a["password_hash"], a["salt"], sub, tok, a.get("is_active", 1),
                         a.get("created_at", 0), a.get("updated_at", 0)))
con.commit(); con.close()
PY
}

# Восстановление окружения на ТОТ ЖЕ сервер/домен (аварийное).
restore_full() {
  local tmp=$1 host=$2
  local man="$tmp/backup/manifest.env"
  warn "--full восстанавливает окружение. Домен и IP должны совпадать с бэкапом, иначе используйте --import."
  if [[ -f $man ]]; then
    local b_kit b_dom
    b_kit=$( . "$man" 2>/dev/null; echo "${BACKUP_KIT_VERSION:-}" )
    b_dom=$( . "$man" 2>/dev/null; echo "${BACKUP_DOMAIN:-}" )
    [[ -n $b_kit && $b_kit != "$KIT_VERSION" ]] && warn "бэкап сделан на kit $b_kit, у вас $KIT_VERSION"
    [[ -n $b_dom && -n ${DOMAIN:-} && $b_dom != "$DOMAIN" ]] && warn "в бэкапе домен $b_dom, а сейчас $DOMAIN — для смены домена нужен --import"
  fi

  say "Останавливаю службы..."
  systemctl stop x-ui kit-portal 2>/dev/null || true

  if [[ -f "$tmp/etc/x-ui/x-ui.db" ]]; then
    install -d -m 700 /etc/x-ui
    cp -f "$tmp/etc/x-ui/x-ui.db" /etc/x-ui/x-ui.db
    rm -f /etc/x-ui/x-ui.db-wal /etc/x-ui/x-ui.db-shm
    chmod 600 /etc/x-ui/x-ui.db
    python3 -c 'import sqlite3,sys;print(sqlite3.connect(sys.argv[1]).execute("PRAGMA integrity_check").fetchone()[0])' /etc/x-ui/x-ui.db 2>/dev/null | grep -qi '^ok' \
      || die "База панели повреждена (integrity_check не ok) — восстановление остановлено."
    say "база панели восстановлена"
  fi
  if [[ -f "$tmp/etc/kit/portal.db" ]]; then
    install -d -m 700 /etc/kit
    cp -f "$tmp/etc/kit/portal.db" /etc/kit/portal.db
    rm -f /etc/kit/portal.db-wal /etc/kit/portal.db-shm
    chmod 600 /etc/kit/portal.db
    say "база кабинета восстановлена"
  fi

  local f
  for f in etc/x-ui/install-result.env etc/kit/kit.env etc/kit-sub/config.json root/3x-ui.txt; do
    if [[ -f "$tmp/$f" ]]; then install -d -m 700 "$(dirname "/$f")"; cp -f "$tmp/$f" "/$f"; fi
  done
  chmod 600 /etc/x-ui/install-result.env /etc/kit/kit.env 2>/dev/null || true
  [[ -f "$tmp/etc/nginx/kit-stream.conf" ]] && cp -f "$tmp/etc/nginx/kit-stream.conf" /etc/nginx/kit-stream.conf
  if [[ -f "$tmp/etc/nginx/conf.d/kit.conf" ]]; then install -d /etc/nginx/conf.d; cp -f "$tmp/etc/nginx/conf.d/kit.conf" /etc/nginx/conf.d/kit.conf; fi
  [[ -d "$tmp/var/www/kit" ]] && { install -d /var/www/kit; cp -a "$tmp/var/www/kit/." /var/www/kit/ 2>/dev/null || true; }
  [[ -d "$tmp/root/cert/self" ]] && { install -d -m 700 /root/cert/self; cp -a "$tmp/root/cert/self/." /root/cert/self/ 2>/dev/null || true; }
  [[ -d "$tmp/root/cert/custom" ]] && { install -d -m 700 /root/cert/custom; cp -a "$tmp/root/cert/custom/." /root/cert/custom/ 2>/dev/null || true; }
  chmod 600 /root/cert/*/privkey.pem 2>/dev/null || true

  # HOST под текущий сервер: в бэкапе остался старый IP.
  local newhost=${host:-}
  [[ -n $newhost ]] || newhost=$(public_ip)
  if [[ -n $newhost && -f /etc/kit/kit.env ]]; then sed -i "s#^HOST=.*#HOST=$newhost#" /etc/kit/kit.env || true; say "HOST → $newhost"; fi

  systemctl daemon-reload 2>/dev/null || true
  systemctl start x-ui kit-portal 2>/dev/null || true
  systemctl is-active -q x-ui || warn "x-ui не запустился — проверьте сертификат (LE перевыпускается на домене)."
  if nginx -t >/dev/null 2>&1; then systemctl reload nginx 2>/dev/null || true; else warn "nginx не принял конфиг — обычно нет сертификата (/root/cert/ip|domain)."; fi
  echo
  say "${G}${B}Окружение восстановлено.${N} Проверьте: kit check."
  echo "${D}Если домен сменился — используйте --import, а не --full.${N}"
  echo
}

usage() {
  cat <<EOF
${B}kit${N} $KIT_VERSION – управление сервером 3X-UI KIT

Пользователи (по умолчанию — только устойчивые протоколы: REALITY + WebSocket):
  kit user add имя [--gb 50] [--days 30] [--devices 3] [--all]   добавить пользователя и подписку
                                                          (--all – сразу все протоколы)
  kit user attach имя протокол                            выдать доп. протокол по запросу:
                                                          hy2, tuic, ss, awg, awg3, xhttp, trojan, mtproto, wg
  kit user list                                           трафик, срок, статус
  kit user link имя [--all]                               подписка и QR; --all – ещё vpn:// и tg://
  kit user limit имя [--gb N] [--days N] [--devices N]    изменить лимиты (0 – без ограничений)
  kit user passwd имя [пароль]                            сменить пароль в личном кабинете
  kit user token имя                                      сбросить ссылку на подписку Sing-box
  kit user off имя  /  kit user on имя                    выключить и включить
  kit user del имя                                        удалить

Сервер:
  kit update            обновить kit и подписку kit-sub сейчас (пользователи и ссылки не меняются)
  kit update --manual   выключить автообновление (--auto – включить обратно)
  kit backup            резервная копия (базы + переносимые данные для нового сервера)
  kit restore [режим] <файл>
                        --import (по умолчанию) — перенести пользователей и кабинеты на новый сервер
                        --full — восстановить окружение (только тот же сервер и домен)
                        --check — показать содержимое архива, ничего не менять
  kit check             проверить сервер: службы, сертификат, подписка, сайт маскировки, права
  kit fix [--dry-run]   исправить безопасное: перезапустить службы, права, автообновление, сертификат
  kit version           версия kit, панели и ядра
EOF
}

# Регистр и «users» вместо «user» не должны ломать команду; имя пользователя (третье слово) не трогаем.
cmd_key="${1:-} ${2:-}"; cmd_key=${cmd_key,,}; cmd_key=${cmd_key/users /user }
case "$cmd_key" in
  "user add") shift 2; cmd_add "$@" ;;
  "user attach") shift 2; cmd_attach "$@" ;;
  "user list") cmd_list; update_hint ;;
  "user link") shift 2; cmd_link "$@" ;;
  "user limit") shift 2; cmd_limit "$@" ;;
  "user passwd") shift 2; cmd_passwd "$@" ;;
  "user token") shift 2; cmd_token "$@" ;;
  "user off") cmd_toggle "${3:-}" false ;;
  "user on") cmd_toggle "${3:-}" true ;;
  "user del") shift 2; cmd_del "$@" ;;
  "update "*) shift; cmd_update "$@" ;;
  "backup "*) cmd_backup ;;
  "restore "*) shift; cmd_restore "$@" ;;
  "check "*) cmd_check ;;
  "fix "*) shift; cmd_fix "$@" ;;
  "version "*|"--version "*|"-v "*) cmd_version ;;
  *) usage; update_hint ;;
esac
