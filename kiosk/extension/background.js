chrome.runtime.onMessage.addListener((message, _sender, sendResponse) => {
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
