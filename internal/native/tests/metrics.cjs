'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict'),path=require('node:path');
const context={};vm.createContext(context);vm.runInContext(fs.readFileSync(path.join(__dirname,'../assets/metrics.js'),'utf8'),context);
const m=context.FarMetricsMath,p=[{v:10,r:null},{v:20,r:5},{v:null,r:null},{v:30,r:2}];
const arr=v=>Array.from(v);
assert.deepEqual(arr(m.transform(p,'counter','auto','none')),[null,5,null,2]);
assert.deepEqual(arr(m.transform(p,'gauge','auto','minmax')),[0,50,null,100]);
assert.deepEqual(arr(m.transform(p,'gauge','raw','index')),[100,200,null,300]);
assert.deepEqual(arr(m.transform(p,'gauge','raw','none',2,1)),[21,41,null,61]);
assert.deepEqual(arr(m.transform([{v:0},{v:0}],'gauge','raw','index')),[null,null]);
assert.deepEqual(arr(m.transform([{v:5},{v:5}],'gauge','raw','zscore')),[0,0]);
const layout=m.defaultLayout([{id:'cpu',name:'podtoast_cpu_usage_seconds_total'},{id:'mem',name:'podtoast_memory_usage_bytes'},{id:'net',name:'podtoast_pod_network_receive_bytes_total'}]);
assert.equal(Object.keys(layout.selected).length,1);assert.equal(layout.charts.length,1);
console.log('metrics transforms, null gaps, normalization, mixed-series defaults: OK');

const scoped=m.defaultLayout([
 {id:'a',name:'podtoast_cpu_usage_seconds_total',labels:{farshmac_pod:'one',farshmac_container:'app'}},
 {id:'b',name:'podtoast_cpu_usage_seconds_total',labels:{farshmac_pod:'two',farshmac_container:'app'}},
 {id:'dup',name:'kubernetes_container_cpu_usage_cores',labels:{farshmac_pod:'one',farshmac_container:'app'}},
 {id:'mem',name:'podtoast_memory_usage_bytes'}]);
assert.deepEqual(Object.keys(scoped.selected).sort(),['a','b']);

assert.equal(m.human(1073741824,'B'),'1 GiB');
assert.equal(m.human(1048576,'B/s'),'1 MiB/s');
assert.equal(m.human(.25,'cores'),'250 mCPU');
assert.equal(m.human(.012,'s'),'12 ms');
assert.equal(m.human(.5,'ratio'),'50%');
assert.equal(m.displayUnit({name:'podtoast_cpu_usage_seconds_total',kind:'counter'}),'cores');
assert.equal(m.displayUnit({name:'podtoast_cpu_usage_seconds_total',kind:'counter'},'raw'),'s');
assert.equal(m.displayUnit({name:'podtoast_memory_usage_bytes',kind:'gauge'},'auto','minmax'),'%');
assert.equal(m.displayUnit({name:'podtoast_pod_network_receive_bytes_total',kind:'counter'}),'B/s');
assert.equal(m.human(null,'B'),'—');
const media={name:'webrtc_sessions_outbound_rtp_packets',kind:'untyped'};
assert.equal(m.effectiveKind(media),'counter');
assert.equal(m.effectiveKind({name:'webrtc_sessions_inbound_rtp_packets_jitter',kind:'untyped'}),'untyped');
assert.equal(m.effectiveKind({name:'memory_bytes',kind:'gauge'}),'gauge');
assert.equal(m.effectiveKind(media,'gauge'),'gauge');
assert.equal(m.displayUnit(media),'packets/s');
const cumulative=[{t:0,v:100,r:null,d:null},{t:2000,v:150,r:25,d:50},{t:6000,v:10,r:-35,d:-140},{t:8000,v:20,r:null,d:null}];
assert.deepEqual(arr(m.transform(cumulative,'counter','rate','none')),[null,25,2.5,null]);
assert.deepEqual(arr(m.transform(cumulative,'counter','delta','none')),[null,50,10,null]);
assert.deepEqual(arr(m.transform(cumulative,'gauge','delta','none')),[null,50,-140,null]);
assert.deepEqual(arr(m.transform(cumulative,'counter','raw','none')),[100,150,10,20]);
console.log('Untyped counters, explicit value types, interval changes and resets: OK');
