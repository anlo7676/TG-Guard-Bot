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
docker() {
  case "$*" in
    *' ps -q app') [[ ! -f "$TG_GUARD_INSTALL_DIR/healthy" ]] || echo abcdef123456;;
    *'/health/live') printf '{"build":{"version":"%s","source":"%s"}}' "\${MOCK_VERSION:-1.8.0}" "\${MOCK_SOURCE:-1234567890ab}";;
    *'/health/ready') [[ ! -f "$TG_GUARD_INSTALL_DIR/not-ready" ]]; return $?;;
  esac
  return 0
}
flock() { [[ ! -f "$TG_GUARD_INSTALL_DIR/locked" ]]; }
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
    rev-parse) if [[ "$4" == HEAD && -f "$TG_GUARD_INSTALL_DIR/old-code" ]]; then echo abcdef1234567890; else echo 1234567890abcdef; fi;;
    merge-base) [[ ! -f "$TG_GUARD_INSTALL_DIR/ahead" ]];;
    reset) echo restored > "$TG_GUARD_INSTALL_DIR/rollback"; return 0;;
    status) [[ ! -f "$TG_GUARD_INSTALL_DIR/dirty" ]] || echo ' M file'; return 0;;
    fetch) [[ ! -f "$TG_GUARD_INSTALL_DIR/offline" ]];;
    merge) [[ ! -f "$TG_GUARD_INSTALL_DIR/diverged" ]] || return 1; rm -f "$TG_GUARD_INSTALL_DIR/old-code";;
    *) return 99;;
  esac
}
`;
  fs.writeFileSync(path.join(root,'harness.sh'), mocks + '\nexport TG_GUARD_INSTALL_DIR="$PWD/app"\n' + installer);
  const run = (extra={}) => spawnSync(process.platform === 'win32' ? 'C:/Program Files/Git/bin/bash.exe' : 'bash', ['-c', 'bash -c "$(cat harness.sh)"'], {cwd:root, encoding:'utf8',env:{...process.env,...extra}});
  try {
    let result = run();
    assert.equal(result.status, 0, result.stderr);
    assert.equal(fs.readFileSync(path.join(target, '.env'), 'utf8'), 'preserve-me\n');
    result = run();
    assert.equal(result.status, 0, result.stderr);
    assert.equal(fs.readFileSync(path.join(target, 'deployed'), 'utf8'), 'deployed\ndeployed\n');
    fs.writeFileSync(path.join(target, 'healthy'), 'test');
    result = run();
    assert.equal(result.status, 0, result.stderr);
    assert.match(result.stdout, /已是最新版本/);
    assert.equal(fs.readFileSync(path.join(target, 'deployed'), 'utf8'), 'deployed\ndeployed\n');
    for (const extra of [{MOCK_VERSION:'1.7.0'}, {MOCK_SOURCE:'other-build'}]) {
      result = run(extra);
      assert.equal(result.status,0,result.stderr);
      assert.match(result.stdout,/重新部署/);
    }
    for (const marker of ['not-ready','old-code']) {
      fs.writeFileSync(path.join(target,marker),'test');
      const before=fs.readFileSync(path.join(target,'deployed'),'utf8');
      result=run();
      assert.equal(result.status,0,result.stderr);
      assert.equal(fs.readFileSync(path.join(target,'deployed'),'utf8'),before+'deployed\n');
      fs.rmSync(path.join(target,marker),{force:true});
    }
    fs.unlinkSync(path.join(target,'healthy'));
    fs.writeFileSync(path.join(target,'deployed'),'deployed\ndeployed\n');
    assert.notEqual(run({TG_GUARD_TARGET_RELEASE:'v9.9.9'}).status,0,'changed stable release must not install');
    for (const marker of ['dirty', 'wrong-remote', 'diverged','locked','ahead']) {
      fs.writeFileSync(path.join(target, marker), 'test');
      result = run();
      assert.notEqual(result.status, 0, marker);
      assert.equal(fs.readFileSync(path.join(target, '.env'), 'utf8'), 'preserve-me\n');
      assert.equal(fs.readFileSync(path.join(target, 'deployed'), 'utf8'), 'deployed\ndeployed\n');
      fs.unlinkSync(path.join(target, marker));
    }
    fs.writeFileSync(path.join(target, 'fail-deploy'), 'test');
    result = run();
    assert.notEqual(result.status,0);
    assert.ok(!fs.existsSync(path.join(target,'rollback')),'same version failure must not repeat the same deployment as rollback');
    fs.writeFileSync(path.join(target, 'fail-deploy'), 'test');
    fs.writeFileSync(path.join(target, 'old-code'), 'test');
    result = run();
    assert.notEqual(result.status, 0, 'failed rollout must be reported');
    assert.ok(fs.existsSync(path.join(target, 'rollback')));
    assert.equal(fs.readFileSync(path.join(target, '.env'), 'utf8'), 'preserve-me\n');
    fs.writeFileSync(path.join(target, 'scripts/manage.sh'), 'echo reached > menu-reached\n');
    fs.writeFileSync(path.join(target, 'offline'), 'test');
    result = run();
    assert.equal(result.status, 0, result.stderr);
    assert.ok(fs.existsSync(path.join(root, 'menu-reached')));
    fs.rmSync(path.join(target, '.git', 'tg-guard-upgrade.lock'), {force:true});
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
