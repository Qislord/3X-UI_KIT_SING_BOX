#!/usr/bin/env python3
"""kit-portal – Личный кабинет пользователя и генератор Sing-box подписок.
https://github.com/itsnotkubrick/3X-UI_KIT

Обеспечивает:
  * Веб-интерфейс личного кабинета (адаптивный SPA, темная тема).
  * Безопасную авторизацию (сессии, HttpOnly cookies, PBKDF2-HMAC-SHA256, rate-limiting).
  * Динамическую генерацию полного JSON-конфига Sing-box с умной маршрутизацией и DoH 1.1.1.1.
  * Управление пользователями через CLI (для интеграции с `kit user add/del/passwd`).

Настройки читаются из /etc/kit/kit.env и /etc/x-ui/install-result.env.
"""

import argparse
import base64
import http.cookies
import http.server
import hmac
import hashlib
import json
import mimetypes
import os
import re
import secrets
import socket
import sqlite3
import ssl
import string
import sys
import threading
import time
import urllib.error
import urllib.parse
import urllib.request

# --- Конфигурация и пути ---
CONFIG_ENV_PATH = os.environ.get("KIT_ENV", "/etc/kit/kit.env")
XUI_ENV_PATH = os.environ.get("XUI_ENV", "/etc/x-ui/install-result.env")
DB_PATH = os.environ.get("PORTAL_DB", "/etc/kit/portal.db")

def get_static_dir():
    if os.environ.get("PORTAL_STATIC"):
        return os.environ["PORTAL_STATIC"]
    base = os.path.dirname(os.path.abspath(__file__))
    adj = os.path.join(base, "portal")
    if os.path.isdir(adj):
        return adj
    parent_adj = os.path.join(os.path.dirname(base), "portal")
    if os.path.isdir(parent_adj):
        return parent_adj
    return adj

STATIC_DIR = get_static_dir()

# Значения по умолчанию (переопределяются из env-файлов)
ENV = {
    "HOST": "127.0.0.1",
    "PORTAL_DOMAIN": "",
    "PORTAL_PORT": "10465",
    "SUB_BASE": "",
    "SUB_PATH": "/sub/",
    "SUB_INTERNAL": "2097",
    "SINGLE": "yes",
    "XUI_PANEL_PORT": "2053",
    "XUI_WEB_BASE_PATH": "xui",
    "XUI_API_TOKEN": "",
}


def load_env_file(path):
    """Считывает переменные окружения из shell env файла."""
    if not os.path.exists(path):
        return
    try:
        with open(path, "r", encoding="utf-8") as f:
            for line in f:
                line = line.strip()
                if not line or line.startswith("#") or "=" not in line:
                    continue
                k, v = line.split("=", 1)
                k = k.strip()
                v = v.strip().strip("'\"")
                ENV[k] = v
    except Exception as e:
        print(f"[WARN] Ошибка чтения {path}: {e}", file=sys.stderr)


load_env_file(CONFIG_ENV_PATH)
load_env_file(XUI_ENV_PATH)

PORTAL_PORT = int(os.environ.get("PORTAL_PORT") or ENV.get("PORTAL_PORT", "10465"))
PORTAL_LISTEN = os.environ.get("PORTAL_LISTEN") or ENV.get("PORTAL_LISTEN", "127.0.0.1")

# Домен портала: берем PORTAL_DOMAIN, либо DOMAIN, либо HOST
DOMAIN = os.environ.get("PORTAL_DOMAIN") or ENV.get("PORTAL_DOMAIN") or ENV.get("DOMAIN") or ENV.get("HOST", "127.0.0.1")
PORTAL_URL = f"https://{DOMAIN}"


# --- База данных SQLite ---
def get_db():
    os.makedirs(os.path.dirname(DB_PATH), exist_ok=True)
    conn = sqlite3.connect(DB_PATH, timeout=10)
    conn.row_factory = sqlite3.Row
    return conn


def init_db():
    conn = get_db()
    with conn:
        conn.execute("""
            CREATE TABLE IF NOT EXISTS users (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                username TEXT UNIQUE NOT NULL COLLATE NOCASE,
                password_hash TEXT NOT NULL,
                salt TEXT NOT NULL,
                sub_id TEXT NOT NULL,
                sub_token TEXT UNIQUE NOT NULL,
                is_active INTEGER DEFAULT 1,
                created_at INTEGER NOT NULL,
                updated_at INTEGER NOT NULL
            )
        """)
        conn.execute("""
            CREATE TABLE IF NOT EXISTS sessions (
                session_id TEXT PRIMARY KEY,
                username TEXT NOT NULL,
                created_at INTEGER NOT NULL,
                expires_at INTEGER NOT NULL
            )
        """)
        conn.execute("""
            CREATE TABLE IF NOT EXISTS login_attempts (
                ip TEXT NOT NULL,
                attempt_time INTEGER NOT NULL
            )
        """)
        conn.execute("CREATE INDEX IF NOT EXISTS idx_users_username ON users(username)")
        conn.execute("CREATE INDEX IF NOT EXISTS idx_users_sub_token ON users(sub_token)")
        conn.execute("CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at)")
        conn.execute("CREATE INDEX IF NOT EXISTS idx_attempts_ip_time ON login_attempts(ip, attempt_time)")
    conn.close()
    try:
        os.chmod(DB_PATH, 0o600)
    except OSError:
        pass


# --- Криптография и безопасность ---
def hash_password(password: str, salt: bytes) -> str:
    """PBKDF2-HMAC-SHA256 с 600 000 итерациями."""
    return hashlib.pbkdf2_hmac("sha256", password.encode("utf-8"), salt, 600000).hex()


