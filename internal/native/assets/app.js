'use strict';
const send = m => window.webkit.messageHandlers.farshmac.postMessage(m);
const common = {fontFamily:'Menlo, Monaco, monospace',fontSize:14,lineHeight:1.15,letterSpacing:0,allowProposedApi:true,convertEol:false,cols:120,rows:36};
const panels = new Terminal({...common,disableStdin:true,scrollback:0,theme:{background:'#000080',foreground:'#55ffff'}});
let term = new Terminal({...common,cursorBlink:true,rows:35,scrollback:4096,theme:FarshMacTerminalTheme,macOptionIsMeta:true,macOptionClickForcesSelection:true,rightClickSelectsWord:true});
const panelEl=document.getElementById('panels'), termEl=document.getElementById('terminal');
term.loadAddon(new Unicode11Addon.Unicode11Addon());term.unicode.activeVersion='11';
panels.open(panelEl);term.open(termEl);
let archive=new TerminalArchive(term);
let terminalContext="local";
const terminalContexts=new Map([["local",{term,archive,element:null}]]);
const commandHistory=new CommandHistory(send,()=>term.focus());
const historyView=new HistoryView(m=>send({...m,Context:terminalContext}),()=>term.focus(),()=>Math.max(1,term.rows-2));
let documentOpen=false;
let mode='panels', cols=120,rows=36;
// Measure cells once per font change, never from a screen whose resize is pending.
let cellW=8.5,cellH=18,resizeTimer,frameBusy=false;
let minSize='';
function measureCells(){
 const screen=panelEl.querySelector('.xterm-screen');
 if(screen&&screen.clientWidth&&screen.clientHeight){cellW=screen.clientWidth/panels.cols;cellH=screen.clientHeight/panels.rows;}
 // The native window must fit the 60x15 cell minimum at the current font size.
 const W=Math.ceil(60*cellW+8-1e-6),H=Math.ceil(15*cellH+8-1e-6);
 if(W+'x'+H!==minSize){minSize=W+'x'+H;send({Type:'minSize',W,H});}
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
function keyMessage(e){
 let key=e.key;
 // Cmd/Ctrl shortcuts in a non-Latin layout report the Latin letter of the key.
 if((e.metaKey||e.ctrlKey)&&[...(key||'')].length===1&&!/^[\x00-\x7f]$/.test(key)&&/^Key[A-Z]$/.test(e.code||'')){key=shortcutLetter(e);if(e.shiftKey)key=key.toUpperCase();}
 return {Type:'key',Key:key,Code:e.code||'',Ctrl:e.ctrlKey,Alt:e.altKey,Meta:e.metaKey,Shift:e.shiftKey};
}
function toggle(e){return (e.metaKey||e.ctrlKey)&&!e.altKey&&!e.shiftKey&&shortcutLetter(e)==='o';}
function copyText(){
 if(mode==='panels'&&window.farshmacMarkdown?.copy())return;
 if(mode==='traffic'){window.farshmacTraffic?.copy();return;}
 if(mode==='metrics'){window.farshmacMetrics?.copy();return;}
 if(commandHistory.visible){commandHistory.copy();return;}
 if(historyView.visible){const value=historyView.copy();if(value)send({Type:'clipboard',Text:value});return;}
 if(mode==='terminal'){const text=term.outputSelection?.copy()||term.getSelection();if(text)send({Type:'clipboard',Text:text});}
 else send({Type:'copy'});
}
function pasteRequest(){send({Type:'pasteRequest'});}
function bindTerminal(target,key="local"){
 target.outputSelection=new TerminalSelection(target,target.element||termEl,()=>mode==='terminal'&&term===target&&!historyView.visible&&!commandHistory.visible);
target.attachCustomKeyEventHandler(e=>{
 if(e.type!=='keydown')return true;
 if(target.outputSelection.key(e))return false;
 if(toggle(e)){e.preventDefault();send(keyMessage(e));return false;}
 if(e.metaKey&&e.key==='.'){e.preventDefault();send({Type:'input',Text:'\x03'});return false;}
 if(e.metaKey&&!e.ctrlKey&&!e.altKey&&['=','+','-','0'].includes(e.key)&&!/^Numpad/.test(e.code||'')){e.preventDefault();send(keyMessage(e));return false;}
 if((e.metaKey||e.ctrlKey&&e.shiftKey)&&shortcutLetter(e)==='c'){e.preventDefault();copyText();return false;}
 if((e.metaKey||e.ctrlKey&&e.shiftKey)&&shortcutLetter(e)==='v'||e.shiftKey&&e.key==='Insert'){e.preventDefault();pasteRequest();return false;}
 if(e.metaKey&&shortcutLetter(e)==='a'){e.preventDefault();target.outputSelection.clear();target.selectAll();return false;}
 return !e.metaKey;
});
target.onData(data=>{target.outputSelection.clear();send({Type:'input',Text:data,Context:key});});
target.onBinary(data=>send({Type:'inputBinary',Text:btoa(data),Context:key}));
}
bindTerminal(term);

function terminalFor(key){
 let session=terminalContexts.get(key);
 if(session)return session;
 const target=new Terminal({...common,cols,rows:rows-1,cursorBlink:true,scrollback:4096,theme:FarshMacTerminalTheme,macOptionIsMeta:true,macOptionClickForcesSelection:true,rightClickSelectsWord:true});
 target.loadAddon(new Unicode11Addon.Unicode11Addon());target.unicode.activeVersion='11';
 const element=document.createElement('div');element.style.width='100%';element.style.height='100%';element.style.display='none';termEl.append(element);
 target.open(element);bindTerminal(target,key);
 session={term:target,archive:new TerminalArchive(target),element};terminalContexts.set(key,session);return session;
}
function selectTerminal(key){
 if(!key||key===terminalContext)return;
 const previous=terminalContexts.get(terminalContext);
 if(previous.element)previous.element.style.display='none';
 else if(previous.term.element)previous.term.element.style.display='none';
 const session=terminalFor(key);
 term=session.term;archive=session.archive;terminalContext=key;
 if(session.element)session.element.style.display='';
 else if(term.element)term.element.style.display='';
 if(historyView.visible)historyView.close();
 if(commandHistory.visible)commandHistory.close();
 if(mode==='terminal')term.focus();
}
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
  selectTerminal(state.terminalContext);
  if(state.fontSize)term.options.fontSize=state.fontSize;
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
  if(state.metrics)window.farshmacMetrics?.update(state.metrics);
  if(state.traffic)window.farshmacTraffic?.update(state.traffic);
  if(mode==='metrics')window.farshmacMetrics?.status(state.footer);
  const writes=[];
  if(state.terminal){const bytes=Uint8Array.from(atob(state.terminal),c=>c.charCodeAt(0));writes.push(archive.write(bytes));}
  if(state.grid)writes.push(new Promise(resolve=>panels.write(state.grid,resolve)));
  await Promise.all(writes);
  const lines=archive.take();if(lines.length)send({Type:'historyAppend',History:lines,Context:terminalContext});
  for(const packet of state.backgroundTerminals||[]){
   const session=terminalFor(packet.Context);
   await session.archive.write(Uint8Array.from(atob(packet.Data),c=>c.charCodeAt(0)));
   const rows=session.archive.take();if(rows.length)send({Type:'historyAppend',History:rows,Context:packet.Context});
  }
  if(state.mode&&state.mode!=='panels'){window.farshmacMarkdown?.update(null);window.farshmacPDF?.update(null);}
  historyView.update(state.history,state.historyPage);
  commandHistory.update(state.commands,state.shellReady,state.commandsError);
  if(state.grid){
   window.farshmacMarkdown?.update(state.markdown,panelEl.querySelector('.xterm-screen').getBoundingClientRect(),panels.cols,panels.rows);
   const img=document.getElementById('quick-preview'), p=state.preview;
   window.farshmacPDF?.update(p);
   img.hidden=!p||!!p.pdf;
   // The F3 image view: the whole grid is black and the picture is centred over it.
   const full=!!(p&&p.full);img.classList.toggle('full',full);panelEl.classList.toggle('image',full);
   if(p&&!p.pdf){const r=panelEl.querySelector('.xterm-screen').getBoundingClientRect();
    if(p.data)img.src=p.data;
    img.style.objectFit=p.stretch?'fill':'';
    // Mask only modal rectangles (including shadows), not the entire chart.
    // Black rectangles combine as a union, including overlapping dialogs.
    const holes=(p.occlusions||[]).map(o=>`<rect x="${o.X-p.X}" y="${o.Y-p.Y}" width="${o.W}" height="${o.H}" fill="black"/>`).join('');
    const mask=holes?'url("data:image/svg+xml,'+encodeURIComponent(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${p.W} ${p.H}" preserveAspectRatio="none"><mask id="m" maskUnits="userSpaceOnUse" x="0" y="0" width="${p.W}" height="${p.H}"><rect width="100%" height="100%" fill="white"/>${holes}</mask><rect width="100%" height="100%" fill="white" mask="url(#m)"/></svg>`)+'")':'';
    img.style.maskImage=img.style.webkitMaskImage=mask;
    img.style.maskSize=img.style.webkitMaskSize='100% 100%';
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
// Held modifiers drive the key bar rows (Shift/Ctrl/Alt/Cmd) like Far Manager.
// Only changes are reported; losing focus releases everything.
let heldModifiers='';
function reportModifiers(e){
 const m={Type:'modifiers',Shift:!!e.shiftKey,Ctrl:!!e.ctrlKey,Alt:!!e.altKey,Meta:!!e.metaKey};
 const state=[m.Shift,m.Ctrl,m.Alt,m.Meta].join();
 if(state===heldModifiers)return;heldModifiers=state;send(m);
}
document.addEventListener('keyup',reportModifiers,true);
document.addEventListener('visibilitychange',()=>{if(document.hidden)reportModifiers({});});
document.addEventListener('keydown',e=>{
 reportModifiers(e);
 if(mode==='panels'&&window.farshmacMarkdown?.key(e))return;
 if(mode==='panels'&&window.farshmacPDF?.key(e))return;
 if(mode==='traffic'){
  if(e.metaKey&&['q','w'].includes(e.key.toLowerCase())||e.altKey&&e.key==='F9'){e.preventDefault();send(keyMessage(e));}
  return;
 }
 if(mode==='metrics'){
  if(e.key==='Escape'&&window.farshmacMetrics?.dialogOpen?.())return;
  if(e.key==='Escape'||e.metaKey&&['q','w'].includes(e.key.toLowerCase())||e.altKey&&e.key==='F9'){e.preventDefault();send(keyMessage(e));}
  return;
 }
 if(e.metaKey&&e.shiftKey&&!e.ctrlKey&&!e.altKey&&(e.code==='KeyH'||e.key.toLowerCase()==='h')&&mode==='terminal'){
  e.preventDefault();e.stopImmediatePropagation();toggleCommands();return;
 }
 if(commandHistory.visible&&toggle(e)){e.preventDefault();commandHistory.close();send(keyMessage(e));return;}
 if(commandHistory.key(e))return;
 if(historyView.visible){
  if(toggle(e)){e.preventDefault();historyView.close();send(keyMessage(e));return;}
  if(historyView.key(e))return;
  if(e.metaKey&&shortcutLetter(e)==='c'){e.preventDefault();copyText();return;}
  if(e.metaKey&&shortcutLetter(e)==='a'){e.preventDefault();historyView.selectAll();return;}
  if(e.metaKey&&shortcutLetter(e)==='v'){e.preventDefault();pasteRequest();return;}
  return;
 }
 if(mode==='terminal')return;
 if(e.isComposing)return;
 if(e.shiftKey&&e.key==='Insert'&&!e.ctrlKey&&!e.metaKey&&!e.altKey){e.preventDefault();pasteRequest();return;}
 if(e.metaKey&&['c','v','x'].includes(shortcutLetter(e)))return;
 e.preventDefault();send(keyMessage(e));
},true);
document.addEventListener('paste',e=>{
 const data=e.clipboardData.getData('text/plain');e.preventDefault();e.stopImmediatePropagation();
 window.farshmacPaste(data);
},true);
window.farshmacCopy=copyText;
window.farshmacCut=()=>{if(mode==='traffic'){window.farshmacTraffic?.copy(true);return;}if(mode==='metrics'){window.farshmacMetrics?.copy(true);return;}if(commandHistory.visible){commandHistory.copy(true);return;}if(mode!=='terminal')send({Type:'cut'});};
window.farshmacSelectAll=()=>{if(mode==='traffic'){window.farshmacTraffic?.selectAll();return;}if(mode==='metrics'){window.farshmacMetrics?.selectAll();return;}if(commandHistory.visible){commandHistory.selectAll();return;}if(historyView.visible)historyView.selectAll();else if(mode==='terminal'){term.outputSelection.clear();term.selectAll();}else send({Type:'selectAll'});};
window.farshmacPaste=data=>{if(mode==='traffic'){window.farshmacTraffic?.paste(data);return;}if(mode==='metrics'){window.farshmacMetrics?.paste(data);return;}if(commandHistory.visible){commandHistory.paste(data);return;}if(mode==='terminal'){if(historyView.visible)historyView.close();term.paste(data);term.focus();}else send({Type:'paste',Text:data});};
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
window.addEventListener('blur',()=>{dragging=false;dragEvent=null;send({Type:'mouseUp'});reportModifiers({});});
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
