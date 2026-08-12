chrome.runtime.onMessage.addListener((message, _sender, sendResponse) => {
  if (message?.type === 'autodarts-play-websocket') {
    fetch('http://127.0.0.1:3182/api/play-event', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ url: message.url, data: message.data, transport: message.transport }),
    }).catch(() => {});
    return false;
  }
  if (message !== 'autodarts-remote-control-qr') return false;
  fetch('http://127.0.0.1:3182/remote-control-qr.svg', { cache: 'no-store' })
    .then(response => {
      if (!response.ok) throw new Error(`QR request failed: ${response.status}`);
      return response.text();
    })
    .then(svg => sendResponse({ svg }))
    .catch(error => sendResponse({ error: error.message }));
  return true;
});
