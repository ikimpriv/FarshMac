'use strict';
(function(root) {
 const palette=['#000000','#cd0000','#00cd00','#cdcd00','#0000ee','#cd00cd','#00cdcd','#e5e5e5','#7f7f7f','#ff0000','#00ff00','#ffff00','#5c5cff','#ff00ff','#00ffff','#ffffff'];
 const theme={background:'#000000',foreground:'#c0c0c0',cursor:'#c0c0c0',selectionBackground:'#465b73'};
 ['black','red','green','yellow','blue','magenta','cyan','white','brightBlack','brightRed','brightGreen','brightYellow','brightBlue','brightMagenta','brightCyan','brightWhite'].forEach((key,i)=>theme[key]=palette[i]);
 function color(index) {
  if(index<16)return parseInt(palette[index].slice(1),16);
  if(index>=232){const n=8+(index-232)*10;return (n<<16)|(n<<8)|n;}
  const n=index-16,level=[0,95,135,175,215,255];return (level[Math.floor(n/36)]<<16)|(level[Math.floor(n/6)%6]<<8)|level[n%6];
 }
 class TerminalArchive {
  constructor(term) {
   this.term=term;this.cell=term.buffer.normal.getNullCell();this.enabled=true;this.rows=[];this.marker=null;this.mark();
   // Capture lines that an erase-history/reset would remove within one write.
   this.hooks=[term.parser.registerCsiHandler({final:'J'},p=>{if(p[0]===3)this.capture();return false;}),
    term.parser.registerEscHandler({final:'c'},()=>{this.capture();return false;}),
    term.parser.registerCsiHandler({prefix:'?',final:'h'},p=>{if(p.includes(1049)||p.includes(1047)||p.includes(47))this.capture();return false;})];
  }
  mark() {
   if(this.term.buffer.active.type!=='normal')return;
   this.marker?.dispose();this.marker=this.term.registerMarker(-this.term.buffer.active.cursorY);
  }
  rgb(cell,fg) {
   if(fg?cell.isFgRGB():cell.isBgRGB())return fg?cell.getFgColor():cell.getBgColor();
   const colors=this.term._core?._themeService?.colors; // pinned xterm 5.5; preserves OSC palette changes
   if(fg?cell.isFgPalette():cell.isBgPalette()) {
    let index=fg?cell.getFgColor():cell.getBgColor();
    if(fg&&index<8&&cell.isBold()&&this.term.options.drawBoldTextInBrightColors)index+=8;
    return colors?.ansi[index] ? colors.ansi[index].rgba>>>8 : color(index);
   }
   return colors ? (fg?colors.foreground:colors.background).rgba>>>8 : fg?0xc0c0c0:0;
  }
  line(line) {
   const runs=[],cell=this.cell;let last=null;
   for(let x=0;x<line.length;x++) {
    line.getCell(x,cell);if(cell.getWidth()===0)continue;
    const fg=this.rgb(cell,true),bg=this.rgb(cell,false);
    const flags=(cell.isBold()?1:0)|(cell.isDim()?2:0)|(cell.isItalic()?4:0)|(cell.isUnderline()?8:0)|(cell.isBlink()?16:0)|(cell.isInverse()?32:0)|(cell.isInvisible()?64:0)|(cell.isStrikethrough()?128:0);
    const text=cell.getChars()||' ';
    if(last&&last[1]===fg&&last[2]===bg&&last[3]===flags)last[0]+=text;
    else {last=[text,fg,bg,flags];runs.push(last);}
   }
   if(last&&last[2]===0&&last[3]===0){last[0]=last[0].replace(/ +$/,'');if(!last[0])runs.pop();}
   return {c:line.length,w:line.isWrapped,r:runs};
  }
  capture() {
   if(!this.enabled){this.mark();return;}
   const b=this.term.buffer.normal;
   const start=this.marker&&!this.marker.isDisposed?this.marker.line:0;
   // Growing the viewport can pull already archived rows back onto screen.
   // Keep their boundary until they scroll out again instead of duplicating them.
   if(b.baseY<start)return;
   for(let y=Math.max(0,start);y<b.baseY;y++){const line=b.getLine(y);if(line)this.rows.push(this.line(line));}
   this.mark();
  }
  resize(cols,rows){this.capture();this.term.resize(cols,rows);this.capture();}
  async write(data) {
   // A write must fit inside resident scrollback: a marker can never be trimmed
   // before capture, even for streams consisting entirely of LF / soft wraps.
   const chunk=Math.max(1,Math.min(4094,(this.term.options.scrollback??4096)-2));
   for(let at=0;at<data.length;at+=chunk) {
    await new Promise(resolve=>this.term.write(data.subarray(at,at+chunk),resolve));
    this.capture();
   }
  }
  take(){const rows=this.rows;this.rows=[];return rows;}
  dispose(){this.marker?.dispose();this.hooks.forEach(h=>h.dispose());}
 }
 root.TerminalArchive=TerminalArchive;root.FarshMacTerminalTheme=theme;
 if(typeof module!=='undefined')module.exports={TerminalArchive,theme};
})(typeof globalThis!=='undefined'?globalThis:this);
