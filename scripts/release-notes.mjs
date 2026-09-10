import { readFileSync } from 'node:fs';
import { pathToFileURL } from 'node:url';

export function releaseNotes(version, changelog, repository) {
  if (!/^\d+\.\d+\.\d+$/.test(version)) throw new Error('Invalid stable version.');
  if (!/^[\w.-]+\/[\w.-]+$/.test(repository)) throw new Error('Invalid repository.');
  const lines = changelog.split('\n');
  const start = lines.indexOf(`## ${version}`);
  if (start < 0) throw new Error(`Missing changelog for ${version}.`);
  const end = lines.findIndex((line, i) => i > start && line.startsWith('## '));
  const changes = lines.slice(start + 1, end < 0 ? undefined : end).join('\n').trim();
  if (!changes) throw new Error('Release changelog is empty.');
  const tag = `v${version}`;
  const image = `ghcr.io/${repository.toLowerCase()}:${version}`;
  return `## Changelog

${changes}

## Install ${tag}

The Docker image supports Linux AMD64 and ARM64.

\`\`\`sh
docker pull ${image}
\`\`\`

For a new installation, download this version's configuration:

\`\`\`sh
curl -fsSLo compose.release.yml https://raw.githubusercontent.com/${repository}/${tag}/compose.release.yml
curl -fsSLo .env.template https://raw.githubusercontent.com/${repository}/${tag}/.env.template
cp -n .env.template .env
\`\`\`

Configure credentials and the public callback URL in \`.env\` using the [setup guide](https://github.com/${repository}/blob/${tag}/README.md#setup). For Docker, set \`SERVER_HOST=0.0.0.0\`, \`SERVER_PORT=8080\`, and \`DB_PATH=/db/piper.db\`. Apple Music users must also mount their private key at \`APPLE_MUSIC_PRIVATE_KEY_PATH\` inside the container.

Start this exact version:

\`\`\`sh
PIPER_IMAGE=${image} docker compose -f compose.release.yml up -d
\`\`\`

For upgrades, keep your existing \`.env\` and volume. Back up the database before upgrading. Use the same Compose project name or directory as your existing deployment so it reuses the volume. Rolling back an image does not roll back database migrations.

To build this version from source, install Git, Node.js 24, npm, Go as specified in \`go.mod\`, and a C compiler for SQLite:

\`\`\`sh
git clone --branch ${tag} --depth 1 https://github.com/${repository}.git piper-${version}
cd piper-${version}
npm ci
npm run build:css
CGO_ENABLED=1 go build -o piper ./cmd
\`\`\`

Configure \`.env\` as described in the setup guide, then run \`./piper\`.
`;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const { version } = JSON.parse(readFileSync('package.json', 'utf8'));
  process.stdout.write(releaseNotes(version, readFileSync('CHANGELOG.md', 'utf8'), process.env.GITHUB_REPOSITORY || 'teal-fm/piper'));
}
