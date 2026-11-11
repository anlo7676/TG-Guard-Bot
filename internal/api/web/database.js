'use strict';
async function database(){
 return {html:card('下载数据库备份','包含项目全部 MySQL 数据表；不包含 .env、Redis 或 Telegram 历史消息。',`<p>备份使用当前配置加密密钥加密。请另存服务器的 .env；密钥丢失后无法恢复。Web 备份最大 32 MiB（按编码内存预算计算），较大数据库请使用服务器备份脚本。</p><button class="primary" id="download-database">下载加密备份</button>`)+card('恢复数据库','恢复将覆盖当前 MySQL 数据。请先下载当前备份，并确认待恢复文件来源。',`<form id="restore-database"><label>加密备份文件<input type="file" name="backup" accept=".tggbackup" required></label><p>恢复时会暂停机器人和后台写入；成功后需在服务器执行 <code>docker compose restart app</code>（非 Docker 部署请重启应用进程）。重启会清空本机器人使用的 Redis 数据库，包括登录会话与临时状态。历史待处理任务也会恢复，请确认它们仍适合执行。</p><label><input type="checkbox" name="saved" required>我已另存当前数据库备份，了解覆盖与重启要求</label><label>输入 RESTORE 确认覆盖<input name="confirmation" autocomplete="off" required pattern="RESTORE"></label><button class="primary" type="submit">覆盖并恢复数据库</button><p id="database-result" role="status"></p></form>`),bind:()=>{
  const download=$('#download-database'),form=$('#restore-database'),result=$('#database-result');
  download.onclick=async()=>{
   if(download.disabled)return;download.disabled=true;
   try{const response=await fetch('/api/v1/database/backup',{credentials:'same-origin'});if(!response.ok){const data=await response.json();throw Error(data.error||'备份失败')}
    const blob=await response.blob(),url=URL.createObjectURL(blob),link=document.createElement('a');link.href=url;link.download=`tg-guard-${new Date().toISOString().replace(/[:.]/g,'-')}.tggbackup`;document.body.appendChild(link);link.click();link.remove();setTimeout(()=>URL.revokeObjectURL(url),60000);toast('备份已生成，请确认文件已保存');
   }catch(e){toast(e.message,true)}finally{download.disabled=false}
  };
  form.onsubmit=async e=>{
   e.preventDefault();const button=form.querySelector('button[type="submit"]');if(button.disabled)return;
   const file=form.elements.backup.files[0];
   if(!file||!form.elements.saved.checked||form.elements.confirmation.value!=='RESTORE'){result.textContent='请选择备份文件，另存当前备份并输入 RESTORE。';return}
   if(file.size>32*1024*1024+128){result.textContent='备份文件超过 32 MiB 上限。';return}
   button.disabled=true;download.disabled=true;result.textContent='正在校验备份并等待当前业务结束，请保持页面打开…';
   try{const response=await fetch('/api/v1/database/restore',{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/octet-stream','X-CSRF-Token':state.csrf,'X-Confirm-Restore':'RESTORE'},body:file});const data=await response.json();if(!response.ok)throw Error(data.error||'恢复失败');result.textContent=data.message+' 重启命令：docker compose restart app';
   }catch(e){result.textContent=e.message+' 若请求中断，恢复结果可能尚未确认；请检查服务日志，业务暂停时需重启应用。';button.disabled=false;download.disabled=false}
  };
 }};
}
