import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';

const dir = import.meta.dir;
const original = await readFile(join(dir, '../Ponder.icon/Assets/orbs.svg'), 'utf8');
const source = JSON.parse(await readFile(join(dir, '../Ponder.icon/icon.json'), 'utf8'));
const variants = [
  { id: '01-cobalt-on-white', title: '01 / Cobalt on white', note: 'Crisp white · saturated blue', bg: ['#FFFFFF', '#EDF2FA'], orb: ['#629EFF', '#2054D7'], base: '#FFFFFF', ink: '#2864EB' },
  { id: '02-soft-blue-on-white', title: '02 / Soft blue on white', note: 'Cool white · softer blue', bg: ['#FFFFFF', '#F0F5FC'], orb: ['#91C2FF', '#4685DD'], base: '#FFFFFF', ink: '#6BA6F4' },
  { id: '03-white-on-blue', title: '03 / White on blue', note: 'Bright blue · pearl white', bg: ['#438FFF', '#1756DE'], orb: ['#FFFFFF', '#DFEBFF'], base: '#2469ED', ink: '#FFFFFF' },
  { id: '04-white-on-deep-blue', title: '04 / White on deep blue', note: 'Deep blue · pearl white', bg: ['#2C60CA', '#12317D'], orb: ['#FFFFFF', '#D9E7FF'], base: '#1B439E', ink: '#FFFFFF' },
];

function art(v: typeof variants[number]) {
  const prefix = v.id;
  const mark = original.match(/<g fill="[^"]+">([\s\S]*?)<\/g>/)![1].replaceAll('#orb', `#${prefix}-orb`);
  const path = original.match(/<path id="orb"([\s\S]*?)\/>/)![1];
  return `<defs>
    <linearGradient id="${prefix}-bg" x2="0.25" y2="1"><stop stop-color="${v.bg[0]}"/><stop offset="1" stop-color="${v.bg[1]}"/></linearGradient>
    <linearGradient id="${prefix}-ink" x2="0.15" y2="1"><stop stop-color="${v.orb[0]}"/><stop offset="1" stop-color="${v.orb[1]}"/></linearGradient>
    <linearGradient id="${prefix}-edge" x2="0" y2="1"><stop stop-color="white" stop-opacity=".75"/><stop offset=".5" stop-color="white" stop-opacity=".06"/><stop offset="1" stop-color="white" stop-opacity=".3"/></linearGradient>
    <filter id="${prefix}-shadow" x="-30%" y="-30%" width="160%" height="170%"><feDropShadow dx="0" dy="8" stdDeviation="9" flood-color="#09234B" flood-opacity=".17"/></filter>
    <path id="${prefix}-orb"${path}/>
  </defs>
  <rect x="100" y="100" width="824" height="824" rx="220" fill="url(#${prefix}-bg)"/>
  <rect x="102" y="102" width="820" height="820" rx="218" fill="none" stroke="url(#${prefix}-edge)" stroke-width="4"/>
  <g transform="translate(100 100) scale(.8046875)"><g fill="url(#${prefix}-ink)" stroke="url(#${prefix}-edge)" stroke-width="2.5" filter="url(#${prefix}-shadow)">${mark}</g></g>`;
}

const cards: string[] = [];
for (const [i, v] of variants.entries()) {
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="1024" height="1024" viewBox="0 0 1024 1024">${art(v)}</svg>`;
  await writeFile(join(dir, `${v.id}.svg`), svg);
  const iconDir = join(dir, `${v.id}.icon`);
  await mkdir(join(iconDir, 'Assets'), { recursive: true });
  await writeFile(join(iconDir, 'Assets/orbs.svg'), original.replace(/(<g fill=")[^"]+("\>)/, `$1${v.ink}$2`));
  const config = structuredClone(source);
  const rgb = v.base.slice(1).match(/../g)!.map(x => (parseInt(x, 16) / 255).toFixed(5));
  config.fill.solid = `srgb:${rgb.join(',')},1.00000`;
  await writeFile(join(iconDir, 'icon.json'), JSON.stringify(config, null, 2) + '\n');
  const rendered = Bun.spawnSync(['rsvg-convert', join(dir, `${v.id}.svg`), '-o', join(dir, `${v.id}.png`)]);
  if (rendered.exitCode) throw new Error(rendered.stderr.toString());
  const encoded = Buffer.from(svg).toString('base64');
  const x = 48 + (i % 2) * 678;
  const y = 180 + Math.floor(i / 2) * 514;
  cards.push(`<g transform="translate(${x} ${y})">
    <rect width="654" height="490" rx="24" fill="#FFFFFF" stroke="#E1E5EB"/>
    <text x="28" y="42" font-size="23" font-weight="600" fill="#17253A">${v.title}</text>
    <text x="28" y="70" font-size="16" fill="#627086">${v.note}</text>
    <image href="data:image/svg+xml;base64,${encoded}" x="143" y="82" width="368" height="368"/>
    <rect x="24" y="389" width="116" height="77" rx="16" fill="#F0F2F6"/>
    <image href="data:image/svg+xml;base64,${encoded}" x="28" y="395" width="64" height="64"/>
    <image href="data:image/svg+xml;base64,${encoded}" x="96" y="411" width="32" height="32"/>
    <rect x="514" y="389" width="116" height="77" rx="16" fill="#202838"/>
    <image href="data:image/svg+xml;base64,${encoded}" x="518" y="395" width="64" height="64"/>
    <image href="data:image/svg+xml;base64,${encoded}" x="586" y="411" width="32" height="32"/>
  </g>`);
}
const board = `<svg xmlns="http://www.w3.org/2000/svg" width="1440" height="1260" viewBox="0 0 1440 1260">
<rect width="1440" height="1260" fill="#F4F6F9"/>
<g font-family="Helvetica Neue, Helvetica, Arial, sans-serif">
<text x="48" y="67" font-size="17" letter-spacing="3" fill="#61718A">PONDER / ICON COLOR STUDIES</text>
<text x="48" y="117" font-size="36" font-weight="600" fill="#17253A">Same orbs. Two color directions.</text>
<text x="48" y="149" font-size="18" fill="#627086">White + blue · original vector geometry · large and small previews</text>
${cards.join('\n')}
<text x="48" y="1230" font-size="15" fill="#6D7B90">Concept renders with illustrative shading. Final Icon Composer lighting may vary.</text>
</g></svg>`;
await writeFile(join(dir, 'comparison.svg'), board);
const result = Bun.spawnSync(['rsvg-convert', join(dir, 'comparison.svg'), '-o', join(dir, 'comparison.png')]);
if (result.exitCode) throw new Error(result.stderr.toString());
console.log(`Created four mockups, editable Icon Composer sources, and comparison.png in ${dir}`);
