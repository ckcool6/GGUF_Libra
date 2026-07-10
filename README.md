## GGUF Player

**What is this**

// todo

**Project Highlights**

// todo


### Tech Stack

* Backend: Go (net/http, godotenv)
* Frontend Build Tool: Vite, @tailwindcss/vite
* Core Dependencies: Tailwind CSS v4, Marked, Highlight.js, Twemoji

### How to Setup and Run Locally

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

Once the service is active, navigate to `http://127.0.0.1:8099` in your web browser to start chatting with your completely localized gguf player!

