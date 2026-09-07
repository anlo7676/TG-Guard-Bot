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

test('ad rule text editor preserves regex alternation and disabled state',()=>{
 const h=setup();const text='包含|删除|广告词\n停用正则|封禁|优惠(代购|返利)';h.ctx.ruleText=text;
 const rendered=vm.runInContext('formatAdRules(parseAdRules(ruleText))',h.ctx);assert.equal(rendered,text);
 assert.throws(()=>vm.runInContext("parseAdRules('包含|未知|词')",h.ctx));
});
test('group editor exposes welcome template and direct local rule actions',async()=>{
 const h=setup();vm.runInContext(`api=async path=>path==='/api/v1/rule-catalog'?[{key:'advertising',label:'广告招揽',score:25,action:''},{key:'ad_tasks',label:'刷单返佣',score:60,action:'delete',pattern:'刷单.*返佣',example:'刷单返佣日结'}]:({welcome_enabled:true,welcome_text:'欢迎 {name}',rules:{advertising:{enabled:true,score:25,action:'mute'}},ad_rules:[{mode:'contains',pattern:'广告词',enabled:true,action:'delete'}]});`,h.ctx);
 await vm.runInContext("editGroup('-1001')",h.ctx);const html=h.get('#group-editor').innerHTML;
 for(const text of ['id="jump-default-rules"','id="default-rules"','1 类广告预设','name="welcome_text"','欢迎 {name}','name="ad_rules"','包含|删除|广告词','name="rule-advertising-action"','value="mute" selected','封禁并清理发言','刷单返佣日结','name="rule-ad_tasks-action"','value="delete" selected'])assert.ok(html.includes(text),text);
 assert.ok(html.indexOf('name="rule-ad_tasks-action"')<html.indexOf('name="rule-advertising-action"'));
 assert.equal(typeof h.get('#jump-default-rules').onclick,'function');
});

function feedbackSetup(){const h=setup();h.note={value:'',focus(){}};h.get('#inline-action-form').elements={note:h.note};vm.runInContext("editFeedback('-1001',17)",h.ctx);h.submitInline=()=>h.get('#inline-action-form').onsubmit({preventDefault(){}});return h}
test('feedback requires explicit nonempty input and retains failed input for retry',async()=>{
 const h=feedbackSetup();assert.equal(h.ctx.calls.length,0);await h.submitInline();assert.equal(h.ctx.calls.length,0);
 h.note.value=' 本地规则误命中 ';h.ctx.testError='暂时失败';await h.submitInline();assert.equal(h.get('#inline-action-error').textContent,'暂时失败');assert.equal(h.note.value,' 本地规则误命中 ');assert.match(h.get('#feedback-editor').innerHTML,/inline-action-form/);
 h.ctx.testError=null;vm.runInContext("state.chat='-9999'",h.ctx);await h.submitInline();assert.equal(h.ctx.calls[1].path,'/api/v1/groups/-1001/feedback');assert.equal(h.ctx.calls[1].options.body.note,'本地规则误命中');assert.equal(h.get('#feedback-editor').innerHTML,'');
});
test('inline confirmation cancellation and duplicate submission are safe',async()=>{
 const h=setup();vm.runInContext("inlineAction('#keyword-editor','删除 #1','确认删除',()=>api('/delete',{}))",h.ctx);assert.equal(h.ctx.calls.length,0);h.get('#inline-action-cancel').onclick();assert.equal(h.ctx.calls.length,0);
 vm.runInContext("inlineAction('#keyword-editor','删除 #1','确认删除',()=>new Promise(resolve=>{calls.push('delete');release=resolve}))",h.ctx);
 const submit=()=>h.get('#inline-action-form').onsubmit({preventDefault(){}});const first=submit();await submit();h.get('#inline-action-cancel').onclick();assert.equal(h.ctx.calls.length,1);assert.notEqual(h.get('#keyword-editor').innerHTML,'');h.ctx.release();await first;assert.equal(h.get('#keyword-editor').innerHTML,'');
});
test('web workflows never depend on native confirmation dialogs',()=>{const s=fs.readFileSync(path.join(__dirname,'../internal/api/web/app.js'),'utf8');assert.doesNotMatch(s,/\b(?:prompt|confirm)\s*\(/)});

test('rule workspace exposes saved policy test and readable filters',()=>{const h=setup();const html=vm.runInContext("ruleLab({moderation_enabled:true,ai_enabled:true,ai_threshold:50,direct_threshold:80},[{pattern:'test'}])",h.ctx);for(const text of ['rule-lab-form','50–79','不会发送消息','足球红包引流','正常招聘','new_member'])assert.ok(html.includes(text),text)});
