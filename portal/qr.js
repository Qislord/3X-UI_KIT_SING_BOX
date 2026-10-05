/**
 * Minimal Standalone QR Code SVG Generator in Pure Vanilla JavaScript
 * Zero external dependencies. 100% offline & private.
 */
(function (global) {
  'use strict';

  // Galois Field GF(256) math for QR error correction
  const EXP = new Uint8Array(512);
  const LOG = new Uint8Array(256);
  for (let i = 0, x = 1; i < 255; i++) {
    EXP[i] = x;
    EXP[i + 255] = x;
    LOG[x] = i;
    x = (x << 1) ^ (x >= 128 ? 0x11d : 0);
  }

  function gfMul(x, y) {
    return x === 0 || y === 0 ? 0 : EXP[LOG[x] + LOG[y]];
  }

  function rsGenPoly(n) {
    let p = [1];
    for (let i = 0; i < n; i++) {
      const next = new Array(p.length + 1).fill(0);
      for (let j = 0; j < p.length; j++) {
        next[j] ^= gfMul(p[j], EXP[i]);
        next[j + 1] ^= p[j];
      }
      p = next;
    }
    return p;
  }

  function rsEncode(msg, ecLen) {
    const poly = rsGenPoly(ecLen);
    const res = new Uint8Array(msg.length + ecLen);
    res.set(msg);
    for (let i = 0; i < msg.length; i++) {
      const coef = res[i];
      if (coef !== 0) {
        for (let j = 0; j < poly.length; j++) {
          res[i + j] ^= gfMul(poly[j], coef);
        }
      }
    }
    return res.slice(msg.length);
  }

  // QR Version capacity (byte mode, EC level L)
  const CAPACITIES = [
    0, 17, 32, 53, 78, 106, 134, 154, 192, 230, 271, 321, 367, 425, 458, 520
  ];
  const EC_WORDS = [
    0, 7, 10, 15, 20, 26, 36, 40, 48, 60, 72, 80, 96, 104, 120, 132
  ];

  function selectVersion(len) {
    for (let v = 1; v < CAPACITIES.length; v++) {
      if (len <= CAPACITIES[v]) return v;
    }
    return 15; // Max supported in compact version
  }

  function makeQRMatrix(text) {
    const bytes = new TextEncoder().encode(text);
    const ver = selectVersion(bytes.length);
    const size = 17 + 4 * ver;
    const ecLen = EC_WORDS[ver];

    // Stream encoding (Byte mode: 0100)
    const bits = [];
    function push(val, len) {
      for (let i = len - 1; i >= 0; i--) bits.push((val >> i) & 1);
    }

    push(0b0100, 4); // Byte mode
    push(bytes.length, ver < 10 ? 8 : 16);
    for (const b of bytes) push(b, 8);
    push(0, Math.min(4, CAPACITIES[ver] * 8 - bits.length)); // Terminator
    while (bits.length % 8 !== 0) bits.push(0);

    const dataWords = [];
    for (let i = 0; i < bits.length; i += 8) {
      let b = 0;
      for (let j = 0; j < 8; j++) b = (b << 1) | bits[i + j];
      dataWords.push(b);
    }

    // Pad bytes
    const pad = [0xec, 0x11];
    let padIdx = 0;
    while (dataWords.length < CAPACITIES[ver]) {
      dataWords.push(pad[padIdx++ % 2]);
    }

    const ec = rsEncode(new Uint8Array(dataWords), ecLen);
    const finalWords = [...dataWords, ...ec];

    // Grid matrix
    const grid = Array.from({ length: size }, () => new Int8Array(size).fill(-1));

    function markFinder(r, c) {
      for (let dr = -1; dr <= 7; dr++) {
        for (let dc = -1; dc <= 7; dc++) {
          const nr = r + dr, nc = c + dc;
          if (nr < 0 || nr >= size || nc < 0 || nc >= size) continue;
          if (dr >= 0 && dr <= 6 && dc >= 0 && dc <= 6) {
            grid[nr][nc] = (dr === 0 || dr === 6 || dc === 0 || dc === 6 || (dr >= 2 && dr <= 4 && dc >= 2 && dc <= 4)) ? 1 : 0;
          } else {
            grid[nr][nc] = 0;
          }
        }
      }
    }

    markFinder(0, 0);
    markFinder(0, size - 7);
    markFinder(size - 7, 0);

    // Timing patterns
    for (let i = 8; i < size - 8; i++) {
      grid[6][i] = i % 2 === 0 ? 1 : 0;
      grid[i][6] = i % 2 === 0 ? 1 : 0;
    }
    grid[4 * ver + 9][8] = 1; // Dark module

    // Place data bits in zig-zag
    let bitIdx = 0;
    const finalBits = [];
    for (const w of finalWords) {
      for (let i = 7; i >= 0; i--) finalBits.push((w >> i) & 1);
    }

    for (let right = size - 1; right > 0; right -= 2) {
      if (right === 6) right--;
      for (let vert = 0; vert < size; vert++) {
        for (let j = 0; j < 2; j++) {
          const x = right - j;
          const y = ((right + 1) & 2) === 0 ? size - 1 - vert : vert;
          if (grid[y][x] === -1) {
            const b = bitIdx < finalBits.length ? finalBits[bitIdx++] : 0;
            // Apply mask (pattern 0: (row + col) % 2 === 0)
            const mask = ((x + y) % 2 === 0);
            grid[y][x] = mask ? (b ^ 1) : b;
          }
        }
      }
    }

    return grid;
  }

  function generateSVG(text, sizePx = 220) {
    try {
      const matrix = makeQRMatrix(text);
      const n = matrix.length;
      const margin = 2;
      const total = n + margin * 2;
      const cellSize = (sizePx / total).toFixed(2);

      let paths = '';
      for (let r = 0; r < n; r++) {
        for (let c = 0; c < n; c++) {
          if (matrix[r][c] === 1) {
            const x = (c + margin) * cellSize;
            const y = (r + margin) * cellSize;
            paths += `M${x},${y}h${cellSize}v${cellSize}h-${cellSize}z `;
          }
        }
      }

      return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${sizePx} ${sizePx}" width="${sizePx}" height="${sizePx}">
        <rect width="100%" height="100%" fill="#ffffff" rx="8"/>
        <path d="${paths}" fill="#0b0f19"/>
      </svg>`;
    } catch (e) {
      return `<div style="padding:20px;word-break:break-all;color:#000;font-size:12px;">${text}</div>`;
    }
  }

  global.QRGenerator = { generateSVG };
})(typeof window !== 'undefined' ? window : globalThis);
