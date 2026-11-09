const assert=require('node:assert/strict');
const {context,sent,writes}=require('./harness.cjs')();
const settle=()=>new Promise(resolve=>setImmediate(resolve));
(async()=>{
 context.window.farshmacUpdate({mode:'terminal',terminal:Buffer.from('startup output').toString('base64')});
 assert.equal(context.document.body.className,'terminal','show terminal before waiting for output parsing');
 assert.equal(sent.length,0,'still apply renderer backpressure');
 writes.shift()();await settle();
 assert.equal(sent.at(-1).Type,'frameDone');
 context.window.farshmacUpdate({mode:'panels',grid:'panels'});
 assert.equal(context.document.body.className,'panels','return to panels immediately');
 writes.shift()();await settle();
 console.log('terminal mode changes precede asynchronous output parsing: OK');
})().catch(e=>{console.error(e);process.exitCode=1});
