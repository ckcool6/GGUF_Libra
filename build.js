/**
 * Copyright 2026 Lu ZhiYuan
 * SPDX-License-Identifier: AGPL-3.0-only
 */

const { execSync } = require('child_process');
const path = require('path');
const fs = require('fs');

// ANSI Color definitions
const c = {
    reset: '\x1b[0m',
    bold: '\x1b[1m',
    red: '\x1b[31m',
    green: '\x1b[32m',
    cyan: '\x1b[36m',
    blue: '\x1b[34m',
};

// Formatted colored status tags
const TAG = {
    step: `${c.bold}${c.cyan}[STEP]${c.reset}`,
    ok: `${c.bold}${c.green}[OK]${c.reset}`,
    fail: `${c.bold}${c.red}[FAIL]${c.reset}`,
    info: `${c.bold}${c.blue}[INFO]${c.reset}`,
};

// Configuration
const RELEASE_DIR = path.join(__dirname, 'release');
const EXE_NAME = process.platform === 'win32' ? 'gguf-libra.exe' : 'gguf-libra';

// Command execution wrapper
function run(command, cwd = process.cwd()) {
    const cwdDisplay = cwd !== process.cwd() ? ` (cwd: ${cwd})` : '';
    console.log(`\n${TAG.step} Executing: ${command}${cwdDisplay}`);
    try {
        execSync(command, { stdio: 'inherit', cwd, shell: true });
        console.log(`${TAG.ok} Done`);
    } catch (error) {
        console.error(`\n${TAG.fail} Command failed: ${command}`);
        process.exit(1);
    }
}

// Safe file copy helper
function copyIfExists(srcFile, destDir) {
    const src = path.join(__dirname, srcFile);
    if (fs.existsSync(src)) {
        fs.copyFileSync(src, path.join(destDir, srcFile));
        console.log(`${TAG.info} Copied file: ${srcFile}`);
    }
}

console.log(`${c.bold}================ Starting Build Pipeline ================${c.reset}`);

// 0. Clean and prepare release directory
if (fs.existsSync(RELEASE_DIR)) {
    fs.rmSync(RELEASE_DIR, { recursive: true, force: true });
}
fs.mkdirSync(RELEASE_DIR, { recursive: true });

// 1. Build frontend distribution
run('npm install');
run('npm run build');

// 2. Compile Go binary into release directory
const outBinaryPath = path.join(RELEASE_DIR, EXE_NAME);
run(`go build -x -o "${outBinaryPath}"`);

// 3. Archive frontend distribution and configurations
console.log(`\n${TAG.info} Archiving base runtime assets...`);
fs.cpSync(path.join(__dirname, 'dist'), path.join(RELEASE_DIR, 'dist'), { recursive: true });
console.log(`${TAG.info} Copied directory: dist/`);

copyIfExists('system_prompt.json', RELEASE_DIR);
copyIfExists('chain_history.json', RELEASE_DIR);

// create empty data.bin 
const dataBinPath = path.join(RELEASE_DIR, 'data.bin');
fs.writeFileSync(dataBinPath, Buffer.alloc(0));
console.log(`${TAG.ok} Created empty data.bin placeholder in release directory`);

// 4. Compile Python parser into standalone executable
console.log(`\n${TAG.info} Compiling Python parser to standalone executable...`);
const targetParserPy = path.join(RELEASE_DIR, 'parser_py');

// Output parser.exe directly into release/parser_py
run(
    `uv run pyinstaller --onefile --console --clean --distpath "${targetParserPy}" --name parser main.py`,
    path.join(__dirname, 'parser_py')
);

console.log(`\n${c.bold}================ Build Completed Successfully ================${c.reset}`);
console.log(`
Release package structure:
release/
├── ${EXE_NAME}
├── data.bin               (empty placeholder)
├── system_prompt.json
├── chain_history.json
├── dist/                  (bundled frontend assets)
└── parser_py/
    └── parser.exe         (standalone Python binary, zero dependencies required)
`);