def verify_password(password: str, salt_hex: str, stored_hash: str) -> bool:
    salt = bytes.fromhex(salt_hex)
    computed = hash_password(password, salt)
    return hmac.compare_digest(computed, stored_hash)


def generate_secure_password(length: int = 16) -> str:
    """Генерирует криптографически стойкий пароль."""
    alphabet = string.ascii_letters + string.digits + "!@#$%^&*-_=+"
    # Гарантируем наличие разных классов символов
    while True:
        pwd = "".join(secrets.choice(alphabet) for _ in range(length))
        if (any(c.islower() for c in pwd) and
            any(c.isupper() for c in pwd) and
            any(c.isdigit() for c in pwd) and
            any(c in "!@#$%^&*-_=+" for c in pwd)):
            return pwd


def generate_token(length: int = 32) -> str:
    return secrets.token_urlsafe(length)[:length]


# --- Управление пользователями в БД ---
def db_add_user(username: str, sub_id: str, password: str = None) -> dict:
    init_db()
    if not re.match(r"^[A-Za-z0-9_.-]{1,32}$", username):
        raise ValueError("Некорректное имя пользователя (допустимы: буквы, цифры, _ . - до 32 символов)")

    if not password:
        password = generate_secure_password()

    salt = secrets.token_bytes(32)
    pwd_hash = hash_password(password, salt)
    sub_token = generate_token(32)
    now = int(time.time())

    conn = get_db()
    try:
        with conn:
            conn.execute("""
                INSERT INTO users (username, password_hash, salt, sub_id, sub_token, is_active, created_at, updated_at)
                VALUES (?, ?, ?, ?, ?, 1, ?, ?)
                ON CONFLICT(username) DO UPDATE SET
                    sub_id=excluded.sub_id,
                    password_hash=excluded.password_hash,
                    salt=excluded.salt,
                    updated_at=excluded.updated_at
            """, (username, pwd_hash, salt.hex(), sub_id, sub_token, now, now))
    finally:
        conn.close()

    return {
        "username": username,
        "password": password,
        "sub_id": sub_id,
        "sub_token": sub_token,
    }


def db_delete_user(username: str):
    init_db()
    conn = get_db()
    try:
        with conn:
            conn.execute("DELETE FROM users WHERE username = ?", (username,))
            conn.execute("DELETE FROM sessions WHERE username = ?", (username,))
    finally:
        conn.close()


def db_toggle_user(username: str, active: bool):
    init_db()
    conn = get_db()
    try:
        with conn:
            conn.execute("UPDATE users SET is_active = ?, updated_at = ? WHERE username = ?",
                         (1 if active else 0, int(time.time()), username))
            if not active:
                conn.execute("DELETE FROM sessions WHERE username = ?", (username,))
    finally:
        conn.close()


def db_set_password(username: str, new_password: str):
    init_db()
    salt = secrets.token_bytes(32)
    pwd_hash = hash_password(new_password, salt)
    conn = get_db()
    try:
        with conn:
            conn.execute("UPDATE users SET password_hash = ?, salt = ?, updated_at = ? WHERE username = ?",
                         (pwd_hash, salt.hex(), int(time.time()), username))
            # Завершаем старые сессии при смене пароля
            conn.execute("DELETE FROM sessions WHERE username = ?", (username,))
    finally:
        conn.close()


def db_rotate_token(username: str) -> str:
    init_db()
    new_token = generate_token(32)
    conn = get_db()
    try:
        with conn:
            conn.execute("UPDATE users SET sub_token = ?, updated_at = ? WHERE username = ?",
                         (new_token, int(time.time()), username))
    finally:
        conn.close()
    return new_token


def db_get_user(username: str):
    init_db()
    conn = get_db()
    try:
        row = conn.execute("SELECT * FROM users WHERE username = ?", (username,)).fetchone()
        return dict(row) if row else None
    finally:
        conn.close()


def db_get_user_by_token(token: str):
    init_db()
    conn = get_db()
    try:
        row = conn.execute("SELECT * FROM users WHERE sub_token = ? AND is_active = 1", (token,)).fetchone()
        return dict(row) if row else None
    finally:
        conn.close()


def db_list_users():
    init_db()
    conn = get_db()
    try:
        rows = conn.execute("SELECT id, username, sub_id, sub_token, is_active, created_at, updated_at FROM users ORDER BY username").fetchall()
        return [dict(r) for r in rows]
    finally:
        conn.close()


# --- Сессии и Rate Limiting ---
def is_rate_limited(ip: str, username: str = "") -> bool:
    """Не более 5 попыток входа за последние 5 минут (300 сек)."""
    key = f"{ip}:{username.lower()}" if ip in ("127.0.0.1", "::1") and username else ip
    conn = get_db()
    now = int(time.time())
    cutoff = now - 300
    try:
        with conn:
            conn.execute("DELETE FROM login_attempts WHERE attempt_time < ?", (now - 3600,))
            row = conn.execute("SELECT COUNT(*) as cnt FROM login_attempts WHERE ip = ? AND attempt_time >= ?", (key, cutoff)).fetchone()
            return row["cnt"] >= 5
    finally:
        conn.close()


def record_login_attempt(ip: str, username: str = ""):
    key = f"{ip}:{username.lower()}" if ip in ("127.0.0.1", "::1") and username else ip
    conn = get_db()
    now = int(time.time())
    try:
        with conn:
            conn.execute("INSERT INTO login_attempts (ip, attempt_time) VALUES (?, ?)", (key, now))
    finally:
        conn.close()


def clear_login_attempts(ip: str, username: str = ""):
    key = f"{ip}:{username.lower()}" if ip in ("127.0.0.1", "::1") and username else ip
    conn = get_db()
    try:
        with conn:
            conn.execute("DELETE FROM login_attempts WHERE ip = ?", (key,))
    finally:
        conn.close()


