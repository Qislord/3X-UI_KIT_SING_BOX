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

  // Дашборд
  const accountStatusText = document.getElementById('accountStatusText');
  const accountUserComment = document.getElementById('accountUserComment');
  const trafficUsedText = document.getElementById('trafficUsedText');
  const trafficProgressBar = document.getElementById('trafficProgressBar');
  const trafficPercentText = document.getElementById('trafficPercentText');
  const expireDateText = document.getElementById('expireDateText');
  const daysLeftText = document.getElementById('daysLeftText');
  const subUrlInput = document.getElementById('subUrlInput');

  // Кнопки
  const copySubBtn = document.getElementById('copySubBtn');
  const copyJsonBtn = document.getElementById('copyJsonBtn');
  const qrBtn = document.getElementById('qrBtn');
  const rotateTokenBtn = document.getElementById('rotateTokenBtn');

  // Модалка QR
  const qrModal = document.getElementById('qrModal');
  const closeQrModalBtn = document.getElementById('closeQrModalBtn');
  const qrCanvasBox = document.getElementById('qrCanvasBox');
  const qrTextValue = document.getElementById('qrTextValue');
  const copyFromQrBtn = document.getElementById('copyFromQrBtn');

  // Смена пароля
  const changePasswordForm = document.getElementById('changePasswordForm');
  const oldPasswordInput = document.getElementById('oldPasswordInput');
  const newPasswordInput = document.getElementById('newPasswordInput');
  const passwordAlert = document.getElementById('passwordAlert');

  // Табы
  const tabButtons = document.querySelectorAll('.tab-btn');
  const tabContents = document.querySelectorAll('.tab-content');

  // Футер
  const currentYearSpan = document.getElementById('currentYear');
  if (currentYearSpan) currentYearSpan.textContent = new Date().getFullYear();

  let currentUserData = null;

  // --- Вспомогательные функции форматирования ---
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
    const toast = document.createElement('div');
    toast.className = `toast toast-${type}`;
    toast.textContent = message;
    container.appendChild(toast);
    setTimeout(() => {
      toast.style.opacity = '0';
      toast.style.transform = 'translateY(10px)';
      toast.style.transition = 'all 0.3s ease';
      setTimeout(() => toast.remove(), 300);
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
      showToast('Не удалось скопировать, скопируйте вручную', 'error');
      return false;
    }
  }

  // --- API Запросы ---
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
      throw new Error(data.error || `Ошибка сервера (${resp.status})`);
    }
    return data;
  }

  // --- Загрузка данных пользователя ---
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
    const domain = data.domain || 'skip-flash.ru';

    headerUsername.textContent = user.username || 'Клиент';
    if (portalBrandTitle) portalBrandTitle.textContent = domain;

    // Ссылка подписки
    const subUrl = data.sub_url || `https://${domain}/sub/singbox?token=${user.sub_token}`;
    subUrlInput.value = subUrl;

    // Статус аккаунта
    const isEnable = stats.enable !== undefined ? stats.enable : true;
    if (isEnable) {
      accountStatusText.textContent = 'Активен';
      accountStatusText.className = 'stat-value text-success';
      accountUserComment.textContent = 'Все протоколы онлайн';
    } else {
      accountStatusText.textContent = 'Отключен';
      accountStatusText.className = 'stat-value text-danger';
      accountUserComment.textContent = 'Доступ временно приостановлен';
    }

    // Трафик
    const usedBytes = stats.used || 0;
    const totalGB = stats.totalGB || 0;
    const totalBytes = totalGB * 1073741824;

    if (totalGB > 0) {
      trafficUsedText.textContent = `${formatBytes(usedBytes)} / ${totalGB} ГБ`;
      const pct = Math.min(100, Math.round((usedBytes / totalBytes) * 100));
      trafficProgressBar.style.width = pct + '%';
      trafficPercentText.textContent = `${pct}% использовано`;

      if (pct >= 90) {
        trafficProgressBar.style.background = 'var(--danger)';
      } else if (pct >= 70) {
        trafficProgressBar.style.background = 'var(--warning)';
      } else {
        trafficProgressBar.style.background = 'linear-gradient(90deg, var(--accent) 0%, var(--accent-light) 100%)';
      }
    } else {
      trafficUsedText.textContent = `${formatBytes(usedBytes)} / ∞`;
      trafficProgressBar.style.width = '100%';
      trafficProgressBar.style.background = 'linear-gradient(90deg, var(--accent) 0%, var(--accent-light) 100%)';
      trafficPercentText.textContent = 'Безлимитный тариф';
    }

    // Срок действия
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
  }

  // --- Обработка входа (Login) ---
  loginForm.addEventListener('submit', async (e) => {
    e.preventDefault();
    loginAlert.hidden = true;
    const username = usernameInput.value.trim();
    const password = passwordInput.value;

    if (!username || !password) {
      loginAlert.textContent = 'Заполните логин и пароль';
      loginAlert.hidden = false;
      return;
    }

    submitLoginBtn.disabled = true;
    submitLoginBtn.querySelector('.btn-text').textContent = 'Проверка...';

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
      loginAlert.textContent = err.message || 'Ошибка входа';
      loginAlert.hidden = false;
    } finally {
      submitLoginBtn.disabled = false;
      submitLoginBtn.querySelector('.btn-text').textContent = 'Войти в кабинет';
    }
  });

  // Переключение видимости пароля
  togglePasswordBtn.addEventListener('click', () => {
    if (passwordInput.type === 'password') {
      passwordInput.type = 'text';
      togglePasswordBtn.textContent = '🔒';
    } else {
      passwordInput.type = 'password';
      togglePasswordBtn.textContent = '👁️';
    }
  });

  // --- Выход из системы (Logout) ---
  logoutBtn.addEventListener('click', async () => {
    try {
      await apiRequest('/api/logout', { method: 'POST' });
    } catch (e) {
      // Игнорируем
    }
    currentUserData = null;
    passwordInput.value = '';
    showToast('Вы успешно вышли из системы');
    renderLogin();
  });

  // --- Копирование ссылки подписки ---
  copySubBtn.addEventListener('click', () => {
    const url = subUrlInput.value;
    if (url) {
      copyToClipboard(url, '⚡ Ссылка на Sing-box подписку скопирована!');
    }
  });

  subUrlInput.addEventListener('click', () => {
    subUrlInput.select();
  });

  // --- Копирование сырого JSON ---
  copyJsonBtn.addEventListener('click', async () => {
    const url = subUrlInput.value;
    if (!url) return;
    copyJsonBtn.disabled = true;
    const origText = copyJsonBtn.innerHTML;
    copyJsonBtn.innerHTML = '<span>⏳</span> Загрузка...';

    try {
      const res = await fetch(url);
      if (!res.ok) throw new Error('Ошибка получения конфига');
      const text = await res.text();
      await copyToClipboard(text, '📄 Полный Sing-box JSON скопирован!');
    } catch (err) {
      showToast('Не удалось загрузить JSON: ' + err.message, 'error');
    } finally {
      copyJsonBtn.disabled = false;
      copyJsonBtn.innerHTML = origText;
    }
  });

  // --- Сброс токена подписки ---
  rotateTokenBtn.addEventListener('click', async () => {
    const confirm = window.confirm(
      'Вы уверены, что хотите сбросить ссылку на подписку?\n\n' +
      'Старая ссылка на всех ваших устройствах перестанет работать. ' +
      'Вам потребуется обновить её в приложении Sing-box.'
    );
    if (!confirm) return;

    try {
      const res = await apiRequest('/api/rotate-token', { method: 'POST' });
      if (res.success && res.sub_url) {
        subUrlInput.value = res.sub_url;
        showToast('Токен обновлен! Новая ссылка готова.', 'success');
      }
    } catch (err) {
      showToast('Ошибка сброса: ' + err.message, 'error');
    }
  });

  // --- Смена пароля ---
  changePasswordForm.addEventListener('submit', async (e) => {
    e.preventDefault();
    passwordAlert.hidden = true;

    const old_password = oldPasswordInput.value;
    const new_password = newPasswordInput.value;

    if (new_password.length < 8) {
      passwordAlert.textContent = 'Новый пароль должен содержать не менее 8 символов';
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

  // --- Табы операционных систем ---
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

  // --- Модалка QR-кода и легковесный генератор SVG QR ---
  function openQrModal() {
    const url = subUrlInput ? subUrlInput.value.trim() : '';
    if (!url) {
      showToast('Сначала войдите в систему', 'error');
      return;
    }

    qrCanvasBox.innerHTML = '';
    qrTextValue.textContent = url;

    // Генерируем QR-код через SVG
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

  if (qrBtn) qrBtn.addEventListener('click', openQrModal);

  if (closeQrModalBtn) {
    closeQrModalBtn.addEventListener('click', (e) => {
      e.preventDefault();
      e.stopPropagation();
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
      const url = subUrlInput ? subUrlInput.value.trim() : '';
      if (url) {
        copyToClipboard(url, 'Ссылка скопирована!');
      }
      closeQrModal();
    });
  }

  // --- Локальный генератор QR-кода на чистом JS (100% offline & zero network) ---
  function renderQrCode(container, text) {
    if (!container || !text) return;
    if (window.QRGenerator && typeof window.QRGenerator.generateSVG === 'function') {
      container.innerHTML = window.QRGenerator.generateSVG(text, 220);
    } else {
      container.innerHTML = `<div style="padding:20px;word-break:break-all;font-size:0.8rem">${text}</div>`;
    }
  }

  // --- Первичная инициализация ---
  closeQrModal();
  checkAuth();

})();
