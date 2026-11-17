(function(){
 const root=document.getElementById('markdown-preview'),body=document.getElementById('markdown-body'),caption=document.getElementById('markdown-caption'),source=document.getElementById('markdown-source');
 let packet=null,lastKey='',lastHTML='';
 const send=m=>window.webkit.messageHandlers.farshmac.postMessage(m);
 source.addEventListener('click',()=>send({Type:'key',Key:'M',Meta:true,Shift:true}));
 root.addEventListener('mousedown',()=>{if(packet&&!packet.Full&&!packet.Blocked)send({Type:'click',X:packet.X,Y:packet.Y,Button:0})});
 root.addEventListener('wheel',e=>e.stopPropagation());
 body.addEventListener('click',e=>{
  const a=e.target.closest('a');if(!a)return;e.preventDefault();
  const href=a.getAttribute('href')||'';if(!href.startsWith('#'))return;
  let id;try{id=decodeURIComponent(href.slice(1))}catch{return}
  const target=Array.from(body.querySelectorAll('[id]')).find(n=>n.id===id);
  if(target)root.scrollTop+=target.getBoundingClientRect().top-root.getBoundingClientRect().top-44;
 });
 window.farshmacMarkdown={
  update(p,screen,cols,rows){
   packet=p;root.hidden=!p;if(!p){lastKey='';lastHTML='';body.innerHTML='';return}
   root.dataset.theme=p.theme||'Classic';root.style.pointerEvents=p.Blocked?'none':'auto';
   root.style.left=(screen.left+p.X*screen.width/cols)+'px';root.style.top=(screen.top+p.Y*screen.height/rows)+'px';root.style.width=(p.W*screen.width/cols)+'px';root.style.height=(p.H*screen.height/rows)+'px';
   if(lastKey!==p.key){root.scrollTop=0;root.scrollLeft=0;lastKey=p.key}
   if(p.html!==undefined&&p.html!==lastHTML){body.innerHTML=p.html;lastHTML=p.html}
   caption.textContent=p.Full?'Markdown · Esc Close':'Markdown · Quick view';source.hidden=!p.Full;
   const holes=(p.occlusions||[]).map(o=>`<rect x="${o.X-p.X}" y="${o.Y-p.Y}" width="${o.W}" height="${o.H}" fill="black"/>`).join('');
   const mask=holes?'url("data:image/svg+xml,'+encodeURIComponent(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${p.W} ${p.H}" preserveAspectRatio="none"><mask id="m" maskUnits="userSpaceOnUse" x="0" y="0" width="${p.W}" height="${p.H}"><rect width="100%" height="100%" fill="white"/>${holes}</mask><rect width="100%" height="100%" fill="white" mask="url(#m)"/></svg>`)+'")':'';
   root.style.maskImage=root.style.webkitMaskImage=mask;root.style.maskSize=root.style.webkitMaskSize='100% 100%';
  },
  copy(){if(!packet||packet.Blocked)return false;const selection=window.getSelection();if(selection&&root.contains(selection.anchorNode)){const text=selection.toString();if(text)send({Type:'clipboard',Text:text});return true}return false},
  key(e){
   if(!packet||packet.Blocked||!packet.Active)return false;
   if(e.metaKey&&e.key.toLowerCase()==='c'){if(!this.copy())return false}
   else if(e.metaKey&&e.key.toLowerCase()==='a'){const r=document.createRange();r.selectNodeContents(body);const s=window.getSelection();s.removeAllRanges();s.addRange(r)}
   else if(!e.metaKey&&!e.ctrlKey&&!e.altKey&&['ArrowDown','ArrowUp','ArrowLeft','ArrowRight','PageDown','PageUp','Home','End',' '].includes(e.key)){
    const page=Math.max(40,root.clientHeight-50);
    if(e.key==='Home')root.scrollTop=0;else if(e.key==='End')root.scrollTop=root.scrollHeight;
    else if(e.key==='ArrowLeft'||e.key==='ArrowRight')root.scrollLeft+=(e.key==='ArrowLeft'?-48:48);
    else root.scrollTop+=e.key==='ArrowDown'?36:e.key==='ArrowUp'?-36:e.key==='PageUp'||e.key===' '&&e.shiftKey?-page:page;
   } else return false;
   e.preventDefault();e.stopImmediatePropagation();return true;
  }
 };
})();
