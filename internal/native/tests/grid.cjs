'use strict';
// Exercise the exact bundled parser without a browser or a separate terminal
// dependency. ANSI is supplied by the Go Grid renderer's regression test.
global.self = global;
const {Terminal} = require('../assets/vendor/xterm.js');
let input = '';
process.stdin.setEncoding('utf8');
process.stdin.on('data', chunk => input += chunk);
process.stdin.on('end', async () => {
  const {width, height, frames} = JSON.parse(input);
  const terminal = new Terminal({cols: width, rows: height, allowProposedApi: true});
  try {
    for (const frame of frames) {
      await new Promise(resolve => terminal.write(frame.ansi, resolve));
      for (let y = 0; y < height; y++) {
        const line = terminal.buffer.active.getLine(y);
        for (let x = 0; x < width; x++) {
          const want = frame.cells[y * width + x];
          const cell = line.getCell(x);
          const got = cell.getChars();
          if (want === '') {
            // xterm may use either a wide-cell continuation or a space for
            // characters that its older width table considers narrow.
            if (got === '' || got === ' ') continue;
          }
          if (got !== want) {
            throw new Error(`Cell ${x},${y}: ${JSON.stringify(got)} != ${JSON.stringify(want)}`);
          }
        }
      }
    }
  } finally { terminal.dispose(); }
}).on('error', error => { throw error; });
