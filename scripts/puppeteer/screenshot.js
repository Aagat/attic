import puppeteer from 'puppeteer';
import path from 'path';
import yargs from 'yargs';
import { hideBin } from 'yargs/helpers';
import { fileURLToPath } from 'url';

// Get __dirname equivalent in ES modules
const __dirname = path.dirname(fileURLToPath(import.meta.url));

// Parse command line arguments
const argv = yargs(hideBin(process.argv))
  .option('url', {
    description: 'URL to capture screenshot of',
    type: 'string',
    demandOption: true
  })
  .help()
  .argv;

async function captureScreenshot(url) {
  try {
    // Launch browser with extensions
    const browser = await puppeteer.launch({
      headless: 'new',
      args: [
        '--no-sandbox',
        '--disable-setuid-sandbox',
        '--disable-dev-shm-usage',
        '--disable-accelerated-2d-canvas',
        '--disable-gpu',
        '--disable-notifications',
        '--disable-infobars',
        '--disable-web-security',
        '--disable-features=IsolateOrigins,site-per-process',
        `--disable-extensions-except=${path.join(__dirname, 'extensions/ublock')},${path.join(__dirname, 'extensions/idcac')}`,
        `--load-extension=${path.join(__dirname, 'extensions/ublock')},${path.join(__dirname, 'extensions/idcac')}`
      ]
    });

    try {
      // Create new page
      const page = await browser.newPage();

      // Add script to auto-close popups
      await page.evaluateOnNewDocument(() => {
        window.addEventListener('load', () => {
          // Close common popup/dialog buttons
          const closeButtons = document.querySelectorAll('[class*="close"], [class*="popup"], [id*="close"], [id*="popup"], button[aria-label*="close"]');
          closeButtons.forEach(button => button.click());
        });
      });

      // Set viewport
      await page.setViewport({
        width: 1920,
        height: 1080
      });

      // Set user agent
      await page.setUserAgent('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36');

      // Enable JavaScript
      await page.setJavaScriptEnabled(true);

      // Navigate to URL and wait for rendering to complete
      await page.goto(url, {
        waitUntil: ['networkidle2'],
        timeout: 120000
      });

      // Wait for content to load with increased timeout
      await page.waitForSelector('body', { timeout: 60000 });

      // Add additional wait to ensure dynamic content loads
      await new Promise(resolve => setTimeout(resolve, 5000));

      // Take screenshot
      const screenshot = await page.screenshot({
        type: 'jpeg',
        quality: 80,
        fullPage: true,
        encoding: 'base64'
      });

      // Output base64-encoded screenshot
      process.stdout.write(screenshot);

    } finally {
      await browser.close();
    }
  } catch (error) {
    console.error(error.message);
    process.exit(1);
  }
}

// Run the screenshot capture
captureScreenshot(argv.url); 
