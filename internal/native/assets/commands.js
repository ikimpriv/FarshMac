'use strict';
// Letter of a Cmd/Ctrl shortcut. A Latin e.key wins (Dvorak, Colemak); for
// non-Latin layouts fall back to the physical key code.
function shortcutLetter(e){
 const key=e.key||'';
 if(/^[A-Za-z]$/.test(key))return key.toLowerCase();
 if(/^Key[A-Z]$/.test(e.code||''))return e.code.slice(3).toLowerCase();
 return key.toLowerCase();
}
class CommandHistory {
 constructor(send,focus) {
  this.send=send;this.focus=focus;this.visible=false;
  this.el=document.getElementById('commands');
  this.list=document.getElementById('command-list');
  this.edit=document.getElementById('command-edit');
  this.caption=document.getElementById('command-caption');
  this.list.onchange=()=>{this.edit.value=this.items[this.list.selectedIndex]??'';};
  this.list.ondblclick=()=>this.run();
  document.getElementById('command-run').onclick=()=>this.run();
  document.getElementById('command-cancel').onclick=()=>this.close();
 }
 open(){this.visible=true;this.el.hidden=false;this.items=[];this.list.replaceChildren();this.edit.value='';this.caption.textContent='Loading command history…';this.requested=false;this.request();this.list.focus();}
 request(){if(this.ready&&!this.requested){this.requested=true;this.send({Type:'commandHistory'});}else if(!this.ready){this.caption.textContent='Waiting for shell prompt… · Esc: cancel';}}
 copy(cut=false){const text=this.edit.value.slice(this.edit.selectionStart,this.edit.selectionEnd);if(text)this.send({Type:'clipboard',Text:text});if(cut)this.edit.setRangeText('',this.edit.selectionStart,this.edit.selectionEnd,'end');}
 paste(text){this.edit.focus();this.edit.setRangeText(text,this.edit.selectionStart,this.edit.selectionEnd,'end');}
 selectAll(){this.edit.focus();this.edit.select();}
 update(items,ready,error){
  this.ready=ready;
  document.getElementById('command-run').disabled=!ready;
  if(!this.visible)return;
  if(!this.requested)this.request();
  if(items){
   this.items=items;this.list.replaceChildren();
   for(const command of items){const option=document.createElement('option');option.textContent=command.replace(/\n/g,' ↵ ');this.list.append(option);}
   this.list.selectedIndex=items.length?0:-1;this.edit.value=items[0]??'';
   this.caption.textContent=error?error+' · Esc: cancel':items.length?'Command history — Enter: run · F4: edit · Esc: cancel':'Command history is empty';
  }
 }
 close(){this.visible=false;this.el.hidden=true;this.focus();}
 run(){if(!this.ready||!this.edit.value.trim())return;this.send({Type:'commandHistoryRun',Text:this.edit.value});this.close();}
 key(e){
  if(!this.visible)return false;
  if(e.metaKey&&['c','x','v','a'].includes(shortcutLetter(e))){e.preventDefault();const key=shortcutLetter(e);if(key==='c'||key==='x')this.copy(key==='x');else if(key==='a')this.selectAll();else this.send({Type:'pasteRequest'});}
  else if(e.key==='Escape'){e.preventDefault();this.close();}
  else if(e.key==='F4'){e.preventDefault();this.edit.focus();}
  else if(e.key==='Enter'&&!e.shiftKey){e.preventDefault();this.run();}
  return true;
 }
}
