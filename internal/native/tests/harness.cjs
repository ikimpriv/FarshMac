'use strict';
const fs=require('node:fs'),vm=require('node:vm'),path=require('node:path');
module.exports=function harness(){
 const sent=[],timers=new Map(),listeners={},docListeners={},docListenerOptions={},writes=[],terminals=[],elements=new Map();let serial=0;
 const screen={clientWidth:1020,clientHeight:648,getBoundingClientRect(){return {left:0,top:0,width:this.clientWidth,height:this.clientHeight}}};
 class Element{
  constructor(){this.events={};this.style={};this.children=[];this.dataset={};this.classList={add(){},remove(){}};this.textContent='';}
  querySelector(){return screen} addEventListener(name,fn){this.events[name]=fn} append(...c){this.children.push(...c)} replaceChildren(){this.children=[]}
  contains(e){return this===e||this.children.includes(e)} focus(){} blur(){} setPointerCapture(){this.captured=true} hasPointerCapture(){return this.captured} releasePointerCapture(){this.captured=false}
 }
 class Terminal{
  constructor(options){this.options=options;this.cols=options.cols;this.rows=options.rows;this.unicode={};this.resizeCount=0;this.selection='';this.buffer={active:{type:'normal',viewportY:1},normal:{baseY:0}};this.modes={mouseTrackingMode:'none'};terminals.push(this)}
  open(){} loadAddon(){} resize(c,r){this.cols=c;this.rows=r;this.resizeCount++} attachCustomKeyEventHandler(fn){this.key=fn} onData(fn){this.data=fn} onBinary(fn){this.binary=fn} focus(){}
  write(data,done){writes.push(done)} getSelection(){return this.selection} hasSelection(){return !!this.selection} clearSelection(){this.selection=''} selectAll(){this.selectedAll=true} paste(data){this.pasted=data}
 }
 class TerminalArchive{constructor(term){this.term=term}resize(c,r){this.term.resize(c,r)}write(data){return new Promise(resolve=>this.term.write(data,resolve))}take(){return []}}
 const selection={toString(){return 'selected history'},removeAllRanges(){},addRange(){}};
 const document={getElementById(id){if(!elements.has(id))elements.set(id,new Element());return elements.get(id)},body:new Element(),addEventListener(n,fn,options){docListeners[n]=fn;docListenerOptions[n]=options},activeElement:null,createElement(){return new Element()},createRange(){return {selectNodeContents(){}}}};
 const context={Terminal,TerminalArchive,FarshMacTerminalTheme:{},Unicode11Addon:{Unicode11Addon:class{}},Uint8Array,Math,Promise,atob:s=>Buffer.from(s,'base64').toString('binary'),btoa:s=>Buffer.from(s,'binary').toString('base64'),setTimeout(fn){const id=++serial;timers.set(id,fn);return id},clearTimeout(id){timers.delete(id)},innerWidth:1028,innerHeight:656,document};
 context.window={webkit:{messageHandlers:{farshmac:{postMessage(m){sent.push(m)}}}},addEventListener(n,fn){listeners[n]=fn},getSelection(){return selection}};
 vm.createContext(context);
 for(const name of ['history_view.js','commands.js','app.js'])vm.runInContext(fs.readFileSync(path.join(__dirname,'../assets',name),'utf8'),context);
 return {context,sent,timers,listeners,docListeners,docListenerOptions,writes,terminals,elements,flushTimers(){const copy=[...timers.values()];timers.clear();copy.forEach(fn=>fn())}};
};
