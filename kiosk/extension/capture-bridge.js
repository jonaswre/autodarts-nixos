window.addEventListener('autodarts-play-websocket', event => {
  const detail = event.detail;
  if (!detail || typeof detail.url !== 'string' || typeof detail.data !== 'string') return;
  chrome.runtime.sendMessage({
    type: 'autodarts-play-websocket',
    url: detail.url,
    data: detail.data,
    transport: detail.transport || 'websocket',
  });
});
