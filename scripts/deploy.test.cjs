'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const {spawnSync} = require('node:child_process');
const source = path.resolve(__dirname);
const token = '123456789:' + 'A'.repeat(35);
const config = `BOT_TOKEN=${token}\nADMIN_API_TOKEN=${'b'.repeat(64)}\nMYSQL_PASSWORD=existing-db\nMYSQL_ROOT_PASSWORD=existing-root\nREDIS_PASSWORD=existing-redis\n`;
for (const kind of ['bash', ...(process.platform === 'win32' ? ['pwsh'] : [])]) {
  test(`${kind}: first install, update and failure preserve configuration`, () => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'tg-guard-deploy-test-'));
    try {
      fs.mkdirSync(path.join(root, 'scripts'));
      fs.copyFileSync(path.join(source, `deploy.${kind === 'bash' ? 'sh' : 'ps1'}`), path.join(root, 'scripts', `deploy.${kind === 'bash' ? 'sh' : 'ps1'}`));
      fs.writeFileSync(path.join(root, 'scripts', 'open-panel.ps1'), "Set-Content -LiteralPath panel-opened 'yes'\n");
      const run = (fail = false, failTicket = false) => {
        if (kind === 'pwsh') {
          const harness = `function global:docker { if ($args -contains 'up') { Add-Content -LiteralPath docker-start 'yes'; $global:LASTEXITCODE = ${fail ? 1 : 0} } else { $global:LASTEXITCODE = 0 } }\nfunction global:Read-Host { return '${token}' }\n& ./scripts/deploy.ps1\n`;
          return spawnSync('pwsh', ['-NoProfile', '-Command', harness], {cwd:root, encoding:'utf8'});
        }
        const bash = process.platform === 'win32' ? 'C:/Program Files/Git/bin/bash.exe' : 'bash';
        const harness = `docker() { case "$*" in *' up '*) printf 'yes\\n' >> docker-start; return ${fail ? 1 : 0};; esac; }; curl() { cat >/dev/null; ${failTicket?'return 1;':`printf '%s' '{"ticket":"test-ticket"}';`} }; export -f docker curl; bash scripts/deploy.sh`;
        return spawnSync(bash, ['-c', harness], {cwd:root, input:token+'\n', encoding:'utf8'});
      };
      let result = run();
      assert.equal(result.status, 0, result.stderr);
      let saved = fs.readFileSync(path.join(root, '.env'), 'utf8');
      assert.match(saved, /SETTINGS_ENCRYPTION_KEY=[a-f0-9]{64}/);
      assert.match(saved, /ADMIN_API_TOKEN=[a-f0-9]{64}/);
      assert.ok(!result.stdout.includes(token));
      assert.ok(!result.stdout.includes(saved.match(/ADMIN_API_TOKEN=(.*)/)[1]));
      result = run();
      assert.equal(result.status, 0, result.stderr);
      assert.equal(fs.readFileSync(path.join(root, '.env'), 'utf8'), saved);
      if (kind === 'bash') {
        result=run(false,true);assert.equal(result.status,0,'ticket failure must not roll back healthy service');
        fs.writeFileSync(path.join(root,'.env'),config+'PANEL_DOMAIN=guard.example.com\n');
        result=run();assert.equal(result.status,0,result.stderr);assert.match(result.stdout,/https:\/\/guard.example.com\/#ticket=/);
        fs.writeFileSync(path.join(root, '.env'), config + 'PANEL_BIND=0.0.0.0\nPANEL_HOST=203.0.113.10\n');
        result = run();
        assert.equal(result.status, 0, result.stderr);
        assert.ok(result.stdout.includes('http://203.0.113.10:8080/#ticket=test-ticket'));
        assert.ok(!result.stdout.includes('本机后台登录地址'));
      }
      fs.writeFileSync(path.join(root, '.env'), config);
      result = run(true);
      assert.notEqual(result.status, 0);
      assert.equal(fs.readFileSync(path.join(root, '.env'), 'utf8'), config);
      fs.rmSync(path.join(root, 'docker-start'));
      fs.writeFileSync(path.join(root, '.env'), 'BOT_TOKEN=old\n');
      result = run();
      assert.notEqual(result.status, 0);
      assert.equal(fs.readFileSync(path.join(root, '.env'), 'utf8'), 'BOT_TOKEN=old\n');
      assert.ok(!fs.existsSync(path.join(root, 'docker-start')));
    } finally {
      const resolved = path.resolve(root);
      assert.ok(resolved.startsWith(path.resolve(os.tmpdir()) + path.sep));
      assert.ok(path.basename(resolved).startsWith('tg-guard-deploy-test-'));
      fs.rmSync(resolved, {recursive:true, force:true});
    }
  });
}
