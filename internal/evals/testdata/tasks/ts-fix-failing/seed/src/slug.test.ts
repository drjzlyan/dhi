import { test } from 'node:test';
import assert from 'node:assert/strict';
import { slugify } from './slug.ts';

test('words joined by single dashes', () => {
  assert.equal(slugify('Hello World'), 'hello-world');
  assert.equal(slugify('  Hello,   World!  '), 'hello-world');
});

test('digits kept, accents dropped', () => {
  assert.equal(slugify('Release 2.0 — Café'), 'release-2-0-caf');
});
