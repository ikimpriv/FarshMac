const assert=require('node:assert/strict');
const {context,sent,writes,terminals}=require('./harness.cjs')();
const settle=()=>new Promise(resolve=>setImmediate(resolve));
async function frame(state){
 context.window.farshmacUpdate({mode:'terminal',cols:80,rows:25,...state});
 for(let i=0;i<10;i++){while(writes.length)writes.shift()();await settle();}
}
(async()=>{
 await frame({terminalContext:'local',terminal:Buffer.from('local').toString('base64')});
 const local=terminals[1];
 local.selection='retained local screen';
 await frame({terminalContext:'remote:a',terminal:Buffer.from('remote').toString('base64')});
 const remote=terminals[2];remote.selection='retained remote screen';
 await frame({terminalContext:'local',backgroundTerminals:[{Context:'remote:a',Data:Buffer.from('background output').toString('base64')}]});
 context.window.farshmacCopy();
 assert.equal(sent.at(-1).Text,'retained local screen');
 await frame({terminalContext:'remote:a'});
 context.window.farshmacCopy();
 assert.equal(sent.at(-1).Text,'retained remote screen');
 assert.equal(terminals.length,3,'returning to a context must reuse its terminal');
 const output=t=>t.output.map(b=>Buffer.from(b).toString()).join('');
 assert.equal(output(local),'local');
 assert.equal(output(remote),'remotebackground output');
 await frame({terminalContext:'local'});
 remote.data('DSR response');
 assert.equal(sent.at(-1).Context,'remote:a','background replies must retain their context');
 console.log('terminal context screens, background output and selection isolation: OK');
})().catch(e=>{console.error(e);process.exitCode=1});
