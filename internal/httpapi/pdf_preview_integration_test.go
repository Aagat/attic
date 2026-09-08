package httpapi

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"attic/internal/application"

	"github.com/chromedp/chromedp"
)

func previewFixture() []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 446 595] /Resources << /Font << /F1 5 0 R >> >> /Contents 6 0 R >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 446 595] /Resources << /Font << /F1 5 0 R >> >> /Contents 7 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	for _, text := range []string{"First page of a readable article", "Second page of a readable article"} {
		stream := "BT /F1 16 Tf 40 530 Td (" + text + ") Tj ET\n"
		objects = append(objects, fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(stream), stream))
	}
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	for i, object := range objects {
		offsets = append(offsets, pdf.Len())
		fmt.Fprintf(&pdf, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	start := pdf.Len()
	fmt.Fprintf(&pdf, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&pdf, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&pdf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF", len(offsets), start)
	return pdf.Bytes()
}

func TestPDFPreviewBrowserIntegration(t *testing.T) {
	if os.Getenv("ATTIC_WEB_INTEGRATION") != "1" {
		t.Skip("ATTIC_WEB_INTEGRATION is not set")
	}
	pdf := previewFixture()
	if path := os.Getenv("ATTIC_PREVIEW_TEST_PDF"); path != "" {
		var err error
		pdf, err = os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
	}
	server, archive, worker := testServer(t)
	if _, err := archive.SubmitURL(context.Background(), application.SubmitURLRequest{URL: "https://example.com/article"}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	var delayArtifact atomic.Bool
	artifactStarted := make(chan struct{}, 1)
	artifactCancelled := make(chan struct{}, 1)
	httpServer := httptest.NewServer(browserItemsFixture(t, server, archive, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/jobs/job-http/artifact" && server.authorized(r) {
			if delayArtifact.Load() {
				artifactStarted <- struct{}{}
				<-r.Context().Done()
				artifactCancelled <- struct{}{}
				return
			}
			w.Header().Set("Content-Type", "application/pdf")
			w.Write(pdf)
			return
		}
		server.ServeHTTP(w, r)
	})))
	defer httpServer.Close()
	options := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.ExecPath("/usr/bin/chromium"))
	allocator, cancel := chromedp.NewExecAllocator(context.Background(), options...)
	defer cancel()
	ctx, cancel := chromedp.NewContext(allocator, chromedp.WithErrorf(func(string, ...any) {}))
	defer cancel()
	ctx, cancel = context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(390, 844), chromedp.Navigate(httpServer.URL), chromedp.WaitVisible("#login"),
		chromedp.SendKeys("#access-key", "secret-token"),
		chromedp.Click("#login-form button"),
		chromedp.WaitVisible(".article .actions button"),
		chromedp.Click(".article .actions button"),
		chromedp.Poll(`document.getElementById('pdf-canvas').dataset.page==='1'`, nil),
	); err != nil {
		var status string
		chromedp.Run(ctx, chromedp.Text("#reader-status", &status))
		t.Fatalf("first page: %v; status: %s", err, status)
	}
	var painted bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(()=>{const c=document.getElementById('pdf-canvas');const p=c.getContext('2d').getImageData(0,0,c.width,c.height).data;return p.some((value,i)=>i%4!==3&&value<150);})()`, &painted)); err != nil || !painted {
		t.Fatalf("blank canvas: %v", err)
	}
	// Once a page is painted, idle layout must not keep retriggering rendering.
	var before, after int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.readerStatusChanges=0; window.readerObserver=new MutationObserver(()=>window.readerStatusChanges++);window.readerObserver.observe(document.getElementById('reader-status'),{childList:true});window.readerStatusChanges`, &before), chromedp.Sleep(time.Second), chromedp.Evaluate(`window.readerStatusChanges`, &after)); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("idle PDF preview repeatedly rendered: version %d -> %d", before, after)
	}
	// Phone browser chrome and expanding help change height, not render scale.
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(390, 700), chromedp.Sleep(400*time.Millisecond), chromedp.Evaluate(`window.readerStatusChanges`, &after)); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("height-only resize restarted rendering: %d -> %d", before, after)
	}
	var previousWidth int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('pdf-canvas').width`, &previousWidth), chromedp.EmulateViewport(430, 700), chromedp.Poll(fmt.Sprintf(`document.getElementById('pdf-canvas').width !== %d && document.getElementById('reader-status').textContent === ''`, previousWidth), nil)); err != nil {
		t.Fatalf("width resize did not render: %v", err)
	}
	if path := os.Getenv("ATTIC_PREVIEW_SCREENSHOT"); path != "" {
		var png []byte
		if err := chromedp.Run(ctx, chromedp.CaptureScreenshot(&png)); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, png, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := chromedp.Run(ctx, chromedp.Click("#pdf-next"), chromedp.Poll(`document.getElementById('pdf-canvas').dataset.page==='2'`, nil),
		chromedp.Evaluate(`document.getElementById('pdf-page').value=document.getElementById('pdf-page').max;document.getElementById('pdf-page').dispatchEvent(new Event('change'))`, nil),
		chromedp.Poll(`document.getElementById('pdf-canvas').dataset.page===document.getElementById('pdf-page').max`, nil),
		chromedp.Click("#pdf-larger"), chromedp.Poll(`document.getElementById('reader-status').textContent === '' && parseFloat(document.getElementById('pdf-canvas').style.width)>document.getElementById('pdf-viewer').clientWidth`, nil),
		chromedp.Click("#close-reader"), chromedp.Poll(`document.getElementById('pdf-canvas').width===0 && !document.getElementById('reader').open`, nil),
	); err != nil {
		t.Fatal(err)
	}
	// Closing the reader must cancel its pending download, not just hide a dialog.
	delayArtifact.Store(true)
	if err := chromedp.Run(ctx, chromedp.Click(".article .actions button")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-artifactStarted:
	case <-ctx.Done():
		t.Fatal("artifact request did not start")
	}
	if err := chromedp.Run(ctx, chromedp.Click("#close-reader")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-artifactCancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("closing reader left download running")
	}
	if err := chromedp.Run(ctx, chromedp.Poll(`!document.getElementById('reader').open && document.getElementById('download').hidden && document.getElementById('pdf-canvas').width===0`, nil)); err != nil {
		t.Fatal(err)
	}
	t.Log("Real PDF rendered to canvas at phone width; next page, last page, zoom and close passed")
}
