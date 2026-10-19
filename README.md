# GGUF Libra

## What is this
A local LLM chat client focused on role-play and chat, based on `llama-server` (llama.cpp). It reshapes how the model recalls past context through a custom algorithm, allowing the model to remember key information while also forgetting trivial details.

## Why Design "Forgetting"?
The model does not need to remember everything. Recalling trivial, insignificant interactions pollutes the context window and degrades conversation quality.

## Core Features
- **Git-Style Dialogue Branches**: Chat in isolated side branches where the model will not recall events from the main branch. You can also pass a summary from the main branch to let the model chat with background context.
- **Custom `data.bin` Database**: Does not search the entire history. It uses a custom algorithm to rank memories and only retrieves the most significant ("heaviest") memories. See `parser_py/parser.py` for how this weight is calculated.
- **RAG Vector Database**: In this project, RAG is strictly used for global background settings (e.g., character personas). Plain `.txt` files with sections under 500 characters/words are recommended for the most accurate retrieval.
- **YAML System Prompts**: Supports `system_prompts.yaml` for defining prompts, removing the need for complex JSON-style character cards.
- **Local & Private**: No cloud database or telemetry. Chat history is stored strictly on your local machine (`chain_history.json`) for topological recall, and can also be exported as `.txt` files.
- **PWA Support**: Can be installed as a Progressive Web App.
- **No Accounts / Logins**: This is a local client for `llama-server`, so no login system is needed.
- **No Frontend Frameworks**: Built with pure HTML, CSS, and Vanilla JavaScript (no React or Vue).
- **No Backend Frameworks**: Uses only Go's standard `net/http` package.
- **No SQL / External Database**: Operates completely without SQL or heavy database daemons, powered solely by native files (data.bin and local vector storage).

## How to Use (Windows)
Start two `llama-server` instances on ports `8021` (chat model) and `8022` (embedding model).
The following example uses `gemma-4-12B` and `embeddinggemma-300M`:

First, create the directory `D:\llama_cpp\models` and download the GGUF files for `gemma-4-12b` and `embedding-gemma-300m` and place them inside. Then, run the following two scripts:

### Script 1: gemma-4-12b (Main Model)

```powershell
Set-Location -Path "D:\llama_cpp"
$Host.UI.RawUI.WindowTitle = 'LLaMA - Main Model (Port: 8021)'

Write-Host "Starting Main Model Service on port 8021..." -ForegroundColor Yellow


.\llama-server.exe `
    -m "models\gemma-4-12B-it-qat-UD-Q4_K_XL.gguf" `
    --mmproj "models\gemma-4-12b-mmproj-F16.gguf" `
    -ngl 46 `
    -c 16384 `
    --port 8021 `
    --host 0.0.0.0 `
    --cache-ram 2048 `
    --cache-type-k q8_0 `
    --cache-type-v q8_0 `
    --keep 0 `
    -fa on `
    -np 1 `
    --reasoning off `
    --repeat-penalty 1.1
```

### Script 2: embedding-gemma-300m (Embedding Model)
```powershell
Set-Location -Path "D:\llama_cpp"
$Host.UI.RawUI.WindowTitle = 'LLaMA - Embedding (Port: 8022)'
Write-Host "Starting Embedding Service on port 8022..." -ForegroundColor Cyan


.\llama-server.exe `
    -m "models\embeddinggemma-300M-Q8_0.gguf" `
    -ngl 0 `
    -c 2048 `
    -b 2048 `
    -ub 2048 `
    --port 8022 `
    --host 0.0.0.0 `
    --embedding `
    --pooling mean
```
### Run `gguf-libra.exe`
Run the compiled executable `gguf-libra.exe` and navigate to `http://127.0.0.1:8099`.

Click the **Settings** button in the top-right corner, set the following URLs, and click **Save**:
- **API URL**: `http://127.0.0.1:8021/v1/chat/completions`
- **Embedding URL**: `http://127.0.0.1:8022/v1/embeddings`

*(Optional: Run `gguf-libra.exe -port 0` to assign a random available port. Default is 8099.)*


## Screenshot
(Add your screenshots here)

## How to Build (windows)
### 1.Prerequisites
- Go 1.22.2
- Node.js v26.3.1
- uv 0.12.22

### 2.Run

```bash
node build.js
```

## How to Build on Linux (Experimental / Manual)

The project theoretically supports Linux. However, `build.js` is currently tailored for Windows environments (using PyInstaller to create `.exe` binaries) and has not been extensively tested on Linux. 

You can build and run it manually on Linux using the steps below:

### 1. Prerequisites
- Go 1.22.2
- Node.js v26.3.1
- uv 0.12.22

### 2. Build Steps

1. In the project root directory, build the frontend and backend:
   ```bash
   npm run build
   go build
   ```
2. Set up the Python environment for the memory parser:
   ```bash
   cd parser_py
   uv sync
   ```
   
> [!NOTE]
> **Linux Runtime Differences**  
> On Windows, the build script compiles the parser into `parser.exe`, and the Web UI provides an **"Open Parser"** button to launch it with one click. This UI button is currently unavailable on Linux.  
> 
> To process and update memories, manually run the parser from the `parser_py` directory:
> ```bash
> uv run main.py
> ```


## Theoretical Foundation: Why `logic_t`?

We model the conversation tree as a **discrete heat diffusion system** over an undirected graph:

1. **Diffusion Step (Heat Equation)**:
   $$u_i(t+1) = u_i(t) + dt \cdot \sum_{j \in \text{Neighbor}(i)} (u_j(t) - u_i(t))$$
   *Matrix form:* $u_{t+1} = u_t - dt \cdot (L \cdot u_t)$

2. **Decay of the Slowest Mode**:
   $$\|u_{t+1}\| \approx \|u_t\| \cdot (1 - \lambda_2 \cdot dt)$$
   *Where $\lambda_2$ is the algebraic connectivity (second smallest eigenvalue of the Laplacian matrix $L$).*

3. **Characteristic Relaxation Time**:
   $$\text{logic\_t} = \frac{1}{\lambda_2}$$

### Physical Intuition:
- **Trivial small-talk**: Represents a shallow graph with no topological bottlenecks ($\lambda_2$ is large). Information diffuses to equilibrium almost instantaneously, yielding a low $\text{logic\_t}$ (pruned as forgotten).
- **Deep reasoning chains**: Possess tight structural bottlenecks and elongated paths ($\lambda_2 \to 0$). Information takes a long time to dissipate, yielding a high $\text{logic\_t}$ (anchored as high-priority memory).

For the full numerical implementation, see `parser_py/parser.py`.

## license
This project is licensed under the GNU Affero General Public License v3.0 (AGPL-3.0).
