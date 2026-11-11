const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
const path=require('node:path');
async function setup(){
 const nodes=new Map(),get=q=>{if(!nodes.has(q))nodes.set(q,{disabled:false,textContent:''});return nodes.get(q)};
 const button={disabled:false},form=get('#restore-database');
 form.elements={backup:{files:[{size:20,name:'test.tggbackup'}]},saved:{checked:false},confirmation:{value:''}};
 form.querySelector=()=>button;
 const calls=[],ctx=vm.createContext({$:get,card:(a,b,c)=>a+b+c,state:{csrf:'test-csrf'},fetch:async(...args)=>{calls.push(args);return {ok:true,json:async()=>({message:'数据库已恢复，请重启'})}},toast(){},setTimeout(){},URL:{},document:{}});
 vm.runInContext(fs.readFileSync(path.join(__dirname,'../internal/api/web/database.js'),'utf8'),ctx);
 const view=await vm.runInContext('database()',ctx);view.bind();
 return {ctx,get,form,button,calls,view,submit:()=>form.onsubmit({preventDefault(){}})};
}
test('restore requires explicit confirmation and does not upload on opening',async()=>{
 const h=await setup();assert.equal(h.calls.length,0);assert.match(h.view.html,/docker compose restart app/);
 await h.submit();assert.equal(h.calls.length,0);
 h.form.elements.saved.checked=true;h.form.elements.confirmation.value='RESTORE';await h.submit();
 assert.equal(h.calls.length,1);const [url,options]=h.calls[0];assert.equal(url,'/api/v1/database/restore');
 assert.equal(options.headers['X-CSRF-Token'],'test-csrf');assert.equal(options.headers['X-Confirm-Restore'],'RESTORE');assert.equal(options.body,h.form.elements.backup.files[0]);
 assert.equal(h.button.disabled,true);assert.match(h.get('#database-result').textContent,/已恢复/);
 await h.submit();assert.equal(h.calls.length,1);
});
test('restore rejects oversized files before network and preserves failed input',async()=>{
 const h=await setup();h.form.elements.saved.checked=true;h.form.elements.confirmation.value='RESTORE';h.form.elements.backup.files[0].size=33*1024*1024;
 await h.submit();assert.equal(h.calls.length,0);assert.match(h.get('#database-result').textContent,/超过/);
 h.form.elements.backup.files[0].size=20;h.ctx.fetch=async()=>({ok:false,json:async()=>({error:'备份已损坏'})});await h.submit();
 assert.match(h.get('#database-result').textContent,/备份已损坏/);assert.equal(h.button.disabled,false);assert.equal(h.form.elements.backup.files[0].name,'test.tggbackup');
});
