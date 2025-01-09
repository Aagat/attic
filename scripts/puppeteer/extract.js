import puppeteer from 'puppeteer';
import yargs from 'yargs';
import { hideBin } from 'yargs/helpers';
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';

// Get the directory name of the current module
const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

// Parse command line arguments
const argv = yargs(hideBin(process.argv))
  .option('url', {
    description: 'URL to extract content from',
    type: 'string',
    demandOption: true
  })
  .help()
  .argv;

// Read both Readability files
const readabilityJs = fs.readFileSync(
  path.join(__dirname, 'node_modules', '@mozilla', 'readability', 'Readability.js'),
  'utf8'
);
const readerableJs = fs.readFileSync(
  path.join(__dirname, 'node_modules', '@mozilla', 'readability', 'Readability-readerable.js'),
  'utf8'
);

async function extractContent(url) {
  let browser;
  try {
    // Launch browser
    browser = await puppeteer.launch({
      headless: 'new',
      args: [
        '--no-sandbox',
        '--disable-setuid-sandbox',
        '--disable-dev-shm-usage',
        '--disable-accelerated-2d-canvas',
        '--disable-gpu',
        '--disable-notifications',
        '--disable-extensions',
        '--disable-infobars',
        '--disable-web-security',
        '--disable-features=IsolateOrigins,site-per-process',
      ]
    });

    // Create new page
    const page = await browser.newPage();

    // Set viewport
    await page.setViewport({
      width: 1920,
      height: 1080
    });

    // Set user agent
    await page.setUserAgent('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36');

    // Enable JavaScript
    await page.setJavaScriptEnabled(true);

    // Navigate to URL with timeout
    await page.goto(url, {
      waitUntil: 'networkidle0',
      timeout: 30000
    });

    // Wait for content to load
    await page.waitForSelector('body', { timeout: 5000 });

    // Capture screenshot first
    const screenshot = await page.screenshot({
      type: 'jpeg',
      quality: 80,
      fullPage: true,
      encoding: 'base64'
    });

    // Inject both Readability scripts
    await page.evaluate(readerableJs);
    await page.evaluate(readabilityJs);

    // Check if the page is probably readable first
    const isProbablyReaderable = await page.evaluate(() => {
      return isProbablyReaderable(document, {
        minContentLength: 140,
        minScore: 20
      });
    });

    // Run Readability in the browser context
    const article = await page.evaluate(() => {
      const documentClone = document.cloneNode(true);
      const reader = new Readability(documentClone);
      const article = reader.parse();
      return article;
    });

    // Return result as JSON with whatever content we got
    console.log(JSON.stringify({
      content: article?.content || '',
      textContent: article?.textContent || '',
      title: article?.title || '',
      byline: article?.byline || '',
      excerpt: article?.excerpt || '',
      length: article?.length || 0,
      siteName: article?.siteName || '',
      isReadable: isProbablyReaderable && article !== null,
      screenshot: screenshot
    }));

  } catch (error) {
    // Return error as JSON with the screenshot we already captured
    console.log(JSON.stringify({
      error: error.message,
      screenshot: screenshot || '',
      isReadable: false
    }));
    process.exit(1);
  } finally {
    if (browser) {
      await browser.close();
    }
  }
}

// Run the extraction
extractContent(argv.url); 
