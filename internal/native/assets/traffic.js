/* A bounded, offline traffic instrument. Network control lives in PodToast. */
(function(global){
 'use strict';
 const human=(v,u)=>global.FarMetricsMath.human(v,u);
 class History {
  constructor(){this.points=[];this.previous=null;this.selected=0;this.flow=[];this.flowPrevious=null;}
  select(id){if(this.selected!==id){this.selected=id;this.flow=[];this.flowPrevious=null;}}
  update(p){
   if(this.previous&&p.time-this.previous.time<800)return;
   const s=p.state,open=(s.connections||[]).filter(c=>!c.closed).length;
   const sample=(last,sent,received)=>{const dt=last?(p.time-last.time)/1000:0;return {time:p.time,sent,received,tx:dt>0&&sent>=last.sent?(sent-last.sent)/dt:null,rx:dt>0&&received>=last.received?(received-last.received)/dt:null,open};};
   const point=sample(this.previous,s.Sent||0,s.Received||0);this.previous=point;this.points.push(point);if(this.points.length>300)this.points.shift();
   const c=(s.connections||[]).find(c=>c.id===this.selected);
   if(c){const f=sample(this.flowPrevious,c.Sent||0,c.Received||0);this.flowPrevious=f;this.flow.push(f);if(this.flow.length>300)this.flow.shift();}
  }
 }
 class RateRing {
  constructor(){this.t=new Float64Array(300);this.tx=new Float64Array(300);this.rx=new Float64Array(300);this.size=0;this.next=0;}
  add(t,tx,rx){const i=this.next;this.t[i]=t;this.tx[i]=tx??NaN;this.rx[i]=rx??NaN;this.next=(i+1)%300;this.size=Math.min(300,this.size+1)}
  points(){const out=[];for(let n=0;n<this.size;n++){const i=(this.next-this.size+n+300)%300;out.push({time:this.t[i],tx:this.tx[i],rx:this.rx[i]})}return out}
 }
 class Comparison {
  constructor(){this.previous=new Map();this.rings=new Map();this.rates=new Map();this.epoch=0;this.at=0;}
  update(packet,selected){const c=packet.state.capture;if(!c)return;if(c.since!==this.epoch){this.previous.clear();this.rings.clear();this.rates.clear();this.at=0;this.epoch=c.since}
   const at=c.at||packet.time;if(at<=this.at)return;this.at=at;const previous=new Map();
   for(const [prefix,items] of [['f:',c.flows||[]],['g:',c.groups||[]]])for(const item of items){const id=prefix+item.id,last=this.previous.get(id),dt=last?(at-last.at)/1000:0;const tx=!c.error&&last&&dt>0&&item.sent>=last.sent?(item.sent-last.sent)/dt:null,rx=!c.error&&last&&dt>0&&item.received>=last.received?(item.received-last.received)/dt:null;previous.set(id,{at,sent:item.sent,received:item.received});this.rates.set(id,{tx,rx});
    if(prefix==='g:'||selected.has(id)){let ring=this.rings.get(id);if(!ring){ring=new RateRing();this.rings.set(id,ring)}ring.add(packet.time,tx,rx)}
   }
   this.previous=previous;for(const id of this.rates.keys())if(!previous.has(id))this.rates.delete(id);
   let extra=0;for(const id of this.rings.keys())if(id.startsWith('f:')&&!selected.has(id)){if(++extra>64)this.rings.delete(id)}
  }
 }
 global.FarTrafficMath={History,RateRing,Comparison};
 if(typeof document==='undefined')return;
 const root=document.getElementById('traffic');if(!root)return;
 const el=(tag,text,cls)=>{const n=document.createElement(tag);if(text!==undefined)n.textContent=text;if(cls)n.className=cls;return n;};
 const send=(Key,extra={})=>global.webkit.messageHandlers.farshmac.postMessage({Type:'traffic',Key,...extra});
 const button=(text,fn)=>{const n=el('button',text);n.type='button';n.addEventListener('click',fn);return n;};
 let packet=null,selected=0,range=180,scope='all',filter='',onlyOpen=false,frame=0,editing=null,view='flows',socketProtocol='all',socketFilter='',socketPage=0;
 const history=new History(),podHistory=new History(),comparison=new Comparison(),chosen=new Set();
 let analysisProtocol='all',analysisDirection='all',analysisFilter='',analysisGroup=null,analysisPage=0,rateKind='total',sumSelection=false;
 const header=el('header'),heading=el('div'),title=el('h1','Traffic monitor'),subtitle=el('p','Connecting…'),status=el('span','Connecting','traffic-status');
 heading.append(title,subtitle);header.append(heading,status,button('Close monitor · Esc',()=>send('close')));
 const notice=el('div',undefined,'traffic-notice');notice.setAttribute('role','status');
 const portBar=el('section',undefined,'traffic-port'),portHeading=el('div'),portTitle=el('strong','Port conditions'),portSummary=el('span','No port conditions');portHeading.append(portTitle,portSummary);
 const portButtons=[button('Conditions',()=>editFaults(portTarget())),button('Block port',()=>act('faults',{...packet.state.portFaults,lossPercent:packet.state.portFaults?.lossPercent===100?0:100},portTarget())),button('Clear port',()=>act('clear',{},portTarget())),button('Reset open connections',()=>act('reset',{},portTarget()))];
 portButtons[3].className='traffic-reset';portBar.append(portHeading,...portButtons);
 const navigation=el('nav',undefined,'traffic-navigation'),flowsTab=button('Connections',()=>switchAnalysis('flows')),groupsTab=button('Port groups',()=>switchAnalysis('groups')),overviewTab=button('Sockets & listeners',()=>setView('overview')),faultTab=button('Fault injection',()=>setView('faults')),enable=button('Test network conditions…',()=>openExperiment());
 const disable=button('Stop fault injection',()=>send('stop'));navigation.append(flowsTab,groupsTab,overviewTab,faultTab,enable,disable);
 const graphLabel=el('div',undefined,'traffic-graph-label');
 const charts=el('section'  ,undefined,'traffic-charts'),chartBar=el('div',undefined,'traffic-chart-bar');
 const scopeSelect=el('select');scopeSelect.setAttribute('aria-label','Chart scope');for(const [v,t] of [['all','All monitored connections'],['selected','Selected connection']]){const o=el('option',t);o.value=v;scopeSelect.append(o)}
 scopeSelect.addEventListener('change',()=>{scope=scopeSelect.value;schedule()});
 const rangeSelect=el('select');rangeSelect.setAttribute('aria-label','Chart time range');for(const n of [60,180,300]){const o=el('option',n/60+' min');o.value=n;rangeSelect.append(o)}rangeSelect.value=range;rangeSelect.addEventListener('change',()=>{range=Number(rangeSelect.value);schedule()});
 const legend=el('div',undefined,'traffic-legend'),tx=el('span','↑ Sent —','traffic-tx'),rx=el('span','↓ Received —','traffic-rx');legend.append(tx,rx);
 chartBar.append(scopeSelect,rangeSelect,legend);
 const canvas=el('canvas');canvas.setAttribute('role','img');canvas.setAttribute('aria-label','Traffic rates over time');
 const activeLabel=el('div','Open connections','traffic-active-label');
 const activeCanvas=el('canvas',undefined,'traffic-active');activeCanvas.setAttribute('role','img');activeCanvas.setAttribute('aria-label','Concurrent monitored connections over time');
 charts.append(graphLabel,chartBar,canvas,activeLabel,activeCanvas);
 const tools=el('div',undefined,'traffic-tools'),search=el('input');search.type='search';search.placeholder='Find address or HTTP path';search.setAttribute('aria-label','Filter connections');search.addEventListener('input',()=>{filter=search.value.toLowerCase();renderRows()});
 const openLabel=el('label'),openOnly=el('input');openOnly.type='checkbox';openOnly.addEventListener('change',()=>{onlyOpen=openOnly.checked;renderRows()});openLabel.append(openOnly,document.createTextNode('Open only'));
 const count=el('span',undefined,'traffic-count');tools.append(search,openLabel,count);
 const scroll=el('div',undefined,'traffic-table-scroll'),table=el('table'),thead=el('thead'),headRow=el('tr');for(const h of ['State','Source → destination','Sent / received','HTTP','Conditions','Actions'])headRow.append(el('th',h));thead.append(headRow);const body=el('tbody');table.append(thead,body);scroll.append(table);
 const empty=el('p','Waiting for new TCP connections matching the selected port…','traffic-empty');
 const footer=el('footer'),selectedText=el('span','Select a connection','traffic-selected');
 footer.append(selectedText,el('span','F2 Conditions · F4 Block · F6 Clear · F8 Reset','traffic-key-hints'));
 const modal=el('dialog'),form=el('form'),modalTitle=el('h2','Connection conditions'),explain=el('p','Applied to this connection only. Rate and delay affect each direction; loss drops packets sent from the client.'),inputs={};
 form.append(modalTitle,explain);
 for(const [key,label,max,step] of [['delayMS','Delay per forwarded chunk (ms)',30000,1],['rateKiB','Rate per direction (KiB/s; 0 = unlimited)',1048576,1],['lossPercent','Packet loss (%)',100,.1]]){
  const l=el('label',label),i=el('input');i.type='number';i.min=0;i.max=max;i.step=step;i.required=true;l.append(i);form.append(l);inputs[key]=i;
 }
 const modalActions=el('div'),apply=el('button','Apply');apply.type='submit';modalActions.append(button('Cancel',()=>modal.close()),apply);form.append(modalActions);modal.append(form);
 form.addEventListener('submit',e=>{e.preventDefault();if(!form.reportValidity())return;const f={};for(const [k,i] of Object.entries(inputs))f[k]=Number(i.value);act('faults',f,editing);modal.close()});
 const inventory=el('section',undefined,'traffic-inventory'),inventoryTools=el('div',undefined,'traffic-tools'),protocolSelect=el('select');protocolSelect.setAttribute('aria-label','Socket protocol');
 for(const [v,t] of [['all','TCP + UDP'],['tcp','TCP'],['udp','UDP']]){const o=el('option',t);o.value=v;protocolSelect.append(o)}protocolSelect.onchange=()=>{socketProtocol=protocolSelect.value;socketPage=0;renderInventory()};
 const socketSearch=el('input');socketSearch.type='search';socketSearch.placeholder='Filter address, port or state';socketSearch.setAttribute('aria-label','Filter pod sockets');socketSearch.oninput=()=>{socketFilter=socketSearch.value.toLowerCase();socketPage=0;renderInventory()};
 const socketCount=el('span',undefined,'traffic-count');inventoryTools.append(protocolSelect,socketSearch,socketCount);
 const socketScroll=el('div',undefined,'traffic-table-scroll'),socketTable=el('table'),socketHead=el('thead'),socketHeadRow=el('tr');for(const h of ['Protocol','State','Local address','Peer address','Test conditions'])socketHeadRow.append(el('th',h));socketHead.append(socketHeadRow);const socketBody=el('tbody');socketTable.append(socketHead,socketBody);socketScroll.append(socketTable);
 const socketEmpty=el('p',undefined,'traffic-empty'),pages=el('div',undefined,'traffic-pages'),prev=button('Previous',()=>{socketPage--;renderInventory()}),next=button('Next',()=>{socketPage++;renderInventory()}),pageLabel=el('span');pages.append(prev,pageLabel,next);
 const inventoryNote=el('p','All containers share this pod network namespace. Per-socket byte rates and container ownership are not available in this view.','traffic-inventory-note');inventory.append(inventoryTools,socketScroll,socketEmpty,pages,inventoryNote);
 const experiment=el('dialog'),experimentForm=el('form'),experimentTitle=el('h2','Enable fault injection');experimentForm.append(experimentTitle,el('p','This changes traffic on one IPv4 TCP port. Only new connections enter the proxy; the application sees the proxy source address. Existing sockets remain visible in the overview.'));
 const experimentInputs={};
 for(const [key,label,value] of [['port','TCP port',''],['direction','Direction','both'],['peerCIDR','Peer IPv4 CIDR','0.0.0.0/0']]){
  const l=el('label',label),i=el(key==='direction'?'select':'input');
  if(key==='direction'){for(const v of ['both','inbound','outbound']){const o=el('option',v);o.value=v;i.append(o)}}else if(key==='port'){i.type='number';i.min=1;i.max=65535;i.step=1}else i.type='text';i.value=value;i.required=true;l.append(i);experimentInputs[key]=i;experimentForm.append(l)
 }
 const expButtons=el('div'),expSubmit=el('button','Enable for this port');expSubmit.type='submit';expButtons.append(button('Cancel',()=>experiment.close()),expSubmit);experimentForm.append(expButtons);experiment.append(experimentForm);
 experimentForm.onsubmit=e=>{e.preventDefault();if(!experimentForm.reportValidity())return;send('start',{Text:JSON.stringify({interactive:true,durationSeconds:30,port:Number(experimentInputs.port.value),direction:experimentInputs.direction.value,peerCIDR:experimentInputs.peerCIDR.value})});experiment.close();setView('faults')};
 const analysis=el('section',undefined,'traffic-inventory'),analysisTools=el('div',undefined,'traffic-tools');
 const analysisSearch=el('input');analysisSearch.type='search';analysisSearch.placeholder='Filter address, port or state';analysisSearch.setAttribute('aria-label','Filter measured traffic');analysisSearch.oninput=()=>{analysisGroup=null;analysisFilter=analysisSearch.value.toLowerCase();analysisPage=0;renderAnalysis()};
 function choice(options,label,fn){const n=el('select');n.setAttribute('aria-label',label);for(const [value,text] of options){const o=el('option',text);o.value=value;n.append(o)}n.onchange=()=>fn(n.value);return n}
 const analysisProto=choice([['all','TCP + UDP'],['tcp','TCP'],['udp','UDP']],'Traffic protocol',v=>{analysisProtocol=v;analysisPage=0;renderAnalysis()}),analysisDir=choice([['all','Both directions'],['inbound','Incoming · local port'],['outbound','Outgoing · remote port']],'Connection direction',v=>{analysisDirection=v;analysisPage=0;renderAnalysis()});
 const analysisCount=el('span',undefined,'traffic-count');analysisTools.append(analysisProto,analysisDir,analysisSearch,analysisCount);
 const analysisScroll=el('div',undefined,'traffic-table-scroll'),analysisTable=el('table'),analysisHead=el('thead'),analysisBody=el('tbody');analysisTable.append(analysisHead,analysisBody);analysisScroll.append(analysisTable);
 const analysisPages=el('div',undefined,'traffic-pages'),analysisPrev=button('Previous',()=>{analysisPage--;renderAnalysis()}),analysisNext=button('Next',()=>{analysisPage++;renderAnalysis()}),analysisPageText=el('span');analysisPages.append(analysisPrev,analysisPageText,analysisNext);
 const analysisNote=el('p',undefined,'traffic-inventory-note');analysis.append(analysisTools,analysisScroll,analysisPages,analysisNote);
 const selectionTools=el('div',undefined,'traffic-selection-tools'),selectionText=el('span','Select rows to compare their traffic'),clearSelection=button('Clear selection',()=>{chosen.clear();renderAnalysis();schedule()}),directionChoice=choice([['total','Sent + received'],['rx','Received'],['tx','Sent']],'Chart traffic',v=>{rateKind=v;schedule()}),sumLabel=el('label'),sumCheck=el('input');sumCheck.type='checkbox';sumCheck.onchange=()=>{sumSelection=sumCheck.checked;schedule()};sumLabel.append(sumCheck,document.createTextNode('Show sum'));selectionTools.append(selectionText,directionChoice,sumLabel,clearSelection);
 const comparisonLegend=el('div',undefined,'traffic-comparison-legend');charts.append(selectionTools,comparisonLegend);
 const analysisRows=new Map();
 function switchAnalysis(v){if(view!==v){chosen.clear();analysisPage=0;analysisGroup=null;analysisFilter='';analysisSearch.value='';analysisRows.clear();analysisBody.replaceChildren()}setView(v)}
 function analysisItems(){const cap=packet?.state.capture;const items=view==='groups'?cap?.groups:cap?.flows;return (items||[]).filter(x=>(!analysisGroup||x.port===analysisGroup.port&&x.protocol===analysisGroup.protocol&&x.direction===analysisGroup.direction)&&(analysisProtocol==='all'||x.protocol.startsWith(analysisProtocol))&&(analysisDirection==='all'||x.direction===analysisDirection)&&(!analysisFilter||[x.local,x.remote,x.port,x.state,x.protocol,x.direction].join(' ').toLowerCase().includes(analysisFilter)))}
 function renderAnalysis(){
  const grouped=view==='groups',items=analysisItems().slice().sort((a,b)=>(b.sent+b.received)-(a.sent+a.received)),prefix=grouped?'g:':'f:';
  analysisPage=Math.max(0,Math.min(analysisPage,Math.max(0,Math.ceil(items.length/100)-1)));
  const page=items.slice(analysisPage*100,analysisPage*100+100),ids=new Set(page.map(x=>prefix+x.id));
  const headers=grouped?['Chart','Direction','Protocol','Port','Connections observed','Sent','Received','Send/s','Receive/s','']:['Chart','Direction','Protocol','Local address','Peer address','State','Sent','Received','Send/s','Receive/s'];
  if(analysisHead.dataset.kind!==view){analysisHead.dataset.kind=view;const row=el('tr');for(const name of headers)row.append(el('th',name));analysisHead.replaceChildren(row);analysisRows.clear();analysisBody.replaceChildren()}
  for(const [id,row] of analysisRows)if(!ids.has(id)){row.remove();analysisRows.delete(id)}
  page.forEach((item,index)=>{const id=prefix+item.id;let row=analysisRows.get(id);if(!row){row=el('tr');for(let i=0;i<10;i++)row.append(el('td'));const check=el('input');check.type='checkbox';check.setAttribute('aria-label','Chart '+(grouped?item.direction+' '+item.protocol+' port '+item.port:item.local+' to '+item.remote));check.onchange=()=>{if(check.checked&&chosen.size>=64){check.checked=false;selectionText.textContent='Select up to 64 rows';return}if(check.checked)chosen.add(id);else chosen.delete(id);renderAnalysis();schedule()};row.children[0].append(check);if(grouped){row.children[9].append(button('Show connections',()=>{const group={...item};switchAnalysis('flows');analysisDirection=group.direction;analysisDir.value=group.direction;analysisProtocol=group.protocol.startsWith('tcp')?'tcp':'udp';analysisProto.value=analysisProtocol;analysisGroup=group;analysisFilter='';analysisSearch.value='';analysisSearch.placeholder='Port '+group.port+' · type to clear group filter';renderAnalysis()}))}analysisRows.set(id,row)}
   const r=comparison.rates.get(id)||{},values=grouped?[item.direction==='inbound'?'Incoming':'Outgoing',item.protocol.toUpperCase(),String(item.port),String(item.connections),human(item.sent,'B'),human(item.received,'B'),human(r.tx,'B/s'),human(r.rx,'B/s')]:[item.direction==='inbound'?'Incoming':'Outgoing',item.protocol.toUpperCase(),item.local,item.remote,item.state,human(item.sent,'B'),human(item.received,'B'),human(r.tx,'B/s'),human(r.rx,'B/s')];
   values.forEach((v,i)=>row.children[i+1].textContent=v);row.children[0].firstChild.checked=chosen.has(id);row.className=chosen.has(id)?'selected':'';if(analysisBody.children[index]!==row)analysisBody.insertBefore(row,analysisBody.children[index]||null)
  });
  analysisCount.textContent=items.length+(grouped?' port groups':' connections');analysisPageText.textContent=(analysisPage+1)+' / '+Math.max(1,Math.ceil(items.length/100));analysisPrev.disabled=analysisPage===0;analysisNext.disabled=(analysisPage+1)*100>=items.length;analysisPages.hidden=items.length<=100;
  selectionText.textContent=chosen.size?chosen.size+' selected':'Select rows to compare their traffic';
  const cap=packet?.state.capture;analysisNote.textContent=cap?.error?cap.error:'IP bytes since '+(cap?new Date(cap.since).toLocaleTimeString():'monitoring started')+'; includes headers and retransmissions. Short connections remain in port totals. Connection charts collect history while selected; group charts retain 5 minutes.';
  if(cap?.dropped||cap?.unparsed||cap?.evicted||cap?.groupLimit)analysisNote.textContent+=' Diagnostics: '+(cap.dropped||0)+' kernel drops; '+(cap.unparsed||0)+' unparsed/non-TCP/UDP packets; '+(cap.evicted||0)+' old flow records evicted'+(cap.groupLimit?'; group limit reached':'')+'.';
 }
 root.append(header,navigation,notice,portBar,charts,analysis,inventory,tools,scroll,empty,footer,modal,experiment);
 const socketRows=new Map();
 function setView(v){view=v;const measured=v==='flows'||v==='groups';flowsTab.setAttribute('aria-pressed',v==='flows');groupsTab.setAttribute('aria-pressed',v==='groups');overviewTab.setAttribute('aria-pressed',v==='overview');faultTab.setAttribute('aria-pressed',v==='faults');analysis.hidden=!measured;selectionTools.hidden=!measured;comparisonLegend.hidden=!measured;inventory.hidden=v!=='overview';tools.hidden=scroll.hidden=footer.hidden=v!=='faults';scopeSelect.hidden=v!=='faults';portBar.hidden=v!=='faults'||!packet?.state.active;empty.hidden=v!=='faults'||!!packet?.state.connections?.length;renderInventory();if(measured)renderAnalysis();schedule()}

 function openExperiment(socket){
  if(!packet||packet.pending||packet.error||packet.state.active)return;
  const split=a=>Number(a.slice(a.lastIndexOf(':')+1));
  let port='',direction='both';
  if(socket){const localPort=split(socket.local),listening=packet.state.overview.sockets.some(s=>s.protocol===socket.protocol&&s.state==='LISTEN'&&split(s.local)===localPort);direction=listening||socket.state==='LISTEN'?'inbound':'outbound';port=direction==='inbound'?localPort:split(socket.remote)}
  experimentInputs.port.value=port;experimentInputs.direction.value=direction;experiment.showModal();experimentInputs.port.focus()
 }
 function renderInventory(){
  const overview=packet?.state.overview;if(!overview)return;
  const items=(overview.sockets||[]).filter(s=>(socketProtocol==='all'||s.protocol.startsWith(socketProtocol))&&(!socketFilter||[s.protocol,s.local,s.remote,s.state].join(' ').toLowerCase().includes(socketFilter))).slice().sort((a,b)=>[a.protocol,a.local,a.remote,a.inode].join('|').localeCompare([b.protocol,b.local,b.remote,b.inode].join('|')));
  socketPage=Math.max(0,Math.min(socketPage,Math.max(0,Math.ceil(items.length/100)-1)));
  const shown=items.slice(socketPage*100,socketPage*100+100),ids=new Set(shown.map(s=>[s.protocol,s.inode,s.local,s.remote].join('|')));
  for(const [id,row] of socketRows)if(!ids.has(id)){row.remove();socketRows.delete(id)}
  shown.forEach((s,index)=>{const id=[s.protocol,s.inode,s.local,s.remote].join('|');let row=socketRows.get(id);if(!row){row=el('tr');for(let i=0;i<5;i++)row.append(el('td'));const b=button('Test port…',()=>openExperiment(s));row.children[4].append(b);socketRows.set(id,row)}
   const texts=[s.protocol.toUpperCase(),s.state,s.local,s.remote];texts.forEach((t,i)=>row.children[i].textContent=t);
   const b=row.children[4].firstChild,compatible=s.protocol==='tcp';b.disabled=!compatible||packet.state.active||packet.pending||!!packet.error;b.textContent=compatible?'Test port…':'Observation only';b.title=compatible?'Choose conditions explicitly; existing sockets are not redirected':'Fault injection currently supports IPv4 TCP only';
   if(socketBody.children[index]!==row)socketBody.insertBefore(row,socketBody.children[index]||null)
  });
  socketCount.textContent=items.length+' sockets'+(overview.truncated?' (limited snapshot)':'');socketEmpty.hidden=items.length>0;socketEmpty.textContent=socketFilter||socketProtocol!=='all'?'No sockets match this filter.':overview.warnings?.length?'Socket data is unavailable or incomplete; see the message above.':'No TCP or UDP sockets are present in this snapshot.';
  pageLabel.textContent=(socketPage+1)+' / '+Math.max(1,Math.ceil(items.length/100));prev.disabled=socketPage===0;next.disabled=(socketPage+1)*100>=items.length;pages.hidden=items.length<=100
 }

 const rows=new Map();
 function current(){return packet?.state.connections?.find(c=>c.id===selected)}
 function portTarget(){return {scope:'port',port:packet?.state.config.port,id:0}}
 function connectionTarget(id=selected){return {scope:'connection',id}}
 function allowed(target=connectionTarget()){const c=packet?.state.connections?.find(c=>c.id===target?.id);return !!(target&&packet?.state.active&&!packet.error&&!packet.pending&&(target.scope==='port'||c&&!c.closed))}
 function act(action,faults={},target=connectionTarget()){if(!allowed(target))return;send('action',{Text:JSON.stringify({...target,action,faults})});}
 function describe(f){return [f?.delayMS?f.delayMS+' ms/chunk':'',f?.rateKiB?human(f.rateKiB*1024,'B/s')+' per connection':'',f?.lossPercent?f.lossPercent+'% loss':''].filter(Boolean).join(' · ')}
 function editFaults(target=connectionTarget()){
  if(!allowed(target))return;editing={...target};
  const port=target.scope==='port',f=port?packet.state.portFaults:packet.state.connections.find(c=>c.id===target.id)?.faults;
  modalTitle.textContent=port?'TCP '+target.port+' — port conditions':'Connection #'+target.id+' — additional conditions';
  explain.textContent=port?'Applies to existing and future traffic matching this port, direction and peer range. Rate is per connection and direction; reset affects only current monitored connections.':'Port conditions remain active. Delays add; the lower nonzero rate wins; packet-loss probabilities compound. Clearing this connection leaves port conditions unchanged.';
  for(const [k,i] of Object.entries(inputs))i.value=f?.[k]||0;modal.showModal();inputs.delayMS.focus()
 }
 function select(id){selected=id;history.select(id);renderRows();schedule();}
 function visible(){return (packet?.state.connections||[]).filter(c=>(!onlyOpen||!c.closed)&&(!filter||[c.Client,c.client,c.destination,c.HTTPMethod,c.HTTPPath,c.HTTPStatus].join(' ').toLowerCase().includes(filter))).slice().reverse()}
 function renderRows(){
  const connections=visible(),ids=new Set(connections.map(c=>c.id));
  if(selected&&!ids.has(selected)){selected=0;history.select(0);schedule()}
  for(const [id,row] of rows)if(!ids.has(id)){row.remove();rows.delete(id)}
  for(const [index,c] of connections.entries()){
   let row=rows.get(c.id);if(!row){row=el('tr');row.tabIndex=0;row.addEventListener('click',()=>select(c.id));row.addEventListener('focus',()=>{if(selected!==c.id)select(c.id)});for(let i=0;i<6;i++)row.append(el('td'));
    const target=connectionTarget(c.id),actions=row.children[5];actions.className='traffic-row-actions';
    const invoke=fn=>e=>{e.stopPropagation();select(c.id);fn()};
    for(const [label,fn] of [['Conditions',()=>editFaults(target)],['Block',()=>{const f=packet.state.connections.find(x=>x.id===c.id)?.faults;act('faults',{...f,lossPercent:f?.lossPercent===100?0:100},target)}],['Clear',()=>act('clear',{},target)],['Reset',()=>act('reset',{},target)]]){
     const b=el('button',label);b.type='button';b.addEventListener('click',invoke(fn));b.setAttribute('aria-label',label+' connection '+c.id);if(label==='Reset')b.className='traffic-reset';actions.append(b)
    }
    rows.set(c.id,row)}
   row.className=(c.id===selected?'selected ':'')+(c.closed?'closed':'');row.setAttribute('aria-selected',c.id===selected?'true':'false');
   const fs=describe(c.faults),inherited=describe(packet.state.portFaults);
   const text=[c.closed?'Closed':'Open',c.client+' → '+c.destination,human(c.Sent||0,'B')+' / '+human(c.Received||0,'B'),[c.HTTPMethod,c.HTTPPath,c.HTTPStatus].filter(Boolean).join(' ')||'—',[inherited?'Port: '+inherited:'',fs?'Connection: '+fs:''].filter(Boolean).join('\n')||'None'];
   [...row.children].slice(0,5).forEach((td,i)=>{if(td.textContent!==text[i])td.textContent=text[i]});for(const b of row.children[5].children)b.disabled=!allowed(connectionTarget(c.id));row.children[5].children[1].textContent=c.faults?.lossPercent===100?'Unblock':'Block';row.children[5].children[1].setAttribute('aria-label',row.children[5].children[1].textContent+' connection '+c.id);row.title=c.error||'';if(body.children[index]!==row)body.insertBefore(row,body.children[index]||null);
  }
  count.textContent=connections.length+' shown · '+(packet?.state.Accepted||0)+' accepted';empty.hidden=view!=='faults'||connections.length>0;empty.textContent=packet?.state.active?'Waiting for new connections through the enabled fault proxy. Existing sockets remain in Connections & listeners.':'Fault injection is off. Use Test network conditions to choose a port explicitly.';
  const c=current();selectedText.textContent=packet?.pending?'Applying…':c?'Connection #'+c.id+(c.closed?' · closed':''):'Select a connection';
  portTitle.textContent='TCP '+packet?.state.config.port+' · all matching traffic';portSummary.textContent=describe(packet?.state.portFaults)||'No port conditions';
  for(const b of portButtons)b.disabled=!allowed(portTarget());portButtons[1].textContent=packet?.state.portFaults?.lossPercent===100?'Unblock port':'Block port';

 }
 function schedule(){if(!frame)frame=global.requestAnimationFrame(()=>{frame=0;draw()})}
 function plot(node,points,keys,unit,colors){
  const r=node.getBoundingClientRect(),dpr=global.devicePixelRatio||1,w=r.width,h=r.height;if(w<1||h<1)return;
  node.width=Math.round(w*dpr);node.height=Math.round(h*dpr);const ctx=node.getContext('2d');ctx.scale(dpr,dpr);ctx.clearRect(0,0,w,h);
  const end=packet?.time||Date.now(),start=end-range*1000,pts=points.filter(p=>p.time>=start),max=Math.max(unit==='B/s'?1:2,...pts.flatMap(p=>keys.map(k=>p[k]||0)))*1.12,left=88,right=14,top=12,bottom=24,pw=w-left-right,ph=h-top-bottom;
  ctx.font='11px -apple-system, sans-serif';ctx.fillStyle='#a7c2dc';ctx.strokeStyle='#375070';ctx.lineWidth=1;
  for(let i=0;i<3;i++){const y=top+ph*i/2;ctx.beginPath();ctx.moveTo(left,y);ctx.lineTo(w-right,y);ctx.stroke();ctx.textAlign='right';ctx.fillText(unit==='connections'?String(Math.round(max*(1-i/2))):human(max*(1-i/2),unit),left-9,y+4)}
  ctx.textAlign='left';ctx.fillText('-'+range/60+' min',left,h-6);ctx.textAlign='right';ctx.fillText('Now',w-right,h-6);
  keys.forEach((key,i)=>{ctx.strokeStyle=colors[i];ctx.lineWidth=1.8;ctx.beginPath();let pen=false,last=0;for(const p of pts){if(!Number.isFinite(p[key])){pen=false;continue}const x=left+(p.time-start)/(range*1000)*pw,y=top+ph*(1-p[key]/max);if(!pen||p.time-last>4000)ctx.moveTo(x,y);else ctx.lineTo(x,y);pen=true;last=p.time}ctx.stroke()});
 }
 function draw(){const measured=view==='flows'||view==='groups',overview=view!=='faults',points=overview?podHistory.points:scope==='selected'?history.flow:history.points,last=points.at(-1);graphLabel.textContent=overview?'Total pod traffic · all non-loopback interfaces':'Traffic through fault proxy only';activeLabel.textContent=(overview?'Established TCP sockets · ':'Open controlled connections · ')+((overview?podHistory:history).points.at(-1)?.open||0);tx.textContent=(overview?'Pod ↑ Sent ':'↑ Sent ')+human(last?.tx,'B/s');rx.textContent=(overview?'Pod ↓ Received ':'↓ Received ')+human(last?.rx,'B/s');plot(canvas,points,['tx','rx'],'B/s',['#ffc96b','#54e0d3']);plot(activeCanvas,overview?podHistory.points:history.points,['open'],'connections',['#87baff']);comparisonLegend.replaceChildren();
  if(!measured||!chosen.size)return;
  graphLabel.textContent=(view==='groups'?'Selected port groups':'Selected connections')+' · '+(rateKind==='total'?'sent + received':rateKind==='tx'?'sent':'received')+' · IP bytes/s';
  const color=['#54e0d3','#ffc96b','#87baff','#f69abd','#c2d98c','#b9a3ff','#ff936f','#91dbe6'],rows=[],end=packet.time,start=end-range*1000;let high=1;
  for(const id of chosen){const ring=comparison.rings.get(id),pts=(ring?.points()||[]).filter(p=>p.time>=start),values=pts.map(p=>rateKind==='total'?p.tx+p.rx:p[rateKind]);for(const v of values)if(Number.isFinite(v))high=Math.max(high,v);const item=(id.startsWith('g:')?packet.state.capture?.groups:packet.state.capture?.flows)?.find(x=>String(x.id)===id.slice(2));const label=item?(id.startsWith('g:')?item.direction+' '+item.protocol.toUpperCase()+'/'+item.port:item.local+' ↔ '+item.remote):id;rows.push({id,pts,values,label})}
  if(sumSelection){const sums=new Map();for(const row of rows)row.pts.forEach((p,i)=>{if(Number.isFinite(row.values[i])){const value=sums.get(p.time)||{value:0,count:0};value.value+=row.values[i];value.count++;sums.set(p.time,value)}});const pts=[...sums].sort((a,b)=>a[0]-b[0]).map(([time,value])=>({time,value:value.count===rows.length?value.value:NaN}));const values=pts.map(p=>p.value);for(const v of values)if(Number.isFinite(v))high=Math.max(high,v);rows.push({id:'sum',pts,values,label:'Sum of selection'})}
  high*=1.1;const r=canvas.getBoundingClientRect(),dpr=global.devicePixelRatio||1,w=r.width,h=r.height,ctx=canvas.getContext('2d');ctx.clearRect(0,0,w,h);const left=88,pw=w-left-14,top=12,ph=h-top-24;ctx.font='11px -apple-system,sans-serif';ctx.textAlign='right';ctx.fillStyle='#a7c2dc';ctx.strokeStyle='#375070';ctx.lineWidth=1;
  for(let i=0;i<3;i++){const y=top+i*ph/2;ctx.beginPath();ctx.moveTo(left,y);ctx.lineTo(w-14,y);ctx.stroke();ctx.fillText(human(high*(1-i/2),'B/s'),left-9,y+4)}
  rows.forEach((row,i)=>{const stroke=row.id==='sum'?'#ffffff':color[i%color.length];ctx.strokeStyle=stroke;ctx.lineWidth=row.id==='sum'?2.5:1.6;ctx.setLineDash(row.id==='sum'?[5,3]:[]);ctx.beginPath();let pen=false,lastT=0;row.pts.forEach((p,j)=>{const v=row.values[j];if(!Number.isFinite(v)){pen=false;return}const x=left+(p.time-start)/(range*1000)*pw,y=top+ph*(1-v/high);if(!pen||p.time-lastT>4000)ctx.moveTo(x,y);else ctx.lineTo(x,y);pen=true;lastT=p.time});ctx.stroke();const label=el('span',row.label+' · '+human(row.values.at(-1),'B/s'));label.style.color=stroke;comparisonLegend.append(label)});ctx.setLineDash([]);ctx.fillStyle='#a7c2dc';ctx.textAlign='left';ctx.fillText('-'+range/60+' min',left,h-6);ctx.textAlign='right';ctx.fillText('Now',w-14,h-6)
 }

 global.farshmacTraffic={
 copy(cut=false){const n=document.activeElement;let text=String(global.getSelection()||'');if(n instanceof HTMLInputElement&&n.selectionStart!==null){text=n.value.slice(n.selectionStart,n.selectionEnd);if(cut){n.setRangeText('',n.selectionStart,n.selectionEnd,'end');n.dispatchEvent(new Event('input',{bubbles:true}))}}if(text)global.webkit.messageHandlers.farshmac.postMessage({Type:'clipboard',Text:text})},
 paste(text){const n=document.activeElement;if(n instanceof HTMLInputElement){if(n.selectionStart!==null)n.setRangeText(text,n.selectionStart,n.selectionEnd,'end');else n.value=text;n.dispatchEvent(new Event('input',{bubbles:true}))}},
 selectAll(){const n=document.activeElement;if(n instanceof HTMLInputElement)n.select();else global.getSelection()?.selectAllChildren(root)},
 update(p){packet=p;history.update(p);comparison.update(p,chosen);const o=p.state.overview;if(o){const established=(o.sockets||[]).filter(s=>s.protocol.startsWith('tcp')&&s.state==='ESTABLISHED');podHistory.update({time:p.time,state:{Sent:o.countersValid?o.sent:NaN,Received:o.countersValid?o.received:NaN,connections:established}});if(!o.countersValid){const point=podHistory.points.at(-1);if(point)point.tx=point.rx=null;podHistory.previous=null}}
 title.textContent=p.title;subtitle.textContent='Pod network · all containers · TCP / UDP · IPv4 / IPv6';status.textContent=p.error?'Disconnected':p.state.active?'Fault injection enabled':p.state.capture?.error?'Socket inventory only':'Monitoring TCP + UDP';status.className='traffic-status '+(p.error?'error':'live');notice.textContent=p.error||p.state.cleanupError||p.actionError||p.state.capture?.error||(o?.warnings?.length?o.warnings.join(' · '):o?.truncated?'Socket snapshot is limited to 4096 entries.':p.state.active?'Traffic is being modified on '+p.state.config.direction+' TCP/'+p.state.config.port+'. Stop fault injection to restore normal traffic.':'Monitoring only. Traffic is counted without redirecting or modifying it.');enable.disabled=!!(p.pending||p.error||p.state.active);disable.hidden=!p.state.active;disable.disabled=!!p.pending;renderRows();setView(view)}};

 new ResizeObserver(schedule).observe(charts);
 document.addEventListener('keydown',e=>{
  if(!document.body.classList.contains('traffic')||e.metaKey||e.ctrlKey||e.altKey)return;
  if(modal.open||experiment.open)return;
  if(['INPUT','SELECT','TEXTAREA'].includes(document.activeElement?.tagName)&&e.key!=='Escape')return;
  const keys=['Escape','F2','F4','F6','F8','ArrowUp','ArrowDown'];if(!keys.includes(e.key))return;e.preventDefault();
  if(e.key==='Escape'){send('close');return}if(view!=='faults')return;if(e.key==='F2'){editFaults();return}if(e.key==='F4'){act('faults',{...current()?.faults,lossPercent:current()?.faults?.lossPercent===100?0:100});return}if(e.key==='F6'){act('clear');return}if(e.key==='F8'){act('reset');return}
  const list=visible(),i=list.findIndex(c=>c.id===selected),next=list[Math.max(0,Math.min(list.length-1,i+(e.key==='ArrowDown'?1:-1)))];if(next){select(next.id);rows.get(next.id)?.scrollIntoView({block:'nearest'})}
 });
})(window);