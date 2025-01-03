import fs from 'fs';
import path from 'path';
import https from 'https';
import { fileURLToPath } from 'url';
import AdmZip from 'adm-zip';
import { Octokit } from '@octokit/rest';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const octokit = new Octokit();

// Define all extensions here
const EXTENSIONS = {
  ublock: {
    name: 'uBlock Origin Lite',
    owner: 'uBlockOrigin',
    repo: 'uBOL-home',
    assetFilter: (asset) => asset.name.includes('chromium.mv3.zip'),
    directory: 'ublock'
  },
  idcac: {
    name: "I Don't Care About Cookies",
    owner: 'OhMyGuus',
    repo: 'I-Dont-Care-About-Cookies',
    assetFilter: (asset) => asset.name.endsWith('chrome-source.zip'),
    directory: 'idcac'
  }
};

async function downloadFile(url, outputPath) {
  return new Promise((resolve, reject) => {
    const file = fs.createWriteStream(outputPath);
    https.get(url, {
      headers: {
        'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36'
      },
      followAllRedirects: true
    }, response => {
      // Handle redirects
      if (response.statusCode === 302 || response.statusCode === 301) {
        file.close();
        downloadFile(response.headers.location, outputPath)
          .then(resolve)
          .catch(reject);
        return;
      }

      // Check if the response is successful
      if (response.statusCode !== 200) {
        reject(new Error(`Server responded with status code: ${response.statusCode}`));
        return;
      }

      response.pipe(file);
      file.on('finish', () => {
        file.close();
        resolve();
      });
    }).on('error', err => {
      fs.unlink(outputPath, () => {});
      reject(err);
    });
  });
}

async function setupExtension(extensionsDir, extension) {
  const extensionDir = path.join(extensionsDir, extension.directory);
  
  // Create directory if it doesn't exist
  if (!fs.existsSync(extensionDir)) {
    fs.mkdirSync(extensionDir, { recursive: true });
  }

  // Get latest release
  const release = await octokit.repos.getLatestRelease({
    owner: extension.owner,
    repo: extension.repo
  });

  const asset = release.data.assets.find(extension.assetFilter);

  if (asset) {
    console.log(`Downloading ${extension.name} ${release.data.tag_name}...`);
    const zipPath = path.join(extensionsDir, `${extension.directory}.zip`);
    await downloadFile(asset.browser_download_url, zipPath);
    
    // Clear directory and extract
    fs.rmSync(extensionDir, { recursive: true, force: true });
    fs.mkdirSync(extensionDir);
    const zip = new AdmZip(zipPath);
    zip.extractAllTo(extensionDir, true);
    fs.unlinkSync(zipPath);
    
    console.log(`Successfully installed ${extension.name}`);
  } else {
    throw new Error(`No suitable release asset found for ${extension.name}`);
  }
}

async function setupExtensions() {
  const extensionsDir = path.join(__dirname, 'extensions');

  // Create extensions directory if it doesn't exist
  if (!fs.existsSync(extensionsDir)) {
    fs.mkdirSync(extensionsDir, { recursive: true });
  }

  try {
    // Process all extensions
    for (const extension of Object.values(EXTENSIONS)) {
      await setupExtension(extensionsDir, extension);
    }

    console.log('All extensions setup completed successfully!');
  } catch (error) {
    console.error('Error setting up extensions:', error);
    process.exit(1);
  }
}

setupExtensions(); 