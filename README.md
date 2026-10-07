# Agent Design Based on Forgetting Mechanism: Determining What to Forget by Weighing Dialogues

## Introduction

When exploring complex problems, traditional linear dialogue architectures introduce two fundamental bottlenecks:

1. **Context Contamination and Attention Dispersion (Lost in the Middle)**: Interleaving exploratory side quests, syntax corrections, and dead ends directly into the main thread rapidly exhausts the finite context window, diluting essential signals.
2. **Lack of Persistent Reasoning Depth**: Conventional RAG relies strictly on semantic similarity across isolated text snippets, lacking an overarching derivation chain and structural reasoning progression.

**GGUF Libra** addresses these challenges through a cognitive-inspired design:

- **Interaction Layer**: Abstracts conversation history into a directed tree, providing Git-like branching (Fork), lossless distillation (Squash & Merge), and lifecycle archiving (Archive).
- **Retrieval Layer**: Employs spectral graph theory (Laplacian matrix eigenvalues) to mathematically quantify the reasoning depth of a dialogue tree ($\text{Logic } T$), generating topological memory maps.
- **Compute Layer**: Runs entirely on local hardware with direct, slot-level control over the inference engine and physical KV cache.

---

## Core Features

### Dialogue as Version Control (Git-like Dialogue Tree)
Transitions conversational context from a linear log to a version-controlled tree:
- **Side Chat**: Branch off from any point to explore sub-problems in an isolated sandbox. Vector database querying is cleared on branching to prevent contextual crosstalk.
- **Squash & Merge**: Once an exploratory topic concludes, the branch is distilled into a concise summary node grafted back onto the main trunk. Verbose intermediate steps are pruned from the active context while remaining safely archived.
- **Lifecycle Archiving**: Converts mature conversation chapters into compressed seeds, resetting the active token budget while retaining continuity.

### Topological Memory via Spectral Graph Theory
Replaces superficial text embeddings with structural topological metrics:
- **Graph Laplacian Construction**: Deconstructs conversation trees into adjacency matrices to formulate the unnormalized Laplacian operator:
  $$L = D - A$$
  *(where $D$ is the degree matrix and $A$ is the adjacency matrix)*.
- **Algebraic Connectivity and Relaxation Time ($\text{Logic } T = 1 / \lambda_2$)**: Solves for the second smallest eigenvalue (Fiedler value $\lambda_2$). Deep, sequential derivation chains exhibit lower algebraic connectivity and significantly higher relaxation times ($\text{Logic } T$), granting them highest priority during retrieval indexing (`data.bin`).
- **Derivation Chain Injection**: Retrieves and prepends structured causal steps (`Step n: Based on [A] -> Derived [B]`) directly into the prompt to reinforce deductive momentum.

### Syntax-Aware AST Code Chunking (Tree-sitter RAG)
- Leverages **Tree-sitter** for native syntax tree parsing, chunking source code along natural semantic boundaries (functions, classes, methods) rather than arbitrary character or token counts.
- Currently supports fine-grained indexing for `.cpp`, `.py`, `.go`, and `.js` source files.

### Low-Level Inference and Hardware Binding (Llama.cpp Deep Binding)
- **Physical KV Cache Erasure**: Initiating a new session dispatches explicit purge requests to the underlying `llama.cpp` instance, evicting the slot's physical KV cache directly from GPU VRAM.
- **Context Capacity Monitoring**: Continuously polls slot utilization via `/props` (`n_past / n_ctx`), raising visual alerts when memory usage exceeds 85%.

---

## How to Use

### 1. Launch Inference Services
GGUF Libra relies on `llama.cpp` for local inference. Two concurrent services are required:
- **Chat Model**: Handles conversational responses and summarization (e.g., Gemma 4 E4B on port `8021`).
- **Embedding Model**: Handles vectorization for RAG retrieval (e.g., Embedding Gemma 300M on port `8022`).

Use the following powershell script to launch both services in the background:

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

### 2. Launch the Application Client
Run the compiled executable `gguf-libra.exe` (or `go run .` during development) and navigate to:
`http://127.0.0.1:8099`

### 3. Conduct Conversations
Use the action bar beneath messages to dynamically manage conversation flow:
- **Side Chat**: Branch into a focused sandbox.
- **Summary**: Generate an editable summary of the active node.
- **Merge**: Condense and graft branch insights back to the mainline.
- **Archive**: Compress the current thread into a foundational seed for the next chapter.

### 4. Weigh and Persist Dialogue Memory
To index high-value reasoning trees into the persistent memory store:
1. Navigate to the parser directory:
   ```bash
   cd parser_py
   uv run main.py
   ```
2. In the interactive terminal interface:
   - Run `save`: Computes the Graph Laplacian eigenvalues of the active conversation, calculates the topological relaxation weight ($\text{Logic } T$), and persists the sorted derivation tree to `data.bin`.
   - Run `data`: Inspects topological weights and metadata across all indexed trees.

---

## screenshot

<img width="1790" height="951" alt="Image" src="https://github.com/user-attachments/assets/6a4e15b7-da91-4183-abbd-02b28be4c600" />

<img width="1467" height="831" alt="Image" src="https://github.com/user-attachments/assets/99e177b0-28f5-484b-83cf-cefe2a6db973" />

<img width="1781" height="899" alt="Image" src="https://github.com/user-attachments/assets/4ae89cb7-ab3e-4223-ba33-395c79bf1f72" />


## How to Build

### Prerequisites
- [Go](https://go.dev/) 1.22.2
- [Node.js](https://nodejs.org/) v26.3.1
- [uv](https://github.com/astral-sh/uv) (Fast Python package runner)

### Build Steps
From the repository root directory, execute:

```bash
# 1. Install frontend dependencies and bundle static distribution
npm install
npm run build

# 2. build parser py
cd parser_py
uv sync
uv run pyinstaller --onefile --console --clean --distpath . --name parser main.py
cd ..

# 3. Compile the Go backend binary
go build -x
```

The resulting `gguf-libra.exe` runs self-contained alongside the generated `dist/` directory.