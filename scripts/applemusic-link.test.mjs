import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import vm from 'node:vm';

const template = readFileSync(new URL('../pages/templates/applemusic_link.gohtml', import.meta.url), 'utf8');
const script = [...template.matchAll(/<script>([\s\S]*?)<\/script>/g)].at(-1)[1];

// Exercise the page's actual script. MusicKit and the token-saving HTTP endpoint
// are external boundaries; the fake models MusicKit's cached-token short circuit.
function page({ resetError, configure } = {}) {
  let cachedToken = 'invalid-old-token';
  let finishReset;
  const reset = new Promise(resolve => { finishReset = resolve; });
  const elements = Object.fromEntries(['authorizeBtn', 'status', 'unlinkForm'].map(id => [id, {
    disabled: id === 'authorizeBtn' && /id="authorizeBtn"\s+disabled/.test(template),
    textContent: '',
    listeners: {},
    addEventListener(type, fn) { this.listeners[type] = fn; },
  }]));
  const saved = [];
  let prompts = 0;
  const music = {
    async unauthorize() {
      await reset;
      if (resetError) throw resetError;
      cachedToken = '';
    },
    async authorize() {
      if (!cachedToken) { prompts++; cachedToken = 'fresh-token'; }
      return cachedToken;
    },
    addEventListener() {},
  };
  let configured = false;
  const context = vm.createContext({
    document: { getElementById: id => elements[id] },
    window: {
      MusicKit: {
        configure() {
          const result = configure?.();
          if (result?.then) return result.then(() => { configured = true; });
          configured = true;
        },
        getInstance() {
          assert.equal(configured, true, 'MusicKit configuration must finish first');
          return music;
        },
      },
      addEventListener() {},
    },
    console: { error() {}, warn() {}, debug() {} },
    fetch: async (url, options) => {
      saved.push({ url, token: JSON.parse(options.body).userToken });
      return { ok: true };
    },
  });
  vm.runInContext(script.replace(/setup\(\);\s*$/, 'globalThis.ready = setup();'), context);
  return { elements, saved, finishReset, ready: context.ready, prompts: () => prompts };
}

test('re-link clears the stale browser token before allowing authorization', async () => {
  const p = page();
  assert.equal(p.elements.authorizeBtn.disabled, true);
  p.finishReset();
  await p.ready;
  assert.equal(p.elements.authorizeBtn.disabled, false);
  await p.elements.authorizeBtn.listeners.click();
  assert.equal(p.prompts(), 1);
  assert.deepEqual(p.saved, [{ url: '/api/v1/applemusic/authorize', token: 'fresh-token' }]);
  assert.equal(p.elements.status.textContent, 'Authorized and saved.');
});

test('a failed reset keeps authorization disabled and does not save a stale token', async () => {
  const p = page({ resetError: new Error('reset failed') });
  p.finishReset();
  await p.ready;
  assert.equal(p.elements.authorizeBtn.disabled, true);
  assert.match(p.elements.status.textContent, /Setup failed: reset failed/);
  assert.deepEqual(p.saved, []);
});

test('waits for asynchronous MusicKit configuration', async () => {
  let configured = false;
  const p = page({ configure: async () => {
    await Promise.resolve();
    configured = true;
  } });
  p.finishReset();
  await p.ready;
  assert.equal(configured, true);
  assert.equal(p.elements.authorizeBtn.disabled, false);
});

test('unlink uses the server form route even if MusicKit cannot load', () => {
  const form = template.match(/<form\s+id="unlinkForm"[\s\S]*?<\/form>/)[0];
  assert.match(form, /method="post"/);
  assert.match(form, /action="\/unlink-applemusic"/);
  assert.doesNotMatch(form, /MusicKit|handleUnlink/);
});
