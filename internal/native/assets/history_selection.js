'use strict';
// Adapter for the bounded, currently loaded disk-history page. It keeps the
// same selection gestures without reading the entire spool into memory.
class HistorySelection {
 constructor(view){
  this.view=view;this.lines=[];this.cols=1;this.cellWidth=8;this.cellHeight=16;
  const self=this;
  const buffer={get length(){return self.lines.length},get viewportY(){return 0},baseY:0,cursorX:0,cursorY:0,getLine:y=>self.lines[y]};
  this.terminal={buffer:{active:buffer},modes:{mouseTrackingMode:'none'},get cols(){return self.cols},get rows(){return Math.max(1,self.lines.length)},focus:()=>view.el.focus(),getSelection:()=>view.copyDOM(),hasSelection:()=>!!view.copyDOM(),clearSelection:()=>window.getSelection().removeAllRanges(),select:(x,y,n)=>self.select(x,y,n),scrollLines:()=>{},scrollToLine:()=>{}};
  this.selection=new TerminalSelection(this.terminal,view.content,()=>view.visible,()=>{
   const r=view.content.getBoundingClientRect();return {left:r.left-view.content.scrollLeft,top:r.top-view.content.scrollTop,width:self.cols*self.cellWidth,height:self.lines.length*self.cellHeight};
  });
 }
 update(){
  this.selection.clear();const rows=[...this.view.content.children].filter(e=>e.className==='history-line');
  const style=getComputedStyle(this.view.content),canvas=document.createElement('canvas'),ctx=canvas.getContext('2d');ctx.font=style.font;
  this.cellWidth=ctx.measureText('M').width||8;this.cellHeight=parseFloat(style.lineHeight)||16;
  const segments=new Intl.Segmenter(undefined,{granularity:'grapheme'});this.cols=Math.max(1,Math.floor(this.view.content.clientWidth/this.cellWidth));
  this.lines=rows.map(row=>{
   const text=row.textContent.replace(/\u200b/g,'');const cells=[],offsets=[];let offset=0;
   for(const {segment} of segments.segment(text)){
    const width=Math.max(1,Math.round(ctx.measureText(segment).width/this.cellWidth));offsets.push(offset);cells.push({getChars:()=>segment,getWidth:()=>width});
    for(let n=1;n<width;n++){offsets.push(offset);cells.push({getChars:()=>'',getWidth:()=>0})}offset+=segment.length;
   }
   offsets.push(offset);this.cols=Math.max(this.cols,cells.length);
   return {row,text,offsets,getCell:x=>cells[x],translateToString:(trim,start=0,end=cells.length)=>cells.slice(start,end).map(c=>c.getChars()||'').join('')};
  });
  this.view.content.append(this.selection.overlay);
 }
 endpoint(line,column){
  const row=this.lines[Math.max(0,Math.min(this.lines.length-1,line))];if(!row)return null;
  let offset=row.offsets[Math.min(column,row.offsets.length-1)]||0;
  const walker=document.createTreeWalker(row.row,NodeFilter.SHOW_TEXT);let node,last;
  while(node=walker.nextNode()){last=node;if(offset<=node.length)return {node,offset};offset-=node.length}
  return last?{node:last,offset:last.length}:{node:row.row,offset:0};
 }
 select(x,y,length){
  const start=this.endpoint(y,x),at=y*this.cols+x+length,end=this.endpoint(Math.floor(at/this.cols),at%this.cols);
  if(!start||!end)return;const range=document.createRange();range.setStart(start.node,start.offset);range.setEnd(end.node,end.offset);const s=window.getSelection();s.removeAllRanges();s.addRange(range);
 }
 key(e){return this.selection.key(e)}
 copy(){return this.selection.copy()}
}
