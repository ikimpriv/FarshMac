'use strict';
class HistoryView {
 constructor(send,live,rows) {
  this.send=send;this.live=live;this.rows=rows;this.visible=false;this.pending=false;this.request=0;
  this.status={first:0,total:0};this.top=0;this.wanted=0;
  this.el=document.getElementById('history');this.content=document.getElementById('history-lines');this.caption=document.getElementById('history-caption');
  document.getElementById('history-older').onclick=()=>this.move(-this.rows());
  document.getElementById('history-newer').onclick=()=>this.move(this.rows());
  document.getElementById('history-live').onclick=()=>this.close();
  this.el.addEventListener('wheel',e=>{if(Math.abs(e.deltaX)>Math.abs(e.deltaY))return;e.preventDefault();this.move(Math.sign(e.deltaY)*Math.max(1,Math.min(12,Math.ceil(Math.abs(e.deltaY)/20))));},{passive:false});
 }
 update(status,page) {
  if(status)this.status=status;
  if(!page||page.request!==this.request)return;
  this.pending=false;
  if(!this.visible)return;
  this.top=page.start;
  this.content.replaceChildren();
  const hex=n=>'#'+(n>>>0).toString(16).padStart(6,'0');
  for(const line of page.lines) {
   const row=document.createElement('div');row.className='history-line';row.dataset.wrapped=String(!!line.w);
   if(!line.r.length)row.textContent='\u200b';
   for(const [text,fg,bg,flags] of line.r) {
    const span=document.createElement('span');span.textContent=text;
    span.style.color=hex(flags&32?bg:fg);span.style.backgroundColor=hex(flags&32?fg:bg);
    if(flags&1)span.style.fontWeight='bold';if(flags&2)span.style.opacity='.5';if(flags&4)span.style.fontStyle='italic';
    span.style.textDecoration=[flags&8?'underline':'',flags&128?'line-through':''].join(' ');
    if(flags&64)span.style.visibility='hidden';row.append(span);
   }
   this.content.append(row);
  }
  this.caption.textContent=page.error||(page.lines.length===0?'History is empty':`History ${page.start+1}–${page.start+page.lines.length} / ${page.total}${page.first?' · oldest rows expired':''}`);
  if(this.wanted!==page.start&&this.wanted>=page.first&&this.wanted<=Math.max(page.first,page.total-this.rows()))this.load();
 }
 open(top){this.visible=true;document.body.classList.add('archive');this.el.focus();this.wanted=top??Math.max(this.status.first,this.status.total-this.rows());this.load();}
 close(){this.visible=false;this.pending=false;this.request++;document.body.classList.remove('archive');this.live();}
 move(delta){this.wanted=Math.max(this.status.first,Math.min(this.wanted+delta,Math.max(this.status.first,this.status.total-this.rows())));this.load();}
 load(){
  if(this.pending)return;this.pending=true;this.request++;
  this.send({Type:'historyRead',Start:this.wanted,Count:this.rows(),Request:this.request});
 }
 key(e){
  if(!this.visible)return false;
  const moves={ArrowUp:-1,ArrowDown:1,PageUp:-this.rows(),PageDown:this.rows()};
  if(e.key==='Escape'||e.key==='End'){this.close();}
  else if(e.key==='Home'){this.wanted=this.status.first;this.load();}
  else if(e.key in moves){this.move(moves[e.key]);}
  else return false;
  e.preventDefault();return true;
 }
 copy(){
  const selection=window.getSelection();
  if(!selection.rangeCount)return selection.toString().replace(/\u200b/g,'');
  const selected=selection.getRangeAt(0);let text='',first=true;
  for(const row of this.content.children){
   if(!selected.intersectsNode(row))continue;
   const part=document.createRange();part.selectNodeContents(row);
   if(row.contains(selected.startContainer))part.setStart(selected.startContainer,selected.startOffset);
   if(row.contains(selected.endContainer))part.setEnd(selected.endContainer,selected.endOffset);
   if(!first&&row.dataset.wrapped!=='true')text+='\n';text+=part.toString().replace(/\u200b/g,'');first=false;
  }
  return text;
 }
 selectAll(){const range=document.createRange();range.selectNodeContents(this.content);const s=window.getSelection();s.removeAllRanges();s.addRange(range);}
}
