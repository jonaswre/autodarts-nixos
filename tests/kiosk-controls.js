const fs = require('fs');
const vm = require('vm');

class Element {
  constructor(tag) {
    this.tag = tag;
    this.children = [];
    this.attributes = {};
  }
  setAttribute(name, value) { this.attributes[name] = value; }
  attachShadow() { this.shadow = new Element('shadow'); return this.shadow; }
  addEventListener() {}
  append(...children) { this.children.push(...children); }
  prepend(child) { this.children.unshift(child); }
  set innerHTML(_value) {
    this.status = new Element('span');
    this.children = [new Element('strong'), this.status];
  }
  querySelector(selector) { return selector === 'span' ? this.status : null; }
}

const created = [];
const scheduled = [];
let attempts = 0;
const context = {
  console,
  encodeURIComponent,
  location: { hostname: 'play.autodarts.io', href: 'https://play.autodarts.io/' },
  document: {
    createElement(tag) { const element = new Element(tag); created.push(element); return element; },
    documentElement: new Element('html'),
  },
  chrome: {
    runtime: {
      lastError: null,
      sendMessage(_message, callback) {
        attempts++;
        if (attempts === 1) {
          this.lastError = { message: 'service starting' };
          callback(undefined);
          this.lastError = null;
          return;
        }
        callback({ svg: '<svg></svg>' });
      },
    },
  },
  setTimeout(callback) { scheduled.push(callback); },
};

vm.runInNewContext(fs.readFileSync(process.argv[2], 'utf8'), context);
while (scheduled.length) scheduled.shift()();
const remote = created.find(element => element.attributes['aria-label'] === 'Remote control QR code');
if (attempts !== 2) throw new Error(`expected retry, got ${attempts} request(s)`);
if (!remote) throw new Error('remote-control overlay was not rendered');
if (remote.status.textContent !== 'Scan with your phone') throw new Error('success state was not shown');
if (!remote.children.some(element => element.tag === 'img' && element.alt === 'QR code for browser remote control')) {
  throw new Error('QR image was not rendered after retry');
}
