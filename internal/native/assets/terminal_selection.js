'use strict';
// Selection uses public xterm buffer APIs. Each shell owns its own controller;
// no terminal input is synthesized when the user selects output.
class TerminalSelection {
 constructor(term,element,active,rect){
  this.screenRect=rect||(()=>element.querySelector('.xterm-screen').getBoundingClientRect());
  this.term=term;this.element=element;this.active=active;this.anchor=null;this.end=null;this.rect=false;this.drag=false;
  this.overlay=document.createElement('div');this.overlay.className='terminal-selection-overlay';element.append(this.overlay);
  element.addEventListener('mousedown',e=>this.down(e),true);
  element.addEventListener('mousemove',e=>this.move(e),true);
  element.addEventListener('mouseup',e=>this.up(e),true);
  // Capturing at the document keeps a drag alive outside the terminal surface.
  document.addEventListener('mousemove',e=>{if(this.drag&&!element.contains(e.target))this.move(e)},true);
  document.addEventListener('mouseup',e=>{if(this.drag)this.up(e)},true);
  term.onScroll?.(()=>this.render());
  term.onResize?.(()=>this.clear());
  term.onWriteParsed?.(()=>{if(this.buffer!==term.buffer.active)this.clear();else {
   if(this.anchorMarker?.isDisposed||this.endMarker?.isDisposed){this.clear();return}
   if(this.anchorMarker)this.anchor.y=this.anchorMarker.line;if(this.endMarker)this.end.y=this.endMarker.line;
   this.render();
  }});
 }
 clear(){this.anchorMarker?.dispose();this.endMarker?.dispose();this.anchorMarker=this.endMarker=null;this.anchor=this.end=null;this.rect=false;this.drag=false;clearTimeout(this.timer);this.overlay.replaceChildren();this.term.clearSelection();}
 bufferLength(){return this.term.buffer.active.length||this.term.rows}
 point(e){const r=this.screenRect();return {x:Math.max(0,Math.min(this.term.cols,Math.floor((e.clientX-r.left)/(r.width/this.term.cols)))),y:Math.max(0,Math.min(this.bufferLength()-1,this.term.buffer.active.viewportY+Math.floor((e.clientY-r.top)/(r.height/this.term.rows))))}}
 stop(e){e.preventDefault();e.stopImmediatePropagation()}
 down(e){
  if(!this.active()||e.button!==0||e.ctrlKey||e.metaKey)return;
  if(this.term.modes.mouseTrackingMode!=='none'&&!e.shiftKey&&!e.altKey)return;
  this.stop(e);this.term.focus();this.buffer=this.term.buffer.active;
  const p=this.point(e);if(!e.shiftKey||!this.anchor)this.anchor=p;
  this.end=p;this.rect=!!e.altKey;this.drag=true;
  if(e.detail===2&&!this.rect){this.anchor=this.word(p,-1);this.end=this.word(p,1)}
  if(e.detail>=3&&!this.rect){this.anchor={x:0,y:p.y};this.end={x:this.term.cols,y:p.y}}
  this.remember();this.render();
 }
 move(e){if(!this.drag)return;this.stop(e);this.end=this.point(e);this.remember();this.render();clearTimeout(this.timer);
  const r=this.screenRect();
  const direction=e.clientY<r.top?-1:e.clientY>r.top+r.height?1:0;
  if(direction){this.term.scrollLines(direction);this.timer=setTimeout(()=>this.move(e),45)}
 }
 up(e){if(!this.drag)return;this.stop(e);this.drag=false;clearTimeout(this.timer);this.render()}
 cell(p){return this.term.buffer.active.getLine(p.y)?.getCell(p.x)}
 step(p,d){let x=p.x+d,y=p.y;const n=this.term.cols;
  if(x<0){if(y===0)return {x:0,y};y--;x=n-1}
  if(x>n){if(y>=this.bufferLength()-1)return {x:n,y};y++;x=1}
  if(x<n&&this.cell({x,y})?.getWidth()===0)x+=d;
  return {x:Math.max(0,Math.min(n,x)),y};
 }
 word(p,d){let q={...p};const line=this.term.buffer.active.getLine(p.y);if(!line)return q;
  const isWord=x=>{const s=line.getCell(x)?.getChars()||' ';return /[\p{L}\p{N}_]/u.test(s)};
  if(d<0){while(q.x>0&&!isWord(q.x-1))q.x--;while(q.x>0&&isWord(q.x-1))q.x--}
  else {while(q.x<this.term.cols&&!isWord(q.x))q.x++;while(q.x<this.term.cols&&isWord(q.x))q.x++}
  if(q.x<this.term.cols&&line.getCell(q.x)?.getWidth()===0)q.x+=d;
  return q;
 }
 key(e){
  if(!this.active()||e.isComposing)return false;
  if(e.key==='Escape'&&this.anchor&&(this.rect?this.anchor.x!==this.end.x:this.term.hasSelection())){this.clear();this.stop(e);return true}
  if(!e.shiftKey||!['ArrowLeft','ArrowRight','ArrowUp','ArrowDown','Home','End'].includes(e.key))return false;
  const b=this.term.buffer.active;
  if(!this.anchor){const old=this.term.getSelectionPosition?.();this.anchor=old?.start||{x:b.cursorX||0,y:(b.baseY||0)+(b.cursorY||0)};this.end=old?.end||{...this.anchor}}
  this.buffer=b;this.rect=false;
  let p={...this.end};const d=e.key==='ArrowLeft'?-1:1;
  if(e.key==='Home'||e.key==='End'||e.metaKey&&['ArrowLeft','ArrowRight'].includes(e.key))p.x=e.key==='Home'||d<0?0:this.lineEnd(p.y);
  else if(e.key==='ArrowLeft'||e.key==='ArrowRight')p=e.altKey||e.ctrlKey?this.word(p,d):this.step(p,d);
  else p.y=Math.max(0,Math.min(this.bufferLength()-1,p.y+(e.key==='ArrowUp'?-1:1)));
  this.end=p;this.remember();this.render();if(p.y<b.viewportY)this.term.scrollToLine(p.y);else if(p.y>=b.viewportY+this.term.rows)this.term.scrollToLine(p.y-this.term.rows+1);
  this.stop(e);return true;
 }
 remember(){
  this.anchorMarker?.dispose();this.endMarker?.dispose();const b=this.term.buffer.active,cursor=(b.baseY||0)+(b.cursorY||0);
  this.anchorMarker=this.term.registerMarker?.(this.anchor.y-cursor);this.endMarker=this.term.registerMarker?.(this.end.y-cursor);
 }
 lineEnd(y){const line=this.term.buffer.active.getLine(y);let x=this.term.cols;while(x>0){const c=line?.getCell(x-1);if(c?.getChars()?.trim()||c?.getWidth()===0)break;x--}return x}
 render(){
  this.overlay.replaceChildren();if(!this.anchor||!this.end)return;
  const t=this.term,a=this.anchor,b=this.end;
  if(!this.rect){const start=a.y*t.cols+a.x,end=b.y*t.cols+b.x;const lo=Math.min(start,end);t.select(lo%t.cols,Math.floor(lo/t.cols),Math.abs(end-start));return}
  t.clearSelection();const r=this.screenRect(),parent=this.element.getBoundingClientRect();
  const left=Math.min(a.x,b.x),right=Math.max(a.x,b.x),top=Math.min(a.y,b.y),bottom=Math.max(a.y,b.y);
  for(let y=Math.max(top,t.buffer.active.viewportY);y<=Math.min(bottom,t.buffer.active.viewportY+t.rows-1);y++){
   const mark=document.createElement('div');mark.style.cssText=`position:absolute;left:${r.left-parent.left+left*r.width/t.cols}px;top:${r.top-parent.top+(y-t.buffer.active.viewportY)*r.height/t.rows}px;width:${(right-left)*r.width/t.cols}px;height:${r.height/t.rows}px;background:rgba(0,180,180,.4)`;this.overlay.append(mark);
  }
 }
 copy(){if(!this.rect||!this.anchor)return this.term.getSelection();const a=this.anchor,b=this.end,left=Math.min(a.x,b.x),right=Math.max(a.x,b.x);if(left===right)return '';
  const out=[];for(let y=Math.min(a.y,b.y);y<=Math.max(a.y,b.y);y++)out.push(this.term.buffer.active.getLine(y)?.translateToString(false,left,right)||'');return out.join('\n');
 }
}
