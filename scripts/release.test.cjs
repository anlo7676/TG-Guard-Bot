const {test}=require('node:test');
const assert=require('node:assert/strict');
const {spawnSync}=require('node:child_process');
const path=require('node:path');
const script=path.resolve(__dirname,'release.sh').replaceAll('\\','/');
const mocks=`gh(){ case "$2" in view) [[ "$MOCK_PRIOR" != missing ]] || return 1; printf '%s' "$MOCK_PRIOR";; list) printf '%s' "$MOCK_LATEST";; *) printf 'operation: %s\\n' "$*";; esac; }; export -f gh;`;
function run(mode,prior='missing',latest='',tag='v1.9.0'){
 return spawnSync(process.platform==='win32'?'C:/Program Files/Git/bin/bash.exe':'bash',['-c',mocks+` bash "${script}" ${mode}`],{encoding:'utf8',env:{...process.env,GITHUB_REF_NAME:tag,MOCK_PRIOR:prior,MOCK_LATEST:latest}});
}
test('release upload resumes prerelease and never replaces stable assets',()=>{
 for(const [prior,expected] of [['missing','release create'],['true','release upload']]){const r=run('upload',prior);assert.equal(r.status,0,r.stderr);assert.ok(r.stdout.includes(expected));}
 const r=run('upload','false');assert.equal(r.status,0,r.stderr);assert.doesNotMatch(r.stdout,/create|upload/);
});
test('older release cannot replace latest and invalid versions fail closed',()=>{
 let r=run('promote','true','v1.10.0');assert.equal(r.status,0,r.stderr);assert.match(r.stdout,/--latest=false/);
 r=run('promote','true','v1.8.0');assert.equal(r.status,0,r.stderr);assert.match(r.stdout,/--latest=true/);
 assert.notEqual(run('promote','true','not-a-version').status,0);
 assert.notEqual(run('upload','missing','','vbad').status,0);
});
