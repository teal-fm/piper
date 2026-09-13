import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';

const script = resolve('scripts/tangled-image.sh');
const sha = 'a'.repeat(40);
function run(mode, overrides = {}) {
  const cwd = mkdtempSync(join(tmpdir(), 'piper-spindle-'));
  const bin = join(cwd, 'bin');
  mkdirSync(bin);
  const log = join(cwd, 'commands.jsonl');
  writeFileSync(join(cwd, 'package.json'), JSON.stringify({ version: '0.1.0' }));
  const stub = `#!/usr/bin/env node
const fs = require('node:fs');
const name = require('node:path').basename(process.argv[1]);
const args = process.argv.slice(2);
fs.appendFileSync(process.env.COMMAND_LOG, JSON.stringify({name, args}) + '\\n');
if (name === 'git') {
  if (args[0] === 'rev-parse') console.log(process.env.TEST_SHA);
  if (args[0] === 'ls-remote') console.log(process.env.REMOTE_SHA + '\\t' + args.at(-1));
}
if (name === 'docker' && args.includes('build') && process.env.FAIL_BUILD === '1') process.exit(1);
`;
  for (const name of ['git', 'docker', 'skopeo']) writeFileSync(join(bin, name), stub, { mode: 0o755 });
  let output = '', error;
  try {
    output = execFileSync('bash', [script, mode], {
      cwd, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'],
      env: {
        ...process.env, PATH: `${bin}:${process.env.PATH}`, COMMAND_LOG: log,
        TEST_SHA: sha, REMOTE_SHA: sha,
        PIPER_TANGLED_IMAGE: 'ghcr.io/teal-fm/piper-tangled',
        TANGLED_PIPELINE_KIND: mode === 'pr' ? 'pull_request' : 'push',
        TANGLED_SHA: sha, TANGLED_REF: mode === 'release' ? 'refs/tags/v0.1.0' : 'refs/heads/main',
        TANGLED_REF_TYPE: mode === 'release' ? 'tag' : 'branch', TANGLED_REF_NAME: 'v0.1.0',
        TANGLED_PR_SOURCE_SHA: sha, TANGLED_PR_SOURCE_BRANCH: 'feature/test', TANGLED_PR_TARGET_BRANCH: 'main',
        TANGLED_REPO_URL: 'https://tangled.org/teal.fm/piper',
        TANGLED_REGISTRY_USERNAME: 'ci', TANGLED_REGISTRY_TOKEN: 'secret-test-token',
        ...overrides,
      },
    });
  } catch (caught) {
    error = caught;
  }
  const calls = readFileSync(log, 'utf8').trim().split('\n').filter(Boolean).map(line => JSON.parse(line));
  rmSync(cwd, { recursive: true, force: true });
  return { output, error, calls };
}
const tags = result => result.calls.filter(call => call.name === 'skopeo' && call.args[0] === 'copy').map(call => call.args.at(-1));

test('main builds both architectures, stamps the source revision, and publishes isolated tags', () => {
  const result = run('main');
  assert.ifError(result.error);
  assert.deepEqual(tags(result), [
    'docker://ghcr.io/teal-fm/piper-tangled:sha-aaaaaaa',
    'docker://ghcr.io/teal-fm/piper-tangled:main',
  ]);
  const build = result.calls.find(call => call.name === 'docker' && call.args.includes('build'));
  assert.ok(build.args.includes('linux/amd64,linux/arm64'));
  assert.ok(build.args.includes(`PIPER_BUILD_REVISION=${sha}`));
  assert.ok(build.args.includes('PIPER_BUILD_CHANNEL=main'));
  assert.ok(!JSON.stringify(result.calls).includes('secret-test-token'));
});

test('fork PRs build without trying to log in or publish', () => {
  const result = run('pr', { TANGLED_REGISTRY_USERNAME: '', TANGLED_REGISTRY_TOKEN: '' });
  assert.ifError(result.error);
  assert.ok(result.calls.some(call => call.name === 'docker' && call.args.includes('build')));
  assert.ok(!result.calls.some(call => call.name === 'skopeo'));
  assert.match(result.output, /publishing is unavailable/);
});

test('trusted PR tags stay stable across updates and differ by branch pair', () => {
  const first = run('pr');
  const updated = run('pr', { TEST_SHA: 'b'.repeat(40), REMOTE_SHA: 'b'.repeat(40), TANGLED_PR_SOURCE_SHA: 'b'.repeat(40) });
  const other = run('pr', { TANGLED_PR_TARGET_BRANCH: 'release/test' });
  for (const result of [first, updated, other]) assert.ifError(result.error);
  assert.equal(tags(first).at(-1), tags(updated).at(-1));
  assert.notEqual(tags(first)[0], tags(updated)[0]);
  assert.notEqual(tags(first).at(-1), tags(other).at(-1));
  assert.match(tags(first).at(-1), /:pr-[a-f0-9]{12}$/);
});

test('superseded branches and failed builds do not publish', () => {
  for (const overrides of [{ REMOTE_SHA: 'b'.repeat(40) }, { FAIL_BUILD: '1' }]) {
    const result = run('main', overrides);
    assert.deepEqual(tags(result), []);
    assert.ok(!result.calls.some(call => call.name === 'skopeo'));
  }
});

test('release tags must match package.json and use the release agent', () => {
  const valid = run('release');
  assert.ifError(valid.error);
  assert.deepEqual(tags(valid).map(tag => tag.split(':').at(-1)), ['0.1.0', 'v0.1.0', 'latest']);
  assert.ok(valid.calls.some(call => call.args.includes('PIPER_BUILD_CHANNEL=release')));
  const invalid = run('release', { TANGLED_REF_NAME: 'v0.2.0' });
  assert.ok(invalid.error);
  assert.ok(!invalid.calls.some(call => call.name === 'docker'));
});

test('replaying an old release does not move latest backwards', () => {
  const result = run('release', { REMOTE_SHA: 'b'.repeat(40) });
  assert.ifError(result.error);
  assert.deepEqual(tags(result).map(tag => tag.split(':').at(-1)), ['0.1.0', 'v0.1.0']);
});

test('manual or mismatched push contexts cannot publish main', () => {
  for (const overrides of [{ TANGLED_PIPELINE_KIND: 'manual' }, { TANGLED_REF: 'refs/heads/other' }, { TANGLED_SHA: 'b'.repeat(40) }]) {
    const result = run('main', overrides);
    assert.ok(result.error);
    assert.ok(!result.calls.some(call => call.name === 'docker'));
  }
});
