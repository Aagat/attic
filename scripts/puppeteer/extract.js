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
      fullPage: true
    });
    const screenshotBase64 = Buffer.from(screenshot).toString('base64');

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
      content: article.content,
      textContent: article.textContent,
      title: article.title,
      byline: article.byline,
      excerpt: article.excerpt,
      length: article.length,
      siteName: article.siteName,
      isReadable: true,
      screenshot: screenshotBase64
    }));

  } catch (error) {
    // If we have a browser page, try to get a screenshot even if readability failed
    let errorScreenshot = '';
    if (browser) {
      try {
        const page = (await browser.pages())[0];
        if (page) {
          const screenshot = await page.screenshot({
            type: 'jpeg',
            quality: 80,
            fullPage: true
          });
          errorScreenshot = Buffer.from(screenshot).toString('base64');
        }
      } catch (screenshotError) {
        console.error('Failed to capture error screenshot:', screenshotError);
      }
    }

    // Return error as JSON with screenshot if available
    console.log(JSON.stringify({
      error: error.message,
      screenshot: errorScreenshot,
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