def create_session(username: str) -> str:
    session_id = secrets.token_urlsafe(32)
    now = int(time.time())
    expires = now + (30 * 86400)  # 30 дней
    conn = get_db()
    try:
        with conn:
            conn.execute("INSERT INTO sessions (session_id, username, created_at, expires_at) VALUES (?, ?, ?, ?)",
                         (session_id, username, now, expires))
    finally:
        conn.close()
    return session_id


def get_user_from_session(session_id: str):
    if not session_id:
        return None
    conn = get_db()
    now = int(time.time())
    try:
        row = conn.execute("""
            SELECT u.* FROM sessions s
            JOIN users u ON s.username = u.username
            WHERE s.session_id = ? AND s.expires_at > ? AND u.is_active = 1
        """, (session_id, now)).fetchone()
        return dict(row) if row else None
    finally:
        conn.close()


def delete_session(session_id: str):
    if not session_id:
        return
    conn = get_db()
    try:
        with conn:
            conn.execute("DELETE FROM sessions WHERE session_id = ?", (session_id,))
    finally:
        conn.close()


# --- Взаимодействие с 3X-UI API ---
def get_3xui_client_info(username: str) -> dict:
    """Запрашивает данные о трафике и статусе клиента из API 3X-UI."""
    token = ENV.get("XUI_API_TOKEN", "")
    port = ENV.get("XUI_PANEL_PORT", "2053")
    base_path = ENV.get("XUI_WEB_BASE_PATH", "").strip("/")
    path_prefix = f"/{base_path}" if base_path else ""

    ctx = ssl.create_default_context()
    ctx.check_hostname = False
    ctx.verify_mode = ssl.CERT_NONE

    for scheme in ("https", "http"):
        url = f"{scheme}://127.0.0.1:{port}{path_prefix}/panel/api/clients/list"
        req = urllib.request.Request(url, headers={"Authorization": f"Bearer {token}"})
        try:
            with urllib.request.urlopen(req, timeout=5, context=ctx if scheme == "https" else None) as resp:
                data = json.loads(resp.read().decode("utf-8"))
                if not data.get("success"):
                    continue
                clients = data.get("obj") or []
                if isinstance(clients, dict) and "clients" in clients:
                    clients = clients["clients"]

                # Ищем клиента с email == username
                matched = [c for c in clients if c.get("email") == username]
                if not matched:
                    return {}
                c = matched[0]
                traffic = c.get("traffic") or {}
                up = traffic.get("up", 0)
                down = traffic.get("down", 0)
                return {
                    "email": c.get("email"),
                    "subId": c.get("subId"),
                    "enable": c.get("enable", True),
                    "totalGB": c.get("totalGB", 0),
                    "expiryTime": c.get("expiryTime", 0),
                    "up": up,
                    "down": down,
                    "used": up + down,
                    "limitIp": c.get("limitIp", 0),
                }
        except Exception:
            continue
    return {}


# --- Парсер ссылок подписки и генератор Sing-Box ---
def fix_tg_link(link: str) -> str:
    """Заменяет внутренний порт 10445 на публичный порт 443 для MTProto прокси Telegram."""
    if not link or not link.startswith("tg://"):
        return link
    # Заменяем внутренний порт 10445 на внешний 443 (в режиме единого порта через Nginx)
    link = re.sub(r'([?&]port=)10445\b', r'\g<1>443', link)
    # Если указан server=127.0.0.1 или localhost, подставляем актуальный домен или хост
    host = DOMAIN or ENV.get("HOST", "")
    if host and host not in ("127.0.0.1", "localhost"):
        link = re.sub(r'([?&]server=)(?:127\.0\.0\.1|localhost)\b', rf'\g<1>{host}', link)
    return link


def fetch_single_sub(sub_id: str) -> str:
    """Загружает одну подписку из локального бекенда 3X-UI."""
    if not sub_id:
        return ""
    sub_internal = ENV.get("SUB_INTERNAL", "2097")
    sub_path = "/" + ENV.get("SUB_PATH", "sub").strip("/") + "/"
    host = ENV.get("PORTAL_DOMAIN") or ENV.get("DOMAIN") or ENV.get("HOST", "127.0.0.1")

    ctx = ssl.create_default_context()
    ctx.check_hostname = False
    ctx.verify_mode = ssl.CERT_NONE

    for scheme in ("http", "https"):
        url = f"{scheme}://127.0.0.1:{sub_internal}{sub_path}{sub_id}"
        req = urllib.request.Request(url, headers={
            "User-Agent": "v2rayN/7.0",
            "Host": host,
            "Accept": "*/*"
        })
        try:
            with urllib.request.urlopen(req, timeout=10, context=ctx if scheme == "https" else None) as r:
                body = r.read().decode("utf-8", "replace").strip()
                # Если вернулась строка base64, декодируем её
                if "://" not in body:
                    try:
                        padding = "=" * (-len(body) % 4)
                        body = base64.b64decode(body + padding).decode("utf-8", "replace").strip()
                    except Exception:
                        pass
                if body:
                    return body
        except Exception:
            continue
    return ""


