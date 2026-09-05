const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
const path=require('node:path');
function setup(){
 const nodes=new Map();
 const node=()=>({innerHTML:'',hidden:false,disabled:false,value:'',focus(){},scrollIntoView(){},addEventListener(){}});
 const get=q=>{if(!nodes.has(q))nodes.set(q,node());return nodes.get(q)};
 const reason=node(),button=node();get('#authorization-form').elements={reason};get('#authorization-form').querySelector=()=>button;
 const ctx=vm.createContext({document:{querySelector:get,querySelectorAll:()=>[]},window:{},console,setTimeout,clearTimeout,prompt(){throw Error('native prompt unsupported')},calls:[],testError:null});
 const source=fs.readFileSync(path.join(__dirname,'../internal/api/web/app.js'),'utf8').replace(/boot\(\);\s*$/,'');
 vm.runInContext(source,ctx);
 vm.runInContext("state.groups=[{chat_id:-1001,title:'测试群'}]; api=async (path,options)=>{calls.push({path,options});if(testError)throw Error(testError)}; loadGroups=async()=>{};render=async()=>{};toast=()=>{};editAuthorization('-1001','approved');",ctx);
 return {ctx,get,reason,button,submit:()=>get('#authorization-form').onsubmit({preventDefault(){},submitter:button})};
}
test('opening and cancelling approval never submits or invokes native prompt',()=>{
 const h=setup();assert.match(h.get('#group-editor').innerHTML,/确认批准/);assert.equal(h.ctx.calls.length,0);
 h.get('#cancel-authorization').onclick();assert.equal(h.get('#group-editor').innerHTML,'');assert.equal(h.ctx.calls.length,0);
});
test('explicit form submission sends group status reason and clears form on success',async()=>{
 const h=setup();h.reason.value=' 测试批准 ';await h.submit();assert.equal(h.ctx.calls.length,1);
 const call=JSON.parse(JSON.stringify(h.ctx.calls[0]));assert.deepEqual(call,{path:'/api/v1/groups/-1001/authorization',options:{method:'PUT',body:{status:'approved',reason:'测试批准'}}});
 assert.equal(h.get('#group-editor').innerHTML,'');assert.equal(h.button.disabled,false);
});
test('failed request retains form and reason, shows error and allows retry',async()=>{
 const h=setup();h.ctx.testError='审批保存失败';h.reason.value='保留原因';await h.submit();
 assert.match(h.get('#group-editor').innerHTML,/authorization-form/);assert.equal(h.reason.value,'保留原因');assert.equal(h.get('#authorization-error').textContent,'审批保存失败');assert.equal(h.get('#authorization-error').hidden,false);assert.equal(h.button.disabled,false);assert.equal(h.get('#cancel-authorization').disabled,false);
 h.ctx.testError=null;await h.submit();assert.equal(h.ctx.calls.length,2);assert.equal(h.get('#group-editor').innerHTML,'');
});
test('double submission while request pending does not issue duplicate approval',async()=>{
 const h=setup();vm.runInContext('api=()=>new Promise(resolve=>{release=resolve})',h.ctx);const first=h.submit();assert.equal(h.button.disabled,true);await h.submit();h.ctx.release();await first;assert.equal(h.button.disabled,false);
});
