'use strict';
const assert=require('node:assert/strict');
const {context,sent,elements,docListeners}=require('./harness.cjs')();
const settle=()=>new Promise(r=>setImmediate(r));
const key=(key,extra={})=>docListeners.keydown({key,preventDefault(){},...extra});
(async()=>{
 context.window.farshmacUpdate({mode:'terminal',shellReady:true});await settle();
 context.window.farshmacToggleHistory();assert.equal(sent.at(-1).Type,'commandHistory');
 const commands=['echo "first"\nprintf second','ls -la'];
 context.window.farshmacUpdate({mode:'terminal',shellReady:true,commands});await settle();
 assert.equal(elements.get('command-list').children.length,2);
 assert.equal(elements.get('command-edit').value,commands[0]);
 elements.get('command-list').selectedIndex=1;elements.get('command-list').onchange();
 assert.equal(elements.get('command-edit').value,commands[1]);
 elements.get('command-edit').value='ls -lh';key('Enter');
 assert.deepEqual(JSON.parse(JSON.stringify(sent.at(-1))),{Type:'commandHistoryRun',Text:'ls -lh'});
 assert.equal(elements.get('commands').hidden,true);
 context.window.farshmacToggleHistory();const n=sent.length;key('Escape');assert.equal(sent.length,n,'cancel must not execute');
 context.window.farshmacUpdate({mode:'terminal',shellReady:false});await settle();
 context.window.farshmacToggleHistory();assert.equal(sent.length,n+1,'busy shell must not receive history request');
 context.window.farshmacUpdate({mode:'terminal',shellReady:true});await settle();assert.equal(sent.filter(m=>m.Type==='commandHistory').length,3);
 // Cmd+O closes the overlay and still reaches Go (also in a non-Latin layout).
 key('Escape');context.window.farshmacToggleHistory();assert.equal(elements.get('commands').hidden,false);
 key('щ',{metaKey:true,code:'KeyO'});
 assert.equal(elements.get('commands').hidden,true);assert.equal(sent.at(-1).Type,'key');assert.equal(sent.at(-1).Key,'o');assert.equal(sent.at(-1).Meta,true);
 // A failed request or exited shell answers with an empty list and a reason.
 context.window.farshmacToggleHistory();
 context.window.farshmacUpdate({mode:'terminal',shellReady:true,commands:[],commandsError:'Shell exited; history unavailable'});await settle();
 assert.equal(elements.get('command-caption').textContent,'Shell exited; history unavailable · Esc: cancel');
 key('Escape');assert.equal(elements.get('commands').hidden,true);
 console.log('Command history: shell snapshot, selection, editing, run, cancel and busy shell: OK');
})().catch(e=>{console.error(e);process.exitCode=1});
