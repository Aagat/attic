import puppeteer from 'puppeteer';
import { Readability } from '@mozilla/readability';
import { JSDOM } from 'jsdom';
import yargs from 'yargs';
import { hideBin } from 'yargs/helpers';

// Parse command line arguments
const argv = yargs(hideBin(process.argv))
  .option('url', {
    description: 'URL to extract content from',
    type: 'string',
    demandOption: true
  })
  .help()
  .argv;

async function extractContent(url) {
  try {
    // Launch browser
    const browser = await puppeteer.launch({
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

    try {
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

      // Set request interception to block unnecessary resources
      await page.setRequestInterception(true);
      page.on('request', (request) => {
        const resourceType = request.resourceType();
        if (['image', 'stylesheet', 'font', 'media'].includes(resourceType)) {
          request.abort();
        } else {
          request.continue();
        }
      });

      // Navigate to URL with timeout
      await page.goto(url, {
        waitUntil: 'networkidle0',
        timeout: 30000
      });

      // Wait for content to load
      await page.waitForSelector('body', { timeout: 5000 });

      // Get page content
      const html = await page.content();

      // Parse content with Readability
      const dom = new JSDOM(html, { url });
      const reader = new Readability(dom.window.document);
      const article = reader.parse();

      if (!article) {
        throw new Error('Failed to parse article content');
      }

      // Return result as JSON
      console.log(JSON.stringify({
        content: article.textContent,
        title: article.title,
        byline: article.byline,
        excerpt: article.excerpt
      }));

    } finally {
      await browser.close();
    }
  } catch (error) {
    // Return error as JSON
    console.log(JSON.stringify({
      error: error.message
    }));
    process.exit(1);
  }
}

// Run the extraction
extractContent(argv.url); 
