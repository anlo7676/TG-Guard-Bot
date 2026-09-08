'use strict';
const {test}=require('node:test');const assert=require('node:assert/strict');const fs=require('node:fs');const os=require('node:os');const path=require('node:path');const {spawnSync}=require('node:child_process');
test('updater setup installs a separate service without exposing Docker to the app',()=>{
 const root=fs.mkdtempSync(path.join(os.tmpdir(),'tg-updater-test-'));const project=path.join(root,'project');fs.mkdirSync(path.join(project,'scripts'),{recursive:true});fs.mkdirSync(path.join(project,'.git'));fs.mkdirSync(path.join(project,'units'));fs.writeFileSync(path.join(project,'.env'),'TEST_ONLY=1\n');
 const script=fs.readFileSync(path.join(__dirname,'setup-updater.sh'),'utf8').replaceAll('/usr/local/lib/tg-guard','$PWD/runtime').replaceAll('/etc/systemd/system','$PWD/units');fs.writeFileSync(path.join(project,'scripts/setup-updater.sh'),script);
 const mock=`uname(){ echo Linux; }; id(){ echo 0; }; chown(){ return 0; }; systemctl(){ printf '%s\\n' "$*" >> service-calls; }; docker(){ printf '%s\\n' '#!/bin/sh' 'echo test-agent' > "\${@: -1}"; }; source scripts/setup-updater.sh`;
 try{
  let result=spawnSync(process.platform==='win32'?'C:/Program Files/Git/bin/bash.exe':'bash',['-c',mock],{cwd:project,encoding:'utf8'});assert.equal(result.status,0,result.stderr);
  const unit=fs.readFileSync(path.join(project,'units/tg-guard-updater.service'),'utf8');assert.match(unit,/ExecStart=.*updater -update-agent/);assert.match(unit,/KillMode=control-group/);assert.match(fs.readFileSync(path.join(project,'service-calls'),'utf8'),/enable --now tg-guard-updater/);
  fs.writeFileSync(path.join(project,'.updates/active.json'),'{}');result=spawnSync(process.platform==='win32'?'C:/Program Files/Git/bin/bash.exe':'bash',['-c',mock],{cwd:project,encoding:'utf8'});assert.notEqual(result.status,0);assert.match(result.stdout,/正在升级/);
  const compose=fs.readFileSync(path.join(__dirname,'../compose.yaml'),'utf8');assert.doesNotMatch(compose,/docker\.sock|privileged:\s*true/);assert.match(compose,/\.updates:\/var\/lib\/tg-guard-updates/);
 }finally{assert.ok(path.resolve(root).startsWith(path.resolve(os.tmpdir())+path.sep));fs.rmSync(root,{recursive:true,force:true})}
});
