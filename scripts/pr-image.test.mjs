import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

// Exercise the exact scripts used by Actions without credentials or a registry.
const workflow = readFileSync(new URL('../.github/workflows/publish-pr-image.yaml', import.meta.url), 'utf8');
const scripts = [...workflow.matchAll(/          script: \|\n((?:            .*\n|\n)+)/g)]
  .map(match => match[1].replace(/^            /gm, ''));
assert.equal(scripts.length, 2);
const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;
const runScript = (index, bindings) => new AsyncFunction(...Object.keys(bindings), scripts[index])(...Object.values(bindings));
const sha = 'a'.repeat(40);
const pr = { number: 42, state: 'open', head: { sha, repo: { id: 123 }, ref: 'feature' } };
const context = {
  repo: { owner: 'teal-fm', repo: 'piper' },
  payload: { workflow_run: {
    path: '.github/workflows/pr-image.yaml', head_sha: sha,
    head_repository: { id: 123 }, head_branch: 'feature', html_url: 'https://example.com/run',
  } },
};

test('finds fork PRs without workflow_run.pull_requests and rejects stale or unrelated heads', async () => {
  const outputs = {};
  await runScript(0, {
    context, process: { env: { GITHUB_REPOSITORY: 'teal-fm/piper' } },
    core: { setOutput: (key, value) => { outputs[key] = value; }, info() {} },
    github: {
      rest: { pulls: { list: 'list' } },
      paginate: async () => [pr,
        { ...pr, number: 43, head: { ...pr.head, sha: 'old' } },
        { ...pr, number: 44, head: { ...pr.head, repo: { id: 999 } } },
        { ...pr, number: 45, head: { ...pr.head, ref: 'other' } },
      ],
    },
  });
  assert.deepEqual(outputs, { numbers: '[42]', sha, image: 'ghcr.io/teal-fm/piper-pr' });
});

test('skips completed runs with no current open PR', async () => {
  await runScript(0, {
    context, process: { env: {} },
    core: { setOutput() { assert.fail('must not publish'); }, info() {} },
    github: { rest: { pulls: { list: 'list' } }, paginate: async () => [] },
  });
});

test('rejects another workflow with the same display name', async () => {
  await assert.rejects(runScript(0, {
    context: { ...context, payload: { workflow_run: { path: '.github/workflows/other.yaml' } } },
  }), /Unexpected source workflow/);
});

async function publish({ comments = [], current = pr, afterCopy = current, failCopy = false } = {}) {
  const calls = [];
  let reads = 0;
  const github = {
    rest: {
      pulls: { get: async () => ({ data: reads++ === 0 ? current : afterCopy }) },
      issues: {
        listComments: 'comments',
        createComment: async args => calls.push(['create', args]),
        updateComment: async args => calls.push(['update', args]),
      },
    },
    paginate: async () => comments,
  };
  const bindings = {
    context, github,
    process: { env: { IMAGE: 'ghcr.io/teal-fm/piper-pr', HEAD_SHA: sha, PR_NUMBERS: '[42]', RUNNER_TEMP: '/tmp/check', REGISTRY_TOKEN: 'test-token', GITHUB_ACTOR: 'maintainer' } },
    require: name => ({
      'node:path': { join },
      'node:fs': { rmSync: (...args) => calls.push(['cleanup', ...args]) },
      'node:child_process': { execFileSync: (command, args) => {
        calls.push([command, args]);
        if (failCopy && args[0] === 'copy') throw new Error('copy failed');
      } },
    })[name],
  };
  if (failCopy) await assert.rejects(runScript(1, bindings), /copy failed/);
  else await runScript(1, bindings);
  return calls;
}

test('copies all platforms to the PR tag and posts pull instructions', async () => {
  const calls = await publish();
  const copy = calls.find(([command, args]) => command === 'skopeo' && args[0] === 'copy')[1];
  assert.ok(copy.includes('--all'));
  assert.equal(copy.at(-1), 'docker://ghcr.io/teal-fm/piper-pr:pr-42');
  const comment = calls.find(([type]) => type === 'create')[1];
  assert.equal(comment.issue_number, 42);
  assert.match(comment.body, /docker pull ghcr.io\/teal-fm\/piper-pr:pr-42/);
  assert.ok(comment.body.includes(sha));
  assert.equal(calls.at(-1)[0], 'cleanup');
});

test('updates the bot comment and ignores a user copying its marker', async () => {
  const calls = await publish({ comments: [
    { id: 1, user: { login: 'contributor' }, body: '<!-- piper-pr-image -->' },
    { id: 2, user: { login: 'github-actions[bot]' }, body: '<!-- piper-pr-image -->\nOld build' },
  ] });
  assert.equal(calls.find(([type]) => type === 'update')[1].comment_id, 2);
  assert.ok(!calls.some(([type]) => type === 'create'));
});

test('closed and superseded PRs are not published', async () => {
  for (const current of [{ ...pr, state: 'closed' }, { ...pr, head: { ...pr.head, sha: 'new' } }]) {
    const calls = await publish({ current });
    assert.ok(!calls.some(([type, args]) => type === 'skopeo' && args[0] === 'copy'));
    assert.ok(!calls.some(([type]) => ['create', 'update'].includes(type)));
  }
});

test('does not comment if the PR advances during upload or the upload fails', async () => {
  for (const options of [{ afterCopy: { ...pr, head: { ...pr.head, sha: 'new' } } }, { failCopy: true }]) {
    const calls = await publish(options);
    assert.ok(!calls.some(([type]) => ['create', 'update'].includes(type)));
    assert.equal(calls.at(-1)[0], 'cleanup');
  }
});