def fetch_raw_subscription(sub_id: str) -> str:
    """Загружает исходную подписку, а также подписки -tg и -awg (как в 3x-ui.sh)."""
    parts = []
    main_sub = fetch_single_sub(sub_id)
    if main_sub:
        parts.append(main_sub)

    # В 3x-ui.sh MTProto и AmneziaWG могут находиться в подписках с суффиксами -tg и -awg
    if not sub_id.endswith("-tg") and not sub_id.endswith("-awg"):
        extra_tg = fetch_single_sub(f"{sub_id}-tg")
        if extra_tg and extra_tg not in parts:
            parts.append(extra_tg)
        extra_awg = fetch_single_sub(f"{sub_id}-awg")
        if extra_awg and extra_awg not in parts:
            parts.append(extra_awg)

    full_text = "\n".join(parts)

    # Исправляем ссылки MTProto для Telegram
    fixed_lines = []
    for line in full_text.splitlines():
        line = line.strip()
        if not line:
            continue
        if line.startswith("tg://"):
            line = fix_tg_link(line)
        fixed_lines.append(line)
    return "\n".join(fixed_lines)


def parse_query_params(qs: str) -> dict:
    return {k: v[0] for k, v in urllib.parse.parse_qs(qs).items()}


def parse_proxy_link(link: str) -> dict:
    """Парсит ссылки vless, vmess, trojan, ss, hy2, tuic, wireguard."""
    link = link.strip()
    if not link or link.startswith("#"):
        return None

    try:
        u = urllib.parse.urlparse(link)
        scheme = u.scheme.lower()
        tag = urllib.parse.unquote(u.fragment) if u.fragment else f"{scheme.upper()}-{u.hostname}:{u.port}"
        params = parse_query_params(u.query)

        if scheme == "vless":
            uuid = urllib.parse.unquote(u.username or "")
            security = params.get("security", "none").lower()
            network = params.get("type", "tcp").lower()
            flow = params.get("flow", "")

            outbound = {
                "type": "vless",
                "tag": tag,
                "server": u.hostname,
                "server_port": u.port or 443,
                "uuid": uuid,
            }
            if flow:
                outbound["flow"] = flow

            tls = {}
            if security in ("tls", "reality"):
                tls["enabled"] = True
                tls["server_name"] = params.get("sni") or params.get("host") or u.hostname
                fp = params.get("fp", "chrome")
                if fp:
                    tls["utls"] = {"enabled": True, "fingerprint": fp}
                if security == "reality":
                    tls["reality"] = {
                        "enabled": True,
                        "public_key": params.get("pbk", ""),
                        "short_id": params.get("sid", ""),
                    }
                alpn = params.get("alpn")
                if alpn:
                    tls["alpn"] = [x.strip() for x in alpn.split(",") if x.strip()]
                outbound["tls"] = tls

            transport = {}
            if network == "ws":
                transport["type"] = "ws"
                transport["path"] = urllib.parse.unquote(params.get("path", "/"))
                host_hdr = params.get("host")
                if host_hdr:
                    transport["headers"] = {"Host": host_hdr}
                outbound["transport"] = transport
            elif network == "grpc":
                transport["type"] = "grpc"
                transport["service_name"] = urllib.parse.unquote(params.get("serviceName", params.get("path", "")))
                outbound["transport"] = transport
            elif network in ("httpupgrade", "xhttp"):
                transport["type"] = network
                transport["path"] = urllib.parse.unquote(params.get("path", "/"))
                host_hdr = params.get("host")
                if host_hdr:
                    transport["host"] = host_hdr
                outbound["transport"] = transport

            return outbound

        elif scheme in ("hy2", "hysteria2"):
            auth = urllib.parse.unquote(u.username or "")
            if u.password:
                auth += ":" + urllib.parse.unquote(u.password)
            sni = params.get("sni") or u.hostname
            insecure = params.get("insecure") in ("1", "true")

            tls = {
                "enabled": True,
                "server_name": sni,
            }
            if insecure:
                tls["insecure"] = True
            alpn = params.get("alpn")
            if alpn:
                tls["alpn"] = [x.strip() for x in alpn.split(",") if x.strip()]

            outbound = {
                "type": "hysteria2",
                "tag": tag,
                "server": u.hostname,
                "server_port": u.port or 443,
                "password": auth,
                "tls": tls,
            }
            obfs = params.get("obfs")
            if obfs:
                outbound["obfs"] = {
                    "type": obfs,
                    "password": params.get("obfs-password", "")
                }
            return outbound

        elif scheme == "tuic":
            uuid = urllib.parse.unquote(u.username or "")
            password = urllib.parse.unquote(u.password or "")
            sni = params.get("sni") or u.hostname

            tls = {
                "enabled": True,
                "server_name": sni,
            }
            alpn = params.get("alpn")
            if alpn:
                tls["alpn"] = [x.strip() for x in alpn.split(",") if x.strip()]
            else:
                tls["alpn"] = ["h3"]

            return {
                "type": "tuic",
                "tag": tag,
                "server": u.hostname,
                "server_port": u.port or 443,
                "uuid": uuid,
                "password": password,
                "congestion_control": params.get("congestion_control", "bbr"),
                "udp_relay_mode": params.get("udp_relay_mode", "native"),
                "tls": tls,
            }

        elif scheme == "trojan":
            password = urllib.parse.unquote(u.username or "")
            sni = params.get("sni") or u.hostname
            tls = {
                "enabled": True,
                "server_name": sni,
            }
            alpn = params.get("alpn")
            if alpn:
                tls["alpn"] = [x.strip() for x in alpn.split(",") if x.strip()]

            outbound = {
                "type": "trojan",
                "tag": tag,
                "server": u.hostname,
                "server_port": u.port or 443,
                "password": password,
                "tls": tls,
            }
            network = params.get("type", "tcp").lower()
            if network == "grpc":
                outbound["transport"] = {
                    "type": "grpc",
                    "service_name": urllib.parse.unquote(params.get("serviceName", params.get("path", "")))
                }
            elif network == "ws":
                outbound["transport"] = {
                    "type": "ws",
                    "path": urllib.parse.unquote(params.get("path", "/"))
                }
            return outbound

        elif scheme == "ss":
            body = link[len("ss://"):]
            frag = ""
            if "#" in body:
                body, frag = body.split("#", 1)
                tag = urllib.parse.unquote(frag)
            if "?" in body:
                body = body.split("?", 1)[0]
            body = body.rstrip("/")

            if "@" in body:
                userinfo, hostport = body.rsplit("@", 1)
                if ":" not in userinfo:
                    try:
                        padding = "=" * (-len(userinfo) % 4)
                        userinfo = base64.b64decode(userinfo + padding).decode("utf-8")
                    except Exception:
                        pass
            else:
                try:
                    padding = "=" * (-len(body) % 4)
                    decoded = base64.b64decode(body + padding).decode("utf-8")
                    userinfo, hostport = decoded.rsplit("@", 1)
                except Exception:
                    return None

            method, password = userinfo.split(":", 1)
            server, port = hostport.split(":", 1)
            return {
                "type": "shadowsocks",
                "tag": tag,
                "server": server,
                "server_port": int(port),
                "method": method.lower(),
                "password": password,
            }

        elif scheme == "vmess":
            raw_b64 = link[len("vmess://"):].split("#")[0]
            padding = "=" * (-len(raw_b64) % 4)
            j = json.loads(base64.b64decode(raw_b64 + padding).decode("utf-8"))
            outbound = {
                "type": "vmess",
                "tag": j.get("ps") or tag,
                "server": j.get("add"),
                "server_port": int(j.get("port", 443)),
                "uuid": j.get("id"),
                "security": j.get("scy", "auto"),
                "alter_id": int(j.get("aid", 0)),
            }
            if j.get("tls") in ("tls", "reality"):
                outbound["tls"] = {
                    "enabled": True,
                    "server_name": j.get("sni") or j.get("host") or j.get("add"),
                }
            network = (j.get("net") or "tcp").lower()
            if network == "ws":
                outbound["transport"] = {
                    "type": "ws",
                    "path": j.get("path", "/"),
                }
            elif network == "grpc":
                outbound["transport"] = {
                    "type": "grpc",
                    "service_name": j.get("path", ""),
                }
            return outbound

    except Exception as e:
        print(f"[DEBUG] Пропуск ссылки {link[:25]}...: {e}")
        return None

    return None


