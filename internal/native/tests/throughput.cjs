'use strict';
const assert=require('node:assert/strict');global.self=global;
const {Terminal}=require('../assets/vendor/xterm.js');
const {TerminalArchive}=require(process.argv[2]||'../assets/history.js');
(async()=>{
 const t=new Terminal({cols:120,rows:35,scrollback:4096,allowProposedApi:true});
 const a=new TerminalArchive(t);let count=0,writes=0;
 const write=t.write.bind(t);t.write=(data,done)=>{writes++;write(data,done)};
 const started=performance.now(),total=30000;
 const bytes=Buffer.from(Array.from({length:total},(_,i)=>`\x1b[32mline-${i}\x1b[0m\r\n`).join(''));
 for(let i=0;i<bytes.length;i+=16384){await a.write(bytes.subarray(i,i+16384));for(const line of a.take()){assert.equal(line.r.map(x=>x[0]).join(''),`line-${count++}`)}}
 assert.equal(count,total-34);assert(t.buffer.normal.length<=4131);
 console.log(JSON.stringify({milliseconds:Math.round(performance.now()-started),bytes:bytes.length,writeCalls:writes,archived:count}));
 // Worst-case stream: each byte can scroll a row. No rows may be lost.
 await a.write(Buffer.from('\n'.repeat(20000)));assert.equal(a.take().length,20000);
 a.dispose();t.dispose();
})().catch(e=>{console.error(e);process.exitCode=1});
