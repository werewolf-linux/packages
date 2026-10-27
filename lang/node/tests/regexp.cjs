'use strict';
const assert = require('node:assert/strict');
const supplementary = '\u{1df95}';
const hasUnicode18 = supplementary.toUpperCase() === 'SS';
for (const [a, b, expected] of [
  ['a', 'A', true],
  ['k', '\u212a', true],
  ['\u00df', '\u1e9e', true],
  ['\u00df', supplementary, hasUnicode18],
  ['\u1e9e', supplementary, hasUnicode18],
]) {
  for (const [pattern, subject] of [[a, b], [b, a]]) {
    for (const flags of ['iu', 'iv']) {
      for (const body of [pattern, `[${pattern}]`, `(?:${pattern})`, `${pattern}{1}`]) {
        const re = new RegExp(`^${body}$`, flags);
        assert.equal(re.test(subject), expected, `${re} against ${subject}`);
      }
      const behind = new RegExp(`(?<=^${pattern})x$`, flags);
      assert.equal(behind.test(`${subject}x`), expected, `${behind}`);
    }
  }
}
for (const bmp of ['\u00df', '\u1e9e']) {
  for (const pattern of [bmp, `[${bmp}]`, `(?:${bmp})`]) {
    assert.equal(new RegExp(`^${pattern}$`, 'i').test(supplementary), false);
  }
}
console.log('PASS: Unicode case folding preserves code-unit and code-point modes');
