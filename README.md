# GGUF_LIBRA

## What is this
A local LLM chat client focused on role-play and chat, based on `llama-server` (llama.cpp). It reshapes how the model recalls past context through a custom algorithm, allowing the model to remember key information while also forgetting trivial details.

## Why Design "Forgetting"?
The model does not need to remember everything. Recalling trivial, insignificant interactions pollutes the context window and degrades conversation quality.

## Core Features
- **RAG Vector Database**: In this project, RAG is strictly used for global background settings (e.g., character personas). Plain `.txt` files with sections under 500 characters/words are recommended for the most accurate retrieval.
- **Custom `data.bin` Database**: Does not search the entire history. It uses a custom algorithm to rank memories and only retrieves the most significant ("heaviest") memories. See `parser_py/parser.py` for how this weight is calculated.
- **YAML System Prompts**: Supports `system_prompts.yaml` for defining prompts, removing the need for complex JSON-style character cards.
- **Git-Style Dialogue Branches**: Chat in isolated side branches where the model will not recall events from the main branch. You can also pass a summary from the main branch to let the model chat with background context.
- **TXT Export**: Conversations can be exported as `.txt` files. The application itself does not persist user chat records.
- **No Accounts / Logins**: This is a local client for `llama-server`, so no login system is needed.
- **PWA Support**: Can be installed as a Progressive Web App.
- **No Frontend Frameworks**: Built with pure HTML, CSS, and Vanilla JavaScript (no React or Vue).
- **No Backend Frameworks**: Uses only Go's standard `net/http` package.

## How to Use (Windows)
Start two `llama-server` instances on ports 8021 (chat model) and 8022 (embedding model).
The following example uses `gemma-4-12B` and `embeddinggemma-300M`:

```powershell
# Change working directory
Set-Location -Path "D:\llama_cpp"

Write-Host "======================================================" -ForegroundColor Yellow
Write-Host " Starting dual services in a single window:" -ForegroundColor Yellow
Write-Host "   ▶ Main Model API:      http://0.0.0.0:8021" -ForegroundColor Green
Write-Host "   ▶ Embedding API:       http://0.0.0.0:8022" -ForegroundColor Cyan
Write-Host " Logs from both services will stream here. Press Ctrl + C to exit." -ForegroundColor Yellow
Write-Host "======================================================`n" -ForegroundColor Yellow

# Arguments for Embedding service (Port 8022)
$embedArgs = @(
    "-m", "models\embeddinggemma-300M-Q8_0.gguf",
    "-ngl", "0",
    "-c", "2048",
    "-b", "2048",
    "-ub", "2048",
    "--port", "8022",
    "--host", "0.0.0.0",
    "--embedding",
    "--pooling", "mean"
)

# Arguments for Main Model service (Port 8021)
$mainArgs = @(
    "-m", "models\gemma-4-12B-it-qat-UD-Q4_K_XL.gguf",
    "--mmproj", "models\gemma-4-12b-mmproj-F16.gguf",
    "-ngl", "45",
    "-c", "16384",
    "--port", "8021",
    "--host", "0.0.0.0",
    "--cache-ram", "2048",
    "--cache-type-k", "q8_0",
    "--cache-type-v", "q8_0",
    "--keep", "0",
    "-fa", "on",
    "-np", "1",
    "--reasoning", "off",
    "--repeat-penalty", "1.1"
)

# 1. Start Embedding service in background (-NoNewWindow routes stdout/stderr directly into this console)
$embedProcess = Start-Process -FilePath ".\llama-server.exe" -ArgumentList $embedArgs -PassThru -NoNewWindow

try {
    # 2. Run Main Model in the foreground (streaming logs to the same window)
    & ".\llama-server.exe" @mainArgs
}
finally {
    # 3. Clean up: Automatically kill the Embedding background process when exiting or pressing Ctrl + C
    if ($embedProcess -and -not $embedProcess.HasExited) {
        Write-Host "`nStopping Embedding service..." -ForegroundColor Yellow
        Stop-Process -Id $embedProcess.Id -Force
    }
}
```
Run the compiled executable `gguf-libra.exe` and navigate to:`http://127.0.0.1:8099`
(Optional: Run `gguf-libra.exe -port 0` to assign a random available port. Default is 8099.)

## Screenshot
(Add your screenshots here)

## How to Build
### 1.Prerequisites
- Go 1.22.2
- Node.js v26.3.1
- uv 0.12.22

### 2.Run

```bash
node build.js
```
## license
This project is licensed under the GNU Affero General Public License v3.0 (AGPL-3.0).
