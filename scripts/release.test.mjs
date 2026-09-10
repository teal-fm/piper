import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { releaseNotes } from './release-notes.mjs';

const changelog = '# Piper\n\n## 0.0.16\n\n### Patch Changes\n\n- Fix retries.\n\n## 0.0.15\n\n- Old change.\n';
test('release notes contain only the requested changelog and pin installation to that version', () => {
  const notes = releaseNotes('0.0.16', changelog, 'teal-fm/piper');
  assert.match(notes, /Fix retries\./);
  assert.doesNotMatch(notes, /Old change|:latest|\/main\//);
  assert.match(notes, /docker pull ghcr.io\/teal-fm\/piper:0.0.16/);
  assert.match(notes, /PIPER_IMAGE=ghcr.io\/teal-fm\/piper:0.0.16 docker compose/);
  assert.match(notes, /piper\/v0.0.16\/compose.release.yml/);
  assert.match(notes, /git clone --branch v0.0.16 --depth 1/);
});
test('the final changelog entry works without a following heading', () => {
  const notes = releaseNotes('0.0.15', changelog, 'teal-fm/piper');
  assert.match(notes, /Old change/);
  assert.doesNotMatch(notes, /Fix retries/);
});
test('missing, empty and prerelease versions fail before publication', () => {
  assert.throws(() => releaseNotes('0.0.17', changelog, 'teal-fm/piper'), /Missing changelog/);
  assert.throws(() => releaseNotes('0.0.16', '## 0.0.16\n\n## 0.0.15', 'teal-fm/piper'), /empty/);
  assert.throws(() => releaseNotes('0.0.16-beta.1', changelog, 'teal-fm/piper'), /stable version/);
  assert.throws(() => releaseNotes('0.0.16', changelog, 'bad repo'), /repository/);
});
test('version synchronization detects drift, updates the Go agent, and is idempotent', () => {
  const cwd = mkdtempSync(join(tmpdir(), 'piper-version-'));
  const script = resolve('scripts/version.mjs');
  try {
    mkdirSync(join(cwd, 'models'));
    writeFileSync(join(cwd, 'package.json'), JSON.stringify({ version: '0.1.0' }));
    writeFileSync(join(cwd, 'models/constants.go'), 'package models\n\nconst SubmissionAgent = "piper/v0.0.14"\n');
    const run = (...args) => execFileSync(process.execPath, [script, ...args], { cwd, stdio: 'pipe' });
    assert.throws(() => run('--check'));
    run();
    const updated = readFileSync(join(cwd, 'models/constants.go'), 'utf8');
    assert.match(updated, /piper\/v0.1.0/);
    run('--check');
    run();
    assert.equal(readFileSync(join(cwd, 'models/constants.go'), 'utf8'), updated);
    writeFileSync(join(cwd, 'models/constants.go'), 'package models\n');
    assert.throws(() => run());
  } finally {
    rmSync(cwd, { recursive: true, force: true });
  }
});

test('PR validation accepts added changesets and consumed release changesets, but rejects omissions', () => {
  const cwd = mkdtempSync(join(tmpdir(), 'piper-changeset-'));
  const script = resolve('scripts/check-changeset.mjs');
  const git = (...args) => execFileSync('git', args, { cwd, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trim();
  try {
    git('init');
    git('config', 'user.email', 'test@example.com');
    git('config', 'user.name', 'Release test');
    mkdirSync(join(cwd, '.changeset'));
    writeFileSync(join(cwd, 'package.json'), JSON.stringify({ version: '0.0.14' }));
    writeFileSync(join(cwd, '.changeset/pending.md'), '---\n"piper": patch\n---\n\nFix retries.\n');
    git('add', '.');
    git('commit', '-m', 'base');
    const base = git('rev-parse', 'HEAD');
    const run = () => execFileSync(process.execPath, [script], { cwd, env: { ...process.env, BASE_SHA: base }, stdio: 'pipe' });
    assert.throws(run);
    writeFileSync(join(cwd, '.changeset/new.md'), '---\n---\n');
    git('add', '.');
    git('commit', '-m', 'empty changeset');
    run();
    git('reset', '--hard', base);
    writeFileSync(join(cwd, '.changeset/extra.md'), '---\n---\n');
    writeFileSync(join(cwd, 'package.json'), JSON.stringify({ version: '0.0.15' }));
    git('add', '.');
    git('commit', '-m', 'manual bump with changeset');
    assert.throws(run);
    git('reset', '--hard', base);
    rmSync(join(cwd, '.changeset/pending.md'));
    writeFileSync(join(cwd, 'package.json'), JSON.stringify({ version: '0.0.15' }));
    writeFileSync(join(cwd, 'CHANGELOG.md'), '## 0.0.15\n\n- Fix retries.\n');
    git('add', '.');
    git('commit', '-m', 'release');
    run();
    writeFileSync(join(cwd, 'package.json'), JSON.stringify({ version: '0.0.13' }));
    git('add', '.');
    git('commit', '-m', 'bad version');
    assert.throws(run);
  } finally {
    rmSync(cwd, { recursive: true, force: true });
  }
});

test('PR validation allows introducing the version field while adopting Changesets', () => {
  const cwd = mkdtempSync(join(tmpdir(), 'piper-changeset-'));
  const script = resolve('scripts/check-changeset.mjs');
  const git = (...args) => execFileSync('git', args, { cwd, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trim();
  try {
    git('init');
    git('config', 'user.email', 'test@example.com');
    git('config', 'user.name', 'Release test');
    mkdirSync(join(cwd, '.changeset'));
    writeFileSync(join(cwd, 'package.json'), JSON.stringify({ dependencies: {} }));
    git('add', '.');
    git('commit', '-m', 'base');
    const base = git('rev-parse', 'HEAD');
    writeFileSync(join(cwd, '.changeset/onboard.md'), '---\n"piper": patch\n---\n\nAdopt Changesets.\n');
    writeFileSync(join(cwd, 'package.json'), JSON.stringify({ version: '0.0.14', dependencies: {} }));
    git('add', '.');
    git('commit', '-m', 'adopt changesets');
    execFileSync(process.execPath, [script], { cwd, env: { ...process.env, BASE_SHA: base }, stdio: 'pipe' });
  } finally {
    rmSync(cwd, { recursive: true, force: true });
  }
});
