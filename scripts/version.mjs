import { readFileSync, writeFileSync } from 'node:fs';

const { version } = JSON.parse(readFileSync('package.json', 'utf8'));
if (!/^\d+\.\d+\.\d+$/.test(version)) throw new Error('Only stable releases are supported.');
const path = 'models/constants.go';
const source = readFileSync(path, 'utf8');
const pattern = /const SubmissionAgent = "piper\/v\d+\.\d+\.\d+"/;
if (!pattern.test(source)) throw new Error('Cannot find SubmissionAgent.');
const expected = source.replace(pattern, `const SubmissionAgent = "piper/v${version}"`);
if (process.argv.includes('--check')) {
  if (source !== expected) throw new Error('SubmissionAgent differs from package.json. Run npm run version.');
} else {
  writeFileSync(path, expected);
}
