'use strict';
const send = m => window.webkit.messageHandlers.farshmac.postMessage(m);
const common = {fontFamily:'Menlo, Monaco, monospace',fontSize:14,lineHeight:1.15,letterSpacing:0,allowProposedApi:true,convertEol:false,cols:120,rows:36};
const panels = new Terminal({...common,disableStdin:true,scrollback:0,theme:{background:'#000080',foreground:'#55ffff'}});
const term = new Terminal({...common,cursorBlink:true,rows:35,scrollback:4096,theme:FarshMacTerminalTheme,macOptionIsMeta:true,macOptionClickForcesSelection:true,rightClickSelectsWord:true});
const panelEl=document.getElementById('panels'), termEl=document.getElementById('terminal');
term.loadAddon(new Unicode11Addon.Unicode11Addon());term.unicode.activeVersion='11';
panels.open(panelEl);term.open(termEl);
const archive=new TerminalArchive(term);
const commandHistory=new CommandHistory(send,()=>term.focus());
const historyView=new HistoryView(send,()=>term.focus(),()=>Math.max(1,term.rows-2));
let documentOpen=false;
let mode='panels', cols=120,rows=36;
// Measure cells once per font change, never from a screen whose resize is pending.
let cellW=8.5,cellH=18,resizeTimer,frameBusy=false;
function measureCells(){
 const screen=panelEl.querySelector('.xterm-screen');
 if(screen&&screen.clientWidth&&screen.clientHeight){cellW=screen.clientWidth/panels.cols;cellH=screen.clientHeight/panels.rows;}
}
function dimensions(){
 const nextCols=Math.max(60,Math.min(300,Math.floor((innerWidth-8)/cellW)));
 const nextRows=Math.max(15,Math.min(120,Math.floor((innerHeight-8)/cellH)));
 if(nextCols===cols&&nextRows===rows)return;
 cols=nextCols;rows=nextRows;
 send({Type:'resize',Cols:cols,Rows:rows});
}
// Coalesce live-resize bursts. Observing the resized body caused layout feedback.
window.addEventListener('resize',()=>{clearTimeout(resizeTimer);resizeTimer=setTimeout(dimensions,60);});
setTimeout(()=>{measureCells();dimensions();},100);
function keyMessage(e){return {Type:'key',Key:e.key,Code:e.code||'',Ctrl:e.ctrlKey,Alt:e.altKey,Meta:e.metaKey,Shift:e.shiftKey};}
function toggle(e){return (e.metaKey||e.ctrlKey)&&!e.altKey&&!e.shiftKey&&e.key.toLowerCase()==='o';}
function copyText(){
 if(commandHistory.visible){commandHistory.copy();return;}
 if(historyView.visible){const value=historyView.copy();if(value)send({Type:'clipboard',Text:value});return;}
 if(mode==='terminal'){if(term.hasSelection())send({Type:'clipboard',Text:term.getSelection()});}
 else send({Type:'copy'});
}
function pasteRequest(){send({Type:'pasteRequest'});}
term.attachCustomKeyEventHandler(e=>{
 if(e.type!=='keydown')return true;
 if(toggle(e)){e.preventDefault();send(keyMessage(e));return false;}
 if(e.metaKey&&e.key==='.'){e.preventDefault();send({Type:'input',Text:'\x03'});return false;}
 if((e.metaKey||e.ctrlKey&&e.shiftKey)&&e.key.toLowerCase()==='c'){e.preventDefault();copyText();return false;}
 if((e.metaKey||e.ctrlKey&&e.shiftKey)&&e.key.toLowerCase()==='v'||e.shiftKey&&e.key==='Insert'){e.preventDefault();pasteRequest();return false;}
 if(e.metaKey&&e.key.toLowerCase()==='a'){e.preventDefault();term.selectAll();return false;}
 return !e.metaKey;
});
term.onData(data=>send({Type:'input',Text:data}));
term.onBinary(data=>send({Type:'inputBinary',Text:btoa(data)}));
termEl.addEventListener('wheel',e=>{
 if(e.deltaY<0&&term.buffer.active.type==='normal'&&term.buffer.active.viewportY===0&&historyView.status.total>term.buffer.normal.baseY&&(term.modes.mouseTrackingMode==='none'||e.shiftKey||e.altKey)){
  e.preventDefault();e.stopImmediatePropagation();historyView.open(Math.max(historyView.status.first,historyView.status.total-term.buffer.normal.baseY-historyView.rows()));
 }
},{capture:true,passive:false});
// A native frame is acknowledged only after parsing and submitting its archive
// batch. Go pauses PTY delivery until the asynchronous disk write completes.
window.farshmacUpdate = state => {
 frameBusy=true;
 const update=async()=>{
  archive.enabled=!(state.history?.error);
  if(state.fontSize&&panels.options.fontSize!==state.fontSize){
   panels.options.fontSize=state.fontSize;term.options.fontSize=state.fontSize;
   document.getElementById('history-lines').style.fontSize=state.fontSize+'px';
   setTimeout(()=>{measureCells();dimensions();},100);
  }
  if(state.cols&&state.rows){
   if(panels.cols!==state.cols||panels.rows!==state.rows)panels.resize(state.cols,state.rows);
   if(term.cols!==state.cols||term.rows!==state.rows-1)archive.resize(state.cols,state.rows-1);
  }
  // Show the requested surface before waiting for terminal parsing / archiving.
  if(mode!==state.mode){mode=state.mode;if(commandHistory.visible)commandHistory.close();if(historyView.visible)historyView.close();document.body.className=mode;if(mode==='terminal')term.focus();else document.activeElement?.blur();}
  const writes=[];
  if(state.terminal){const bytes=Uint8Array.from(atob(state.terminal),c=>c.charCodeAt(0));writes.push(archive.write(bytes));}
  if(state.grid)writes.push(new Promise(resolve=>panels.write(state.grid,resolve)));
  await Promise.all(writes);
  const lines=archive.take();if(lines.length)send({Type:'historyAppend',History:lines});
  historyView.update(state.history,state.historyPage);
  commandHistory.update(state.commands,state.shellReady);
  if(state.grid){
   const img=document.getElementById('quick-preview'), p=state.preview;
   img.hidden=!p;
   if(p){const r=panelEl.querySelector('.xterm-screen').getBoundingClientRect();
    if(p.data)img.src=p.data;
    img.style.left=(r.left+p.X*r.width/panels.cols)+'px';img.style.top=(r.top+p.Y*r.height/panels.rows)+'px';
    img.style.width=(p.W*r.width/panels.cols)+'px';img.style.height=(p.H*r.height/panels.rows)+'px';
   }else{img.src='';}
  }
  documentOpen=!!state.document;
  if(state.clipboard!==undefined)send({Type:'clipboard',Text:state.clipboard});
  document.getElementById('footer').textContent=(state.history?.error?'History: '+state.history.error+' · ':'')+(state.footer||'')+(state.mode==='terminal'?' · ⌘⇧H Commands':'');
 };
 update().catch(error=>send({Type:'terminalError',Text:String(error)})).finally(()=>{frameBusy=false;send({Type:'frameDone',More:!!state.terminal});});
};
function toggleHistory(){if(mode!=='terminal')return false;if(historyView.visible)historyView.close();else historyView.open();return true;}
window.farshmacToggleOutputHistory=toggleHistory;
function toggleCommands(){if(mode!=='terminal')return false;if(commandHistory.visible)commandHistory.close();else {if(historyView.visible)historyView.close();commandHistory.open();}return true;}
window.farshmacToggleHistory=toggleCommands;
document.addEventListener('keydown',e=>{
 if(e.metaKey&&e.shiftKey&&!e.ctrlKey&&!e.altKey&&(e.code==='KeyH'||e.key.toLowerCase()==='h')&&mode==='terminal'){
  e.preventDefault();e.stopImmediatePropagation();toggleCommands();return;
 }
 if(commandHistory.key(e))return;
 if(historyView.visible){
  if(toggle(e)){e.preventDefault();historyView.close();send(keyMessage(e));return;}
  if(historyView.key(e))return;
  if(e.metaKey&&e.key.toLowerCase()==='c'){e.preventDefault();copyText();return;}
  if(e.metaKey&&e.key.toLowerCase()==='a'){e.preventDefault();historyView.selectAll();return;}
  if(e.metaKey&&e.key.toLowerCase()==='v'){e.preventDefault();pasteRequest();return;}
  return;
 }
 if(mode==='terminal')return;
 if(e.isComposing)return;
 if(e.shiftKey&&e.key==='Insert'&&!e.ctrlKey&&!e.metaKey&&!e.altKey){e.preventDefault();pasteRequest();return;}
 if(e.metaKey&&['c','v','x'].includes(e.key.toLowerCase()))return;
 e.preventDefault();send(keyMessage(e));
},true);
document.addEventListener('paste',e=>{
 const data=e.clipboardData.getData('text/plain');e.preventDefault();e.stopImmediatePropagation();
 window.farshmacPaste(data);
},true);
window.farshmacCopy=copyText;
window.farshmacCut=()=>{if(commandHistory.visible){commandHistory.copy(true);return;}if(mode!=='terminal')send({Type:'cut'});};
window.farshmacSelectAll=()=>{if(commandHistory.visible){commandHistory.selectAll();return;}if(historyView.visible)historyView.selectAll();else if(mode==='terminal')term.selectAll();else send({Type:'selectAll'});};
window.farshmacPaste=data=>{if(commandHistory.visible){commandHistory.paste(data);return;}if(mode==='terminal'){if(historyView.visible)historyView.close();term.paste(data);term.focus();}else send({Type:'paste',Text:data});};
function pointer(e,type){const screen=panelEl.querySelector('.xterm-screen'),r=screen.getBoundingClientRect();return {Type:type,X:Math.floor((e.clientX-r.left)/(r.width/panels.cols)),Y:Math.floor((e.clientY-r.top)/(r.height/panels.rows)),Clicks:e.detail||1,Button:e.button===0&&e.ctrlKey?2:e.button,Buttons:e.buttons,Shift:e.shiftKey,Alt:e.altKey,Meta:e.metaKey,Ctrl:e.ctrlKey};}
let dragging=false,dragEvent=null,dragTimer=null;
function flushDrag(){dragTimer=null;if(dragEvent&&dragging){const m=pointer(dragEvent,'drag');send(m);if(m.Y<(documentOpen?1:3)||m.Y>=(documentOpen?rows-1:rows-6))dragTimer=setTimeout(flushDrag,60);else dragEvent=null;}}
panelEl.addEventListener('pointerdown',e=>{
 if(e.button!==0&&e.button!==2)return;
 e.preventDefault();panels.clearSelection();
 if(documentOpen&&(e.button===2||e.ctrlKey))return;
 const m=pointer(e,'click');m.Clicks=1;send(m);
 dragging=true;
 if(dragging)panelEl.setPointerCapture(e.pointerId);
});
panelEl.addEventListener('dblclick',e=>{if(e.button===0&&!e.ctrlKey){e.preventDefault();const m=pointer(e,'click');m.Clicks=2;send(m);}});
panelEl.addEventListener('click',e=>{if(documentOpen&&e.button===0&&e.detail>=3){const m=pointer(e,'click');m.Clicks=3;send(m);}});
panelEl.addEventListener('pointermove',e=>{
 if(!dragging||!(e.buttons&3))return;dragEvent=e;
 if(!dragTimer)dragTimer=setTimeout(flushDrag,25);
});
function endDrag(e){if(dragTimer)clearTimeout(dragTimer);flushDrag();dragging=false;dragEvent=null;if(dragTimer)clearTimeout(dragTimer);dragTimer=null;send({Type:'mouseUp'});if(panelEl.hasPointerCapture(e.pointerId))panelEl.releasePointerCapture(e.pointerId);}
panelEl.addEventListener('pointerup',endDrag);panelEl.addEventListener('pointercancel',endDrag);
window.addEventListener('blur',()=>{dragging=false;dragEvent=null;send({Type:'mouseUp'});});
panelEl.addEventListener('wheel',e=>{e.preventDefault();const m=pointer(e,'wheel');m.Delta=Math.sign(e.deltaY)*Math.max(1,Math.min(12,Math.ceil(Math.abs(e.deltaY)/20)));send(m);},{passive:false});
// Browser navigation/context actions must never replace the file-manager UI.
const menu=document.createElement('div');menu.className='clipboard-menu';menu.hidden=true;document.body.append(menu);
for(const [label,action] of [['Copy',copyText],['Paste',pasteRequest],['Select all',()=>window.farshmacSelectAll()]]){const b=document.createElement('button');b.textContent=label;b.onclick=()=>{menu.hidden=true;action();};menu.append(b);}
document.addEventListener('pointerdown',e=>{if(!menu.contains(e.target))menu.hidden=true;});
document.addEventListener('contextmenu',e=>{
 e.preventDefault();
 if(mode!=='terminal'&&!documentOpen)return; // Far-style right button marks files.
 if(mode==='terminal'&&!historyView.visible&&term.modes.mouseTrackingMode!=='none'&&!e.shiftKey&&!e.altKey)return;
 menu.style.left=Math.min(e.clientX,innerWidth-130)+'px';menu.style.top=Math.min(e.clientY,innerHeight-100)+'px';menu.hidden=false;
});
document.addEventListener('compositionend',e=>{if(mode!=='terminal'&&e.data)send({Type:'paste',Text:e.data});});
