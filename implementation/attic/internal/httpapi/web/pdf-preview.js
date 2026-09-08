'use strict';
// Owns a single canvas and PDF worker. No browser PDF plugin or blob iframe.
class AtticPDFPreview {
 constructor() {
  this.viewer=document.getElementById('pdf-viewer');this.canvas=document.getElementById('pdf-canvas');
  this.status=document.getElementById('reader-status');this.input=document.getElementById('pdf-page');
  this.generation=0;this.renderVersion=0;this.zoom=1;
  document.getElementById('pdf-prev').onclick=()=>this.go(this.number-1);
  document.getElementById('pdf-next').onclick=()=>this.go(this.number+1);
  this.input.onchange=()=>this.go(Number(this.input.value));
  document.getElementById('pdf-smaller').onclick=()=>this.setZoom(this.zoom/1.25);
  document.getElementById('pdf-larger').onclick=()=>this.setZoom(this.zoom*1.25);
  this.observer=new ResizeObserver(()=>{clearTimeout(this.resizeTimer);this.resizeTimer=setTimeout(()=>{if(this.doc)this.render();},150);});
  this.observer.observe(this.viewer);
 }
 close() {
  this.generation++;this.renderVersion++;clearTimeout(this.resizeTimer);
  if(this.renderTask)this.renderTask.cancel();this.renderTask=null;
  const task=this.loadingTask;this.loadingTask=null;this.doc=null;
  if(task)task.destroy().catch(()=>{});
  this.canvas.width=0;this.canvas.height=0;delete this.canvas.dataset.page;
  document.getElementById('pdf-controls').hidden=true;this.viewer.hidden=true;
 }
 async load(blob) {
  this.close();const generation=this.generation;
  const pdfjs=await import('/pdfjs/pdf.mjs');
  const data=await blob.arrayBuffer();if(generation!==this.generation)return;
  pdfjs.GlobalWorkerOptions.workerSrc='/pdfjs/pdf.worker.mjs';
  this.loadingTask=pdfjs.getDocument({data,isEvalSupported:false,isOffscreenCanvasSupported:false,cMapUrl:'/pdfjs/cmaps/',cMapPacked:true,standardFontDataUrl:'/pdfjs/standard_fonts/',wasmUrl:'/pdfjs/wasm/'});
  const doc=await this.loadingTask.promise;if(generation!==this.generation)return;
  this.doc=doc;this.number=1;this.zoom=1;this.input.max=doc.numPages;
  document.getElementById('pdf-total').textContent=String(doc.numPages);
  document.getElementById('pdf-controls').hidden=false;this.viewer.hidden=false;
  await this.render();
 }
 go(number) {if(!this.doc)return;this.number=Math.max(1,Math.min(this.doc.numPages,Math.round(number)||1));this.viewer.scrollTop=0;this.render();}
 setZoom(zoom) {this.zoom=Math.max(1,Math.min(2.5,zoom));this.render();}
 async render() {
  if(!this.doc)return;
  const doc=this.doc,generation=this.generation,version=++this.renderVersion,number=this.number;
  const oldTask=this.renderTask;
  if(oldTask){oldTask.cancel();try{await oldTask.promise;}catch{}}
  if(generation!==this.generation||version!==this.renderVersion)return;
  this.status.textContent='Rendering page '+number+'…';
  try {
   const page=await doc.getPage(number);
   if(generation!==this.generation||version!==this.renderVersion)return;
   const base=page.getViewport({scale:1});
   const cssScale=Math.max(1,this.viewer.clientWidth-24)/base.width*this.zoom;
   const css=page.getViewport({scale:cssScale});
   // Cap the canvas at four megapixels, including on high-density phones.
   const ratio=Math.min(devicePixelRatio||1,2,Math.sqrt(4000000/(css.width*css.height)));
   const viewport=page.getViewport({scale:cssScale*ratio});
   this.canvas.width=Math.ceil(viewport.width);this.canvas.height=Math.ceil(viewport.height);
   this.canvas.style.width=css.width+'px';this.canvas.style.height=css.height+'px';
   this.renderTask=page.render({canvasContext:this.canvas.getContext('2d'),viewport});
   await this.renderTask.promise;
   if(generation!==this.generation||version!==this.renderVersion)return;
   this.renderTask=null;this.canvas.dataset.page=String(number);
   this.canvas.setAttribute('aria-label','PDF page '+number+' of '+doc.numPages);
   this.input.value=number;document.getElementById('pdf-prev').disabled=number===1;
   document.getElementById('pdf-next').disabled=number===doc.numPages;
   document.getElementById('pdf-smaller').disabled=this.zoom<=1;
   document.getElementById('pdf-larger').disabled=this.zoom>=2.5;
   this.status.textContent='';page.cleanup();
  }catch(error){
   if(generation===this.generation&&version===this.renderVersion&&error.name!=='RenderingCancelledException')this.status.textContent='Preview could not be rendered. You can still download or share the PDF.';
  }
 }
}
globalThis.AtticPDFPreview=AtticPDFPreview;
