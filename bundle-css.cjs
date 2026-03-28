const fs = require('fs');
const path = require('path');

/**
 * CSS Bundler (Simple concatenation for @import)
 * Why: To reduce HTTP requests by merging multiple CSS files into one.
 */

const entryFile = path.resolve(__dirname, 'static/style.css');
const outputFile = path.resolve(__dirname, 'static/css/main.bundle.css');

function bundleCss(filePath, visited = new Set()) {
    if (visited.has(filePath)) return '';
    visited.add(filePath);

    if (!fs.existsSync(filePath)) {
        console.error(`Error: File not found - ${filePath}`);
        return '';
    }

    let content = fs.readFileSync(filePath, 'utf8');
    const importRegex = /@import\s+["'](.+?)["'];/g;

    let bundledContent = `/* --- Start of ${path.relative(__dirname, filePath)} --- */\n`;
    
    // Process imports recursively
    let match;
    let lastIndex = 0;
    
    // Reset regex index
    importRegex.lastIndex = 0;

    while ((match = importRegex.exec(content)) !== null) {
        const importPath = match[1];
        // Handle relative paths from the current file's directory
        const absolutePath = path.resolve(path.dirname(filePath), importPath);
        
        // Add content before the import
        bundledContent += content.substring(lastIndex, match.index);
        
        // Recursively add imported file content
        bundledContent += bundleCss(absolutePath, visited);
        
        lastIndex = importRegex.lastIndex;
    }
    
    // Add remaining content after imports
    bundledContent += content.substring(lastIndex);
    bundledContent += `\n/* --- End of ${path.relative(__dirname, filePath)} --- */\n`;

    return bundledContent;
}

try {
    console.log(`Starting CSS bundling from: ${entryFile}`);
    const finalCss = bundleCss(entryFile);
    
    const outputDir = path.dirname(outputFile);
    if (!fs.existsSync(outputDir)) {
        fs.mkdirSync(outputDir, { recursive: true });
    }
    
    fs.writeFileSync(outputFile, finalCss);
    console.log(`Successfully bundled CSS to: ${outputFile} (Size: ${(finalCss.length / 1024).toFixed(2)} KB)`);
} catch (err) {
    console.error('Failed to bundle CSS:', err);
    process.exit(1);
}
