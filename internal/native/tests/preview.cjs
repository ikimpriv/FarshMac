const assert=require('node:assert/strict');
const h=require('./harness.cjs')();
const settle=()=>new Promise(resolve=>setImmediate(resolve));
async function update(preview){h.context.window.farshmacUpdate({mode:'panels',grid:'frame',preview});for(const done of h.writes.splice(0))if(done)done();await settle();}
(async()=>{
 await update({data:'data:image/png;base64,abc',X:52,Y:7,W:46,H:19});
 const img=h.elements.get('quick-preview');assert.equal(img.hidden,false);assert.equal(img.src,'data:image/png;base64,abc');assert.ok(parseFloat(img.style.width)>0);
 await update({X:52,Y:7,W:46,H:19});assert.equal(img.src,'data:image/png;base64,abc');
 await update({data:'data:image/svg+xml;base64,chart',stretch:true,X:52,Y:5,W:46,H:25});assert.equal(img.style.objectFit,'fill');
 await update({X:52,Y:5,W:46,H:25,occlusions:[{X:60,Y:10,W:20,H:8},{X:62,Y:11,W:20,H:8}]});
 assert.equal(img.hidden,false);
 const mask=decodeURIComponent(img.style.maskImage);
 assert.ok(mask.includes('x="8" y="5" width="20" height="8" fill="black"'));
 assert.ok(mask.includes('x="10" y="6" width="20" height="8" fill="black"'));
 await update({data:'data:image/svg+xml;base64,updated',X:52,Y:5,W:46,H:25});
 assert.equal(img.src,'data:image/svg+xml;base64,updated');assert.equal(img.style.maskImage,'');
 await update(null);assert.equal(img.hidden,true);assert.equal(img.src,'');
 await update({data:'data:image/jpeg;base64,xyz',full:true,X:0,Y:0,W:100,H:40});
 const panelsEl=h.elements.get('panels');
 assert.equal(img.hidden,false);assert.ok(img.classList.contains('full'));assert.ok(panelsEl.classList.contains('image'));
 await update(null);assert.ok(!img.classList.contains('full'));assert.ok(!panelsEl.classList.contains('image'));
 console.log('graphical preview placement, payload reuse, full image view and overlay hiding: OK');
})().catch(e=>{console.error(e);process.exitCode=1});
