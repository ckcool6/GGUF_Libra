## GGUF Player

**What is this**

GGUF Player is a local LLM client that requires no account registration, allowing you to chat with your local models simply by entering the `<url>/v1/chat/completions` endpoint. It supports dark mode and enables you to download and save your conversation history locally as a TXT file.

### How to Setup and Run Locally

```bash
npm install

```

```bash
npm run build

```

```go
go build

```

Once the service is active, navigate to `http://127.0.0.1:8099` in your web browser to start chatting with your completely localized gguf player!


### Tech Stack

* Backend: Go (net/http, godotenv)
* Frontend Build Tool: Vite, @tailwindcss/vite
* Core Dependencies: Tailwind CSS v4, Marked, Highlight.js, Twemoji
