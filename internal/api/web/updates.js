'use strict';
async function updates(){
 const status=await api('/api/v1/upgrades');
 const busy=['queued','running'].includes(status.job.phase);
 const guidance=!status.can_upgrade?'请使用后台恢复密钥登录后执行升级。':!status.enabled?'网页升级服务尚未启用。请在服务器管理菜单选择「启用网页升级」；当前机器人功能不受影响。':'升级仅使用官方已验收稳定版，保留配置与数据；升级前请确认已有数据库和配置备份。';
 const html=card('版本与更新','检查稳定版本，并在此完成服务器升级。',`<div class="update-summary"><div class="stat"><span>当前运行版本</span><div class="stat-number">v${escape(status.current)}</div></div><div class="stat"><span>网页升级服务</span><div>${badge(status.enabled,'已连接','未启用')}</div></div></div><p class="hint">${escape(guidance)}</p><div class="form-footer"><button class="primary" id="check-upgrade" ${busy?'disabled':''}>检查更新</button><button class="secondary" id="refresh-upgrade">刷新状态</button></div><div id="release-result"></div><div id="upgrade-confirmation"></div><div id="upgrade-progress" role="status"></div>`);
 return {html,bind:()=>{
  const request=state.request;
  const active=()=>state.page==='updates'&&state.request===request&&!$('#app').hidden;
  const progress=(job)=>{$('#upgrade-progress').textContent=`${job.message}${job.version?' · '+job.version:''}${job.updated_at?' · '+new Date(job.updated_at).toLocaleString('zh-CN'):''}`};
  let retries=0;
  const poll=async()=>{if(!active())return;try{const s=await api('/api/v1/upgrades');if(!active())return;progress(s.job);if(s.current!==status.current){render();return}if(['queued','running'].includes(s.job.phase)){setTimeout(poll,3000)}else{toast(s.job.message,s.job.phase==='failed');$('#check-upgrade').disabled=false}}catch(e){if(!active())return;$('#upgrade-progress').textContent='服务正在切换或暂时无法连接，正在重新连接…';if(++retries<120)setTimeout(poll,5000)}};
  progress(status.job);if(busy)setTimeout(poll,3000);
  $('#refresh-upgrade').onclick=()=>render();
  $('#check-upgrade').onclick=e=>submit(e,async()=>{
   const result=await api('/api/v1/upgrades/check');if(!active())return;
   $('#release-result').innerHTML=`<p>${result.available?'发现新版本':'当前无需更新'}：<strong>${escape(result.release.version)}</strong> · <a href="${escape(result.release.url)}" target="_blank" rel="noreferrer">查看发布说明</a></p>${result.available&&status.enabled&&status.can_upgrade?'<button class="primary" id="start-upgrade">升级到此版本</button>':''}`;
   if($('#start-upgrade'))$('#start-upgrade').onclick=()=>inlineAction('#upgrade-confirmation',`升级到 ${result.release.version}`,'升级时后台会短暂断开。确认已备份数据库与配置后继续；启动失败会尝试恢复原服务，数据库结构不会自动回退。',async()=>{
    await api('/api/v1/upgrades',{method:'POST',body:{version:result.release.version}});
    if(!active())return;$('#check-upgrade').disabled=true;$('#start-upgrade').disabled=true;$('#upgrade-progress').textContent='升级任务已提交，等待服务器处理…';setTimeout(poll,2000);
   });
  });
 }};
}
