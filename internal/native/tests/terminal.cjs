'use strict';
const assert=require('node:assert/strict');global.self=global;
const {Terminal}=require('../assets/vendor/xterm.js');
const {Unicode11Addon}=require('../assets/vendor/addon-unicode11.js');
const {TerminalArchive,theme}=require('../assets/history.js');
function terminal(cols=80,rows=5){const t=new Terminal({cols,rows,scrollback:4096,allowProposedApi:true,theme});t.loadAddon(new Unicode11Addon());t.unicode.activeVersion='11';return t;}
const write=(t,data)=>new Promise(resolve=>t.write(data,resolve));
const text=line=>line.r.map(run=>run[0]).join('');
(async()=>{
 const t=terminal();const replies=[],binary=[];t.onData(s=>replies.push(s));t.onBinary(s=>binary.push(s));
 await write(t,'\x1b[31mA\x1b[38;5;196mB\x1b[38;2;12;34;56mC\x1b[0m\x1b[1;4mD');
 const row=t.buffer.normal.getLine(0);
 assert(row.getCell(0).isFgPalette());assert.equal(row.getCell(0).getFgColor(),1);
 assert.equal(row.getCell(1).getFgColor(),196);assert(row.getCell(2).isFgRGB());assert.equal(row.getCell(2).getFgColor(),0x0c2238);
 assert(row.getCell(3).isBold());assert(row.getCell(3).isUnderline());
 await write(t,'\x1b[2;3H\x1b[6n');assert(replies.includes('\x1b[2;3R'));
 await write(t,'\x1b[?2004h');t._core.textarea={value:''};t.paste('one\ntwo');assert.equal(replies.at(-1),'\x1b[200~one\rtwo\x1b[201~');
 await write(t,'\x1b[?1000h\x1b[?1006h');
 t._core.coreMouseService.triggerMouseEvent({col:4,row:2,x:40,y:20,button:0,action:1,ctrl:false,alt:false,shift:false});
 assert.equal(replies.at(-1),'\x1b[<0;5;3M');
 t.resize(200,100);await write(t,'\x1b[?1006l');
 t._core.coreMouseService.triggerMouseEvent({col:140,row:2,x:40,y:20,button:0,action:1,ctrl:false,alt:false,shift:false});
 assert.equal(binary.at(-1).charCodeAt(4),173);
 await write(t,'\x1b[?1000l\x1b[0m\x1b[H😀界');assert.equal(t.buffer.normal.getLine(0).getCell(0).getWidth(),2);assert.equal(t.buffer.normal.getLine(0).getCell(2).getWidth(),2);
 await write(t,'\x1b[H\x1b[1;31mB');
 const styleArchive=new TerminalArchive(t);assert.equal(styleArchive.line(t.buffer.normal.getLine(0)).r[0][1],0xff0000,'bold palette colors must match live bright colors');styleArchive.dispose();
 t.dispose();
 const live=terminal(40,5),archive=new TerminalArchive(live);
 const total=30000;let saved=0;
 for(let first=0;first<total;first+=500){
  const output=Array.from({length:500},(_,i)=>`\x1b[38;2;12;34;56mline-${first+i}\r\n`).join('');
  await archive.write(Buffer.from(output));
  for(const line of archive.take()){assert.equal(text(line),`line-${saved++}`);assert.equal(line.r[0][1],0x0c2238);}
  assert(live.buffer.normal.length<=4101,'resident scrollback must remain bounded');
 }
 assert.equal(saved,total-4);
 archive.resize(40,8);await archive.write(Buffer.from('tail-0\r\ntail-1\r\n'));
 assert.equal(archive.take().length,0,'growing the window must not archive old rows twice');
 archive.resize(40,5);let next=archive.take();for(const line of next)assert.equal(text(line),`line-${saved++}`);
 await archive.write(Buffer.from('\x1b[?1049h'+('alternate\r\n'.repeat(10000))+'\x1b[?1049l'));
 assert.equal(archive.take().length,0,'alternate screen must not pollute normal history');
 await archive.write(Buffer.from('before-clear\r\n'.repeat(20)+'\x1b[3Jafter-clear\r\n'.repeat(1)));
 assert(archive.take().some(line=>text(line)==='before-clear'),'erase-scrollback must not erase the disk archive batch');
 archive.dispose();live.dispose();
 console.log('256 colors, truecolor, SGR, DSR, bracketed paste, binary/SGR mouse, Unicode, 30k archived lines, resize and alternate screen: OK');
})().catch(e=>{console.error(e);process.exitCode=1});
