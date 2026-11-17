(async()=>{
const assert=require('node:assert/strict'),h=require('./harness.cjs')();
const decodes=[];h.context.Image=class {decode(){return new Promise((resolve,reject)=>decodes.push({resolve,reject,image:this}))}};
const flush=async()=>{decodes.shift().resolve();await Promise.resolve()};
const size=h.context.window.farshmacPDFSize,pdf=h.context.window.farshmacPDF;
for(const [w,height] of [[612,792],[792,612],[300,1200]]){
 const s=size(w,height,1000,700,'page',1);assert.ok(s.width<=968.0001&&s.height<=668.0001);assert.ok(Math.abs(s.width/s.height-w/height)<1e-9);
 const width=size(w,height,1000,700,'width',1);assert.equal(width.width,968);assert.ok(Math.abs(width.width/width.height-w/height)<1e-9);
}
const root=h.elements.get('pdf-view'),vp=h.elements.get('pdf-scroll'),img=h.elements.get('pdf-page');vp.clientWidth=1000;vp.clientHeight=700;
const p={pdf:true,data:'data:image/png;base64,pdf',pageWidth:612,pageHeight:792,pages:2,caption:'PDF · page 1 of 2'};
pdf.update(p);await flush();assert.equal(root.hidden,false);assert.ok(parseFloat(img.style.height)<=668.001);
const key=k=>({key:k,preventDefault(){},stopImmediatePropagation(){}});
pdf.key(key('w'));assert.equal(parseFloat(img.style.width),968);pdf.key(key('+'));assert.ok(parseFloat(img.style.width)>968);
vp.scrollTop=120;pdf.update({...p,data:undefined});assert.equal(vp.scrollTop,120);assert.ok(parseFloat(img.style.width)>968);
pdf.key(key('0'));assert.equal(vp.scrollTop,0);assert.ok(parseFloat(img.style.height)<=668.001);assert.equal(pdf.key(key('Escape')),false);
const oldWidth=img.style.width;
for(const k of ['PageDown','PageUp','Home','End','[',']']){pdf.key(key(k));assert.equal(h.sent.at(-1).Key,k)}
vp.scrollTop=100;
pdf.update({...p,page:2,loading:true,data:undefined});assert.equal(img.style.opacity,'1');assert.equal(vp.scrollTop,100);assert.equal(img.src,p.data);
pdf.update({...p,page:2,data:'data:image/png;base64,page2'});assert.equal(img.src,p.data);await flush();assert.equal(img.src,'data:image/png;base64,page2');assert.equal(img.style.width,oldWidth);assert.equal(h.elements.get('pdf-next').disabled,true);assert.equal(h.elements.get('pdf-prev').disabled,false);
h.elements.get('pdf-prev').events.click();assert.equal(h.sent.at(-1).Key,'PageUp');
pdf.update({...p,page:1,data:'data:image/png;base64,stale'});
pdf.update({...p,page:2,data:'data:image/png;base64,new'});
await flush();assert.equal(img.src,'data:image/png;base64,page2');
pdf.update(null);await flush();assert.equal(root.hidden,true);assert.equal(img.src,'');console.log('PDF aspect ratio, fit page/width, zoom, redraw and close: OK');

})().catch(e=>{console.error(e);process.exitCode=1});
