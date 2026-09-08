'use strict';
const test=require('node:test'), assert=require('node:assert/strict'), fs=require('node:fs'), os=require('node:os'), path=require('node:path');
const {spawnSync}=require('node:child_process');
test('server menu changes access without losing credentials and can fetch login without restarting',()=>{
 const root=fs.mkdtempSync(path.join(os.tmpdir(),'tg-guard-menu-test-'));
 try {
  fs.mkdirSync(path.join(root,'scripts'));
  fs.copyFileSync(path.join(__dirname,'manage.sh'),path.join(root,'scripts/manage.sh'));
  fs.writeFileSync(path.join(root,'scripts/deploy.sh'),'echo "$*" >> login-calls\n');
  fs.writeFileSync(path.join(root,'.env'),'BOT_TOKEN=preserved\nADMIN_API_TOKEN=preserved-key\nPANEL_BIND=127.0.0.1\n');
  const run=(input,fail=false)=>spawnSync(process.platform==='win32'?'C:/Program Files/Git/bin/bash.exe':'bash',['-c','docker() { echo "$*" >> docker-calls; [[ "$FAIL_DOCKER" != 1 ]]; }; export -f docker; bash scripts/manage.sh'],{cwd:root,input,encoding:'utf8',env:{...process.env,FAIL_DOCKER:fail?'1':'0'}});
  let result=run('2\n203.0.113.10\n0\n'); assert.equal(result.status,0,result.stderr);
  let env=fs.readFileSync(path.join(root,'.env'),'utf8'); assert.match(env,/BOT_TOKEN=preserved/); assert.match(env,/ADMIN_API_TOKEN=preserved-key/); assert.match(env,/PANEL_BIND=0.0.0.0/); assert.match(env,/PANEL_HOST=203.0.113.10/);
  const calls=fs.readFileSync(path.join(root,'docker-calls'),'utf8'); assert.match(calls,/--no-build --no-deps/);
  result=run('4\n0\n'); assert.equal(result.status,0,result.stderr); assert.equal(fs.readFileSync(path.join(root,'docker-calls'),'utf8'),calls);
  result=run('2\nbad/path\n0\n'); assert.equal(result.status,0,result.stderr); assert.equal(fs.readFileSync(path.join(root,'.env'),'utf8'),env);
  result=run('3\n0\n',true); assert.equal(result.status,0,result.stderr); assert.equal(fs.readFileSync(path.join(root,'.env'),'utf8'),env); assert.ok(result.stdout.includes('运行状态无法确认'));
  fs.writeFileSync(path.join(root,'install.sh'),'printf "%s" "$TG_GUARD_INSTALL_DIR" > update-target\n');
  result=run('5\n0\n'); assert.equal(result.status,0,result.stderr); assert.ok(fs.readFileSync(path.join(root,'update-target'),'utf8').endsWith(path.basename(root)));
  result=run('3\n0\n'); assert.equal(result.status,0,result.stderr); env=fs.readFileSync(path.join(root,'.env'),'utf8'); assert.match(env,/PANEL_BIND=127.0.0.1/); assert.equal((env.match(/PANEL_BIND=/g)||[]).length,1);
 } finally {
  assert.ok(path.resolve(root).startsWith(path.resolve(os.tmpdir())+path.sep)); assert.ok(path.basename(root).startsWith('tg-guard-menu-test-')); fs.rmSync(root,{recursive:true,force:true});
 }
});
