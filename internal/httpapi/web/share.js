'use strict';
// Android commonly puts the URL in shared text rather than the URL field.
// This only prepares the form; shared URLs never authorize a submission.
globalThis.AtticShare = Object.freeze({
 parse(params) {
  for(const key of ['url','text','title']) {
   const value=params.get(key)||'';
   const matches=value.match(/https?:\/\/[^\s<>"\u0000-\u001f]+/gi)||[];
   for(let candidate of matches) {
    // Text shares may wrap links in prose; explicit URL fields keep punctuation.
    if(key!=='url')candidate=candidate.replace(/[.,;!]+$/,'');
    // Strip prose delimiters only when they are unbalanced in the URL.
    for(const [open,close] of [['(',')'],['[',']'],['{','}']]) {
     while(candidate.endsWith(close)&&candidate.split(close).length>candidate.split(open).length)candidate=candidate.slice(0,-1);
    }
    try {
     const url=new URL(candidate);
     if(['https:','http:'].includes(url.protocol)&&!url.username&&!url.password)return url.href;
    }catch{}
   }
  }
  return '';
 }
});
