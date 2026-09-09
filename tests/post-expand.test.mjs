import { readFileSync } from 'node:fs';
import assert from 'node:assert/strict';
import { chromium } from '../ui/node_modules/@playwright/test/index.mjs';
const expression=readFileSync(new URL('../internal/acquisition/post_expand.js',import.meta.url),'utf8');
const browser=await chromium.launch({executablePath:process.env.BROWSER_EXECUTABLE||'/usr/bin/chromium',headless:true,args:['--no-sandbox']});
try {
 const page=await browser.newPage();
 const post=(id,behavior)=>`<article data-testid="tweet"><a href="/author/status/${id}"><time>September 9</time></a><p>Opening text</p><button data-testid="tweet-text-show-more-link" onclick="${behavior}">Show more</button></article>`;
 let fixture=post('123',"this.previousElementSibling.textContent='Full post including the final paragraph';this.remove()")+post('456',"window.wrongPost=true");
 await page.route('https://x.com/**',route=>route.fulfill({contentType:'text/html',body:fixture}));
 await page.goto('https://x.com/author/status/123?s=46');
 assert.equal(await page.evaluate(expression),false);
 assert.match(await page.locator('article').first().innerText(),/final paragraph/);
 assert.equal(await page.evaluate(()=>!!window.wrongPost),false);
 console.log('Requested post expanded; other posts untouched');
 fixture=post('123',"window.attempts=(window.attempts||0)+1");
 await page.goto('https://x.com/author/status/123');
 assert.equal(await page.evaluate(expression),true);
 assert.equal(await page.evaluate(()=>window.attempts),3);
 console.log('Unexpandable post remains marked truncated after three attempts');
 fixture=post('456',"window.wrongPost=true").replace('</article>',post('123',"window.wrongPost=true")+'</article>');
 await page.goto('https://x.com/author/status/789');
 assert.equal(await page.evaluate(expression),false);
 assert.equal(await page.evaluate(()=>!!window.wrongPost),false);
 console.log('Missing requested post cannot expand a quoted or unrelated post');
}finally{await browser.close();}
