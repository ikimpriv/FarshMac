const assert=require('node:assert/strict');
const {context,sent,timers,listeners,writes,flushTimers}=require('./harness.cjs')();
const settle=()=>new Promise(resolve=>setImmediate(resolve));
(async()=>{
 flushTimers();sent.length=0;
 for(let i=0;i<1000;i++){context.innerWidth=800+i%200;listeners.resize()}
 assert.equal(timers.size,1,'resize storms must coalesce');flushTimers();assert.equal(sent.filter(m=>m.Type==='resize').length,1);
 const r=sent.at(-1);sent.length=0;context.window.farshmacUpdate({mode:'panels',cols:r.Cols,rows:r.Rows,grid:'frame',terminal:Buffer.from('output').toString('base64')});
 assert.equal(sent.length,0,'must not acknowledge before xterm parses');writes.shift()();await settle();assert.equal(sent.length,0);writes.shift()();await settle();assert.equal(sent[0].Type,'frameDone');
 sent.length=0;listeners.resize();flushTimers();assert.equal(sent.length,0,'stable geometry must not cause feedback resize');
 console.log('resize coalescing, stable geometry and renderer backpressure: OK');
})().catch(e=>{console.error(e);process.exitCode=1});
