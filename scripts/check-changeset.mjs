import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { releaseNotes } from './release-notes.mjs';

const base = process.env.BASE_SHA;
if (!/^[a-f0-9]{40}$/.test(base || '')) throw new Error('BASE_SHA must be a full Git commit SHA.');
const diff = (...args) => execFileSync('git', ['diff', ...args, `${base}...HEAD`, '--', '.changeset/*.md'], { encoding: 'utf8' }).trim().split('\n').filter(Boolean);
const added = diff('--name-only', '--diff-filter=A').filter(path => path !== '.changeset/README.md');
const current = JSON.parse(readFileSync('package.json', 'utf8')).version;
const previous = JSON.parse(execFileSync('git', ['show', `${base}:package.json`], { encoding: 'utf8' })).version;
if (added.length) {
  // Introducing the version field (adopting Changesets) is not a hand bump.
  if (previous !== undefined && current !== previous) {
    throw new Error('Do not bump versions by hand when adding a changeset.');
  }
  console.log('Changeset found. Use npm run changeset -- --empty for changes without a release.');
} else {
  // Version PRs consume changesets rather than adding one.
  const removed = diff('--name-only', '--diff-filter=D').filter(path => path !== '.changeset/README.md');
  const parts = value => /^\d+\.\d+\.\d+$/.test(value || '') ? value.split('.').map(Number) : [];
  const old = parts(previous), next = parts(current);
  const changed = next.findIndex((n, i) => n !== old[i]);
  if (!removed.length || old.length !== 3 || next.length !== 3 || changed < 0 || next[changed] <= old[changed]) {
    throw new Error('Add a changeset with npm run changeset. Do not bump versions by hand.');
  }
  releaseNotes(current, readFileSync('CHANGELOG.md', 'utf8'), 'teal-fm/piper');
  console.log(`Release PR consumes changesets and bumps ${previous} to ${current}.`);
}
