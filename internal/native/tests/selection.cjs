'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
class Element{
 constructor(){this.children=[];this.style={};this.events={}}
 append(x){this.children.push(x)}replaceChildren(){this.children=[]}addEventListener(k,f){this.events[k]=f}contains(){return true}
 getBoundingClientRect(){return {left:0,top:0,width:200,height:60}}
 querySelector(){return this}
}
const context={document:{createElement:()=>new Element(),addEventListener(){}},setTimeout:()=>1,clearTimeout(){},console};vm.createContext(context);
vm.runInContext(fs.readFileSync(require('node:path').join(__dirname,'../assets/terminal_selection.js'),'utf8'),context);
const Controller=vm.runInContext('TerminalSelection',context);
function fixture(lines=['hello world         ','second row          ','third row           ']){
 const active={type:'normal',viewportY:0,baseY:0,cursorX:11,cursorY:0,length:lines.length,getLine:y=>({getCell:x=>({getChars:()=>lines[y]?.[x]||'',getWidth:()=>1}),translateToString:(trim,start,end)=>lines[y]?.slice(start,end)||''})};
 let selected='';const t={cols:20,rows:3,buffer:{active},modes:{mouseTrackingMode:'none'},clearSelection(){selected=''},getSelection:()=>selected,hasSelection:()=>!!selected,focus(){},select(x,y,n){selected=lines.join('').slice(y*20+x,y*20+x+n)},scrollToLine(y){this.scroll=y},scrollLines(){}};
 const el=new Element(),c=new Controller(t,el,()=>true);return {t,c,el,active};
}
function event(key,extra={}){return {key,shiftKey:true,preventDefault(){this.prevented=true},stopImmediatePropagation(){this.stopped=true},...extra}}
let {t,c,el,active}=fixture();
assert(c.key(event('ArrowLeft')));assert.equal(c.copy(),'d');c.key(event('ArrowLeft'));assert.equal(c.copy(),'ld');c.key(event('ArrowRight'));assert.equal(c.copy(),'d');
c.clear();c.key(event('ArrowLeft',{altKey:true}));assert.equal(c.copy(),'world');c.key(event('ArrowLeft',{metaKey:true}));assert.equal(c.copy(),'hello world');
c.key(event('ArrowRight',{metaKey:true}));assert.equal(c.copy(),'');
c.clear();assert.equal(c.key(event('c',{shiftKey:false,ctrlKey:true})),false,'Ctrl+C must reach shell');
const mouse=(x,y,more={})=>event('',{button:0,shiftKey:false,clientX:x,clientY:y,detail:1,...more});
c.down(mouse(10,5));c.move(mouse(40,45));c.up(mouse(40,45));assert(c.copy().startsWith('ello world'));
c.down(mouse(10,5,{altKey:true}));c.move(mouse(40,45,{altKey:true}));c.up(mouse(40,45));assert.equal(c.copy(),'ell\neco\nhir');assert.equal(c.overlay.children.length,3);
c.down(mouse(40,45,{altKey:true}));c.move(mouse(10,5,{altKey:true}));c.up(mouse(10,5));assert.equal(c.copy(),'ell\neco\nhir','reverse rectangle');
c.clear();t.modes.mouseTrackingMode='sgr';const ordinary=mouse(20,5);c.down(ordinary);assert(!ordinary.prevented,'application mouse must be preserved');
const forced=mouse(20,5,{shiftKey:true});c.down(forced);assert(forced.prevented);c.move(mouse(50,5));c.up(mouse(50,5));assert.equal(c.copy(),'llo');
c.clear();c.down(mouse(0,0,{altKey:true}));assert(c.drag,'rectangle overrides application mouse');
c.clear();active.getLine=y=>({getCell:x=>({getChars:()=>x===0?'界':x===1?'':'x',getWidth:()=>x===0?2:x===1?0:1})});active.cursorX=2;
c.key(event('ArrowLeft'));assert.equal(c.end.x,0,'selection must not split a wide character');
console.log('Terminal selection: characters, words, line bounds, rectangles, mouse override, reverse drag and wide cells: OK');
// Exercise the disk-history adapter with styled-row text and a bounded page.
context.getComputedStyle=()=>({font:'14px Menlo',lineHeight:'20px'});
context.document.createElement=tag=>tag==='canvas'?{getContext:()=>({measureText:s=>({width:[...s].length*10})})}:new Element();
context.window={getSelection:()=>({removeAllRanges(){},addRange(r){context.lastRange=r}})};
context.NodeFilter={SHOW_TEXT:4};context.document.createRange=()=>({setStart(node,offset){this.start={node,offset}},setEnd(node,offset){this.end={node,offset}}});
context.document.createTreeWalker=row=>{let used=false;return {nextNode(){if(used)return null;used=true;return {length:row.textContent.length,text:row.textContent}}}};
vm.runInContext(fs.readFileSync(require('node:path').join(__dirname,'../assets/history_selection.js'),'utf8'),context);
const History=vm.runInContext('HistorySelection',context);
const content=new Element();content.clientWidth=200;content.scrollTop=content.scrollLeft=0;
content.children=['alpha bravo','delta echo'].map(text=>({className:'history-line',textContent:text}));
const history=new History({visible:true,content,el:{focus(){}},copyDOM:()=>''});history.update();
assert.equal(history.lines.length,2);assert.equal(history.cols,20);
history.key(event('ArrowRight',{altKey:true}));assert.equal(context.lastRange.end.offset,5);
history.selection.anchor={x:1,y:0};history.selection.end={x:4,y:1};history.selection.rect=true;
assert.equal(history.copy(),'lph\nelt');
console.log('Disk-history page: keyboard word selection and rectangular copying: OK');
