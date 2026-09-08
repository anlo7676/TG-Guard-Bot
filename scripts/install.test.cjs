'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const {spawnSync} = require('node:child_process');
const installer = fs.readFileSync(path.join(__dirname, '../install.sh'), 'utf8');
test('remote bash -c installer supports first install and safe updates', () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'tg-guard-install-test-'));
  const target = path.join(root, 'app');

  const mocks = `
uname() { echo Linux; }
id() { echo 0; }
docker() { return 0; }
curl() { echo '"tag_name": "v1.8.0",'; }
apt-get() { echo 'Unexpected system mutation' >&2; return 99; }
git() {
  if [[ "$1" == clone ]]; then
    mkdir -p "$TG_GUARD_INSTALL_DIR/.git" "$TG_GUARD_INSTALL_DIR/scripts"
    printf '%s\\n' '#!/bin/bash' 'cd -- "$(dirname -- "$0")/.."' '[[ -f .env ]] || echo preserve-me > .env' 'if [[ -f fail-deploy ]]; then rm fail-deploy; exit 1; fi' 'echo deployed >> deployed' > "$TG_GUARD_INSTALL_DIR/scripts/deploy.sh"
    return 0
  fi
  case "$3" in
    remote) if [[ -f "$TG_GUARD_INSTALL_DIR/wrong-remote" ]]; then echo https://example.com/other; else echo https://github.com/anlo7676/TG-Guard-Bot.git; fi;;
    branch) echo main;;
    switch) return 0;;
    rev-parse) echo 1234567890abcdef;;
    reset) echo restored > "$TG_GUARD_INSTALL_DIR/rollback"; return 0;;
    status) [[ ! -f "$TG_GUARD_INSTALL_DIR/dirty" ]] || echo ' M file'; return 0;;
    fetch) [[ ! -f "$TG_GUARD_INSTALL_DIR/offline" ]];;
    merge) [[ ! -f "$TG_GUARD_INSTALL_DIR/diverged" ]];;
    *) return 99;;
  esac
}
`;
  const run = () => spawnSync(process.platform === 'win32' ? 'C:/Program Files/Git/bin/bash.exe' : 'bash', ['-c', mocks + '\nexport TG_GUARD_INSTALL_DIR="$PWD/app"\n' + installer], {cwd:root, encoding:'utf8'});
  try {
    let result = run();
    assert.equal(result.status, 0, result.stderr);
    assert.equal(fs.readFileSync(path.join(target, '.env'), 'utf8'), 'preserve-me\n');
    result = run();
    assert.equal(result.status, 0, result.stderr);
    assert.equal(fs.readFileSync(path.join(target, 'deployed'), 'utf8'), 'deployed\ndeployed\n');
    for (const marker of ['dirty', 'wrong-remote', 'diverged']) {
      fs.writeFileSync(path.join(target, marker), 'test');
      result = run();
      assert.notEqual(result.status, 0, marker);
      assert.equal(fs.readFileSync(path.join(target, '.env'), 'utf8'), 'preserve-me\n');
      assert.equal(fs.readFileSync(path.join(target, 'deployed'), 'utf8'), 'deployed\ndeployed\n');
      fs.unlinkSync(path.join(target, marker));
    }
    fs.writeFileSync(path.join(target, 'fail-deploy'), 'test');
    result = run();
    assert.notEqual(result.status, 0, 'failed rollout must be reported');
    assert.ok(fs.existsSync(path.join(target, 'rollback')));
    assert.equal(fs.readFileSync(path.join(target, '.env'), 'utf8'), 'preserve-me\n');
    fs.writeFileSync(path.join(target, 'scripts/manage.sh'), 'echo reached > menu-reached\n');
    fs.writeFileSync(path.join(target, 'offline'), 'test');
    result = run();
    assert.equal(result.status, 0, result.stderr);
    assert.ok(fs.existsSync(path.join(root, 'menu-reached')));
    fs.rmdirSync(path.join(target, '.git'));
    result = run();
    assert.notEqual(result.status, 0);
    assert.equal(fs.readFileSync(path.join(target, '.env'), 'utf8'), 'preserve-me\n');
  } finally {
    assert.ok(path.resolve(root).startsWith(path.resolve(os.tmpdir()) + path.sep));
    assert.ok(path.basename(root).startsWith('tg-guard-install-test-'));
    fs.rmSync(root, {recursive:true, force:true});
  }
});
