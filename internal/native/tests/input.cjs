const assert=require('node:assert/strict');
const {context,sent,terminals,elements,flushTimers,docListeners,docListenerOptions}=require('./harness.cjs')();
const term=terminals[1],panels=elements.get('panels');
const event=extra=>({clientX:25,clientY:70,button:0,buttons:1,pointerId:1,detail:1,preventDefault(){this.prevented=true},stopImmediatePropagation(){},...extra});
const settle=()=>new Promise(resolve=>setImmediate(resolve));
(async()=>{
 flushTimers();sent.length=0;
 for(const extra of [{key:'F1',metaKey:true},{key:'F2',altKey:true},{key:'|',metaKey:true,shiftKey:true},{key:'=',metaKey:true},{key:'>',code:'Period',metaKey:true,shiftKey:true}]) {
  docListeners.keydown(event(extra));assert.equal(sent.at(-1).Type,'key');assert.equal(sent.at(-1).Key,extra.key);
 }
 // Non-Latin layouts: Cmd/Ctrl letters map to the physical Latin key; Dvorak keeps e.key.
 docListeners.keydown(event({key:'щ',code:'KeyO',metaKey:true}));assert.equal(sent.at(-1).Key,'o');
 docListeners.keydown(event({key:'Г',code:'KeyU',metaKey:true,shiftKey:true}));assert.equal(sent.at(-1).Key,'U');
 docListeners.keydown(event({key:'r',code:'KeyO',metaKey:true}));assert.equal(sent.at(-1).Key,'r');
 docListeners.keydown(event({key:'щ',code:'KeyO'}));assert.equal(sent.at(-1).Key,'щ','plain typing keeps the layout character');
 docListeners.keydown(event({key:'ç',code:'KeyC',altKey:true}));
 assert.equal(sent.at(-1).Key,'ç');assert.equal(sent.at(-1).Code,'KeyC');assert.equal(sent.at(-1).Alt,true);
 panels.events.pointerdown(event({button:2,buttons:2}));assert.equal(sent.at(-1).Button,2);
 panels.events.pointermove(event({button:2,buttons:2,clientY:90}));panels.events.pointermove(event({button:2,buttons:2,clientY:100}));
 assert.equal(sent.filter(m=>m.Type==='drag').length,0);flushTimers();assert.equal(sent.filter(m=>m.Type==='drag').length,1);
 panels.events.pointerup(event({buttons:0}));assert.equal(sent.at(-1).Type,'mouseUp');
 panels.events.pointerdown(event({ctrlKey:true}));assert.equal(sent.at(-1).Button,2,'macOS Control-click must mark like right click');
 panels.events.pointerup(event({buttons:0}));
 panels.events.wheel(event({deltaY:80}));assert.equal(sent.at(-1).Type,'wheel');assert.equal(sent.at(-1).Delta,4);
 context.window.farshmacUpdate({mode:'terminal',shellReady:true,history:{first:0,total:100}});await settle();
 term.key(event({type:'keydown',ctrlKey:true,key:'o'}));assert.equal(sent.at(-1).Key,'o');assert.equal(sent.at(-1).Ctrl,true);
 assert.equal(term.key(event({type:'keydown',metaKey:true,key:'щ',code:'KeyO'})),false);assert.equal(sent.at(-1).Key,'o');assert.equal(sent.at(-1).Meta,true);
 term.selection='hello';term.key(event({type:'keydown',metaKey:true,key:'c'}));assert.equal(sent.at(-1).Text,'hello');
 term.key(event({type:'keydown',metaKey:true,key:'v'}));assert.equal(sent.at(-1).Type,'pasteRequest');
 context.window.farshmacPaste('one\ntwo');assert.equal(term.pasted,'one\ntwo');
 term.binary('\x1b[M\xff\x80\x21');assert.equal(sent.at(-1).Type,'inputBinary');assert.deepEqual(Buffer.from(sent.at(-1).Text,'base64'),Buffer.from([27,91,77,255,128,33]));
 const historyKey=event({type:'keydown',metaKey:true,shiftKey:true,key:'H',code:'KeyH',stopImmediatePropagation(){this.stopped=true}});
 // Simulate capture -> xterm target -> document bubble, catching open-then-close.
 if(docListenerOptions.keydown===true)docListeners.keydown(historyKey);
 if(!historyKey.stopped)term.key(historyKey);
 if(docListenerOptions.keydown!==true)docListeners.keydown(historyKey);
 assert.equal(historyKey.stopped,true);assert.equal(sent.at(-1).Type,'commandHistory');
 docListeners.keydown(event({key:'Escape'}));
 context.window.farshmacToggleOutputHistory();
 const request=sent.at(-1).Request;
 context.window.farshmacUpdate({mode:'terminal',history:{first:0,total:100},historyPage:{request,start:0,total:100,first:0,lines:[{c:80,w:false,r:[['<script> literal',0xff0000,0,1]]}]}});await settle();
 assert.equal(elements.get('history-lines').children[0].children[0].textContent,'<script> literal');
 docListeners.keydown(event({key:'Escape'}));
 const reads=sent.filter(m=>m.Type==='historyRead').length;
 context.window.farshmacToggleOutputHistory();assert.equal(sent.filter(m=>m.Type==='historyRead').length,reads+1,'native menu must reopen history');
 docListeners.keydown(event({key:'Escape'}));
 console.log('mouse buttons, coalesced drag, wheel, clipboard, binary mouse and paged history: OK');
})().catch(e=>{console.error(e);process.exitCode=1});