def generate_singbox_config(sub_id: str) -> dict:
    """Генерирует полную конфигурацию Sing-box 1.9+ с DNS DoH 1.1.1.1 и умной маршрутизацией."""
    raw_sub = fetch_raw_subscription(sub_id)
    outbounds_list = []
    tags = []

    for line in raw_sub.splitlines():
        line = line.strip()
        if not line or line.startswith(("vpn://", "tg://")):
            continue
        p = parse_proxy_link(line)
        if p and p.get("tag"):
            # Избегаем дубликатов тегов
            base_tag = p["tag"]
            idx = 2
            while p["tag"] in tags:
                p["tag"] = f"{base_tag} {idx}"
                idx += 1
            outbounds_list.append(p)
            tags.append(p["tag"])

    if not outbounds_list:
        raise ValueError("На сервере не найдено совместимых протоколов для Sing-box")

    selector_group = "Выбор подключения"
    auto_group = "Авто (быстрый)"

    full_config = {
        "log": {
            "level": "warn",
            "timestamp": True,
        },
        "dns": {
            "servers": [
                {
                    "tag": "dns-remote",
                    "address": "https://1.1.1.1/dns-query",
                    "address_resolver": "dns-direct",
                    "strategy": "prefer_ipv4",
                    "detour": selector_group,
                },
                {
                    "tag": "dns-backup",
                    "address": "https://8.8.8.8/dns-query",
                    "address_resolver": "dns-direct",
                    "strategy": "prefer_ipv4",
                    "detour": selector_group,
                },
                {
                    "tag": "dns-direct",
                    "address": "https://77.88.8.8/dns-query",
                    "address_resolver": "dns-local",
                    "strategy": "prefer_ipv4",
                    "detour": "direct",
                },
                {
                    "tag": "dns-local",
                    "address": "local",
                    "detour": "direct",
                },
                {
                    "tag": "dns-block",
                    "address": "rcode://success",
                }
            ],
            "rules": [
                {
                    "outbound": "any",
                    "server": "dns-direct"
                },
                {
                    "geosite": "category-ads-all",
                    "server": "dns-block"
                },
                {
                    "geosite": ["category-ru", "ru"],
                    "server": "dns-direct"
                }
            ],
            "final": "dns-remote",
            "strategy": "prefer_ipv4"
        },
        "inbounds": [
            {
                "type": "tun",
                "tag": "tun-in",
                "interface_name": "tun0",
                "inet4_address": "172.19.0.1/30",
                "auto_route": True,
                "strict_route": True,
                "stack": "mixed",
                "sniff": True,
                "sniff_override_destination": False,
            }
        ],
        "outbounds": [
            {
                "type": "selector",
                "tag": selector_group,
                "outbounds": [auto_group] + tags + ["direct", "block"],
                "default": auto_group,
            },
            {
                "type": "urltest",
                "tag": auto_group,
                "outbounds": tags,
                "url": "https://www.gstatic.com/generate_204",
                "interval": "3m",
                "tolerance": 50,
            }
        ] + outbounds_list + [
            {
                "type": "direct",
                "tag": "direct",
            },
            {
                "type": "block",
                "tag": "block",
            },
            {
                "type": "dns",
                "tag": "dns-out",
            }
        ],
        "route": {
            "auto_detect_interface": True,
            "final": selector_group,
            "rules": [
                # 1. DNS трафик
                {"protocol": "dns", "outbound": "dns-out"},
                {"port": 53, "outbound": "dns-out"},

                # 2. Bypass bittorrent -> direct (со скриншота)
                {"protocol": "bittorrent", "outbound": "direct"},

                # 3. Block udp443 (QUIC block со скриншота)
                {"network": "udp", "port": 443, "outbound": "block"},

                # 4. Direct LAN IP & Domains (со скриншота)
                {"ip_is_private": True, "outbound": "direct"},
                {"geoip": "private", "outbound": "direct"},
                {"geosite": "private", "outbound": "direct"},

                # 5. Bypass Russia domains & IP (со скриншота)
                {"geosite": ["category-ru", "ru"], "outbound": "direct"},
                {"geoip": "ru", "outbound": "direct"},

                # 6. Блокировка рекламы
                {"geosite": ["category-ads-all"], "outbound": "block"},
            ]
        },
        "experimental": {
            "cache_file": {
                "enabled": True,
                "store_fakeip": False,
            }
        }
    }

    return full_config


