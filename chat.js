/**
 * Copyright 2026 Lu ZhiYuan
 * SPDX-License-Identifier: AGPL-3.0-only
 */

import DOMPurify from "dompurify";
import { marked } from "marked";
import twemoji from "twemoji";
import hljs from "highlight.js";
import katex from "katex";
import "katex/dist/katex.min.css";
import "highlight.js/styles/atom-one-light.min.css";

// ==================================== UTILITY FUNCTIONS =====================================
// Escape user plain text and handle line breaks
function formatUserText(str) {
    // Create a temporary div to escape text using the browser's textContent property
    const temp = document.createElement("div");
    temp.textContent = str;
    const escapedStr = temp.innerHTML; // Converts < to &lt;, etc.

    // Replace newlines with <br> tags
    return escapedStr.replace(/\n/g, "<br>");
}

// AI Markdown rendering with XSS sanitization
function safeMarkdownParse(content) {
    const rawHtml = marked.parse(content);
    return DOMPurify.sanitize(rawHtml, {
        ADD_TAGS: ["use", "path", "svg"], // Allow KaTeX / Math SVG tags
        ADD_ATTR: ["target", "allow"],
    });
}

// Smart scroll: Auto-scroll only when user is near the bottom
function scrollToBottomIfNear() {
    const isAtBottom =
        chatBox.scrollHeight - chatBox.scrollTop - chatBox.clientHeight < 120;
    if (isAtBottom) {
        chatBox.scrollTop = chatBox.scrollHeight;
    }
}

// Initialize prompt selector dropdown
async function initPromptSelect() {
    const select = document.getElementById("prompt-select");
    if (!select) return;

    try {
        const res = await fetch("/api/prompts", { headers: getHeaders() });

        if (!res.ok) throw new Error(`HTTP status error: ${res.status}`);

        const data = await res.json();

        select.innerHTML = "";
        if (data.prompts && data.prompts.length > 0) {
            data.prompts.forEach((p) => {
                const opt = document.createElement("option");
                opt.value = p.id;
                opt.innerText = p.name;
                if (p.id === data.active) opt.selected = true;
                select.appendChild(opt);
            });
            select.disabled = false; // Enable select dropdown
        } else {
            showPromptError(select, "No prompts available");
        }

        // Listen for prompt change
        select.onchange = async (e) => {
            const newId = e.target.value;

            // If currently generating a response, abort the request
            if (chatAbortController) {
                chatAbortController.abort();
                chatAbortController = null;
            }

            // Reset button and loading states
            loading.classList.add("hidden");
            sendBtn.classList.remove("is-loading");
            sendBtn.innerHTML = "Send";

            try {
                // Notify backend to switch prompt
                const switchRes = await fetch("/api/switch-prompt", {
                    method: "POST",
                    headers: { "Content-Type": "application/json" },
                    body: JSON.stringify({ id: newId }),
                });

                if (!switchRes.ok) throw new Error("Failed to switch prompt");

                // Retrieve local custom_url and clear backend chat history
                const customUrl = localStorage.getItem("custom_api_url") || "";
                await fetch(
                    `/api/new-chat?custom_url=${encodeURIComponent(customUrl)}`,
                );
                updateBranchIndicator("main");

                // Reset frontend UI
                chatBox.innerHTML = `
                    <div class="flex justify-start mb-8">
                        <div class="ai-bubble p-4 rounded-2xl max-w-[90%] markdown-body">
                            Hello!
                        </div>
                    </div>`;

                // Refresh context memory calculation
                get_ctx_usage();
            } catch (err) {
                console.error("Failed to switch prompt or clear chat:", err);
            }
        };
    } catch (e) {
        console.error("Failed to load prompt list:", e);
        showPromptError(select, "Service disconnected");
    }
}

// Helper function to display error/empty state in prompt select
function showPromptError(select, message) {
    select.innerHTML = `<option value="" disabled selected>⚠️ ${message}</option>`;
    select.disabled = true;
}

// Get unified request headers
function getHeaders() {
    const key = localStorage.getItem("custom_api_key") || "";
    return {
        "Content-Type": "application/json",
        Authorization: `Bearer ${key}`, // Standard authorization header checked by backends
    };
}

function updateBranchIndicator(text) {
    const indicator = document.getElementById("branch-indicator");
    if (indicator) {
        indicator.innerText = `current：${text}`;

        // Strip any existing color classes
        indicator.classList.remove(
            "text-gray-600", "dark:text-gray-300",
            "text-emerald-600", "dark:text-emerald-400",
            "text-amber-600", "dark:text-amber-400"
        );

        if (text === "side") {
            // Side branch: text turns green
            indicator.classList.add("text-emerald-600", "dark:text-emerald-400");
        } else {
            // Main branch: text turns amber
            indicator.classList.add("text-amber-600", "dark:text-amber-400");
        }
    }
}

// ==================================== INIT & EXTENSIONS LOAD =============================
// Custom marked extension to accurately intercept $$ and $
const latexExtension = {
    name: "inlineLatex",
    level: "inline",
    start(src) {
        return src.indexOf("$");
    },
    tokenizer(src) {
        // Match block LaTeX $$...$$ first
        const blockMatch = /^\$\$\s*([\s\S]*?)\s*\$\$/.exec(src);
        if (blockMatch) {
            return {
                type: "inlineLatex",
                raw: blockMatch[0],
                text: blockMatch[1],
                displayMode: true,
            };
        }
        // Match inline LaTeX $...$
        const inlineMatch = /^\$([^\$\n]+?)\$/.exec(src);
        if (inlineMatch) {
            return {
                type: "inlineLatex",
                raw: inlineMatch[0],
                text: inlineMatch[1],
                displayMode: false,
            };
        }
    },
    renderer(token) {
        try {
            return katex.renderToString(token.text, {
                displayMode: token.displayMode,
                throwOnError: false,
            });
        } catch (err) {
            return token.raw;
        }
    },
};

marked.use({ extensions: [latexExtension] });

marked.setOptions({
    highlight: (code, lang) => {
        if (lang && hljs.getLanguage(lang))
            return hljs.highlight(code, { language: lang }).value;
        return hljs.highlightAuto(code).value;
    },
    breaks: true,
    gfm: true,
});

window.onload = () => {
    initPromptSelect();
    const savedTheme = localStorage.getItem("theme");

    if (
        savedTheme === "dark" ||
        (!savedTheme &&
            window.matchMedia("(prefers-color-scheme: dark)").matches)
    ) {
        document.documentElement.classList.add("dark-mode-active", "dark");
        updateModeIcon(true);
    } else {
        document.documentElement.classList.remove("dark-mode-active", "dark");
        updateModeIcon(false);
    }

    loadHistory();

    input.addEventListener("keydown", (e) => {
        if (e.key === "Enter" && !e.shiftKey) {
            e.preventDefault();
            if (sendBtn.classList.contains("is-loading")) {
                return;
            }
            send();
        }
    });
    checkLlamaConnection();
    get_ctx_usage();

    setInterval(() => {
        checkLlamaConnection();
        get_ctx_usage();
    }, 10000);
};

