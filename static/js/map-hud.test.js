/**
 * Unit tests for MapHud helpers (Node.js assert).
 * Run: node static/js/map-hud.test.js
 */
'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const vm = require('vm');

const src = fs.readFileSync(path.join(__dirname, 'map-hud.js'), 'utf8');
const sandbox = { window: {}, document: { getElementById: function() { return null; } } };
vm.createContext(sandbox);
vm.runInContext(src, sandbox);

const elTrendArrow = sandbox.window.MapHudElTrendArrow;
assert.ok(typeof elTrendArrow === 'function', 'MapHudElTrendArrow exported');

const aos = 1_000_000;
const tca = 1_300_000;
const los = 1_600_000;

assert.strictEqual(elTrendArrow(1_100_000, tca, aos, los), '\u2191', 'before TCA → up');
assert.strictEqual(elTrendArrow(1_500_000, tca, aos, los), '\u2193', 'after TCA → down');
assert.strictEqual(elTrendArrow(tca, tca, aos, los), '\u2014', 'at TCA → dash');
assert.strictEqual(elTrendArrow(900_000, tca, aos, los), '', 'before AOS → empty');
assert.strictEqual(elTrendArrow(1_700_000, tca, aos, los), '', 'after LOS → empty');
assert.strictEqual(elTrendArrow(1_100_000, 0, aos, los), '', 'no TCA → empty');

console.log('map-hud.test.js: all passed');
