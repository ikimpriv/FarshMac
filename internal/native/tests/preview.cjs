const assert=require('node:assert/strict');
const h=require('./harness.cjs')();
const settle=()=>new Promise(resolve=>setImmediate(resolve));
async function update(preview){h.context.window.farshmacUpdate({mode:'panels',grid:'frame',preview});for(const done of h.writes.splice(0))if(done)done();await settle();}
(async()=>{
 await update({data:'data:image/png;base64,abc',X:52,Y:7,W:46,H:19});
 const img=h.elements.get('quick-preview');assert.equal(img.hidden,false);assert.equal(img.src,'data:image/png;base64,abc');assert.ok(parseFloat(img.style.width)>0);
 await update({X:52,Y:7,W:46,H:19});assert.equal(img.src,'data:image/png;base64,abc');
 await update(null);assert.equal(img.hidden,true);assert.equal(img.src,'');
 console.log('graphical preview placement, payload reuse and overlay hiding: OK');
})().catch(e=>{console.error(e);process.exitCode=1});
