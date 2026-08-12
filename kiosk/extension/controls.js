(() => {
  const onBoardManager = location.hostname === '127.0.0.1';
  const host = document.createElement('div');
  host.id = 'autodarts-kiosk-controls';
  const root = host.attachShadow({ mode: 'closed' });
  const button = document.createElement('button');
  button.type = 'button';
  button.setAttribute('aria-label', onBoardManager ? 'Close Board Manager' : 'Open Board Manager');
  button.textContent = onBoardManager ? '×' : 'Board';
  const remote = document.createElement('aside');
  remote.setAttribute('aria-label', 'Remote control QR code');
  remote.hidden = true;
  remote.innerHTML = '<strong>Remote control</strong><span>Scan with your phone</span>';

  const style = document.createElement('style');
  style.textContent = `
    button {
      position: fixed; z-index: 2147483647; border: 0; color: white;
      background: #212121e8; font: 650 16px system-ui, sans-serif;
      cursor: pointer; box-shadow: 0 2px 10px #0008;
      ${onBoardManager
        ? 'right: 14px; top: 14px; width: 44px; height: 44px; border-radius: 50%; font-size: 26px;'
        : 'right: 0; top: 45%; padding: 14px 9px; border-radius: 8px 0 0 8px; writing-mode: vertical-rl;'}
    }
    button:focus-visible { outline: 3px solid #6db7ff; outline-offset: 2px; }
    aside {
      position: fixed; z-index: 2147483646; left: 50%; bottom: 12px;
      translate: -50% 0; display: grid; grid-template-columns: 82px auto;
      grid-template-rows: auto auto; gap: 2px 11px; align-items: center;
      padding: 8px 13px 8px 8px; border: 1px solid #ffffff24;
      border-radius: 13px; background: #101720ed; color: white;
      box-shadow: 0 4px 24px #0009; font-family: system-ui, sans-serif;
    }
    aside[hidden] { display: none; }
    aside img { grid-row: 1 / 3; width: 82px; height: 82px; border-radius: 7px; background: white; }
    aside strong { align-self: end; font-size: 15px; }
    aside span { align-self: start; color: #b9c4ce; font-size: 12px; }
  `;

  button.addEventListener('click', () => {
    location.href = onBoardManager
      ? 'https://play.autodarts.io/'
      : 'http://127.0.0.1:3180/';
  });
  chrome.runtime.sendMessage('autodarts-remote-control-qr', response => {
    if (chrome.runtime.lastError || !response?.svg) return;
    const image = document.createElement('img');
    image.alt = 'QR code for browser remote control';
    image.src = `data:image/svg+xml;charset=utf-8,${encodeURIComponent(response.svg)}`;
    remote.prepend(image);
    remote.hidden = false;
  });

  root.append(style, button, remote);
  document.documentElement.append(host);
})();
