'use strict';

const fs = require('node:fs/promises');
const path = require('node:path');
const process = require('node:process');

// --- Configuration ---
const COPYRIGHT_HOLDER = 's0up and the autobrr contributors';
const START_YEAR = 2025;
const CURRENT_YEAR = new Date().getFullYear();
const COPYRIGHT_YEAR =
  CURRENT_YEAR > START_YEAR ? `${START_YEAR}-${CURRENT_YEAR}` : `${START_YEAR}`;
const LICENSE = 'GPL-2.0-or-later';

// Excluded directories
const EXCLUDED_DIRS = new Set(['.git', 'node_modules', 'dist', 'build', 'vendor']);

const TS_HEADER = [
  '/*',
  ` * Copyright (c) ${COPYRIGHT_YEAR}, ${COPYRIGHT_HOLDER}.`,
  ` * SPDX-License-Identifier: ${LICENSE}`,
  ' */',
].join('\n');

const GO_HEADER = [
  `// Copyright (c) ${COPYRIGHT_YEAR}, ${COPYRIGHT_HOLDER}.`,
  `// SPDX-License-Identifier: ${LICENSE}`,
].join('\n');

/**
 * Determine whether a file already has the expected header.
 * @param {string[]} lines
 * @param {boolean} isTypescript
 * @returns {boolean}
 */
function hasHeader(lines, isTypescript) {
  if (lines.length === 0) {
    return false;
  }

  if (isTypescript) {
    if (lines.length >= 4) {
      const firstFour = lines.slice(0, 4);
      const hasStart = firstFour[0].trim().startsWith('/*');
      const hasCopyright = firstFour.some((line) => /copyright/i.test(line));
      const hasEnd = firstFour.some((line) => line.includes('*/'));
      return hasStart && hasCopyright && hasEnd;
    }
  } else if (lines.length >= 2) {
    return lines.slice(0, 2).some((line) => /copyright/i.test(line));
  }

  return false;
}

/**
 * Normalize file contents and rewrite with the correct header.
 * @param {string} filePath
 * @returns {Promise<boolean>} true if the file was updated
 */
async function processFile(filePath) {
  let headerToApply = '';
  let linesToRemove = 0;
  let isTypescript = false;

  if (filePath.endsWith('.ts') || filePath.endsWith('.tsx')) {
    headerToApply = TS_HEADER;
    linesToRemove = 4;
    isTypescript = true;
  } else if (filePath.endsWith('.go')) {
    headerToApply = GO_HEADER;
    linesToRemove = 2;
    isTypescript = false;
  } else {
    return false;
  }

  const relativePath = path.relative(process.cwd(), filePath);
  console.log(`CHECKING: ${relativePath}`);

  try {
    const original = await fs.readFile(filePath, 'utf8');
    const originalLines = original.split('\n');

    let remainingLines = originalLines;

    if (hasHeader(originalLines, isTypescript)) {
      console.log(`  UPDATING HEADER: ${relativePath}`);
      remainingLines = originalLines.slice(linesToRemove);
    } else {
      console.log(`  ADDING HEADER: ${relativePath}`);
    }

    while (remainingLines.length > 0 && remainingLines[0].trim() === '') {
      remainingLines.shift();
    }

    const body = remainingLines.join('\n');

    let output = `${headerToApply}\n\n`;
    output += body;

    await fs.writeFile(filePath, output, 'utf8');
    return true;
  } catch (error) {
    console.error(`  ERROR: Failed to process ${relativePath}: ${error.message}`);
    return false;
  }
}

/**
 * Recursively walk directories and process files.
 * @param {string} directory
 * @returns {Promise<number>}
 */
async function walk(directory) {
  const entries = await fs.readdir(directory, { withFileTypes: true });
  let updatedCount = 0;

  for (const entry of entries) {
    if (entry.isDirectory()) {
      if (EXCLUDED_DIRS.has(entry.name)) {
        continue;
      }

      updatedCount += await walk(path.join(directory, entry.name));
    } else if (entry.isFile()) {
      const filePath = path.join(directory, entry.name);
      if (await processFile(filePath)) {
        updatedCount += 1;
      }
    }
  }

  return updatedCount;
}

async function main() {
  const args = process.argv.slice(2);

  if (args.length > 0) {
    const targetPath = path.resolve(args[0]);
    console.log(`Processing single file: ${path.relative(process.cwd(), targetPath)}`);
    const updated = await processFile(targetPath);

    if (updated) {
      console.log(`✅ Successfully updated ${path.relative(process.cwd(), targetPath)}`);
    } else {
      console.log(`❌ No update needed for ${path.relative(process.cwd(), targetPath)}`);
    }
    return;
  }

  console.log('Starting header update process...');
  const updatedCount = await walk(process.cwd());

  console.log('\n=== Summary ===');
  console.log(`Files updated: ${updatedCount}`);
  console.log('\nAll done! Review and commit the changes. ✅');
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