async function loadHistory() {
    try {
        const res = await fetch("/api/history", { headers: getHeaders() });

        const currentBranch = res.headers.get("X-Current-Branch");
        if (currentBranch) {
            updateBranchIndicator(currentBranch);
        }

        const historyList = await res.json();

        if (Array.isArray(historyList) && historyList.length > 0) {
            chatBox.innerHTML = "";

            let lastAiWrapperCol = null;

            historyList.forEach((m) => {
                const isUser = m.role === "user";
                const content = isUser
                    ? formatUserText(m.content)
                    : safeMarkdownParse(m.content);

                if (isUser) {
                    let userBubbleInner = "";
                    if (m.image) {
                        userBubbleInner += `<img src="data:image/jpeg;base64,${m.image}" class="max-w-full rounded-lg mb-2 border border-black/5 dark:border-white/5 shadow-sm">`;
                    }
                    if (m.content) {
                        userBubbleInner += `<div>${content}</div>`;
                    }

                    const html = `
                        <div class="message-row flex justify-start mb-8">
                            <div class="user-bubble p-4 rounded-2xl max-w-[90%]">
                                ${userBubbleInner}
                            </div>
                        </div>`;
                    chatBox.insertAdjacentHTML("beforeend", html);
                } else {
                    const wrapper = document.createElement("div");
                    wrapper.className = "message-row flex justify-start mb-8";
                    wrapper.innerHTML = `
                        <div class="flex flex-col max-w-[90%] w-full">
                            <div class="ai-bubble p-4 rounded-2xl markdown-body">
                                ${content}
                            </div>
                        </div>`;

                    // Check if archived branches exist
                    if (m.archives && m.archives.length > 0) {
                        // Create archive entry button
                        const archiveEntry = document.createElement("div");
                        archiveEntry.className = "mt-2 px-2";
                        archiveEntry.innerHTML = `
                            <button class="flex items-center gap-1.5 text-[11px] font-mono text-emerald-600 dark:text-emerald-400 bg-emerald-50/50 dark:bg-emerald-400/5 px-2 py-1 rounded-lg border border-emerald-200/50 dark:border-emerald-500/20 hover:bg-emerald-100 dark:hover:bg-emerald-400/10 transition-all">
                                <span>📂</span> 
                                <span>View ${m.archives.length} merged branch(es) under this node</span>
                            </button>
                        `;

                        // Bind click event to open modal
                        archiveEntry.querySelector("button").onclick = () =>
                            showArchiveModal(m.archives);

                        wrapper
                            .querySelector(".flex-col")
                            .appendChild(archiveEntry);
                    }

                    if (m.abstract) {
                        const summaryBox = document.createElement("div");
                        summaryBox.className =
                            "summary-box w-full mt-3 p-3.5 border-2 border-dashed border-amber-400/80 dark:border-amber-500/70 bg-amber-50/40 dark:bg-amber-950/20 rounded-xl text-xs text-gray-700 dark:text-gray-200 font-sans shadow-sm transition-all";
                        summaryBox.innerHTML = `
                            <div class="flex items-center justify-between mb-1.5">
                                <div class="flex items-center gap-2 text-amber-600 dark:text-amber-400 font-mono font-medium">
                                    <span>⚡</span> Conversation Summary
                                </div>
                            </div>
                            <div class="summary-content markdown-body text-xs opacity-90">${m.abstract}</div>
                        `;
                        // Insert summary box below AI bubble and above notebook bar
                        wrapper
                            .querySelector(".flex-col")
                            .appendChild(summaryBox);
                    }

                    lastAiWrapperCol = wrapper.querySelector(".flex-col");
                    chatBox.appendChild(wrapper);
                }
            });
            if (lastAiWrapperCol) {
                const notebookBar = createNotebookBar();
                lastAiWrapperCol.appendChild(notebookBar);
            }
            // Code highlighting and emoji parsing
            chatBox
                .querySelectorAll(".ai-bubble pre code")
                .forEach((el) => hljs.highlightElement(el));
            chatBox
                .querySelectorAll(".ai-bubble, .user-bubble")
                .forEach((el) => {
                    twemoji.parse(el, { folder: "svg", ext: ".svg" });
                });
        }
    } catch (e) {
        console.error("Failed to load chat history:", e);
    }
}

// ==================================== SEND MESSAGES ==============================================
let chatAbortController = null;

const chatBox = document.getElementById("chat-box");
const input = document.getElementById("user-input");
const loading = document.getElementById("ai-loading-template");
const sendBtn = document.getElementById("send-btn");

if (input) {
    input.addEventListener("input", () => {
        // Reset height to recalculate scrollHeight
        input.style.height = "auto";

        // Set maximum height (keep consistent with CSS)
        const maxHeight = 200;
        const currentScrollHeight = input.scrollHeight;

        if (currentScrollHeight > maxHeight) {
            // Reached upper limit, lock height and enable scrolling
            input.style.height = maxHeight + "px";
            input.style.overflowY = "auto";
        } else {
            // Below upper limit, adjust height dynamically and hide scrollbar
            input.style.height = currentScrollHeight + "px";
            input.style.overflowY = "hidden";
        }
    });
}

