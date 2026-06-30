**Mini Gemini UI**

Welcome to Mini Gemini UI, a lightweight, fully localized Gemini chat client built with a Go backend and a modern Vite plus Tailwind CSS frontend. It features an elegant interface, lightning-fast responsiveness, and complete support for offline static resource loading along with markdown and code syntax highlighting.

**Project Highlights**

* Fully Localized Dependencies: Say goodbye to traditional CDN links. All frontend third-party libraries are managed locally via npm and optimized through Vite compilation, ensuring the application loads instantly even without an internet connection.
* High-Performance Backend: The native Go server handles the static distribution of your production files seamlessly, offering extreme responsiveness and minimal memory usage.
* Smooth User Experience: Fully integrated with the latest Tailwind CSS styling, featuring a Google-inspired chat bubble design and an adaptive auto-resizing text area.
* Rich Rendering Support: Powered by marked and highlight.js to deliver gorgeous Atom One Dark themed syntax highlighting for all AI code responses.
* PWA Readiness: Built-in support for manifest.json and Service Worker offline configurations, making it ready to be bundled as a native desktop web app wrapper.

**Tech Stack**

* Backend: Go (net/http, godotenv)
* Frontend Build Tool: Vite, @tailwindcss/vite
* Core Dependencies: Tailwind CSS v4, Marked, Highlight.js, Twemoji

**How to Setup and Run Locally**

To get started, clone the repository and install the frontend dependencies. Ensure you have Node.js installed on your machine, then run the following command in your project root directory:

```bash
npm install

```

If you want to debug the frontend interface separately, you can spin up the Vite development server using:

```bash
npm run dev

```

Open the local address provided in your terminal (usually `http://localhost:5173`) to enjoy real-time hot module replacement.

When your interface development is ready for production deployment, run the compilation build command:

```bash
npm run build

```

This generates a production-ready folder in your root directory, containing all compressed and optimized web assets.

Make sure you have configured your local environment file with your credentials. Then, kick off your Go backend service:

```go
go run main.go

```

Once the service is active, navigate to `http://127.0.0.1:8099` in your web browser to start chatting with your completely localized Mini Gemini client!

**Project Directory Structure**

* main.go — The clean and elegant Go backend core handling production static file hosting and API routing.
* index.html — The single-page application entry point optimized by Vite.
* chat.js — The core frontend script handling chat interactions, markdown parsing, and code highlighting.
* style.css — The styling core importing the latest Tailwind v4 engine.
* public/ — The directory storing assets like manifest.json, sw.js, and icon.png that should be copied as-is.
* dist/ — The compiled production bundle containing optimized assets (this directory can be safely added to your gitignore file).