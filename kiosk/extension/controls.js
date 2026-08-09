(() => {
  const onBoardManager = location.hostname === '127.0.0.1';
  const host = document.createElement('div');
  host.id = 'autodarts-kiosk-controls';
  const root = host.attachShadow({ mode: 'closed' });
  const button = document.createElement('button');
  button.type = 'button';
  button.setAttribute('aria-label', onBoardManager ? 'Close Board Manager' : 'Open Board Manager');
  button.textContent = onBoardManager ? '×' : 'Board';

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
  `;

  button.addEventListener('click', () => {
    location.href = onBoardManager
      ? 'https://play.autodarts.io/'
      : 'http://127.0.0.1:3180/';
  });
  root.append(style, button);
  document.documentElement.append(host);
})();
