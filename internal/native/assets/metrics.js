/* Live pod metrics: no network access, external libraries or unbounded history. */
(function(global){
 'use strict';
 const palette=['#54e0d3','#ffc96b','#87baff','#f69abd','#c2d98c','#b9a3ff','#ff936f','#91dbe6'];
 const modes=[['auto','Auto'],['raw','Raw value'],['delta','Change / interval'],['rate','Rate / second']];
 function effectiveKind(s,source='auto'){
  if(source==='counter'||source==='gauge')return source;
  if(s.kind&&s.kind!=='untyped')return s.kind;
  // Untyped exporter output: only documented cumulative names, never jitter,
  // current sessions, memory usage, or a merely increasing sequence of values.
  if(/_total$/.test(s.name)||/^(?:paths|(?:webrtc|rtsps?|rtmps?|srt)_sessions|(?:rtsps?|rtmps?)_conns|hls_(?:sessions|muxers))_(?:(?:inbound|outbound)_bytes|bytes_(?:sent|received)|(?:inbound|outbound)_(?:rtp|rtcp)_packets(?:_(?:lost|in_error|reported_lost|discarded))?|(?:inbound|outbound)_frames_(?:discarded|in_error))$/.test(s.name))return 'counter';
  return s.kind||'untyped';
 }
 function valueOf(point,kind,mode,previous){
  const counter=kind==='counter'||kind==='histogram bucket';
  if(mode==='auto')mode=counter?'rate':'raw';
  if(mode==='raw')return point.v;
  if(!Number.isFinite(point.r))return null; // First sample, failed scrape, or long gap.
  let delta=point.d,rate=point.r;
  if(counter&&delta<0&&Number.isFinite(point.v)){rate=point.v*(rate/delta);delta=point.v;}
  if(previous&&Number.isFinite(previous.v)&&Number.isFinite(point.v)&&point.t>previous.t){
   delta=point.v-previous.v;if(counter&&delta<0)delta=point.v;
   rate=delta/((point.t-previous.t)/1000);
  }
  return mode==='delta'?delta:rate;
 }
 function transform(points,kind,mode,norm,scale=1,offset=0){
  const values=points.map((p,i)=>{const v=valueOf(p,kind,mode,points[i-1]);return Number.isFinite(v)?v*scale+offset:null;});
  const finite=values.filter(Number.isFinite);if(!finite.length)return values;
  const min=Math.min(...finite),max=Math.max(...finite),peak=Math.max(Math.abs(min),Math.abs(max)),base=finite.find(v=>v!==0),mean=finite.reduce((a,b)=>a+b,0)/finite.length;
  const sd=Math.sqrt(finite.reduce((sum,v)=>sum+(v-mean)**2,0)/finite.length);
  return values.map(v=>v===null?null:norm==='minmax'?(max===min?0:100*(v-min)/(max-min)):norm==='peak'?(peak?100*v/peak:0):norm==='index'?(base===undefined?null:100*v/base):norm==='zscore'?(sd?(v-mean)/sd:0):v);
 }
 function format(v){if(!Number.isFinite(v))return '—';const abs=Math.abs(v);if(abs>=1e9)return (v/1e9).toPrecision(4)+'G';if(abs>=1e6)return (v/1e6).toPrecision(4)+'M';if(abs>=1e3)return (v/1e3).toPrecision(4)+'k';if(abs>0&&abs<.001)return v.toExponential(2);return Number(v.toPrecision(4)).toString();}

 function displayUnit(s,mode='auto',norm='none',source='auto'){
  if(norm==='minmax'||norm==='peak')return '%';if(norm==='index')return 'index';if(norm==='zscore')return 'σ';
  const rate=mode==='rate'||mode==='auto'&&['counter','histogram bucket'].includes(effectiveKind(s,source)),name=s.name||'',unit=s.unit||'';
  if(/cpu.*(usage|user|system).*seconds/.test(name)&&rate||/cpu.*cores/.test(name))return 'cores';
  if(/bytes(?:_|$)/.test(name)||unit==='bytes')return rate?'B/s':'B';
  if(/(?:seconds|duration)(?:_|$)/.test(name)||unit==='seconds')return rate?'s/s':'s';
  if(/_ratio$/.test(name)||unit==='ratio')return 'ratio';
  if(/_percent$/.test(name)||unit==='percent')return '%';
  if(/packets(?:_|$)/.test(name))return rate?'packets/s':'packets';
  return rate?'1/s':unit;
 }
 function human(v,unit=''){
  if(!Number.isFinite(v))return '—';const n=x=>Number(x.toPrecision(4)).toString(),a=Math.abs(v);
  if(unit==='B'||unit==='B/s'){const units=['B','KiB','MiB','GiB','TiB','PiB'];let i=0;while(Math.abs(v)>=1024&&i<units.length-1){v/=1024;i++}return n(v)+' '+units[i]+(unit==='B/s'?'/s':'')}
  if(unit==='cores')return a>0&&a<1?n(v*1000)+' mCPU':n(v)+' cores';
  if(unit==='s'){if(a>0&&a<.001)return n(v*1e6)+' µs';if(a>0&&a<1)return n(v*1000)+' ms';if(a>=3600)return n(v/3600)+' h';if(a>=60)return n(v/60)+' min';return n(v)+' s'}
  if(unit==='ratio')return n(v*100)+'%';if(unit==='%')return n(v)+'%';
  return format(v)+(unit==='1/s'?'/s':unit?' '+unit:'');
 }
 function niceMaximum(value,bytes){
  if(!Number.isFinite(value)||value<=0)return 1;
  const step=value/4;if(bytes)return 4*2**Math.ceil(Math.log2(Math.max(1,step)));
  const base=10**Math.floor(Math.log10(step));return 4*base*([1,2,2.5,5,10].find(m=>step<=m*base*(1+1e-12))||10);
 }
 function defaultLayout(catalog){
  const chosen=new Map();
  for(const s of catalog){
   if(!['docker_container_cpu_usage_cores','podtoast_cpu_usage_seconds_total','container_cpu_usage_seconds_total','kubernetes_container_cpu_usage_cores','process_cpu_seconds_total'].includes(s.name))continue;
   const l=s.labels||{},key=[l.farshmac_namespace,l.farshmac_pod,l.farshmac_container||l.container||s.target].join('/');
   const priority=s.name.startsWith('podtoast_')?3:s.name.startsWith('container_')?2:1;
   if(!chosen.has(key)||priority>chosen.get(key).priority)chosen.set(key,{s,priority});
  }
  const selected={};for(const {s} of [...chosen.values()].slice(0,48))selected[s.id]={chart:'cpu',mode:'auto',scale:1,offset:0};
  return {charts:[{id:'cpu',name:'CPU · cores',norm:'none'}],selected,range:300};
 }
 function metricName(s){const memory={podtoast_memory_working_set_bytes:"Memory · Working set",podtoast_memory_anonymous_bytes:"Memory · Applications (anonymous)",podtoast_memory_file_cache_bytes:"Memory · File cache",podtoast_memory_usage_bytes:"Memory · Total"};return memory[s.name]||(s.name.startsWith("podtoast_") ? s.name.slice(9).replaceAll("_", " ") : s.name);}
 function seriesLabel(s){const l=s.labels||{},scope=[l.farshmac_namespace,l.farshmac_pod,l.farshmac_container||l.container].filter(Boolean).join(' / '),extra=Object.entries(l).filter(([k])=>!k.startsWith('farshmac_')&&!['container','namespace','pod'].includes(k)).map(([k,v])=>k+'='+v).join(', ');return (scope?scope+' · ':'')+metricName(s)+(extra?' {'+extra+'}':'')}
 function chartGroup(s){
  if(/cpu.*(usage|seconds)/.test(s.name))return ['cpu','CPU · cores'];
  if(/memory.*bytes/.test(s.name))return ['memory','Memory · bytes'];
  if(/(filesystem|storage).*bytes/.test(s.name))return ['storage','Storage · bytes'];
  if(/network.*bytes/.test(s.name))return ['network','Network · bytes/s'];
  return ['metric-'+s.name,s.name];
 }

 global.FarMetricsMath={transform,valueOf,effectiveKind,format,displayUnit,human,defaultLayout};
 if(typeof document==='undefined')return;
 const root=document.getElementById('metrics');if(!root)return;
 const send=(Key,extra={})=>global.webkit.messageHandlers.farshmac.postMessage({Type:'metrics',Key,...extra});
 const el=(tag,text,cls)=>{const n=document.createElement(tag);if(text!==undefined)n.textContent=text;if(cls)n.className=cls;return n;};
 const button=(text,fn)=>{const n=el('button',text);n.type='button';n.addEventListener('click',fn);return n;};
 const select=(items,value,fn,label)=>{const n=el('select');n.setAttribute('aria-label',label);items.forEach(([v,text])=>{const o=el('option',text);o.value=v;n.append(o)});n.value=value;n.addEventListener('change',()=>fn(n.value));return n;};
 let state=null,catalog=[],series=new Map(),layout=null,session='',searchText='',tab='metrics',cards=new Map(),renderFrame=0,preferredChart='';
 const header=el('header',undefined,'metrics-toolbar'),title=el('h1','Pod metrics'),subtitle=el('span','Discovering…','metrics-subtitle');
 const pause=button('Pause',()=>send('pause',{Button:state?.paused?0:1}));
 const refresh=button('Rediscover',()=>send('rediscover'));
 const detach=button('Detach window',()=>send('detach'));
 const close=button('Back to panels',()=>send('close'));
 const interval=select([['2','Every 2 s'],['5','Every 5 s'],['10','Every 10 s'],['30','Every 30 s'],['60','Every 60 s']],'2',v=>send('interval',{Count:Number(v)}),'Scrape interval');
 const range=select([['60','Last minute'],['300','Last 5 min'],['600','Last 10 min'],['1800','Last 30 min'],['3600','Last hour']],'300',v=>{layout.range=Number(v);pushLayout();drawAll()},'Visible time range');
 const metricsToggle=button('Metrics…',()=>{sidebar.hidden=!sidebar.hidden;if(!sidebar.hidden)search.focus();drawAll()});
 const options=el('details',undefined,'metrics-options');options.append(el('summary','Collection'),interval,refresh);
 header.append(title,subtitle,metricsToggle,pause,range,options,close);
 const body=el('div',undefined,'metrics-body'),sidebar=el('aside'),tabs=el('nav'),list=el('div',undefined,'metrics-catalog'),summary=el('p','', 'metrics-summary');
 const search=el('input');search.type='search';search.placeholder='Find metric or label…';search.setAttribute('aria-label','Find metrics');search.addEventListener('input',()=>{searchText=search.value.toLowerCase();renderList()});
 tabs.append(button('Metrics',()=>{tab='metrics';renderList()}),button('Endpoints',()=>{tab='endpoints';renderList()}));
 const selectedOnly=el('input');selectedOnly.type='checkbox';selectedOnly.addEventListener('change',renderList);const selectedLabel=el('label','Selected only');selectedLabel.prepend(selectedOnly);
 sidebar.hidden=true;sidebar.append(tabs,search,selectedLabel,summary,list);
 const main=el('main'),notice=el('div',undefined,'metrics-notice');notice.setAttribute('role','status');const chartTools=el('div',undefined,'metrics-chart-tools');
 chartTools.append(button('Add chart',()=>{if(layout.charts.length>=12)return;const id='chart-'+Date.now();layout.charts.push({id,name:'Custom chart',norm:'none'});pushLayout();renderCharts();renderList()}),button('Clear selection',()=>{layout.selected={};pushLayout();renderList();renderCharts()}),el('span','Counters default to rate/s. Esc pauses collection.'));
 const operationStatus=el('div',undefined,'metrics-notice');operationStatus.setAttribute('role','status');
 const charts=el('div',undefined,'metrics-charts');main.append(operationStatus,notice,chartTools,charts);body.append(sidebar,main);root.append(header,body);
 root.addEventListener('keydown',e=>{if((e.metaKey||e.ctrlKey)&&e.key.toLowerCase()==='f'){e.preventDefault();sidebar.hidden=false;search.focus()}});
 function pushLayout(automatic=false){if(!automatic)global.farshmacMetrics.manual=true;if(!layout)return;send('configure',{Text:JSON.stringify({keys:Object.keys(layout.selected),layout:JSON.stringify(layout)})})}
 function validLayout(value){
  if(!value||!Array.isArray(value.charts)||!value.charts.length||value.charts.length>12||!value.selected||typeof value.selected!=='object')return false;
  if(Object.keys(value.selected).length>48)return false;
  const ids=new Set();for(const c of value.charts){if(typeof c.id!=='string'||typeof c.name!=='string'||ids.has(c.id)||!['none','minmax','peak','index','zscore'].includes(c.norm))return false;ids.add(c.id)}
  for(const c of Object.values(value.selected)){if(!c||!ids.has(c.chart)||!['auto','raw','rate','delta'].includes(c.mode)||c.source&&!['auto','counter','gauge'].includes(c.source)||!Number.isFinite(c.scale)||!Number.isFinite(c.offset))return false;}
  return [60,300,600,1800,3600].includes(value.range);
 }
 function typeExplanation(s,source='auto'){
  const kind=effectiveKind(s,source),why=source!=='auto'?'User override':kind!==s.kind?'Inferred from metric name':'Exporter type';
  return why+': '+kind+(kind==='counter'||kind==='histogram bucket'?' · Auto = rate/s · resets handled':' · Auto = raw value');
 }
 function addMetric(s,existing){
  root.querySelector('dialog')?.close();root.querySelector('dialog')?.remove();
  const dialog=el('dialog',undefined,'metrics-add-dialog'),heading=el('h2',existing?'Move metric':'Add metric'),caption=el('p',seriesLabel(s),'metrics-muted');
  const suggested=chartGroup(s),compatible=layout.charts.find(c=>c.id===suggested[0]&&Object.entries(layout.selected).filter(([,cfg])=>cfg.chart===c.id).every(([id,cfg])=>{const other=catalog.find(x=>x.id===id);return other&&displayUnit(other,cfg.mode,'none',cfg.source)===displayUnit(s)}));
  const target=select([['new','New chart (separate)'],...layout.charts.map(c=>[c.id,c.name])],existing?'new':layout.charts.some(c=>c.id===preferredChart)?preferredChart:compatible?.id||'new',refresh,'Destination chart');
  const name=el('input');name.value=suggested[1];name.maxLength=80;name.setAttribute('aria-label','New chart title');
  const mode=select(modes,existing?.mode||'auto',refresh,'Display as');
  const source=select([['auto','Auto type'],['counter','Cumulative counter'],['gauge','Gauge / current value']],existing?.source||'auto',refresh,'Value type');
  const explanation=el('p'),warning=el('p',undefined,'metrics-notice'),actions=el('div',undefined,'metrics-dialog-actions');
  function field(label,node){const wrap=el('label',label);wrap.append(node);return wrap}
  const titleField=field('Chart title',name);
  const save=button(existing?'Move':'Add',()=>{
   if(target.value==='new'&&layout.charts.length>=12)return;
   let chart=target.value;
   if(chart==='new'){chart='chart-'+Date.now();layout.charts.push({id:chart,name:name.value.trim()||suggested[1],norm:'none'})}
   layout.selected[s.id]={chart,mode:mode.value,source:source.value,scale:existing?.scale??1,offset:existing?.offset??0};
   dialog.close();dialog.remove();pushLayout();renderList();renderCharts();
  });
  function refresh(){
   titleField.hidden=target.value!=='new';explanation.textContent=typeExplanation(s,source.value)+' · '+displayUnit(s,mode.value,'none',source.value);
   const full=target.value==='new'&&layout.charts.length>=12;save.disabled=full;
   const mixed=Object.entries(layout.selected).some(([id,cfg])=>{const other=catalog.find(x=>x.id===id);return id!==s.id&&cfg.chart===target.value&&other&&displayUnit(other,cfg.mode,'none',cfg.source)!==displayUnit(s,mode.value,'none',source.value)});
   warning.textContent=full?'Chart limit reached (12). Choose an existing chart or remove one.':mixed?'Different units on this chart. Use normalization in chart Settings to compare their shapes.':'';
  }
  actions.append(button('Cancel',()=>{dialog.close();dialog.remove();renderList()}),save);dialog.append(heading,caption,field('Destination',target),titleField,field('Display as',mode),field('Value type',source),explanation,warning,actions);root.append(dialog);refresh();dialog.showModal();target.focus();
  dialog.addEventListener('close',()=>{dialog.remove();renderList()});
  dialog.addEventListener('keydown',e=>{e.stopPropagation();if(e.key==='Enter'&&e.target.tagName==='INPUT'){e.preventDefault();save.click()}});
 }
 function renderList(){
  list.replaceChildren();if(!state||!layout)return;
  if(tab==='endpoints'){
   summary.textContent=state.endpoints.length+' discovered endpoints';
   for(const ep of state.endpoints){const row=el('div',undefined,'metrics-endpoint'),label=el('label'),check=el('input');check.type='checkbox';check.checked=ep.enabled;check.addEventListener('change',()=>send('endpoint',{Text:ep.id,Button:check.checked?1:0}));label.append(check,el('span',ep.name));row.append(label,el('p',ep.error|| (ep.at?ep.samples+' series · '+new Date(ep.at).toLocaleTimeString():'Waiting for first scrape'),ep.error?'metrics-error':'metrics-muted'));list.append(row)}return;
  }
  const selectedCount=Object.keys(layout.selected).length;
  const found=catalog.filter(s=>(!selectedOnly.checked||layout.selected[s.id])&&(`${metricName(s)} ${s.name} ${JSON.stringify(s.labels||{})} ${s.kind} ${s.target}`).toLowerCase().includes(searchText));
  summary.textContent=found.length+' matches · '+selectedCount+'/48 selected'+(found.length>120?' · refine search to see more':'');
  for(const s of found.slice(0,120)){
   const row=el('div',undefined,'metrics-row'),line=el('label'),check=el('input');check.type='checkbox';check.checked=!!layout.selected[s.id];check.disabled=!check.checked&&selectedCount>=48;
   check.addEventListener('change',()=>{if(check.checked){check.checked=false;addMetric(s)}else{delete layout.selected[s.id];pushLayout();renderList();renderCharts()}});
   const name=el('span',metricName(s));name.title=s.help||s.name;line.append(check,name);row.append(line);
   row.append(el('small',seriesLabel(s),'metrics-muted'));
   const details=el('details',undefined,'metrics-details');details.append(el('summary','Details & settings'),el('small',s.kind+' · '+s.target,'metrics-muted'),el('small',JSON.stringify(s.labels||{}),'metrics-labels'));row.append(details);
   const cfg=layout.selected[s.id];if(cfg){
    const controls=el('div',undefined,'metrics-series-controls');
    controls.append(select([...layout.charts.map(c=>[c.id,c.name]),['new','New chart…']],cfg.chart,v=>{if(v==='new'){addMetric(s,cfg);return}cfg.chart=v;pushLayout();renderList();renderCharts()},'Chart for '+s.name),select(modes,cfg.mode,v=>{cfg.mode=v;pushLayout();drawAll()},'Transform for '+s.name));
    row.append(controls);
    const type=select([['auto','Auto type'],['counter','Cumulative counter'],['gauge','Gauge / current value']],cfg.source||'auto',v=>{cfg.source=v;pushLayout();renderList();drawAll()},'Value type for '+s.name);details.append(type);
    row.append(el('small',typeExplanation(s,cfg.source),'metrics-muted'));
    for(const [key,label] of [['scale','Multiply'],['offset','Offset']]){const input=el('input');input.type='number';input.step='any';input.value=cfg[key];input.setAttribute('aria-label',label+' '+s.name);input.addEventListener('change',()=>{const v=Number(input.value);if(Number.isFinite(v)&&Math.abs(v)<=1e12){cfg[key]=v;pushLayout();drawAll()}});const l=el('label',label);l.append(input);details.append(l)}

   }
   list.append(row);
  }
 }
 function renderCharts(){
  charts.replaceChildren();cards.clear();if(!layout)return;
  for(const chart of layout.charts){
   const card=el('section',undefined,'metrics-chart'),bar=el('div',undefined,'metrics-chart-bar'),name=el('input');name.value=chart.name;name.setAttribute('aria-label','Chart title');name.addEventListener('change',()=>{chart.name=name.value.slice(0,80);pushLayout();renderList()});
   const normalization=select([['none','Original units'],['minmax','Min–max 0–100'],['peak','Peak = 100'],['index','First nonzero = 100'],['zscore','Z-score']],chart.norm,v=>{chart.norm=v;pushLayout();drawAll()},'Normalization for '+chart.name);
   const settings=el('details',undefined,'metrics-chart-settings');settings.append(el('summary','Settings'),normalization);bar.append(name,button('Add metrics…',()=>{preferredChart=chart.id;sidebar.hidden=false;tab='metrics';renderList();search.focus()}),settings);if(layout.charts.length>1)settings.append(button('Remove',()=>{layout.charts=layout.charts.filter(c=>c.id!==chart.id);for(const [id,cfg] of Object.entries(layout.selected)){if(cfg.chart===chart.id)delete layout.selected[id]};pushLayout();renderCharts();renderList()}));
   const canvas=el('canvas');canvas.setAttribute('role','img');canvas.setAttribute('aria-label',chart.name+' time series');const legend=el('div',undefined,'metrics-legend'),tooltip=el('div',undefined,'metrics-tooltip');tooltip.hidden=true;
   card.append(bar,canvas,legend,tooltip);charts.append(card);cards.set(chart.id,{chart,card,canvas,legend,tooltip});
   canvas.addEventListener('mousemove',e=>{const view=cards.get(chart.id);drawChart(view,e.offsetX)});canvas.addEventListener('mouseleave',()=>{tooltip.hidden=true;drawChart(cards.get(chart.id))});
  }drawAll();
 }
 function drawAll(){for(const card of cards.values())drawChart(card)}
 function drawChart(view,hover){
  if(!view||!layout)return;const {chart,canvas,legend,tooltip}=view;
  const width=Math.max(200,canvas.clientWidth),height=235,dpr=Math.min(2,global.devicePixelRatio||1);if(canvas.width!==Math.round(width*dpr)||canvas.height!==height*dpr){canvas.width=Math.round(width*dpr);canvas.height=height*dpr}
  const ctx=canvas.getContext('2d');if(!ctx)return;ctx.setTransform(dpr,0,0,dpr,0,0);ctx.clearRect(0,0,width,height);
  const l=92,r=18,t=15,b=30,w=width-l-r,h=height-t-b;
  const ids=Object.keys(layout.selected).filter(id=>layout.selected[id].chart===chart.id),rows=[];
  let end=0;for(const id of ids){const points=series.get(id)?.points||[];if(points.length)end=Math.max(end,points[points.length-1].t)}if(!end)end=Date.now();
  const start=end-layout.range*1000;
  let low=Infinity,high=-Infinity;ids.forEach((id,i)=>{const s=series.get(id)||catalog.find(s=>s.id===id);if(!s)return;const cfg=layout.selected[id],points=(s.points||[]).filter(p=>p.t>=start),values=transform(points,effectiveKind(s,cfg.source),cfg.mode,chart.norm,cfg.scale,cfg.offset);values.forEach(v=>{if(v!==null){low=Math.min(low,v);high=Math.max(high,v)}});rows.push({s,points,values,color:palette[i%palette.length],cfg})});

  const scope=s=>{const a=s.labels||{};return [a.farshmac_namespace,a.farshmac_pod,a.farshmac_container||a.container||s.target].join('/')};
  const references=[];
  if(chart.norm==='none'&&new Set(rows.map(row=>scope(row.s))).size===1)for(const row of rows){
   const unit=displayUnit(row.s,row.cfg.mode,chart.norm,row.cfg.source);
   const resource=/cpu.*usage/.test(row.s.name)&&unit==='cores'?'cpu_cores':/memory.*(usage|working_set)/.test(row.s.name)&&unit==='B'?'memory_bytes':/ephemeral_storage.*used/.test(row.s.name)&&unit==='B'?'ephemeral_storage_bytes':'';
   const chosen=new Map();
   for(const ref of state?.references||[]){
    if(!resource||ref.resource!==resource||scope(ref)!==scope(row.s)||!Number.isFinite(ref.value)||ref.value<=0)continue;
    if(!chosen.has(ref.bound)||ref.name.startsWith('farshmac_container_'))chosen.set(ref.bound,ref);
   }
   for(const ref of chosen.values()){const value=ref.value*(row.cfg.scale??1)+(row.cfg.offset??0);references.push({ref,value,color:ref.bound==='request'?'#55dd77':'#ff6666',unit});low=Math.min(low,value);high=Math.max(high,value)}
  }
  if(!Number.isFinite(low)){low=0;high=1}if(low===high){const pad=Math.abs(low)*.1||1;low-=pad;high+=pad}else{const pad=(high-low)*.07;low-=pad;high+=pad}
  if(rows.length&&rows.every(row=>row.values.every(v=>v===null||v>=0))&&chart.norm==='none'){
   const peak=Math.max(0,...rows.flatMap(row=>row.values.filter(Number.isFinite)),...references.map(ref=>ref.value));
   const bytes=rows.every(row=>['B','B/s'].includes(displayUnit(row.s,row.cfg.mode,chart.norm,row.cfg.source)));
   low=0;high=niceMaximum(peak,bytes);
  }
  const x=time=>l+(time-start)/(end-start)*w,y=v=>t+(high-v)/(high-low)*h;
  const units=new Set(rows.map(row=>displayUnit(row.s,row.cfg.mode,chart.norm,row.cfg.source))),axisUnit=units.size===1?[...units][0]:'';
  ctx.font='11px Menlo, monospace';ctx.lineWidth=1;
  for(let i=0;i<=4;i++){const yy=t+i*h/4;ctx.strokeStyle='#395575';ctx.beginPath();ctx.moveTo(l,yy);ctx.lineTo(width-r,yy);ctx.stroke();ctx.fillStyle='#adbed5';ctx.fillText(human(high-i*(high-low)/4,axisUnit),3,yy+4)}
  ctx.save();
  const divisions=w<380?2:5;
  for(let i=0;i<=divisions;i++){
   const xx=l+w*i/divisions,seconds=(end-start)/1000*(divisions-i)/divisions;
   const label=i===divisions?'Latest':seconds>=60?'−'+Number((seconds/60).toFixed(1))+' min':'−'+Math.round(seconds)+' s';
   ctx.strokeStyle='#294460';ctx.beginPath();ctx.moveTo(xx,t);ctx.lineTo(xx,t+h);ctx.stroke();
   ctx.fillStyle='#adbed5';ctx.textAlign=i===0?'left':i===divisions?'right':'center';ctx.fillText(label,xx,height-8);
  }
  ctx.textAlign='right';ctx.fillStyle='#adbed5';ctx.fillText(new Date(end).toLocaleTimeString()+' latest',width-r,11);
  ctx.restore();
  ctx.strokeStyle='#66809e';ctx.lineWidth=1;ctx.beginPath();ctx.moveTo(l,t);ctx.lineTo(l+w,t);ctx.lineTo(l+w,t+h);ctx.lineTo(l,t+h);ctx.closePath();ctx.stroke();
  ctx.save();ctx.globalAlpha=.55;
  let labelY=t-12;
  for(const {ref,value,color,unit} of references.sort((a,b)=>b.value-a.value)){
   ctx.strokeStyle=color;ctx.lineWidth=1;ctx.lineCap='round';ctx.setLineDash([.1,4]);ctx.beginPath();ctx.moveTo(l,y(value));ctx.lineTo(width-r,y(value));ctx.stroke();
   const yy=Math.max(t+10,y(value)-3,labelY+12);if(yy<t+h){ctx.fillStyle=color;ctx.fillText(ref.bound[0].toUpperCase()+ref.bound.slice(1)+' '+human(ref.value,unit),l+5,yy);labelY=yy}
  }
  ctx.restore();
  legend.replaceChildren();if(units.size>1&&chart.norm==='none')legend.append(el('span','Mixed units — choose normalization in Settings, or move metrics to separate charts.','metrics-notice'));let haveData=false;
  for(const row of rows){ctx.strokeStyle=row.color;ctx.lineWidth=1.8;ctx.beginPath();let connected=false,last=0;row.values.forEach((v,i)=>{const p=row.points[i];if(v===null){connected=false;return};if(p.t-last>Math.max(15000,(state?.interval||2)*4000))connected=false;if(connected)ctx.lineTo(x(p.t),y(v));else ctx.moveTo(x(p.t),y(v));connected=true;last=p.t;haveData=true});ctx.stroke();
   const text=el('span',seriesLabel(row.s)+'  '+human(row.values[row.values.length-1],displayUnit(row.s,row.cfg.mode,chart.norm,row.cfg.source)));text.style.color=row.color;text.title=(row.s.help||'')+' · '+row.cfg.mode;text.tabIndex=0;text.setAttribute('role','button');text.title+=' · Click to edit this metric';const edit=()=>{sidebar.hidden=false;tab='metrics';search.value=row.s.name;searchText=row.s.name.toLowerCase();renderList();search.focus()};text.addEventListener('click',edit);text.addEventListener('keydown',e=>{if(e.key==='Enter')edit()});legend.append(text);
  }
  if(!rows.length||!haveData){ctx.fillStyle='#b5c6df';ctx.font='13px Menlo, monospace';ctx.fillText(rows.length?'Waiting for samples (rates need two scrapes)':'Select metrics in the left pane',l+10,t+h/2)}
  if(hover!==undefined&&hover>=l&&hover<=width-r){const time=start+(hover-l)/w*(end-start);ctx.strokeStyle='#d7e4f3';ctx.setLineDash([3,3]);ctx.beginPath();ctx.moveTo(hover,t);ctx.lineTo(hover,t+h);ctx.stroke();ctx.setLineDash([]);tooltip.replaceChildren(el('strong',new Date(time).toLocaleTimeString()));for(const row of rows){let nearest=-1,best=Infinity;row.points.forEach((p,i)=>{if(Math.abs(p.t-time)<best){nearest=i;best=Math.abs(p.t-time)}});const item=el('div',seriesLabel(row.s)+': '+(best<=Math.max(15000,(state?.interval||2)*3000)?human(row.values[nearest],displayUnit(row.s,row.cfg.mode,chart.norm,row.cfg.source)):'—'));item.style.color=row.color;tooltip.append(item)}tooltip.hidden=false;}
 }
 global.farshmacMetrics={
 dialogOpen(){return !!root.querySelector('dialog[open]')},
 copy(cut=false){const n=document.activeElement;let text=String(global.getSelection()||'');if(n instanceof HTMLInputElement&&n.selectionStart!==null){text=n.value.slice(n.selectionStart,n.selectionEnd);if(cut){n.setRangeText('',n.selectionStart,n.selectionEnd,'end');n.dispatchEvent(new Event('input',{bubbles:true}))}}if(text)global.webkit.messageHandlers.farshmac.postMessage({Type:'clipboard',Text:text})},
 paste(text){const n=document.activeElement;if(n instanceof HTMLInputElement){if(n.selectionStart!==null)n.setRangeText(text,n.selectionStart,n.selectionEnd,'end');else n.value=text;n.dispatchEvent(new Event('input',{bubbles:true}))}},
 selectAll(){const n=document.activeElement;if(n instanceof HTMLInputElement)n.select();else global.getSelection()?.selectAllChildren(root)},
 status(text){operationStatus.textContent=text||'';},update(packet){
  if(!packet)return;const fresh=session!==packet.session;session=packet.session;state=packet;
  if(fresh){this.manual=false;this.awaitingCatalog=false;catalog=[];series.clear();layout=null;searchText='';search.value='';tab='metrics';try{const stored=JSON.parse(packet.layout||'null');if(validLayout(stored)){layout=stored;this.manual=true}}catch{}}
  if(packet.catalog)catalog=packet.catalog;
  series=new Map(packet.series.map(s=>[s.id,s]));
  if(packet.catalog&&layout&&!this.manual){layout=defaultLayout(catalog);pushLayout(true)}
  if(!layout&&catalog.length){layout=defaultLayout(catalog);pushLayout(true)}
  if(!layout)layout={charts:[{id:'custom',name:'Metrics',norm:'none'}],selected:{},range:300};
  // Populate defaults when the first successful scrape arrives after discovery.
  if(!Object.keys(layout.selected).length&&catalog.length&&fresh===false&&this.awaitingCatalog){layout=defaultLayout(catalog);pushLayout(true);this.awaitingCatalog=false;renderCharts()}
  if(!catalog.length&&fresh)this.awaitingCatalog=true;
  title.textContent=packet.title;subtitle.textContent=packet.paused?'Collection paused':packet.endpoints.filter(e=>!e.error&&e.at).length+'/'+packet.endpoints.length+' endpoints';pause.textContent=packet.paused?'Resume':'Pause';interval.value=String(packet.interval);range.value=String(layout.range);detach.hidden=packet.detached;close.textContent='Close';
  notice.textContent=[...(packet.warnings||[]),packet.limit?'Series limit reached (1024). Disable unused endpoints and rediscover.':'',packet.endpoints.some(e=>e.error)?'Some endpoints failed. See Endpoints for details.':''].filter(Boolean).join(' ');
  if(fresh||packet.catalog){renderList();renderCharts()}else{if(tab==='endpoints')renderList();drawAll()}
 }};
 if(typeof ResizeObserver!=='undefined')new ResizeObserver(()=>{if(renderFrame)cancelAnimationFrame(renderFrame);renderFrame=requestAnimationFrame(()=>{renderFrame=0;drawAll()})}).observe(root);
})(typeof window==='undefined'?globalThis:window);