async function send() {
    if (sendBtn.classList.contains("is-loading")) {
        if (chatAbortController) {
            chatAbortController.abort();
        }
        return;
    }

    const text = input.value.trim();
    // Do not send if both text and image are empty
    if (!text && !currentImageBase64) return;

    sendBtn.classList.add("is-loading");
    // Change button UI to stop icon
    sendBtn.innerHTML = `<svg class="w-5 h-5 animate-pulse" fill="currentColor" viewBox="0 0 24 24"><path fill-rule="evenodd" d="M4.5 7.5a3 3 0 013-3h9a3 3 0 013 3v9a3 3 0 01-3 3h-9a3 3 0 01-3-3v-9z" clip-rule="evenodd" /></svg>`;

    // Stash image and reset input
    const imageToSend = currentImageBase64;
    input.value = "";
    input.style.height = "auto";
    clearImage(); // Clear image preview

    // Build user UI bubble
    const safeUserText = formatUserText(text);
    let userBubbleHtml = `<div class="message-row flex justify-start mb-8"><div class="user-bubble p-4 rounded-2xl max-w-[85%]">`;

    // Insert image node if present
    if (imageToSend) {
        userBubbleHtml += `<img src="${imageToSend}" class="max-w-full rounded-lg mb-2 shadow-sm border border-black/5 dark:border-white/5">`;
    }

    // Insert text content if present
    if (safeUserText) {
        userBubbleHtml += `<div>${safeUserText}</div>`;
    }

    userBubbleHtml += `</div></div>`;

    chatBox.insertAdjacentHTML("beforeend", userBubbleHtml);
    loading.classList.remove("hidden");

    // Scroll handling
    const lastMessageImg = chatBox.querySelector(".message-row:last-child img");

    if (lastMessageImg) {
        // Wait for image to load before scrolling
        lastMessageImg.onload = () => {
            chatBox.scrollTo({ top: chatBox.scrollHeight, behavior: "smooth" });
        };
    } else {
        // Plain text: scroll immediately
        chatBox.scrollTo({ top: chatBox.scrollHeight, behavior: "smooth" });
    }

    chatAbortController = new AbortController();

    try {
        const response = await fetch("/api/chat", {
            method: "POST",
            headers: getHeaders(),
            signal: chatAbortController.signal,
            body: JSON.stringify({
                message: text || "\u200B",
                // Strip "data:image/jpeg;base64," prefix and send base64 data only
                image: imageToSend ? imageToSend.split(",")[1] : null,
                custom_url: localStorage.getItem("custom_api_url") || "",
                custom_embedding_url: localStorage.getItem("custom_embedding_url") || "",
            }),
        });

        if (response.status === 401) {
            throw new Error("401_UNAUTHORIZED");
        }

        if (!response.ok) {
            const errorMsg = await extractErrorMessage(response);
            throw new Error(errorMsg);
        }

        await handleStreamResponse(response);
    } catch (err) {
        if (err.name === "AbortError") {
            console.log("User aborted the AI response");
        } else {
            loading.classList.add("hidden");
            let displayTitle = "⚠️ System Error Encountered";
            let displayMsg = formatUserText(
                err.message || "Connection error. Please check backend server.",
            );

            if (err.message === "401_UNAUTHORIZED") {
                displayTitle = "🔑 Authentication Failed";
                displayMsg =
                    "A valid API Key is required to continue. Please verify it in settings.";
            }

            const errorHtml = `
                <div class="flex justify-start mb-4">
                    <div class="p-4 rounded-2xl bg-red-50 text-red-600 border border-red-200 text-sm shadow-sm max-w-[90%]">
                        <div class="font-bold mb-1">${displayTitle}</div>
                        <div>${displayMsg}</div>
                    </div>
                </div>`;

            chatBox.insertAdjacentHTML("beforeend", errorHtml);
            chatBox.scrollTop = chatBox.scrollHeight;
            checkLlamaConnection();
        }
    } finally {
        loading.classList.add("hidden");
        sendBtn.classList.remove("is-loading");
        sendBtn.innerHTML = "Send";
        chatAbortController = null;
        get_ctx_usage();
    }
}

// Core stream chunk handlers
const streamChunkHandlers = {
    model: (json, ctx) => {
        ctx.modelName = json.model.split("/").pop().split("\\").pop();
    },
    usage: (json, ctx) => {
        if (json.usage && json.usage.completion_tokens) {
            ctx.tokenCount = json.usage.completion_tokens;
            ctx.hasOfficialUsage = true;
        }
    },
    choices: (json, ctx) => {
        const content = json.choices?.[0]?.delta?.content || "";
        if (!content) return;

        if (!ctx.startTime) ctx.startTime = Date.now();
        if (!ctx.hasOfficialUsage) ctx.tokenCount++;

        ctx.fullText += content;

        // Push incoming characters into buffer queue
        ctx.charBuffer.push(...content.split(""));

        // On first chunk received: create AI bubble DOM
        if (ctx.isFirstChunk) {
            loading.classList.add("hidden");
            ctx.currentBubbleId = "ai-" + Date.now();
            const html = `
                        <div class="message-row flex justify-start mb-8">
                            <div class="flex flex-col max-w-[90%]">
                                <div id="${ctx.currentBubbleId}" class="ai-bubble p-4 rounded-2xl markdown-body">
                                </div>
                                <div id="meta-${ctx.currentBubbleId}" class="flex items-center gap-3 px-2 mt-1.5 text-xs text-gray-400 dark:text-gray-400 font-mono opacity-80">
                                    <span class="bg-gray-100 dark:bg-white/5 px-1.5 py-0.5 rounded text-[11px]">${ctx.modelName}</span>
                                    <span id="speed-${ctx.currentBubbleId}">⏱️ Calculating...</span>
                                </div>
                            </div>
                        </div>`;
            chatBox.insertAdjacentHTML("beforeend", html);
            ctx.aiBubbleDiv = document.getElementById(ctx.currentBubbleId);
            ctx.isFirstChunk = false;
        }
    },
};

