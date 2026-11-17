(function(){
 // PDF dimensions are points (1/72 inch); CSS pixels are independent of Retina
 // backing pixels. Fit modes use the real scroll viewport, not xterm cell sizes.
 function size(w,h,vw,vh,mode,zoom){
  const bw=Math.max(1,w)*96/72,bh=Math.max(1,h)*96/72;
  const scale=mode==='page'?Math.min(Math.max(1,vw-32)/bw,Math.max(1,vh-32)/bh):mode==='width'?Math.max(1,vw-32)/bw:zoom;
  return {width:bw*scale,height:bh*scale,scale};
 }
 window.farshmacPDFSize=size;
 const root=document.getElementById('pdf-view'),viewport=document.getElementById('pdf-scroll'),img=document.getElementById('pdf-page'),label=document.getElementById('pdf-caption'),percent=document.getElementById('pdf-scale');
 let packet=null,displayed=null,mode='page',zoom=1,lastData='',pendingData='',pendingImage=null,generation=0;
 function cancelDecode(){generation++;if(pendingImage)pendingImage.src='';pendingImage=null;pendingData=''}
 function stage(p){
  if(!p.data||p.data===lastData||p.data===pendingData||p.loading)return;
  cancelDecode();const token=generation,data=p.data;pendingData=data;
  const next=new Image();pendingImage=next;next.src=data;
  next.decode().then(()=>{
   if(token!==generation||!packet)return;
   if(!lastData){mode='page';zoom=1}
   lastData=data;displayed=p;img.src=data;pendingImage=null;pendingData='';
   viewport.scrollTop=0;viewport.scrollLeft=0;layout();
  }).catch(()=>{if(token===generation){pendingImage=null;pendingData=''}});
 }
 function sendKey(key){window.webkit.messageHandlers.farshmac.postMessage({Type:'key',Key:key})}
 document.getElementById('pdf-prev').addEventListener('click',()=>sendKey('PageUp'));
 document.getElementById('pdf-next').addEventListener('click',()=>sendKey('PageDown'));
 function layout(){if(!packet)return;const page=displayed||packet;const s=size(page.pageWidth||img.naturalWidth||612,page.pageHeight||img.naturalHeight||792,viewport.clientWidth,viewport.clientHeight,mode,zoom);img.style.width=s.width+'px';img.style.height=s.height+'px';percent.textContent=Math.round(s.scale*100)+'%';return s}
 function fit(value){mode=value;viewport.scrollTop=0;viewport.scrollLeft=0;layout()}
 function changeZoom(factor){const s=layout();if(!s)return;zoom=Math.max(.1,Math.min(8,s.scale*factor));mode='zoom';layout()}
 document.getElementById('pdf-fit').addEventListener('click',()=>fit('page'));
 document.getElementById('pdf-width').addEventListener('click',()=>fit('width'));
 document.getElementById('pdf-in').addEventListener('click',()=>changeZoom(1.25));
 document.getElementById('pdf-out').addEventListener('click',()=>changeZoom(.8));
 document.getElementById('pdf-close').addEventListener('click',()=>window.webkit.messageHandlers.farshmac.postMessage({Type:'key',Key:'Escape'}));
 img.addEventListener('load',layout);viewport.addEventListener('wheel',e=>e.stopPropagation());window.addEventListener('resize',layout);
 window.farshmacPDF={
  update(p){const previous=packet;packet=p&&p.pdf?p:null;root.hidden=!packet;if(!packet){cancelDecode();lastData='';displayed=null;img.src='';return}
   if(previous&&previous.page!==p.page)cancelDecode();
   stage(p);
   document.getElementById('pdf-position').textContent=(p.page||1)+' / '+p.pages+(p.loading?' · Loading…':'');
   document.getElementById('pdf-prev').disabled=(p.page||1)<=1;document.getElementById('pdf-next').disabled=(p.page||1)>=p.pages;
   img.style.opacity='1';
   label.textContent=p.caption||('PDF · page 1 of '+p.pages);layout();
  },
  key(e){if(!packet||e.metaKey||e.ctrlKey||e.altKey)return false;
   if(['PageDown','PageUp','Home','End', '[', ']'].includes(e.key))sendKey(e.key);
   else if(e.key==='+'||e.key==='=')changeZoom(1.25);else if(e.key==='-')changeZoom(.8);else if(e.key==='0')fit('page');else if(e.key.toLowerCase()==='w')fit('width');
   else if(['ArrowDown','ArrowUp','ArrowLeft','ArrowRight','PageDown','PageUp','Home','End',' '].includes(e.key)){
    if(e.key==='Home')viewport.scrollTop=0;else if(e.key==='End')viewport.scrollTop=viewport.scrollHeight;
    else if(e.key==='ArrowLeft'||e.key==='ArrowRight')viewport.scrollLeft+=e.key==='ArrowLeft'?-40:40;
    else viewport.scrollTop+=e.key==='ArrowDown'?40:e.key==='ArrowUp'?-40:e.key==='PageUp'||e.key===' '&&e.shiftKey?-viewport.clientHeight*.85:viewport.clientHeight*.85;
   }else return false;
   e.preventDefault();e.stopImmediatePropagation();return true;
  }
 };
})();
