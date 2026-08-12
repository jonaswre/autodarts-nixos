(() => {
  const NativeWebSocket = window.WebSocket;
  function ApplianceWebSocket(url, protocols) {
    const socket = protocols === undefined
      ? new NativeWebSocket(url)
      : new NativeWebSocket(url, protocols);
    socket.addEventListener('message', event => {
      if (typeof event.data === 'string') {
        window.dispatchEvent(new CustomEvent('autodarts-play-websocket', {
          detail: { url: String(url), data: event.data },
        }));
      }
    });
    return socket;
  }
  Object.setPrototypeOf(ApplianceWebSocket, NativeWebSocket);
  ApplianceWebSocket.prototype = NativeWebSocket.prototype;
  window.WebSocket = ApplianceWebSocket;

  const nativeFetch = window.fetch;
  window.fetch = async function (...args) {
    const response = await nativeFetch.apply(this, args);
    try {
      const requestUrl = args[0] instanceof Request ? args[0].url : String(args[0]);
      const parsed = new URL(requestUrl, location.href);
      const contentType = response.headers.get('content-type') || '';
      if (parsed.hostname.endsWith('autodarts.io') && contentType.includes('application/json')) {
        response.clone().text().then(data => {
          window.dispatchEvent(new CustomEvent('autodarts-play-websocket', {
            detail: { url: parsed.href, data, transport: 'http-response' },
          }));
        }).catch(() => {});
      }
    } catch {}
    return response;
  };

  const nativeOpen = XMLHttpRequest.prototype.open;
  XMLHttpRequest.prototype.open = function (method, url, ...args) {
    this.addEventListener('load', function () {
      try {
        const parsed = new URL(String(url), location.href);
        const contentType = this.getResponseHeader('content-type') || '';
        if (
          parsed.hostname.endsWith('autodarts.io') &&
          contentType.includes('application/json') &&
          (this.responseType === '' || this.responseType === 'text')
        ) {
          window.dispatchEvent(new CustomEvent('autodarts-play-websocket', {
            detail: { url: parsed.href, data: this.responseText, transport: 'http-response' },
          }));
        }
      } catch {}
    }, { once: true });
    return nativeOpen.call(this, method, url, ...args);
  };
})();