async function handleStreamResponse(response) {
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";

    const ctx = {
        fullText: "",
        displayedText: "", // Rendered characters
        charBuffer: [], // Buffer queue
        isFirstChunk: true,
        aiBubbleDiv: null,
        tokenCount: 0,
        startTime: null,
        modelName: "GGUF Model",
        currentBubbleId: "",
        hasOfficialUsage: false,
        isRenderPending: false,
    };

    // Check and auto-close unclosed Markdown code blocks (```)
    function fixUnclosedCodeBlocks(markdownText) {
        const matches = markdownText.match(/```/g);
        // If an odd count of ``` is matched, an open code block exists
        if (matches && matches.length % 2 !== 0) {
            return markdownText + "\n```"; // Temporarily append closing code block
        }
        return markdownText;
    }

    // Start smooth buffer rendering timer (consume queue at ~16ms with rAF)
    const bufferTimer = setInterval(() => {
        if (
            ctx.charBuffer.length > 0 &&
            ctx.aiBubbleDiv &&
            !ctx.isRenderPending
        ) {
            ctx.isRenderPending = true;

            requestAnimationFrame(() => {
                if (ctx.aiBubbleDiv) {
                    // Dynamic step: adjust consumption rate based on queue backlog
                    const step =
                        ctx.charBuffer.length > 30
                            ? 4
                            : ctx.charBuffer.length > 10
                                ? 2
                                : 1;
                    const chunk = ctx.charBuffer.splice(0, step).join("");
                    ctx.displayedText += chunk;

                    // Close any unclosed code blocks before parsing for marked compatibility
                    const streamMarkdown = fixUnclosedCodeBlocks(
                        ctx.displayedText,
                    );
                    ctx.aiBubbleDiv.innerHTML =
                        safeMarkdownParse(streamMarkdown);

                    // Re-highlight code blocks inside the current bubble after each frame
                    ctx.aiBubbleDiv
                        .querySelectorAll("pre code")
                        .forEach((el) => hljs.highlightElement(el));

                    updateStreamingSpeed(
                        ctx.currentBubbleId,
                        ctx.startTime,
                        ctx.tokenCount,
                    );
                    scrollToBottomIfNear();
                }
                ctx.isRenderPending = false;
            });
        }
    }, 16);

    try {
        while (true) {
            const { done, value } = await reader.read();
            if (done) break;

            buffer += decoder.decode(value, { stream: true });
            const lines = buffer.split("\n");
            buffer = lines.pop();

            for (const line of lines) {
                const trimmed = line.trim();
                if (!trimmed.startsWith("data: ") || trimmed === "data: [DONE]")
                    continue;

                const json = JSON.parse(trimmed.substring(6));

                for (const key in streamChunkHandlers) {
                    if (json[key] !== undefined) {
                        streamChunkHandlers[key](json, ctx);
                    }
                }
            }
        }

        // Flush remaining characters in buffer queue
        while (ctx.charBuffer.length > 0) {
            await new Promise((r) => setTimeout(r, 16));
        }
    } catch (streamError) {
        console.error("Error during stream reading:", streamError);
        throw streamError;
    } finally {
        clearInterval(bufferTimer); // Clear timer when consumed
        finalizeAiBubble(ctx);
        scrollToBottomIfNear();
    }
}

function updateStreamingSpeed(currentBubbleId, startTime, tokenCount) {
    if (!startTime) return;
    const elapsed = (Date.now() - startTime) / 1000;
    if (elapsed > 0) {
        const speed = (tokenCount / elapsed).toFixed(1);
        const speedSpan = document.getElementById(`speed-${currentBubbleId}`);
        if (speedSpan) {
            speedSpan.innerText = ` ${elapsed.toFixed(1)}s / ${speed} t/s`;
        }
    }
}

function finalizeAiBubble(ctx) {
    if (!ctx.aiBubbleDiv) return;

    // Final complete safe Markdown parsing for the last frame
    ctx.aiBubbleDiv.innerHTML = safeMarkdownParse(ctx.fullText);

    // Highlight code blocks and parse emojis
    ctx.aiBubbleDiv
        .querySelectorAll("pre code")
        .forEach((el) => hljs.highlightElement(el));
    twemoji.parse(ctx.aiBubbleDiv, { folder: "svg", ext: ".svg" });

    // Update speed and token usage statistics
    if (ctx.startTime && ctx.currentBubbleId) {
        const elapsed = (Date.now() - ctx.startTime) / 1000;
        const speed = (ctx.tokenCount / (elapsed || 1)).toFixed(1);
        const speedSpan = document.getElementById(
            `speed-${ctx.currentBubbleId}`,
        );
        if (speedSpan) {
            speedSpan.innerHTML = ` took ${elapsed.toFixed(1)}s  (total ${ctx.tokenCount} tokens / speed ${speed} t/s)`;
        }
    }

    document.querySelectorAll(".notebook-bar").forEach((bar) => bar.remove());

    // Append Notebook floating control bar at the bottom
    const notebookBar = createNotebookBar();
    ctx.aiBubbleDiv.parentElement.appendChild(notebookBar);
}

function createNotebookBar() {
    const notebookBar = document.createElement("div");
    notebookBar.className =
        "notebook-bar group relative flex flex-col items-center justify-center my-4 opacity-40 hover:opacity-100 transition-opacity duration-200";

    notebookBar.innerHTML = `
    <div class="w-full relative flex items-center justify-center">
        <!-- Background horizontal line -->
        <div class="absolute inset-0 flex items-center">
            <div class="w-full border-t border-gray-200 dark:border-gray-800"></div>
        </div>
        
        <!-- Floating action button group -->
        <div class="relative flex items-center gap-2 bg-white dark:bg-[#1e1f20] px-3 py-1 rounded-md border border-gray-200 dark:border-gray-700 shadow-sm text-xs font-mono">
            <button class="switch-btn hover:text-rose-500 transition-colors flex items-center gap-1 py-0.5 px-1.5 rounded hover:bg-gray-100 dark:hover:bg-white/5">
                <span class="text-grey-400 font-bold">s</span> switch to main 
            </button>
            <span class="text-gray-300 dark:text-gray-700">|</span>

            <!-- Container for Fork Side Chat with Popover -->
            <div class="relative inline-block">
                <button class="fork-side-btn hover:text-emerald-500 transition-colors flex items-center gap-1 py-0.5 px-1.5 rounded hover:bg-gray-100 dark:hover:bg-white/5">
                    <span class="text-grey-400 font-bold">🌿</span> Side Chat
                </button>
                
                <!-- Floating options popover (hidden by default) -->
                <div class="fork-options-popover hidden absolute bottom-full mb-2 left-1/2 -translate-x-1/2 w-52 bg-white dark:bg-[#1e1f20] border border-gray-200 dark:border-gray-700 rounded-xl shadow-xl p-1.5 z-30 flex flex-col gap-1 text-[11px] animate-fade-in">
                    <button class="fork-with-summary text-left px-2.5 py-1.5 rounded-lg hover:bg-emerald-50 dark:hover:bg-emerald-950/40 text-gray-700 dark:text-gray-200 flex items-center gap-2 transition-colors">
                        <span>⚡</span>
                        <div>
                            <div class="font-medium">With Summary</div>
                            <div class="text-[9px] text-gray-400">Keep core context</div>
                        </div>
                    </button>
                    <button class="fork-clean text-left px-2.5 py-1.5 rounded-lg hover:bg-emerald-50 dark:hover:bg-emerald-950/40 text-gray-700 dark:text-gray-200 flex items-center gap-2 transition-colors">
                        <span>🌱</span>
                        <div>
                            <div class="font-medium">Isolated Sandbox</div>
                            <div class="text-[9px] text-gray-400">Clean slate, zero pollution</div>
                        </div>
                    </button>
                </div>
            </div>

            <span class="text-gray-300 dark:text-gray-700">|</span>
            <button class="summary-btn hover:text-amber-500 transition-colors flex items-center gap-1 py-0.5 px-1.5 rounded hover:bg-gray-100 dark:hover:bg-white/5">
                <span class="text-grey-400 font-bold">⚡</span> Summary
            </button>
            <span class="text-gray-300 dark:text-gray-700">|</span>
            <button class="merge-btn hover:text-purple-500 transition-colors flex items-center gap-1 py-0.5 px-1.5 rounded hover:bg-gray-100 dark:hover:bg-white/5">
                <span class="text-grey-400 font-bold">m</span> Merge
            </button>
            <span class="text-gray-300 dark:text-gray-700">|</span>
            <button class="archive-btn hover:text-blue-500 transition-colors flex items-center gap-1 py-0.5 px-1.5 rounded hover:bg-gray-100 dark:hover:bg-white/5">
                <span class="text-grey-400 font-bold">📑</span> Archive main
            </button>
        </div>
    </div>
    `;

    // Archive event
    const archiveBtn = notebookBar.querySelector(".archive-btn");

    archiveBtn.addEventListener("click", async () => {
        try {
            archiveBtn.innerHTML = `<span class="text-blue-500 animate-spin">⏳</span> Archiving...`;

            let res = await fetch("/api/archive-main", {
                method: "POST",
                headers: getHeaders(),
            });

            // If no summary exists, prompt to generate summary first
            if (res.status === 412) {
                alert("Please click Summary to generate a summary before archiving.");
                archiveBtn.innerHTML = `<span class="text-grey-500 font-bold">📑</span> Archive`;
                return;
            }

            if (res.ok) {
                archiveBtn.innerHTML = `<span class="text-blue-500">✓</span> Done`;

                setTimeout(() => {
                    loadHistory().then(() => {
                        // Scroll to bottom after history reload is complete
                        setTimeout(() => {
                            chatBox.scrollTop = chatBox.scrollHeight;
                        }, 30);
                    });
                }, 50);
            } else {
                const err = await res.json();
                alert(err.error || "Archive failed");
            }
        } catch (e) {
            console.error(e);
        }
    });

    // Bind Fork Side event
    const doForkSide = async (withSummary) => {
        try {
            const res = await fetch("/api/fork-side", {
                method: "POST",
                headers: getHeaders(),
                body: JSON.stringify({ with_summary: withSummary })
            });
            if (res.ok) {
                updateBranchIndicator("side");
                const notice = document.createElement("div");
                notice.className =
                    "text-center my-3 text-xs text-emerald-600 dark:text-emerald-400 font-mono bg-emerald-50 dark:bg-emerald-950/40 py-1 rounded-lg border border-emerald-200 dark:border-emerald-800/50";

                notice.innerText = withSummary
                    ? "🌿 Switched to side branch (Inherited summary)"
                    : "🌱 Switched to isolated side branch (Clean context)";

                chatBox.appendChild(notice);
                chatBox.scrollTop = chatBox.scrollHeight;
            }
        } catch (e) {
            console.error("Fork Side failed:", e);
        }
    };

    const forkBtn = notebookBar.querySelector(".fork-side-btn");
    const popover = notebookBar.querySelector(".fork-options-popover");

    // Toggle options popover visibility
    forkBtn.addEventListener("click", (e) => {
        e.stopPropagation();
        popover.classList.toggle("hidden");
    });

    // Option: Fork with summary
    notebookBar.querySelector(".fork-with-summary").addEventListener("click", async (e) => {
        e.stopPropagation();
        popover.classList.add("hidden");
        await doForkSide(true);
    });

    // Option: Fork isolated sandbox
    notebookBar.querySelector(".fork-clean").addEventListener("click", async (e) => {
        e.stopPropagation();
        popover.classList.add("hidden");
        await doForkSide(false);
    });

    // Close popover when clicking anywhere else on the document
    document.addEventListener("click", () => {
        if (!popover.classList.contains("hidden")) {
            popover.classList.add("hidden");
        }
    }, { once: false });

    // Bind Summary event
    notebookBar
        .querySelector(".summary-btn")
        .addEventListener("click", async () => {
            const btn = notebookBar.querySelector(".summary-btn");

            // If summary box already exists, toggle visibility
            let summaryBox =
                notebookBar.parentElement?.querySelector(".summary-box");
            if (summaryBox) {
                const textarea = summaryBox.querySelector("textarea");
                const hasError = summaryBox.querySelector(".text-rose-500");

                // Check condition: error exists or text content is empty/default
                const isInvalid =
                    hasError ||
                    (textarea &&
                        (textarea.value === "No summary content available." ||
                            textarea.value.trim() === ""));

                if (isInvalid) {
                    // Remove invalid box and re-fetch
                    summaryBox.remove();
                } else {
                    // Toggle visibility if valid
                    summaryBox.classList.toggle("hidden");
                    return;
                }
            }

            // Create yellow dashed summary container
            summaryBox = document.createElement("div");
            summaryBox.className =
                "summary-box w-full mt-3 p-3.5 border-2 border-dashed border-amber-400/80 dark:border-amber-500/70 bg-amber-50/40 dark:bg-amber-950/20 rounded-xl text-xs text-gray-700 dark:text-gray-200 font-sans shadow-sm transition-all";

            // Add action button header
            summaryBox.innerHTML = `
            <div class="flex items-center justify-between mb-1.5">
                <div class="flex items-center gap-2 text-amber-600 dark:text-amber-400 font-mono font-medium">
                    <span>⚡</span> Conversation Summary
                </div>
                <div id="summary-actions" class="flex items-center gap-2">
                    <!-- Save button injected here -->
                </div>
            </div>
            <div class="summary-content markdown-body text-xs opacity-90">Generating summary...</div>
        `;

            if (notebookBar.parentElement) {
                notebookBar.parentElement.appendChild(summaryBox);
            } else {
                notebookBar.appendChild(summaryBox);
            }

            scrollToBottomIfNear();

            const contentDiv = summaryBox.querySelector(".summary-content");
            const actionsDiv = summaryBox.querySelector("#summary-actions");
            const customUrl = localStorage.getItem("custom_api_url") || "";
            const customKey = localStorage.getItem("custom_api_key") || "";

            try {
                btn.innerHTML = `<span class="text-amber-500 animate-spin">⏳</span> Summarizing...`;

                const res = await fetch("/api/generate-abstract", {
                    method: "POST",
                    headers: getHeaders(),
                    body: JSON.stringify({
                        custom_url: customUrl,
                        custom_key: customKey,
                    }),
                });

                if (!res.ok) throw new Error("Failed to generate summary");

                const data = await res.json();
                const textResult = data.abstract || data.DialogAbstract || "";

                // Create textarea
                const textarea = document.createElement("textarea");
                textarea.className =
                    "w-full bg-transparent border-none focus:outline-none text-xs text-gray-700 dark:text-gray-200 resize-none leading-relaxed font-sans mt-1 p-1 rounded hover:bg-black/5 dark:hover:bg-white/5 transition-colors";
                if (!textResult) {
                    textarea.value = "";
                    textarea.placeholder =
                        "No summary content available. Click Summary to regenerate...";
                } else {
                    textarea.value = textResult;
                }

                const adjustHeight = (el) => {
                    el.style.height = "auto";
                    el.style.height = el.scrollHeight + "px";
                };

                // Create save button
                const saveBtn = document.createElement("button");
                saveBtn.className =
                    "hidden flex items-center gap-1 px-2 py-0.5 bg-emerald-500 text-white rounded-md text-[10px] hover:bg-emerald-600 transition-all shadow-sm animate-fade-in";
                saveBtn.innerHTML = `<span>✓</span> Save Changes`;

                // Define save action logic
                const performSave = async () => {
                    saveBtn.innerHTML = `<span>⏳</span> Saving...`;
                    try {
                        await fetch("/api/edit-abstract", {
                            method: "POST",
                            headers: getHeaders(),
                            body: JSON.stringify({ abstract: textarea.value }),
                        });
                        // Show saved state, then hide
                        saveBtn.innerHTML = `<span>✓</span> Saved`;
                        saveBtn.classList.replace(
                            "bg-emerald-500",
                            "bg-blue-500",
                        );
                        setTimeout(() => {
                            saveBtn.classList.add("hidden");
                            saveBtn.classList.replace(
                                "bg-blue-500",
                                "bg-emerald-500",
                            );
                            saveBtn.innerHTML = `<span>✓</span> Save Changes`;
                        }, 1500);
                    } catch (e) {
                        saveBtn.innerHTML = `❌ Failed`;
                    }
                };

                saveBtn.onclick = performSave;

                // Listen to textarea input: show save button on modification
                textarea.addEventListener("input", () => {
                    adjustHeight(textarea);
                    if (saveBtn.classList.contains("hidden")) {
                        saveBtn.classList.remove("hidden");
                    }
                });

                contentDiv.innerHTML = "";
                contentDiv.appendChild(textarea);
                actionsDiv.appendChild(saveBtn);

                setTimeout(() => {
                    adjustHeight(textarea);
                    chatBox.scrollTop = chatBox.scrollHeight;
                }, 50);

                btn.innerHTML = `<span class="text-amber-500 font-bold">✓</span> Summary`;
            } catch (e) {
                console.error("Summary failed:", e);
                contentDiv.innerHTML = `<span class="text-rose-500">Error generating summary: ${e.message}</span>`;
                btn.innerHTML = `<span class="text-amber-500 font-bold">⚡</span> Summary`;
            }
        });

    // Bind Merge event
    const mergeBtn = notebookBar.querySelector(".merge-btn");
    mergeBtn.addEventListener("click", async () => {
        await handleMergeAction(mergeBtn);
    });

    // switch-side-to-main event
    const switchBtn = notebookBar.querySelector(".switch-btn");
    switchBtn.addEventListener("click", async () => {
        if (!confirm("Are you sure you want to discard the current side branch?")) return;

        try {
            switchBtn.innerHTML = `<span class="text-rose-500 animate-spin">⏳</span> switching...`;

            const res = await fetch("/api/switch-side-to-main", {
                method: "POST",
                headers: getHeaders(),
            });

            if (res.ok) {
                updateBranchIndicator("main");
                await loadHistory();
                setTimeout(() => {
                    chatBox.scrollTop = chatBox.scrollHeight;
                }, 50);
            } else {
                throw new Error("Failed to discard branch");
            }
        } catch (e) {
            alert(e.message);
            switchBtn.innerHTML = `<span class="text-rose-500 font-bold">×</span> switch`;
        }
    });

    return notebookBar;
}

async function handleMergeAction(btn) {
    const originalContent = btn.innerHTML;

    try {
        btn.innerHTML = `<span class="text-purple-500 animate-spin">⏳</span> Merging...`;
        btn.disabled = true;

        const customUrl = localStorage.getItem("custom_api_url") || "";
        const customKey = localStorage.getItem("custom_api_key") || "";

        // Attempt merge
        let res = await fetch("/api/merge", {
            method: "POST",
            headers: getHeaders(),
        });

        // If 412 (Precondition Failed), summary is missing; trigger summary first
        if (res.status === 412) {
            btn.innerHTML = `<span class="text-amber-500 animate-pulse">📝</span> Summarizing first...`;

            const sumRes = await fetch("/api/generate-abstract", {
                method: "POST",
                headers: getHeaders(),
                body: JSON.stringify({
                    custom_url: customUrl,
                    custom_key: customKey,
                }),
            });

            if (!sumRes.ok)
                throw new Error("Automatic summary generation failed. Please click Summary manually.");

            // Retry merge after summary succeeds
            res = await fetch("/api/merge", {
                method: "POST",
                headers: getHeaders(),
            });
        }

        if (!res.ok) {
            const errData = await res.json();
            throw new Error(errData.error || "Merge failed");
        }

        // Merge succeeded, refresh view
        btn.innerHTML = `<span class="text-purple-500">✓</span> Done`;
        updateBranchIndicator("main");

        // Small delay to allow user to see "Done" before reload
        setTimeout(() => {
            loadHistory(); // Reload history (currentChain is now positioned at main line end)
            setTimeout(() => {
                chatBox.scrollTop = chatBox.scrollHeight;
            }, 100);
        }, 500);
    } catch (e) {
        console.error("Merge failed:", e);
        alert("Merge failed: " + e.message);
        btn.innerHTML = originalContent;
        btn.disabled = false;
    }
}

async function extractErrorMessage(response) {
    let errorText = `Request failed with status: ${response.status}`;
    try {
        const errJson = await response.json();
        if (errJson.error) return `${errorText} (${errJson.error})`;
        if (errJson.message) return `${errorText} (${errJson.message})`;
    } catch (_) {
        try {
            const text = await response.text();
            if (text) return `${errorText} - ${text}`;
        } catch (_) { }
    }
    return errorText;
}

/**
 * Display archived branch content modal
 * @param {Array} archives - Backend payload format: [[Msg1, Msg2], [MsgA, MsgB]]
 */
function showArchiveModal(archives) {
    const modal = document.getElementById("archive-modal");
    const contentContainer = document.getElementById("archive-content");

    if (!modal || !contentContainer) return;

    // Clear previous content
    contentContainer.innerHTML = "";

    // Iterate through each branch
    archives.forEach((branch, index) => {
        // Divider
        const divider = document.createElement("div");
        divider.className = "relative py-6 flex items-center justify-center";
        divider.innerHTML = `
            <div class="absolute inset-0 flex items-center"><div class="w-full border-t border-gray-200 dark:border-gray-800"></div></div>
            <span class="relative px-3 bg-gray-50 dark:bg-[#131415] text-[10px] text-gray-400 font-mono tracking-widest uppercase">Archived Branch #${index + 1}</span>
        `;
        contentContainer.appendChild(divider);

        // Render each message in the branch
        branch.forEach((msg) => {
            const isUser = msg.role === "user";
            const msgDiv = document.createElement("div");
            msgDiv.className = `flex ${isUser ? "justify-end" : "justify-start"} mb-4`;

            // Simplified bubble style distinct from main thread
            msgDiv.innerHTML = `
                <div class="p-3 rounded-xl max-w-[85%] text-sm shadow-sm border ${isUser
                    ? "bg-blue-500 text-white border-blue-400"
                    : "bg-white dark:bg-[#1e1f20] text-gray-800 dark:text-gray-200 border-gray-100 dark:border-gray-800"
                }">
                    ${isUser ? formatUserText(msg.content) : safeMarkdownParse(msg.content)}
                </div>
            `;
            contentContainer.appendChild(msgDiv);
        });
    });

    // Show modal and disable background page scrolling
    modal.classList.remove("hidden");
    document.body.style.overflow = "hidden";

    // Handle modal close
    const closeBtn = document.getElementById("close-archive");
    const closeModal = () => {
        modal.classList.add("hidden");
        document.body.style.overflow = "";
    };
    closeBtn.onclick = closeModal;
    modal.onclick = (e) => {
        if (e.target === modal) closeModal();
    };
}

document.getElementById("send-btn").addEventListener("click", send);
// ============================= BUTTONS ============================================

// Image handling DOM elements
const imageInput = document.getElementById("image-input");
const imagePreviewWrapper = document.getElementById("image-preview-wrapper");
const imagePreview = document.getElementById("image-preview");
const removeImageBtn = document.getElementById("remove-image");

let currentImageBase64 = null; // Store image data to be sent

// Process and resize image
async function processImage(file) {
    return new Promise((resolve, reject) => {
        const reader = new FileReader();
        reader.readAsDataURL(file);
        reader.onload = (e) => {
            const img = new Image();
            img.src = e.target.result;
            img.onload = () => {
                const canvas = document.createElement("canvas");
                const ctx = canvas.getContext("2d");

                let width = img.width;
                let height = img.height;
                const maxSide = 2048; // Target max dimension

                // Compute aspect-ratio preserved dimensions
                if (width > height) {
                    if (width > maxSide) {
                        height = Math.round(height * (maxSide / width));
                        width = maxSide;
                    }
                } else {
                    if (height > maxSide) {
                        width = Math.round(width * (maxSide / height));
                        height = maxSide;
                    }
                }

                canvas.width = width;
                canvas.height = height;

                // Draw scaled image on canvas
                ctx.drawImage(img, 0, 0, width, height);

                // Export as JPEG
                const dataUrl = canvas.toDataURL("image/jpeg", 1.0);
                resolve(dataUrl);
            };
            img.onerror = reject;
        };
        reader.onerror = reject;
    });
}

// Listen for file selection
imageInput.addEventListener("change", async (e) => {
    const file = e.target.files[0];
    if (!file) return;

    try {
        imagePreview.style.opacity = "0.5";

        // Resize image
        const resizedBase64 = await processImage(file);

        currentImageBase64 = resizedBase64;
        imagePreview.src = resizedBase64;
        imagePreview.style.opacity = "1";
        imagePreviewWrapper.classList.remove("hidden");

        console.log("Image processed to fit within 2048x2048");
    } catch (err) {
        console.error("Failed to process image:", err);
        alert("Failed to process image");
    }
});

// Remove image
removeImageBtn.addEventListener("click", () => {
    clearImage();
});

function clearImage() {
    currentImageBase64 = null;
    imagePreview.src = "";
    imagePreviewWrapper.classList.add("hidden");
    imageInput.value = "";
}

// Download/Export chat
async function downloadChat() {
    try {
        // Fetch raw Markdown chat history from backend
        const res = await fetch("/api/history");
        const data = await res.json();

        if (!data || data.length === 0) {
            alert("No chat history available to export");
            return;
        }

        let content = "--- Chat History ---\n\n";
        data.forEach((m) => {
            if (m.role === "system") return; // Ignore system prompt; export user and AI dialogs only

            const role = m.role === "user" ? "[User]" : "[AI]";
            content += `${role}\n${m.content.trim()}\n\n`;
        });

        const blob = new Blob([content], { type: "text/plain;charset=utf-8" });
        const url = URL.createObjectURL(blob);
        const a = document.createElement("a");
        a.href = url;
        a.download = `chat-${Date.now()}.txt`;
        a.click();
        URL.revokeObjectURL(url);
    } catch (e) {
        console.error("Failed to export chat history:", e);
        alert("Export failed, please try again.");
    }
}

async function newChat() {
    if (confirm("Clear all conversations?")) {
        // Abort in-flight request if currently generating
        if (chatAbortController) {
            chatAbortController.abort();
            chatAbortController = null;
        }

        // Reset button and loading states
        loading.classList.add("hidden");
        sendBtn.classList.remove("is-loading");
        sendBtn.innerHTML = "Send";

        try {
            await fetch("/api/new-chat", { headers: getHeaders() });
            updateBranchIndicator("main");
            chatBox.innerHTML = `
                <div class="flex justify-start mb-8">
                    <div class="ai-bubble p-4 rounded-2xl max-w-[90%] markdown-body">
                        Hello!
                    </div>
                </div>`;
            get_ctx_usage(); // Reset context calculation
        } catch (e) {
            console.error("Failed to clear conversation:", e);
        }
    }
}

function toggleDarkMode() {
    document.documentElement.classList.toggle("dark-mode-active");
    const isDark = document.documentElement.classList.toggle("dark");
    localStorage.setItem("theme", isDark ? "dark" : "light");
    updateModeIcon(isDark);
}

function updateModeIcon(isDark) {
    document.getElementById("mode-icon").innerHTML = isDark
        ? `<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 3v1m0 16v1m9-9h-1M4 12H3m15.364-6.364l-.707.707M6.343 17.657l-.707.707m12.728 0l-.707-.707M6.343 6.343l-.707-.707M12 8a4 4 0 100 8 4 4 0 000-8z"></path>`
        : `<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M20.354 15.354A9 9 0 018.646 3.646 9.003 9.003 0 0012 21a9.003 9.003 0 008.354-5.646z"></path>`;
}

document.getElementById("download-btn").addEventListener("click", downloadChat);
document.getElementById("theme-btn").addEventListener("click", toggleDarkMode);
document.getElementById("new-chat-btn").addEventListener("click", newChat);

// ========================= SETTINGS PAGE =========================================
const modal = document.getElementById("settings-modal");
const customUrlInput = document.getElementById("custom-url");
const customEmbeddingUrlInput = document.getElementById("custom-embedding-url");
const customKeyInput = document.getElementById("custom-key");

document.getElementById("settings-btn").addEventListener("click", () => {
    customUrlInput.value = localStorage.getItem("custom_api_url") || "";
    if (customEmbeddingUrlInput) {
        customEmbeddingUrlInput.value = localStorage.getItem("custom_embedding_url") || "";
    }
    customKeyInput.value = localStorage.getItem("custom_api_key") || "";
    modal.classList.remove("hidden");
});

document.getElementById("close-settings").addEventListener("click", () => {
    modal.classList.add("hidden");
});

modal.addEventListener("click", (e) => {
    if (e.target === modal) modal.classList.add("hidden");
});

document.getElementById("save-settings").addEventListener("click", () => {
    localStorage.setItem("custom_api_url", customUrlInput.value.trim());
    if (customEmbeddingUrlInput) {
        localStorage.setItem("custom_embedding_url", customEmbeddingUrlInput.value.trim());
    }
    localStorage.setItem("custom_api_key", customKeyInput.value.trim());
    modal.classList.add("hidden");
});

// ================================= INDICATOR LIGHT ==========================================
const statusDot = document.getElementById("status-dot");

async function checkLlamaConnection() {
    const customUrl = localStorage.getItem("custom_api_url") || "";
    let healthUrl = "/health";

    if (customUrl) {
        try {
            const urlObj = new URL(customUrl);
            healthUrl = `${urlObj.protocol}//${urlObj.host}/health`;
        } catch (e) {
            console.error("Failed to parse custom URL:", e);
        }
    }

    try {
        const controller = new AbortController();
        const timeoutId = setTimeout(() => controller.abort(), 3000);

        const res = await fetch(healthUrl, {
            method: "GET",
            signal: controller.signal,
            headers: getHeaders(),
        });

        clearTimeout(timeoutId);

        if (res.ok) {
            const data = await res.json();
            if (data.status === "ok") {
                statusDot.className =
                    "w-2.5 h-2.5 rounded-full bg-emerald-500 transition-colors duration-300 shadow-[0_0_8px_rgba(16,185,129,0.5)]";
                statusDot.title = "Connected to llama.cpp successfully";
                return;
            }
        }
        throw new Error("Service status abnormal");
    } catch (err) {
        statusDot.className =
            "w-2.5 h-2.5 rounded-full bg-rose-500 transition-colors duration-300 shadow-[0_0_8px_rgba(244,63,94,0.5)] animate-pulse";
        statusDot.title = "Unable to connect to llama.cpp. Please check backend server status.";
    }
}

// Fetch and update context token usage for llama.cpp
async function get_ctx_usage() {
    const customUrl = localStorage.getItem("custom_api_url") || "";
    const tokenDisplay = document.getElementById("token-usage");
    if (!tokenDisplay) return;

    try {
        const propsUrl = `/api/llama-props?custom_url=${encodeURIComponent(customUrl)}`;
        const res = await fetch(propsUrl, {
            headers: getHeaders(),
        });

        if (res.ok) {
            const data = await res.json();
            const maxCtx = data.default_generation_settings?.n_ctx || 0;
            let currentCtx = 0;

            if (data.slots && data.slots.length > 0) {
                currentCtx = data.slots[0].n_past || 0;
            }

            // Fallback gracefully if maxCtx is invalid
            if (maxCtx <= 0) {
                tokenDisplay.innerText = `-- / -- (0%)`;
                tokenDisplay.classList.remove("text-rose-500");
                return;
            }

            const percentage = Math.min(
                100,
                Math.round((currentCtx / maxCtx) * 100),
            );
            tokenDisplay.innerText = `${currentCtx} tokens / ${maxCtx} tokens (${percentage}%)`;

            if (percentage > 85) {
                tokenDisplay.classList.add("text-rose-500");
            } else {
                tokenDisplay.classList.remove("text-rose-500");
            }
        }
    } catch (err) {
        console.error("Failed to fetch context usage:", err);
        tokenDisplay.innerText = `-- / -- (0%)`;
        tokenDisplay.classList.remove("text-rose-500");
    }
}

// ========================= IN-PAGE SEARCH =========================================
const searchToggleBtn = document.getElementById("search-toggle-btn");
const pageSearchPanel = document.getElementById("page-search-panel");
const pageSearchInput = document.getElementById("page-search-input");
const pageSearchList = document.getElementById("page-search-list");
const closePageSearchBtn = document.getElementById("close-page-search");

// Toggle search panel visibility
searchToggleBtn.addEventListener("click", () => {
    pageSearchPanel.classList.toggle("hidden");
    if (!pageSearchPanel.classList.contains("hidden")) {
        pageSearchInput.focus();
    }
});

// Close search panel
closePageSearchBtn.addEventListener("click", () => {
    pageSearchPanel.classList.add("hidden");
    pageSearchInput.value = '';
    pageSearchList.innerHTML = '';
});

// Listen to search input change
pageSearchInput.addEventListener("input", function () {
    const keyword = this.value.trim();
    pageSearchList.innerHTML = ''; // Clear results

    if (!keyword) return;

    let hasResult = false;
    // Get all user, AI, and summary bubbles in chat
    const chatBubbles = document.querySelectorAll("#chat-box .user-bubble, #chat-box .ai-bubble, #chat-box .summary-box");

    const lowerKeyword = keyword.toLowerCase();

    chatBubbles.forEach((bubble) => {
        // Use textContent for plain text matching only to avoid matching HTML tags
        const text = bubble.textContent;
        const lowerText = text.toLowerCase();

        let startIndex = 0;
        let index;

        // Loop to find all matches in the bubble
        while ((index = lowerText.indexOf(lowerKeyword, startIndex)) > -1) {
            hasResult = true;

            // Extract context snippet (15 characters before and after)
            const start = Math.max(0, index - 15);
            const end = Math.min(text.length, index + keyword.length + 15);
            let snippet = text.substring(start, end);

            if (start > 0) snippet = '...' + snippet;
            if (end < text.length) snippet = snippet + '...';

            // Highlight matching keyword (case-insensitive)
            const regex = new RegExp(`(${keyword})`, 'gi');
            // Escape snippet before injecting into HTML to prevent XSS
            snippet = snippet.replace(/</g, "&lt;").replace(/>/g, "&gt;");
            snippet = snippet.replace(regex, '<span class="text-blue-600 dark:text-blue-400 font-bold bg-blue-50 dark:bg-blue-900/30 px-0.5 rounded">$1</span>');

            // Create list item
            const li = document.createElement("li");
            li.className = "px-4 py-2 border-b border-gray-100 dark:border-gray-800 cursor-pointer hover:bg-gray-50 dark:hover:bg-white/5 transition-colors text-gray-700 dark:text-gray-300";

            // Label message source
            let icon = "🤖";
            let label = "AI";
            if (bubble.classList.contains("user-bubble")) {
                icon = "👤";
                label = "User";
            } else if (bubble.classList.contains("summary-box")) {
                icon = "⚡";
                label = "Summary";
            }

            li.innerHTML = `<div class="text-[10px] text-gray-400 mb-0.5">${icon} ${label}</div><div>${snippet}</div>`;

            // Click to jump to message
            li.addEventListener("click", () => {
                // Smooth scroll to target bubble
                bubble.scrollIntoView({ behavior: "smooth", block: "center" });

                // Trigger highlight pulse animation
                bubble.classList.remove("target-flash");
                void bubble.offsetWidth; // Trigger reflow
                bubble.classList.add("target-flash");

                // Auto-collapse panel on mobile after selection
                if (window.innerWidth < 768) {
                    pageSearchPanel.classList.add("hidden");
                }
            });

            pageSearchList.appendChild(li);

            startIndex = index + keyword.length; // Continue searching remainder
        }
    });

    if (!hasResult) {
        pageSearchList.innerHTML = '<li class="p-4 text-center text-gray-400 dark:text-gray-500">No results found</li>';
    }
});

//=========================== UPLOAD FILE ===========================================
const docInput = document.getElementById("doc-input");
const uploadDocBtn = document.getElementById("upload-doc-btn");

if (docInput) {
    docInput.addEventListener("change", async (e) => {
        const file = e.target.files[0];
        if (!file) return;

        const formData = new FormData();
        formData.append("file", file);

        // Read configured API endpoints and append to payload
        const customUrl = localStorage.getItem("custom_api_url") || "";
        const customEmbeddingUrl = localStorage.getItem("custom_embedding_url") || "";

        formData.append("custom_url", customUrl);
        formData.append("custom_embedding_url", customEmbeddingUrl);

        if (uploadDocBtn) uploadDocBtn.style.pointerEvents = "none";

        try {
            const key = localStorage.getItem("custom_api_key") || "";
            const res = await fetch("/api/upload-doc", {
                method: "POST",
                headers: {
                    Authorization: `Bearer ${key}`
                },
                body: formData,
            });

            if (!res.ok) {
                const errData = await res.json().catch(() => ({}));
                throw new Error(errData.error || "Document parsing or indexing failed");
            }

            // Display indexing success notice in chat box
            const chatBox = document.getElementById("chat-box");
            if (chatBox) {
                const noticeHtml = `
                    <div class="flex justify-center mb-4">
                        <div class="px-3 py-1.5 rounded-xl bg-blue-50 dark:bg-blue-950/40 text-blue-600 dark:text-blue-400 border border-blue-200 dark:border-blue-800/50 text-xs font-mono">
                            📚 Document "${file.name}" indexed successfully into knowledge base
                        </div>
                    </div>`;
                chatBox.insertAdjacentHTML("beforeend", noticeHtml);
                chatBox.scrollTop = chatBox.scrollHeight;
            }

        } catch (err) {
            console.error("Failed to upload document:", err);
            alert("Document indexing failed: " + err.message);
        } finally {
            // Restore button state and reset input to allow uploading file with same name again
            if (uploadDocBtn) uploadDocBtn.style.pointerEvents = "auto";
            docInput.value = "";
        }
    });
}

