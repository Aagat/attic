import http from "node:http";
// Loopback gives the browser a secure context for PWA APIs. Keep the original
// Host and Origin so the real server still enforces its normal CSRF checks.
http
  .createServer((req, res) => {
    const upstream = http.request(
      {
        hostname: "attic",
        port: 8080,
        path: req.url,
        method: req.method,
        headers: req.headers,
      },
      (r) => {
        res.writeHead(r.statusCode, r.headers);
        r.pipe(res);
      },
    );
    upstream.on("error", () => {
      res.writeHead(502);
      res.end();
    });
    req.pipe(upstream);
  })
  .listen(4174, "127.0.0.1");