# --- HTTP Обработчик запросов портала ---
class PortalHandler(http.server.BaseHTTPRequestHandler):
    server_version = "nginx"
    sys_version = ""

    def get_client_ip(self) -> str:
        # X-Real-IP принудительно выставляется Nginx (не может быть подделан клиентом)
        real_ip = self.headers.get("X-Real-IP")
        if real_ip:
            return real_ip.strip()
        fwd = self.headers.get("X-Forwarded-For")
        if fwd:
            parts = [p.strip() for p in fwd.split(",") if p.strip()]
            if parts:
                return parts[-1]
        return self.client_address[0]

    def send_json(self, status: int, data: dict, headers: dict = None):
        body = json.dumps(data, ensure_ascii=False, indent=2).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store, no-cache, must-revalidate")
        self.send_header("X-Content-Type-Options", "nosniff")
        self.send_header("X-Frame-Options", "SAMEORIGIN")
        self.send_header("Referrer-Policy", "strict-origin-when-cross-origin")
        if headers:
            for k, v in headers.items():
                safe_v = str(v).replace("\r", "").replace("\n", "")
                try:
                    safe_v.encode("latin-1")
                except UnicodeEncodeError:
                    safe_v = safe_v.encode("ascii", "replace").decode("ascii")
                self.send_header(k, safe_v)
        self.end_headers()
        if self.command != "HEAD":
            self.wfile.write(body)

    def send_html(self, status: int, html_text: str):
        body = html_text.encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-cache")
        self.send_header("X-Content-Type-Options", "nosniff")
        self.send_header("X-Frame-Options", "SAMEORIGIN")
        self.send_header("Referrer-Policy", "strict-origin-when-cross-origin")
        self.end_headers()
        if self.command != "HEAD":
            self.wfile.write(body)

    def do_HEAD(self):
        self.do_GET()

    def do_OPTIONS(self):
        self.send_response(204)
        self.send_header("Allow", "GET, POST, HEAD, OPTIONS")
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Methods", "GET, POST, OPTIONS, HEAD")
        self.send_header("Access-Control-Allow-Headers", "Content-Type, Authorization, Cookie")
        self.end_headers()

    def parse_session_cookie(self) -> str:
        cookie_header = self.headers.get("Cookie")
        if not cookie_header:
            return None
        c = http.cookies.SimpleCookie()
        try:
            c.load(cookie_header)
            if "kit_session" in c:
                return c["kit_session"].value
        except Exception:
            pass
        return None

    def get_auth_user(self):
        token = self.parse_session_cookie()
        if not token:
            return None
        return get_user_from_session(token)

    def do_GET(self):
        parsed = urllib.parse.urlparse(self.path)
        path = parsed.path.rstrip("/") or "/"
        query = parse_query_params(parsed.query)

        # 1. Эндпоинт Sing-box подписки: /sub/singbox?token=...
        if path == "/sub/singbox":
            token = query.get("token")
            user = None
            if token:
                user = db_get_user_by_token(token)
            else:
                user = self.get_auth_user()

            if not user or not user.get("is_active"):
                return self.send_json(403, {"error": "Недействительный или отозванный токен подписки"})

            try:
                config = generate_singbox_config(user["sub_id"])
                # Заголовки инфо о подписке
                stats = get_3xui_client_info(user["username"])
                try:
                    title_domain = DOMAIN.encode("idna").decode("ascii") if DOMAIN else "VPN"
                except Exception:
                    title_domain = DOMAIN or "VPN"
                extra_headers = {
                    "Profile-Title": f"{title_domain} (Sing-Box)",
                    "Profile-Update-Interval": "24",
                }
                if stats:
                    up = stats.get("up", 0)
                    down = stats.get("down", 0)
                    total = stats.get("totalGB", 0) * 1073741824
                    exp = stats.get("expiryTime", 0) // 1000 if stats.get("expiryTime", 0) > 0 else 0
                    extra_headers["Subscription-Userinfo"] = f"upload={up}; download={down}; total={total}; expire={exp}"

                return self.send_json(200, config, extra_headers)
            except Exception as e:
                return self.send_json(500, {"error": f"Ошибка генерации конфига: {e}"})

        # 2. API Текущий пользователь: /api/me
        if path == "/api/me":
            user = self.get_auth_user()
            if not user:
                return self.send_json(401, {"error": "Требуется авторизация"})

            stats = get_3xui_client_info(user["username"])
            raw_sub = ""
            try:
                raw_sub = fetch_raw_subscription(user["sub_id"])
            except Exception:
                pass

            sub_path = "/" + ENV.get("SUB_PATH", "sub").strip("/") + "/"
            universal_sub_url = f"https://{DOMAIN}{sub_path}{user['sub_id']}"
            singbox_sub_url = f"https://{DOMAIN}/sub/singbox?token={user['sub_token']}"

            # Формируем список индивидуальных ссылок
            raw_lines = [line.strip() for line in raw_sub.splitlines() if line.strip()]
            clean_lines = []
            tg_link = ""
            parsed_proxies = []
            for line in raw_lines:
                if line.startswith("tg://"):
                    line = fix_tg_link(line)
                    tg_link = line
                    clean_lines.append(line)
                    parsed_proxies.append({
                        "tag": "Telegram MTProto Proxy",
                        "type": "TG",
                        "link": line
                    })
                    continue
                clean_lines.append(line)
                p = parse_proxy_link(line)
                if p:
                    parsed_proxies.append({
                        "tag": p.get("tag", "Прокси"),
                        "type": p.get("type", "vless").upper(),
                        "link": line
                    })
                elif "://" in line:
                    scheme = line.split("://")[0].upper()
                    frag = line.split("#")[1] if "#" in line else scheme
                    parsed_proxies.append({
                        "tag": urllib.parse.unquote(frag),
                        "type": scheme,
                        "link": line
                    })
            raw_lines = clean_lines

            return self.send_json(200, {
                "success": True,
                "user": {
                    "username": user["username"],
                    "sub_id": user["sub_id"],
                    "sub_token": user["sub_token"],
                    "created_at": user["created_at"],
                },
                "stats": stats,
                "domain": DOMAIN,
                "universal_sub_url": universal_sub_url,
                "singbox_sub_url": singbox_sub_url,
                "sub_url": universal_sub_url,  # Основная универсальная ссылка по умолчанию
                "raw_subscription": "\n".join(raw_lines),
                "tg_proxy_url": tg_link,
                "proxies": parsed_proxies,
            })

        # 3. Статические файлы фронтенда
        static_abs = os.path.realpath(STATIC_DIR)
        if path in ("/", "/index.html"):
            index_file = os.path.join(static_abs, "index.html")
            if os.path.isfile(index_file):
                with open(index_file, "r", encoding="utf-8") as f:
                    return self.send_html(200, f.read())
            return self.send_html(404, "Portal frontend not found. Check portal/index.html")

        # Отдача CSS, JS, SVG, картинок из STATIC_DIR (с защитой от Path Traversal и URL-декодированием)
        rel_path = urllib.parse.unquote(path).lstrip("/")
        file_path = os.path.realpath(os.path.join(static_abs, rel_path))
        if (file_path == static_abs or file_path.startswith(static_abs + os.sep)) and os.path.isfile(file_path):
            ctype, _ = mimetypes.guess_type(file_path)
            try:
                with open(file_path, "rb") as f:
                    content = f.read()
                self.send_response(200)
                self.send_header("Content-Type", ctype or "application/octet-stream")
                self.send_header("Content-Length", str(len(content)))
                self.send_header("Cache-Control", "no-cache, must-revalidate")
                self.send_header("X-Content-Type-Options", "nosniff")
                self.send_header("X-Frame-Options", "SAMEORIGIN")
                self.send_header("Referrer-Policy", "strict-origin-when-cross-origin")
                self.end_headers()
                if self.command != "HEAD":
                    self.wfile.write(content)
                return
            except Exception:
                pass

        self.send_json(404, {"error": "Not Found"})

    def do_POST(self):
        parsed = urllib.parse.urlparse(self.path)
        path = parsed.path.rstrip("/")
        ip = self.get_client_ip()

        try:
            content_len = int(self.headers.get("Content-Length", 0))
            if content_len < 0:
                content_len = 0
        except (ValueError, TypeError):
            content_len = 0

        if content_len > 100000:
            return self.send_json(413, {"error": "Payload too large"})

        body = self.rfile.read(content_len).decode("utf-8", "replace") if content_len > 0 else "{}"
        try:
            data = json.loads(body)
        except Exception:
            data = {}

        # 1. Авторизация: /api/login
        if path == "/api/login":
            username = str(data.get("username", "")).strip()
            password = str(data.get("password", ""))

            if not username or not password:
                return self.send_json(400, {"error": "Введите имя пользователя и пароль"})

            if is_rate_limited(ip, username):
                return self.send_json(429, {"error": "Слишком много неудачных попыток входа. Подождите 5 минут."})

            user = db_get_user(username)
            if not user or not user["is_active"]:
                record_login_attempt(ip, username)
                # Искусственная задержка против timing attacks
                time.sleep(0.3)
                return self.send_json(401, {"error": "Неверный логин или пароль"})

            if not verify_password(password, user["salt"], user["password_hash"]):
                record_login_attempt(ip, username)
                time.sleep(0.3)
                return self.send_json(401, {"error": "Неверный логин или пароль"})

            # Успешный вход
            clear_login_attempts(ip, username)
            session_id = create_session(user["username"])

            cookie = http.cookies.SimpleCookie()
            cookie["kit_session"] = session_id
            cookie["kit_session"]["path"] = "/"
            cookie["kit_session"]["httponly"] = True
            cookie["kit_session"]["secure"] = True
            cookie["kit_session"]["samesite"] = "Strict"
            cookie["kit_session"]["max-age"] = 30 * 86400

            headers = {"Set-Cookie": cookie.output(header="").strip()}
            return self.send_json(200, {
                "success": True,
                "user": {"username": user["username"], "sub_token": user["sub_token"]}
            }, headers)

        # 2. Выход: /api/logout
        if path == "/api/logout":
            sid = self.parse_session_cookie()
            if sid:
                delete_session(sid)
            cookie = http.cookies.SimpleCookie()
            cookie["kit_session"] = ""
            cookie["kit_session"]["path"] = "/"
            cookie["kit_session"]["httponly"] = True
            cookie["kit_session"]["secure"] = True
            cookie["kit_session"]["samesite"] = "Strict"
            cookie["kit_session"]["max-age"] = 0
            return self.send_json(200, {"success": True}, {"Set-Cookie": cookie.output(header="").strip()})

        # 3. Смена пароля: /api/change-password
        if path == "/api/change-password":
            user = self.get_auth_user()
            if not user:
                return self.send_json(401, {"error": "Требуется авторизация"})

            old_pwd = str(data.get("old_password", ""))
            new_pwd = str(data.get("new_password", ""))

            if not verify_password(old_pwd, user["salt"], user["password_hash"]):
                return self.send_json(400, {"error": "Текущий пароль указан неверно"})

            if len(new_pwd) < 8:
                return self.send_json(400, {"error": "Новый пароль должен содержать минимум 8 символов"})

            db_set_password(user["username"], new_pwd)
            # Инвалидируем старую сессию и создаем новую
            old_sid = self.parse_session_cookie()
            if old_sid:
                delete_session(old_sid)
            new_sid = create_session(user["username"])
            cookie = http.cookies.SimpleCookie()
            cookie["kit_session"] = new_sid
            cookie["kit_session"]["path"] = "/"
            cookie["kit_session"]["httponly"] = True
            cookie["kit_session"]["secure"] = True
            cookie["kit_session"]["samesite"] = "Strict"
            cookie["kit_session"]["max-age"] = 30 * 86400
            return self.send_json(200, {"success": True, "message": "Пароль успешно изменен"}, {"Set-Cookie": cookie.output(header="").strip()})

        # 4. Ротация токена подписки: /api/rotate-token
        if path == "/api/rotate-token":
            user = self.get_auth_user()
            if not user:
                return self.send_json(401, {"error": "Требуется авторизация"})

            new_token = db_rotate_token(user["username"])
            return self.send_json(200, {
                "success": True,
                "sub_token": new_token,
                "sub_url": f"https://{DOMAIN}/sub/singbox?token={new_token}"
            })

        self.send_json(404, {"error": "Not Found"})


