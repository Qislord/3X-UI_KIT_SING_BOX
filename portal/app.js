/**
 * Личный кабинет skip-flash (User Portal) – Vanilla JavaScript SPA
 * https://github.com/itsnotkubrick/3X-UI_KIT
 */

(function () {
  'use strict';

  // --- Элементы DOM ---
  const loginView = document.getElementById('loginView');
  const dashboardView = document.getElementById('dashboardView');
  const headerAuth = document.getElementById('headerAuth');
  const headerUsername = document.getElementById('headerUsername');
  const portalBrandTitle = document.getElementById('portalBrandTitle');
  const logoutBtn = document.getElementById('logoutBtn');

  // Форма входа
  const loginForm = document.getElementById('loginForm');
  const usernameInput = document.getElementById('usernameInput');
  const passwordInput = document.getElementById('passwordInput');
  const loginAlert = document.getElementById('loginAlert');
  const submitLoginBtn = document.getElementById('submitLoginBtn');
  const togglePasswordBtn = document.getElementById('togglePasswordBtn');

  // Статистика дашборда
  const accountStatusText = document.getElementById('accountStatusText');
  const accountUserComment = document.getElementById('accountUserComment');
  const trafficUsedText = document.getElementById('trafficUsedText');
  const trafficProgressBar = document.getElementById('trafficProgressBar');
  const trafficPercentText = document.getElementById('trafficPercentText');
  const expireDateText = document.getElementById('expireDateText');
  const daysLeftText = document.getElementById('daysLeftText');

  // Поля ссылок
  const universalSubUrlInput = document.getElementById('universalSubUrlInput');
  const singboxSubUrlInput = document.getElementById('singboxSubUrlInput');
  const rawAllTextarea = document.getElementById('rawAllTextarea');
  const rawTextareaWrap = document.getElementById('rawTextareaWrap');
  const toggleRawTextBtn = document.getElementById('toggleRawTextBtn');
  const proxiesListGrid = document.getElementById('proxiesListGrid');

  // Кнопки копирования и действий
  const copyUniversalSubBtn = document.getElementById('copyUniversalSubBtn');
  const qrUniversalBtn = document.getElementById('qrUniversalBtn');
  const copySingboxSubBtn = document.getElementById('copySingboxSubBtn');
  const qrSingboxBtn = document.getElementById('qrSingboxBtn');
  const copyJsonBtn = document.getElementById('copyJsonBtn');
  const rotateTokenBtn = document.getElementById('rotateTokenBtn');
  const copyAllRawBtn = document.getElementById('copyAllRawBtn');

  // Telegram баннер и кнопки
  const tgProxyBanner = document.getElementById('tgProxyBanner');
  const openTgProxyBtn = document.getElementById('openTgProxyBtn');
  const copyTgProxyBtn = document.getElementById('copyTgProxyBtn');
  const guideTgProxyBtn = document.getElementById('guideTgProxyBtn');

  // Модалка QR-кода
  const qrModal = document.getElementById('qrModal');
  const qrModalTitle = document.getElementById('qrModalTitle');
  const qrModalHint = document.getElementById('qrModalHint');
  const closeQrModalBtn = document.getElementById('closeQrModalBtn');
  const qrCanvasBox = document.getElementById('qrCanvasBox');
  const qrTextValue = document.getElementById('qrTextValue');
  const copyFromQrBtn = document.getElementById('copyFromQrBtn');

  // Форма смены пароля
  const changePasswordForm = document.getElementById('changePasswordForm');
  const oldPasswordInput = document.getElementById('oldPasswordInput');
  const newPasswordInput = document.getElementById('newPasswordInput');
  const passwordAlert = document.getElementById('passwordAlert');

  // Табы форматов подключения
  const connTabButtons = document.querySelectorAll('.conn-tab-btn');
  const connContents = document.querySelectorAll('.conn-content');

  // Табы операционных систем (инструкции)
  const tabButtons = document.querySelectorAll('.tab-btn');
  const tabContents = document.querySelectorAll('.tab-content');

  // Футер
  const currentYearSpan = document.getElementById('currentYear');
  if (currentYearSpan) currentYearSpan.textContent = new Date().getFullYear();

  let currentUserData = null;
  let currentActiveQrUrl = '';

  // --- Вспомогательные функции ---
  function formatBytes(bytes) {
    if (!bytes || bytes <= 0) return '0 Б';
    const units = ['Б', 'КБ', 'МБ', 'ГБ', 'ТБ'];
    let i = 0;
    let b = Number(bytes);
    while (b >= 1024 && i < units.length - 1) {
      b /= 1024;
      i++;
    }
    return (i === 0 ? Math.round(b) : b.toFixed(1)) + ' ' + units[i];
  }

  function showToast(message, type = 'success') {
    const container = document.getElementById('toastContainer');
    if (!container) return;
    const toast = document.createElement('div');
    toast.className = `toast toast-${type}`;
    toast.textContent = message;
    container.appendChild(toast);
    setTimeout(() => {
      toast.style.opacity = '0';
      toast.style.transition = 'opacity 0.2s ease';
      setTimeout(() => toast.remove(), 250);
    }, 3500);
  }

  async function copyToClipboard(text, successMsg = 'Скопировано в буфер обмена!') {
    if (!text || !String(text).trim()) {
      showToast('Нет данных для копирования', 'error');
      return false;
    }
    try {
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(text);
      } else {
        const ta = document.createElement('textarea');
        ta.value = text;
        ta.style.position = 'fixed';
        ta.style.left = '-9999px';
        document.body.appendChild(ta);
        ta.select();
        document.execCommand('copy');
        ta.remove();
      }
      showToast(successMsg, 'success');
      return true;
    } catch (err) {
      showToast('Выделите и скопируйте вручную', 'error');
      return false;
    }
  }

  // --- Сетевые запросы к API ---
  async function apiRequest(endpoint, options = {}) {
    const opts = {
      headers: {
        'Accept': 'application/json',
        'Content-Type': 'application/json',
      },
      ...options,
    };
    if (opts.body && typeof opts.body === 'object') {
      opts.body = JSON.stringify(opts.body);
    }
    const resp = await fetch(endpoint, opts);
    let data;
    try {
      data = await resp.json();
    } catch (e) {
      data = { error: 'Некорректный ответ сервера' };
    }
    if (!resp.ok) {
      throw new Error(data.error || `Ошибка (${resp.status})`);
    }
    return data;
  }

  // --- Загрузка и рендеринг кабинета ---
  async function checkAuth() {
    try {
      const data = await apiRequest('/api/me');
      if (data && data.success) {
        currentUserData = data;
        renderDashboard(data);
      } else {
        renderLogin();
      }
    } catch (e) {
      renderLogin();
    }
  }

  function renderLogin() {
    loginView.hidden = false;
    dashboardView.hidden = true;
    headerAuth.hidden = true;
    closeQrModal();
  }

  function renderDashboard(data) {
    loginView.hidden = true;
    dashboardView.hidden = false;
    headerAuth.hidden = false;
    closeQrModal();

    const user = data.user || {};
    const stats = data.stats || {};
    const domain = data.domain || window.location.hostname;

    headerUsername.textContent = user.username || 'Клиент';
    if (portalBrandTitle) portalBrandTitle.textContent = domain;

    // 1. Ссылки на подписки
    const universalUrl = data.universal_sub_url || `https://${domain}/sub/${user.sub_id}`;
    const singboxUrl = data.singbox_sub_url || `https://${domain}/sub/singbox?token=${user.sub_token}`;
    const rawSub = data.raw_subscription || '';

    universalSubUrlInput.value = universalUrl;
    singboxSubUrlInput.value = singboxUrl;
    rawAllTextarea.value = rawSub;

    // 2. Telegram прокси
    const tgUrl = data.tg_proxy_url || '';
    if (tgUrl) {
      tgProxyBanner.hidden = false;
      openTgProxyBtn.href = tgUrl;
      if (guideTgProxyBtn) guideTgProxyBtn.href = tgUrl;
    } else {
      tgProxyBanner.hidden = true;
    }

    // 3. Статус аккаунта
    const isEnable = stats.enable !== undefined ? stats.enable : true;
    if (isEnable) {
      accountStatusText.textContent = 'Активен';
      accountStatusText.className = 'stat-value text-success';
      accountUserComment.textContent = 'Все серверы онлайн';
    } else {
      accountStatusText.textContent = 'Отключен';
      accountStatusText.className = 'stat-value text-danger';
      accountUserComment.textContent = 'Доступ временно приостановлен';
    }

    // 4. Расход трафика
    const usedBytes = stats.used || 0;
    const totalGB = stats.totalGB || 0;
    const totalBytes = totalGB * 1073741824;

    if (totalGB > 0) {
      trafficUsedText.textContent = `${formatBytes(usedBytes)} / ${totalGB} ГБ`;
      const pct = Math.min(100, Math.round((usedBytes / totalBytes) * 100));
      trafficProgressBar.style.width = pct + '%';
      trafficPercentText.textContent = `${pct}% использовано`;

      if (pct >= 90) {
        trafficProgressBar.style.backgroundColor = 'var(--danger)';
      } else if (pct >= 70) {
        trafficProgressBar.style.backgroundColor = 'var(--warning)';
      } else {
        trafficProgressBar.style.backgroundColor = 'var(--accent-green)';
      }
    } else {
      trafficUsedText.textContent = `${formatBytes(usedBytes)} / ∞`;
      trafficProgressBar.style.width = '100%';
      trafficProgressBar.style.backgroundColor = 'var(--accent-green)';
      trafficPercentText.textContent = 'Безлимитный тариф';
    }

    // 5. Срок действия
    const expMs = stats.expiryTime || 0;
    if (expMs > 0) {
      const expDate = new Date(expMs);
      const now = new Date();
      const diffDays = Math.ceil((expDate - now) / (1000 * 60 * 60 * 24));

      expireDateText.textContent = expDate.toLocaleDateString('ru-RU');
      if (diffDays > 0) {
        daysLeftText.textContent = `Осталось дней: ${diffDays}`;
      } else {
        daysLeftText.textContent = 'Срок подписки истёк';
        expireDateText.className = 'stat-value text-danger';
      }
    } else {
      expireDateText.textContent = 'Бессрочно';
      daysLeftText.textContent = 'Без ограничений по времени';
    }

    // 6. Рендеринг списка индивидуальных протоколов
    renderProxiesList(data.proxies || []);
  }

  // Рендеринг списка карточек для отдельных протоколов
  function renderProxiesList(proxies) {
    if (!proxiesListGrid) return;
    proxiesListGrid.innerHTML = '';

    if (!proxies || proxies.length === 0) {
      proxiesListGrid.innerHTML = '<div class="loading-placeholder">Протоколы пока не загружены.</div>';
      return;
    }

    proxies.forEach((p) => {
      const card = document.createElement('div');
      card.className = 'proxy-row-card';

      const infoDiv = document.createElement('div');
      infoDiv.className = 'proxy-info';

      const tagBadge = document.createElement('span');
      tagBadge.className = 'proxy-type-tag';
      tagBadge.textContent = p.type || 'PROXY';

      const nameSpan = document.createElement('span');
      nameSpan.className = 'proxy-name';
      nameSpan.textContent = p.tag || 'Сервер';

      infoDiv.appendChild(tagBadge);
      infoDiv.appendChild(nameSpan);

      const actionsDiv = document.createElement('div');
      actionsDiv.className = 'proxy-actions';

      // Кнопка копирования
      const copyBtn = document.createElement('button');
      copyBtn.type = 'button';
      copyBtn.className = 'btn btn-sm btn-secondary';
      copyBtn.innerHTML = '<span>📋</span> Копировать';
      copyBtn.addEventListener('click', () => {
        copyToClipboard(p.link, `Ссылка на ${p.tag} скопирована!`);
      });

      // Кнопка QR
      const qrBtn = document.createElement('button');
      qrBtn.type = 'button';
      qrBtn.className = 'btn btn-sm btn-secondary';
      qrBtn.innerHTML = '<span>📱</span> QR';
      qrBtn.addEventListener('click', () => {
        openQrModal(p.link, `QR-код: ${p.tag}`, `Отсканируйте камерой в приложении (V2Box, Happ, v2rayNG):`);
      });

      actionsDiv.appendChild(copyBtn);
      actionsDiv.appendChild(qrBtn);

      card.appendChild(infoDiv);
      card.appendChild(actionsDiv);
      proxiesListGrid.appendChild(card);
    });
  }

  // --- Обработка входа (Login) ---
  loginForm.addEventListener('submit', async (e) => {
    e.preventDefault();
    loginAlert.hidden = true;
    const username = usernameInput.value.trim();
    const password = passwordInput.value;

    if (!username || !password) {
      loginAlert.textContent = 'Укажите логин и пароль';
      loginAlert.hidden = false;
      return;
    }

    submitLoginBtn.disabled = true;
    submitLoginBtn.querySelector('.btn-text').textContent = 'Вход...';

    try {
      const res = await apiRequest('/api/login', {
        method: 'POST',
        body: { username, password },
      });
      if (res.success) {
        showToast(`Добро пожаловать, ${username}!`);
        await checkAuth();
      }
    } catch (err) {
      loginAlert.textContent = err.message || 'Ошибка авторизации';
      loginAlert.hidden = false;
    } finally {
      submitLoginBtn.disabled = false;
      submitLoginBtn.querySelector('.btn-text').textContent = 'Войти в кабинет';
    }
  });

  // Показ / скрытие пароля
  togglePasswordBtn.addEventListener('click', () => {
    if (passwordInput.type === 'password') {
      passwordInput.type = 'text';
      togglePasswordBtn.textContent = '🔒';
    } else {
      passwordInput.type = 'password';
      togglePasswordBtn.textContent = '👁️';
    }
  });

  // Выход из системы (Logout)
  logoutBtn.addEventListener('click', async () => {
    try {
      await apiRequest('/api/logout', { method: 'POST' });
    } catch (e) {
      // Игнорируем ошибку при логауте
    }
    currentUserData = null;
    passwordInput.value = '';
    showToast('Вы вышли из системы');
    renderLogin();
  });

  // --- Переключение табов формата подключения ---
  connTabButtons.forEach((btn) => {
    btn.addEventListener('click', () => {
      const targetId = btn.getAttribute('data-target');
      connTabButtons.forEach((b) => b.classList.remove('active'));
      connContents.forEach((c) => c.classList.remove('active'));

      btn.classList.add('active');
      const targetContent = document.getElementById(targetId);
      if (targetContent) targetContent.classList.add('active');
    });
  });

  // --- Переключение табов операционных систем (Инструкции) ---
  tabButtons.forEach((btn) => {
    btn.addEventListener('click', () => {
      const targetId = btn.getAttribute('data-tab');
      tabButtons.forEach((b) => b.classList.remove('active'));
      tabContents.forEach((c) => c.classList.remove('active'));

      btn.classList.add('active');
      const targetContent = document.getElementById(targetId);
      if (targetContent) targetContent.classList.add('active');
    });
  });

  // --- Копирование ссылок ---
  copyUniversalSubBtn.addEventListener('click', () => {
    const url = universalSubUrlInput.value;
    if (url) {
      copyToClipboard(url, '⚡ Универсальная ссылка на подписку скопирована!');
    }
  });

  copySingboxSubBtn.addEventListener('click', () => {
    const url = singboxSubUrlInput.value;
    if (url) {
      copyToClipboard(url, '🧠 Ссылка на Sing-box JSON скопирована!');
    }
  });

  copyAllRawBtn.addEventListener('click', () => {
    const text = rawAllTextarea.value;
    if (text) {
      copyToClipboard(text, '📋 Все протоколы скопированы! Можно вставлять в V2Box/v2rayN');
    }
  });

  // Быстрые кнопки копирования из инструкций
  document.querySelectorAll('.copy-quick-sub-btn').forEach((btn) => {
    btn.addEventListener('click', () => {
      const url = universalSubUrlInput.value;
      if (url) {
        copyToClipboard(url, '⚡ Ссылка скопирована в буфер обмена!');
      }
    });
  });

  // Копирование Telegram прокси
  if (copyTgProxyBtn) {
    copyTgProxyBtn.addEventListener('click', () => {
      const url = currentUserData ? currentUserData.tg_proxy_url : '';
      if (url) {
        copyToClipboard(url, '✈️ Ссылка на Telegram прокси скопирована!');
      }
    });
  }

  // Переключение видимости текстового поля всех ключей
  toggleRawTextBtn.addEventListener('click', () => {
    if (rawTextareaWrap.hidden) {
      rawTextareaWrap.hidden = false;
      toggleRawTextBtn.innerHTML = '<span>👁️</span> Скрыть текстовый вид';
    } else {
      rawTextareaWrap.hidden = true;
      toggleRawTextBtn.innerHTML = '<span>👁️</span> Показать текстовый вид';
    }
  });

  // Скачивание / Копирование JSON
  copyJsonBtn.addEventListener('click', async () => {
    const url = singboxSubUrlInput.value;
    if (!url) return;
    copyJsonBtn.disabled = true;
    const origHtml = copyJsonBtn.innerHTML;
    copyJsonBtn.innerHTML = '<span>⏳</span> Загрузка...';

    try {
      const res = await fetch(url);
      if (!res.ok) throw new Error('Ошибка получения конфига');
      const text = await res.text();
      await copyToClipboard(text, '📄 Полный Sing-box JSON конфиг скопирован!');
    } catch (err) {
      showToast('Не удалось загрузить JSON: ' + err.message, 'error');
    } finally {
      copyJsonBtn.disabled = false;
      copyJsonBtn.innerHTML = origHtml;
    }
  });

  // Сброс токена подписки
  rotateTokenBtn.addEventListener('click', async () => {
    const confirm = window.confirm(
      'Вы уверены, что хотите сбросить токен подписки?\n\n' +
      'Старая ссылка на всех ваших устройствах перестанет работать. ' +
      'Вам потребуется вставить новую ссылку в приложения.'
    );
    if (!confirm) return;

    try {
      const res = await apiRequest('/api/rotate-token', { method: 'POST' });
      if (res.success && res.sub_url) {
        singboxSubUrlInput.value = res.sub_url;
        showToast('Токен обновлен! Новая ссылка готова.', 'success');
      }
    } catch (err) {
      showToast('Ошибка сброса: ' + err.message, 'error');
    }
  });

  // Смена пароля
  changePasswordForm.addEventListener('submit', async (e) => {
    e.preventDefault();
    passwordAlert.hidden = true;

    const old_password = oldPasswordInput.value;
    const new_password = newPasswordInput.value;

    if (new_password.length < 8) {
      passwordAlert.textContent = 'Новый пароль должен содержать минимум 8 знаков';
      passwordAlert.className = 'alert alert-danger';
      passwordAlert.hidden = false;
      return;
    }

    try {
      const res = await apiRequest('/api/change-password', {
        method: 'POST',
        body: { old_password, new_password },
      });
      if (res.success) {
        passwordAlert.textContent = 'Пароль успешно обновлен!';
        passwordAlert.className = 'alert alert-info';
        passwordAlert.hidden = false;
        oldPasswordInput.value = '';
        newPasswordInput.value = '';
        showToast('Пароль успешно изменен', 'success');
      }
    } catch (err) {
      passwordAlert.textContent = err.message || 'Ошибка смены пароля';
      passwordAlert.className = 'alert alert-danger';
      passwordAlert.hidden = false;
    }
  });

  // --- Модальное окно QR-кода ---
  function openQrModal(url, title = 'QR-код подписки', hint = 'Наведите камеру в приложении (V2Box, Hiddify, v2rayNG):') {
    if (!url) {
      showToast('Нет данных для QR-кода', 'error');
      return;
    }

    currentActiveQrUrl = url;
    if (qrModalTitle) qrModalTitle.textContent = title;
    if (qrModalHint) qrModalHint.textContent = hint;
    qrCanvasBox.innerHTML = '';
    qrTextValue.textContent = url;

    // Генерируем QR-код через чистый SVG
    renderQrCode(qrCanvasBox, url);

    if (qrModal) {
      qrModal.removeAttribute('hidden');
      qrModal.classList.remove('hidden');
      qrModal.classList.add('active');
      qrModal.style.display = 'flex';
    }
  }

  function closeQrModal() {
    if (!qrModal) return;
    qrModal.setAttribute('hidden', '');
    qrModal.classList.add('hidden');
    qrModal.classList.remove('active');
    qrModal.style.display = 'none';
  }

  qrUniversalBtn.addEventListener('click', () => {
    const url = universalSubUrlInput.value;
    openQrModal(url, 'Универсальная подписка', 'Наведите камеру в приложении (V2Box, Hiddify, v2rayNG):');
  });

  qrSingboxBtn.addEventListener('click', () => {
    const url = singboxSubUrlInput.value;
    openQrModal(url, 'Sing-box JSON профиль', 'Наведите камеру в Sing-box или Karing:');
  });

  if (closeQrModalBtn) {
    closeQrModalBtn.addEventListener('click', (e) => {
      e.preventDefault();
      closeQrModal();
    });
  }

  if (qrModal) {
    qrModal.addEventListener('click', (e) => {
      if (e.target === qrModal) closeQrModal();
    });
  }

  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && qrModal && !qrModal.hasAttribute('hidden')) {
      closeQrModal();
    }
  });

  if (copyFromQrBtn) {
    copyFromQrBtn.addEventListener('click', () => {
      if (currentActiveQrUrl) {
        copyToClipboard(currentActiveQrUrl, 'Ссылка скопирована!');
      }
      closeQrModal();
    });
  }

  function renderQrCode(container, text) {
    if (!container || !text) return;
    if (window.QRGenerator && typeof window.QRGenerator.generateSVG === 'function') {
      container.innerHTML = window.QRGenerator.generateSVG(text, 220);
    } else {
      container.innerHTML = `<div style="padding:20px;word-break:break-all;font-size:0.8rem">${text}</div>`;
    }
  }

  // --- Инициализация ---
  closeQrModal();
  checkAuth();

})();
