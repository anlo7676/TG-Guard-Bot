'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),os=require('node:os'),path=require('node:path');
const {spawnSync}=require('node:child_process');
test('backup publishes complete archives only and preserves config on failure',()=>{
 const root=fs.mkdtempSync(path.join(os.tmpdir(),'tg-guard-backup-test-'));
 const bash=process.platform==='win32'?'C:/Program Files/Git/bin/bash.exe':'bash';
 try {
  fs.mkdirSync(path.join(root,'scripts'));fs.mkdirSync(path.join(root,'.git'));
  fs.copyFileSync(path.join(__dirname,'backup.sh'),path.join(root,'scripts/backup.sh'));
  fs.writeFileSync(path.join(root,'.env'),'TEST_SECRET=not-a-real-secret\n');
  const harness=`docker() { if [[ "$*" == *mysql* ]]; then [[ "$FAIL" != 1 ]] || return 9; echo 'CREATE TABLE example (id INT);'; else echo 'fake-rdb'; fi; }; timeout() { shift; "$@"; }; flock() { return 0; }; git() { echo test-revision; }; export -f docker timeout flock git; bash scripts/backup.sh`;
  const run=fail=>spawnSync(bash,['-c',harness],{cwd:root,encoding:'utf8',env:{...process.env,FAIL:fail?'1':'0'}});
  let r=run(false);assert.equal(r.status,0,r.stderr);
  const archives=()=>fs.readdirSync(path.join(root,'backups')).filter(x=>x.endsWith('.tar.gz'));
  const saved=archives();assert.equal(saved.length,1);
  const verify=spawnSync(bash,['-c',`mkdir verify; tar -xzf "backups/${saved[0]}" -C verify; cd verify; sha256sum -c SHA256SUMS; gzip -t mysql.sql.gz`],{cwd:root,encoding:'utf8'});assert.equal(verify.status,0,verify.stderr);
  r=run(true);assert.notEqual(r.status,0);assert.deepEqual(archives(),saved);
  assert.equal(fs.readFileSync(path.join(root,'.env'),'utf8'),'TEST_SECRET=not-a-real-secret\n');
  assert.ok(!fs.readdirSync(path.join(root,'backups')).some(x=>x.startsWith('.pending.')));
  assert.ok(!r.stdout.includes('not-a-real-secret'));
 } finally {assert.ok(path.resolve(root).startsWith(path.resolve(os.tmpdir())+path.sep));assert.ok(path.basename(root).startsWith('tg-guard-backup-test-'));fs.rmSync(root,{recursive:true,force:true});}
});
