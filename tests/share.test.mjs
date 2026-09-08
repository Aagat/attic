import test from 'node:test';
import assert from 'node:assert/strict';
import vm from 'node:vm';
import {readFileSync} from 'node:fs';
const context=vm.createContext({URL});
vm.runInContext(readFileSync(new URL('../internal/httpapi/web/share.js',import.meta.url),'utf8'),context);
for(const [name,params,want] of [
 ['URL field',{url:'https://example.com/a?x=1&y=2'},'https://example.com/a?x=1&y=2'],
 ['Android text',{text:'An article worth reading\nhttps://example.com/story'},'https://example.com/story'],
 ['title fallback',{title:'https://example.com/story'},'https://example.com/story'],
 ['prefer explicit URL',{url:'https://example.com/a',text:'https://example.com/b'},'https://example.com/a'],
 ['prose parentheses',{text:'Read this (https://example.com/a).'},'https://example.com/a'],
 ['wrapped URL',{text:'(https://example.com/a)'},'https://example.com/a'],
 ['balanced path',{text:'https://example.com/a_(b)'},'https://example.com/a_(b)'],
 ['reject credentials',{url:'https://secret:password@example.com/a'},''],
 ['reject script URL',{url:'javascript:alert(1)'},''],
 ['plain text',{text:'No link in this message'},''],
])test(name,()=>assert.equal(context.AtticShare.parse(new URLSearchParams(params)),want));