class PortalServer(http.server.ThreadingHTTPServer):
    daemon_threads = True
    allow_reuse_address = True


# --- CLI Команды ---
def run_cli():
    parser = argparse.ArgumentParser(description="kit-portal CLI / Service")
    subparsers = parser.add_subparsers(dest="command")

    # serve
    subparsers.add_parser("serve", help="Запуск веб-сервера портала")

    # user
    p_user = subparsers.add_parser("user", help="Управление пользователями")
    user_subs = p_user.add_subparsers(dest="user_action")

    # user add <username> [--password P] [--sub-id S]
    p_add = user_subs.add_parser("add")
    p_add.add_argument("username")
    p_add.add_argument("--password", default=None)
    p_add.add_argument("--sub-id", default=None)

    # user del <username>
    p_del = user_subs.add_parser("del")
    p_del.add_argument("username")

    # user toggle <username> <1|0>
    p_tog = user_subs.add_parser("toggle")
    p_tog.add_argument("username")
    p_tog.add_argument("active", type=int)

    # user passwd <username> <new_password>
    p_pwd = user_subs.add_parser("passwd")
    p_pwd.add_argument("username")
    p_pwd.add_argument("password")

    # user token <username>
    p_tok = user_subs.add_parser("token")
    p_tok.add_argument("username")

    # user list
    user_subs.add_parser("list")

    # user get <username>
    p_get = user_subs.add_parser("get")
    p_get.add_argument("username")

    args = parser.parse_args()

    if args.command == "serve" or not args.command:
        init_db()
        print(f"[*] kit-portal слушает http://{PORTAL_LISTEN}:{PORTAL_PORT} (домен: {DOMAIN})")
        srv = PortalServer((PORTAL_LISTEN, PORTAL_PORT), PortalHandler)
        srv.serve_forever()
        return

    if args.command == "user":
        if args.user_action == "add":
            sub_id = args.sub_id or generate_token(16)
            res = db_add_user(args.username, sub_id, args.password)
            res["portal_url"] = PORTAL_URL
            res["singbox_sub_url"] = f"{PORTAL_URL}/sub/singbox?token={res['sub_token']}"
            print(json.dumps(res, ensure_ascii=False, indent=2))
        elif args.user_action == "del":
            db_delete_user(args.username)
            print(json.dumps({"success": True, "message": f"Пользователь {args.username} удален"}))
        elif args.user_action == "toggle":
            db_toggle_user(args.username, bool(args.active))
            st = "активирован" if args.active else "заблокирован"
            print(json.dumps({"success": True, "message": f"Пользователь {args.username} {st}"}))
        elif args.user_action == "passwd":
            db_set_password(args.username, args.password)
            print(json.dumps({"success": True, "message": f"Пароль для {args.username} обновлен"}))
        elif args.user_action == "token":
            tok = db_rotate_token(args.username)
            print(json.dumps({"success": True, "sub_token": tok, "singbox_sub_url": f"{PORTAL_URL}/sub/singbox?token={tok}"}))
        elif args.user_action == "list":
            users = db_list_users()
            print(json.dumps(users, ensure_ascii=False, indent=2))
        elif args.user_action == "get":
            u = db_get_user(args.username)
            if u:
                del u["password_hash"]
                del u["salt"]
                print(json.dumps(u, ensure_ascii=False, indent=2))
            else:
                print(json.dumps({"error": "User not found"}), file=sys.stderr)
                sys.exit(1)


if __name__ == "__main__":
    run_cli()
