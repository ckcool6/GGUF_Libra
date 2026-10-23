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

//=====================================icons===================================================
const SEND_ICON_HTML = `
<svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
    <line x1="12" y1="19" x2="12" y2="5"></line>
    <polyline points="5 12 12 5 19 12"></polyline>
</svg>`;

const SIDE_BRANCH_HTML = `
<svg xmlns="http://www.w3.org/2000/svg" width="1em" height="1em" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="vertical-align: -0.125em;">
  <path d="M11 20A7 7 0 0 1 9.8 6.1C15.5 5 17 4.48 19 2c1 2 2 4.18 2 8 0 5.5-4.78 10-10 10Z"/>
  <path d="M2 21c0-3 1.85-5.36 5.08-6C9.5 14.52 12 13 13 12"/>
</svg>
`
const CHECKPOINT_MAIN_HTML = `
<svg xmlns="http://www.w3.org/2000/svg" width="1em" height="1em" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="vertical-align: -0.125em;">
  <rect width="14" height="18" x="3" y="3" rx="2"/>
  <path d="M17 7h4v3h-4"/>
  <path d="M17 14h4v3h-4"/>
</svg>
`
const SANDBOX_HTML = `
<svg xmlns="http://www.w3.org/2000/svg" width="1em" height="1em" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="vertical-align: -0.125em;">
  <path d="M12 10a6 6 0 0 0-6-6H3v2a6 6 0 0 0 6 6h3"/>
  <path d="M12 14a6 6 0 0 1 6-6h3v1a6 6 0 0 1-6 6h-3"/>
  <path d="M12 20v-10"/>
</svg>
`
const LOADING_HTML = `
<svg xmlns="http://www.w3.org/2000/svg" width="1em" height="1em" viewBox="0 0 24 24" fill="currentColor" style="vertical-align: -0.125em;">
  <path d="M6 2v6h.01L6 8.01 10 12l-4 4 .01.01H6V22h12v-5.99h-.01L18 16l-4-4 4-3.99-.01-.01H18V2H6zm10 14.5V20H8v-3.5l4-4 4 4z"/>
</svg>
`
const SUMMARY_HTML = `
<svg xmlns="http://www.w3.org/2000/svg" width="1em" height="1em" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="vertical-align: -0.125em;">
  <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"/>
</svg>
`

const SWITCH_MAIN_HTML = `
<svg xmlns="http://www.w3.org/2000/svg" width="1em" height="1em" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="vertical-align: -0.125em;">
  <path d="M9 14 4 9l5-5"/>
  <path d="M4 9h10.5a5.5 5.5 0 0 1 5.5 5.5v0a5.5 5.5 0 0 1-5.5 5.5H11"/>
</svg>
`;

const MERGE_ICON_HTML = `
<svg xmlns="http://www.w3.org/2000/svg" width="1em" height="1em" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="vertical-align: -0.125em;">
  <circle cx="18" cy="18" r="3"/>
  <circle cx="6" cy="6" r="3"/>
  <path d="M6 21V9a9 9 0 0 0 9 9"/>
</svg>
`;

const WARNING_ICON_HTML = `
<svg class="w-4 h-4 text-amber-500 shrink-0 inline-block align-sub" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24">
    <path stroke-linecap="round" stroke-linejoin="round" d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126zM12 15.75h.007v.008H12v-.008z" />
</svg>`;

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
    const triggerBtn = document.getElementById("prompt-select-btn");
    const menuEl = document.getElementById("prompt-select-menu");
    const labelEl = document.getElementById("prompt-select-label");
    const arrowEl = triggerBtn?.querySelector("svg");

    if (!triggerBtn || !menuEl) return;

    try {
        const res = await fetch("/api/prompts", { headers: getHeaders() });
        if (!res.ok) throw new Error(`HTTP status error: ${res.status}`);

        const data = await res.json();
        const prompts = data.prompts || [];
        const activeId = data.active || "";

        menuEl.innerHTML = "";

        if (prompts.length > 0) {
            const currentPrompt = prompts.find(p => p.id === activeId) || prompts[0];
            if (labelEl && currentPrompt) {
                labelEl.innerText = currentPrompt.name;
                triggerBtn.dataset.value = currentPrompt.id;
            }

            prompts.forEach((p) => {
                const isSelected = p.id === (currentPrompt ? currentPrompt.id : activeId);
                const optEl = document.createElement("div");
                optEl.className = `custom-header-option px-3 py-1.5 text-xs rounded-lg cursor-pointer flex items-center justify-between transition-colors hover:bg-black/5 dark:hover:bg-[#282726] ${isSelected ? 'text-[#879A39] font-medium bg-[#879A39]/10' : 'text-gray-700 dark:text-[#CECDC3]'}`;
                optEl.dataset.value = p.id;
                optEl.innerHTML = `
                    <span>${p.name}</span>
                    <span class="check-icon text-[11px] ${isSelected ? '' : 'hidden'}">✓</span>
                `;

                optEl.onclick = async (e) => {
                    e.stopPropagation();
                    const newId = p.id;
                    if (triggerBtn.dataset.value === newId) {
                        menuEl.classList.add("hidden");
                        if (arrowEl) arrowEl.style.transform = "";
                        return;
                    }

                    if (chatAbortController) {
                        chatAbortController.abort();
                        chatAbortController = null;
                    }

                    loading.classList.add("hidden");
                    sendBtn.classList.remove("is-loading");
                    sendBtn.innerHTML = SEND_ICON_HTML;

                    try {
                        const switchRes = await fetch("/api/switch-prompt", {
                            method: "POST",
                            headers: { "Content-Type": "application/json" },
                            body: JSON.stringify({ id: newId }),
                        });

                        if (!switchRes.ok) throw new Error("Failed to switch prompt");

                        labelEl.innerText = p.name;
                        triggerBtn.dataset.value = newId;

                        menuEl.querySelectorAll(".custom-header-option").forEach(item => {
                            item.classList.remove("text-[#879A39]", "font-medium", "bg-[#879A39]/10");
                            item.classList.add("text-gray-700", "dark:text-[#CECDC3]");
                            item.querySelector(".check-icon")?.classList.add("hidden");
                        });
                        optEl.classList.add("text-[#879A39]", "font-medium", "bg-[#879A39]/10");
                        optEl.classList.remove("text-gray-700", "dark:text-[#CECDC3]");
                        optEl.querySelector(".check-icon")?.classList.remove("hidden");

                        get_ctx_usage();
                    } catch (err) {
                        console.error("Failed to switch prompt:", err);
                    } finally {
                        menuEl.classList.add("hidden");
                        if (arrowEl) arrowEl.style.transform = "";
                    }
                };

                menuEl.appendChild(optEl);
            });

            triggerBtn.onclick = (e) => {
                e.stopPropagation();
                const isHidden = menuEl.classList.toggle("hidden");
                if (arrowEl) arrowEl.style.transform = isHidden ? "" : "rotate(180deg)";
            };

            document.addEventListener("click", () => {
                if (!menuEl.classList.contains("hidden")) {
                    menuEl.classList.add("hidden");
                    if (arrowEl) arrowEl.style.transform = "";
                }
            });

        } else {
            labelEl.innerText = "No Prompts";
            triggerBtn.disabled = true;
        }
    } catch (e) {
        console.error("Failed to load prompt list:", e);
        if (labelEl) labelEl.innerText = "Disconnected";
        triggerBtn.disabled = true;
    }
}

// Helper function to display error/empty state in prompt select
function showPromptError(select, message) {
    select.innerHTML = `<option value="" disabled selected>[!] ${message}</option>`;
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

        indicator.classList.remove(
            "text-gray-600", "dark:text-gray-300",
            "text-emerald-600", "dark:text-emerald-400",
            "text-amber-600", "dark:text-amber-400"
        );

        if (text.startsWith("side")) {
            indicator.classList.add("text-emerald-600", "dark:text-emerald-400");
        } else {
            indicator.classList.add("text-amber-600", "dark:text-amber-400");
        }
    }
}

/**
 * Unified modal dialog for both alert notices and confirmations
 * (Replaces native alert/confirm to prevent Electron/Tabby webview focus-freeze bugs)
 * @param {string} message - Message text to display
 * @param {boolean} isAlert - If true, displays as an alert dialog with only the OK button
 * @returns {Promise<boolean>}
 */
function showModalDialog(message, isAlert = false) {
    return new Promise((resolve) => {
        const modal = document.getElementById("confirm-modal");
        const msgEl = document.getElementById("confirm-modal-msg");
        const okBtn = document.getElementById("confirm-modal-ok");
        const cancelBtn = document.getElementById("confirm-modal-cancel");

        msgEl.innerText = message;

        // If alert mode: hide the Cancel button; otherwise show it
        if (isAlert) {
            cancelBtn.classList.add("hidden");
            okBtn.innerText = "Got it";
            okBtn.className = "px-4 py-2 text-xs font-medium text-[#FFFCF0] bg-[#205EA6] hover:bg-[#1B4B82] dark:bg-[#205EA6]/80 dark:hover:bg-[#205EA6] active:scale-[0.98] rounded-xl transition-all shadow-xs";
        } else {
            cancelBtn.classList.remove("hidden");
            okBtn.innerText = "Confirm";
            okBtn.className = "px-4 py-2 text-xs font-medium text-[#FFFCF0] bg-[#AF3029] hover:bg-[#871F1B] dark:bg-[#AF3029]/80 dark:hover:bg-[#AF3029] active:scale-[0.98] rounded-xl transition-all shadow-xs";
        }

        modal.classList.remove("hidden");

        const close = (result) => {
            modal.classList.add("hidden");
            okBtn.onclick = null;
            cancelBtn.onclick = null;
            // Smoothly restore focus back to the user input field
            setTimeout(() => input?.focus(), 50);
            resolve(result);
        };

        okBtn.onclick = () => close(true);
        cancelBtn.onclick = () => close(false);
    });
}

// Confirmation helper returning a Promise<boolean>
function showConfirm(message) {
    return showModalDialog(message, false);
}

// Globally override native window.alert with custom modal
window.alert = function (message) {
    showModalDialog(message, true);
};

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

window.onload = async () => {
    await initPromptSelect();

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

    await loadHistory();

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

        const currentBranch = res.headers.get("X-Current-Branch") || "main";
        if (currentBranch) {
            updateBranchIndicator(currentBranch);
        }

        const currentPrompt = res.headers.get("X-Current-Prompt");
        if (currentPrompt) {
            const triggerBtn = document.getElementById("prompt-select-btn");
            const labelEl = document.getElementById("prompt-select-label");
            const menuEl = document.getElementById("prompt-select-menu");

            if (triggerBtn && labelEl && menuEl) {
                triggerBtn.dataset.value = currentPrompt;

                menuEl.querySelectorAll(".custom-header-option").forEach(item => {
                    const isMatch = item.dataset.value === currentPrompt;
                    if (isMatch) {
                        labelEl.innerText = item.querySelector("span").innerText;
                        item.classList.add("text-[#879A39]", "font-medium", "bg-[#879A39]/10");
                        item.classList.remove("text-gray-700", "dark:text-[#CECDC3]");
                        item.querySelector(".check-icon")?.classList.remove("hidden");
                    } else {
                        item.classList.remove("text-[#879A39]", "font-medium", "bg-[#879A39]/10");
                        item.classList.add("text-gray-700", "dark:text-[#CECDC3]");
                        item.querySelector(".check-icon")?.classList.add("hidden");
                    }
                });
            }
        }

        const historyList = await res.json();

        if (Array.isArray(historyList) && historyList.length > 0) {
            chatBox.innerHTML = "";
            // Reset tracker array to avoid stale DOM references
            SideTracker.bubbles = [];

            // Check if the current active view is inside a side branch lineage
            const isCurrentInSide = currentBranch.toLowerCase().includes("side");

            let lastAiWrapperCol = null;

            historyList.forEach((m) => {
                const isUser = m.role === "user";
                const content = isUser
                    ? formatUserText(m.content)
                    : safeMarkdownParse(m.content);

                const isSideMsg = Boolean(m.branch && m.branch.toLowerCase().includes("side"));

                // Key Rule:
                // 1. While inside a side branch (including nested forks like side#1 -> side#2),
                //    all messages are considered active — never discarded.
                // 2. Only when viewed from the main branch are abandoned side messages treated as discarded.
                const isDiscarded = !isCurrentInSide && isSideMsg;

                if (isUser) {
                    let userBubbleInner = "";
                    if (m.image) {
                        userBubbleInner += `<img src="data:image/jpeg;base64,${m.image}" class="max-w-64 max-h-48 w-auto h-auto object-cover rounded-lg mb-2 shadow-xs border border-black/5 dark:border-white/5 cursor-pointer hover:opacity-90 active:scale-[0.98] transition-all">`;
                    }
                    if (m.content) {
                        userBubbleInner += `<div>${content}</div>`;
                    }

                    const html = `
                        <div class="message-row flex justify-start items-start gap-3 mb-8 ${isDiscarded ? 'is-discarded' : ''}">
                            ${getUserAvatarHtml()}
                            <div class="user-bubble p-4 rounded-2xl max-w-[85%]">
                                ${userBubbleInner}
                            </div>
                        </div>`;
                    chatBox.insertAdjacentHTML("beforeend", html);

                    // Track active side bubbles so "Switch to main" can discard them all at once
                    if (isCurrentInSide && isSideMsg) {
                        SideTracker.bubbles.push(chatBox.lastElementChild);
                    }
                } else {
                    const wrapper = document.createElement("div");
                    wrapper.className = `message-row flex justify-start items-start gap-3 mb-8 ${isDiscarded ? 'is-discarded' : ''}`;
                    wrapper.innerHTML = `
                         ${getAiAvatarHtml()}
                        <div class="flex flex-col max-w-[90%] w-full">
                            <div class="ai-bubble p-4 rounded-2xl markdown-body">
                                ${content}
                            </div>
                        </div>`;

                    const aiBubbleEl = wrapper.querySelector(".ai-bubble");
                    if (aiBubbleEl) {
                        aiBubbleEl.dataset.raw = m.content;
                    }

                    // Check if archived branches exist
                    if (m.archives && m.archives.length > 0) {
                        const archiveEntry = document.createElement("div");
                        archiveEntry.className = "mt-2 px-2";
                        archiveEntry.innerHTML = `
                            <button class="flex items-center gap-1.5 text-[11px] font-mono text-emerald-600 dark:text-emerald-400 bg-emerald-50/50 dark:bg-emerald-400/5 px-2 py-1 rounded-lg border border-emerald-200/50 dark:border-emerald-500/20 hover:bg-emerald-100 dark:hover:bg-emerald-400/10 transition-all">
                                <span>📂</span> 
                                <span>View ${m.archives.length} merged branch(es) under this node</span>
                            </button>
                        `;

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
                                    <span>${SUMMARY_HTML}</span> Conversation Summary
                                </div>
                            </div>
                            <div class="summary-content markdown-body text-xs opacity-90">${m.abstract}</div>
                        `;
                        wrapper
                            .querySelector(".flex-col")
                            .appendChild(summaryBox);
                    }

                    lastAiWrapperCol = wrapper.querySelector(".flex-col");
                    chatBox.appendChild(wrapper);

                    // Track active side bubbles for unified discard action
                    if (isCurrentInSide && isSideMsg) {
                        SideTracker.bubbles.push(wrapper);
                    }
                }
            });

            if (lastAiWrapperCol) {
                const notebookBar = createNotebookBar();
                lastAiWrapperCol.appendChild(notebookBar);
            }

            // Code block syntax highlighting & emoji parsing
            chatBox
                .querySelectorAll(".ai-bubble pre code")
                .forEach((el) => hljs.highlightElement(el));
            attachCodeCopyButtons(chatBox);
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
const openParserBtn = document.getElementById("open-parser-btn");


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
    sendBtn.innerHTML = `<svg class="w-4 h-4 animate-pulse" fill="currentColor" viewBox="0 0 24 24"><rect x="6" y="6" width="12" height="12" rx="2" /></svg>`;

    if (openParserBtn) {
        openParserBtn.classList.add("opacity-40", "pointer-events-none", "cursor-not-allowed");
    }

    // Stash image and reset input
    const imageToSend = currentImageBase64;
    input.value = "";
    input.style.height = "auto";
    clearImage(); // Clear image preview

    // Build user UI bubble
    const safeUserText = formatUserText(text);
    let userBubbleHtml = `<div class="message-row flex justify-start items-start gap-3 mb-8">${getUserAvatarHtml()}<div class="user-bubble p-4 rounded-2xl max-w-[85%]">`;
    // Insert image node if present
    if (imageToSend) {
        userBubbleHtml += `<img src="${imageToSend}" class="max-w-64 max-h-48 w-auto h-auto object-cover rounded-lg mb-2 shadow-xs border border-black/5 dark:border-white/5 cursor-pointer hover:opacity-90 active:scale-[0.98] transition-all">`;
    }

    // Insert text content if present
    if (safeUserText) {
        userBubbleHtml += `<div>${safeUserText}</div>`;
    }

    userBubbleHtml += `</div></div>`;

    chatBox.insertAdjacentHTML("beforeend", userBubbleHtml);
    SideTracker.track(chatBox.lastElementChild);

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
            let displayTitle = `${WARNING_ICON_HTML} System Error Encountered`;
            let displayMsg = formatUserText(
                err.message || "Connection error. Please check backend server.",
            );

            if (err.message === "401_UNAUTHORIZED") {
                displayTitle = "🔑 Authentication Failed";
                displayMsg =
                    "A valid API Key is required to continue. Please verify it in settings.";
            }

            const errorHtml = `
                <div class="flex justify-start mb-6 message-row">
                    <div class="p-4 rounded-2xl bg-[#FFFCF0] dark:bg-[#1C1B1A] border border-[#F2D6D3] dark:border-[#AF3029]/30 text-sm shadow-xs max-w-[90%] transition-colors">
                        <div class="font-bold mb-1.5 flex items-center gap-2 text-[#AF3029] dark:text-[#D14D41]">
                            ${displayTitle}
                        </div>
                        <div class="text-[#6F6E69] dark:text-[#B7B5AC] font-mono text-xs leading-relaxed">
                            ${displayMsg}
                        </div>
                    </div>
                </div>`;

            chatBox.insertAdjacentHTML("beforeend", errorHtml);
            chatBox.scrollTop = chatBox.scrollHeight;
            checkLlamaConnection();
        }
    } finally {
        loading.classList.add("hidden");
        sendBtn.classList.remove("is-loading");
        sendBtn.innerHTML = SEND_ICON_HTML;
        chatAbortController = null;
        if (openParserBtn) {
            openParserBtn.classList.remove("opacity-40", "pointer-events-none", "cursor-not-allowed");
        }
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
                <div class="message-row flex justify-start items-start gap-3 mb-8">
                    ${getAiAvatarHtml()}
                    <div class="flex flex-col max-w-[85%]">
                        <div id="${ctx.currentBubbleId}" class="ai-bubble p-4 rounded-2xl markdown-body">
                        </div>
                        <div id="meta-${ctx.currentBubbleId}" class="flex items-center gap-3 px-2 mt-1.5 text-xs text-gray-400 dark:text-gray-400 font-['JetBrains_Mono'] opacity-80">
                            <span class="bg-gray-100 dark:bg-white/5 px-1.5 py-0.5 rounded text-[11px]">${ctx.modelName}</span>
                            <span id="speed-${ctx.currentBubbleId}">⏱️ Calculating...</span>
                        </div>
                    </div>
                </div>`;
            chatBox.insertAdjacentHTML("beforeend", html);
            ctx.aiBubbleDiv = document.getElementById(ctx.currentBubbleId);
            ctx.isFirstChunk = false;
            SideTracker.track(chatBox.lastElementChild);

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
    ctx.aiBubbleDiv.dataset.raw = ctx.fullText;

    // Highlight code blocks and parse emojis
    ctx.aiBubbleDiv
        .querySelectorAll("pre code")
        .forEach((el) => hljs.highlightElement(el));
    twemoji.parse(ctx.aiBubbleDiv, { folder: "svg", ext: ".svg" });
    attachCodeCopyButtons(ctx.aiBubbleDiv);

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
        "notebook-bar relative flex items-center justify-start my-3 transition-opacity duration-200";

    notebookBar.innerHTML = `
        <div class="inline-flex items-center gap-1.5 text-xs font-['JetBrains_Mono'] whitespace-nowrap">

            <button class="switch-btn flex items-center gap-1 px-2 py-1 rounded-lg bg-white dark:bg-[#1C1B1A] border border-gray-200/90 dark:border-[#282726] text-gray-700 dark:text-[#CECDC3] hover:text-rose-600 dark:hover:text-[#D14D41] hover:border-rose-300 dark:hover:border-[#D14D41]/30 hover:bg-rose-50/60 dark:hover:bg-[#AF3029]/10 shadow-xs active:scale-[0.98] transition-all">
                <span>${SWITCH_MAIN_HTML}</span>
                <span>Switch to main</span>
            </button>

            <div class="relative inline-block">
                <button class="fork-side-btn flex items-center gap-1 px-2 py-1 rounded-lg bg-white dark:bg-[#1C1B1A] border border-gray-200/90 dark:border-[#282726] text-gray-700 dark:text-[#CECDC3] hover:text-emerald-600 dark:hover:text-[#879A39] hover:border-emerald-300 dark:hover:border-[#879A39]/30 hover:bg-emerald-50/60 dark:hover:bg-[#66800B]/10 shadow-xs active:scale-[0.98] transition-all">
                    <span>${SIDE_BRANCH_HTML}</span>
                    <span>Side Chat</span>
                </button>
                
                <div class="fork-options-popover hidden absolute bottom-full mb-2 left-0 w-52 bg-white dark:bg-[#1C1B1A] border border-gray-200 dark:border-[#282726] rounded-xl shadow-lg p-1.5 z-30 flex flex-col gap-1 text-[11px] animate-fade-in">
                    <button class="fork-with-summary text-left px-2.5 py-1.5 rounded-lg hover:bg-emerald-50 dark:hover:bg-[#66800B]/10 text-gray-700 dark:text-[#CECDC3] flex items-center gap-2 transition-colors">
                        <span>${SUMMARY_HTML}</span>
                        <div>
                            <div class="font-medium">With Summary</div>
                        </div>
                    </button>
                    <button class="fork-clean text-left px-2.5 py-1.5 rounded-lg hover:bg-emerald-50 dark:hover:bg-[#66800B]/10 text-gray-700 dark:text-[#CECDC3] flex items-center gap-2 transition-colors">
                        <span>${SANDBOX_HTML}</span>
                        <div>
                            <div class="font-medium">Isolated Sandbox</div>
                        </div>
                    </button>
                </div>
            </div>

            <button class="summary-btn flex items-center gap-1 px-2 py-1 rounded-lg bg-white dark:bg-[#1C1B1A] border border-gray-200/90 dark:border-[#282726] text-gray-700 dark:text-[#CECDC3] hover:text-amber-600 dark:hover:text-[#D0A215] hover:border-amber-300 dark:hover:border-[#D0A215]/30 hover:bg-amber-50/60 dark:hover:bg-[#AD8301]/10 shadow-xs active:scale-[0.98] transition-all">
                <span>${SUMMARY_HTML}</span>
                <span>Summary</span>
            </button>

            <button class="merge-btn flex items-center gap-1 px-2 py-1 rounded-lg bg-white dark:bg-[#1C1B1A] border border-gray-200/90 dark:border-[#282726] text-gray-700 dark:text-[#CECDC3] hover:text-purple-600 dark:hover:text-[#CE5D97] hover:border-purple-300 dark:hover:border-[#CE5D97]/30 hover:bg-purple-50/60 dark:hover:bg-[#8E4399]/10 shadow-xs active:scale-[0.98] transition-all">
                <span>${MERGE_ICON_HTML}</span>
                <span>Merge</span>
            </button>

            <button class="archive-btn flex items-center gap-1 px-2 py-1 rounded-lg bg-white dark:bg-[#1C1B1A] border border-gray-200/90 dark:border-[#282726] text-gray-700 dark:text-[#CECDC3] hover:text-blue-600 dark:hover:text-[#4385BE] hover:border-blue-300 dark:hover:border-[#4385BE]/30 hover:bg-blue-50/60 dark:hover:bg-[#205EA6]/10 shadow-xs active:scale-[0.98] transition-all">
                <span>${CHECKPOINT_MAIN_HTML}</span>
                <span>Checkpoint Main</span>
            </button>
        </div>
    `;

    // Archive event
    const archiveBtn = notebookBar.querySelector(".archive-btn");

    archiveBtn.addEventListener("click", async () => {
        const indicator = document.getElementById("branch-indicator");
        if (indicator && indicator.innerText.includes("side")) {
            alert("Cannot checkpoint main while in a side branch! Please switch back to main or merge first.");
            return;
        }

        const originalContent = archiveBtn.innerHTML;

        try {
            archiveBtn.innerHTML = `<span class="text-blue-500 animate-spin">${LOADING_HTML}</span> Archiving...`;
            archiveBtn.disabled = true;

            let res = await fetch("/api/archive-main", {
                method: "POST",
                headers: getHeaders(),
            });

            // If no summary exists, prompt to generate summary first
            if (res.status === 412) {
                alert("Please click Summary to generate a summary before archiving.");
                archiveBtn.innerHTML = originalContent;
                archiveBtn.disabled = false;
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
                archiveBtn.innerHTML = originalContent;
                archiveBtn.disabled = false;
            }
        } catch (e) {
            console.error(e);
            archiveBtn.innerHTML = originalContent;
            archiveBtn.disabled = false;
        }
    });

    // Bind Fork Side event (Pipeline: Generate Summary -> Review Modal -> Archive Main -> Fork Side)
    const doForkSide = async (withSummary) => {
        try {
            const histRes = await fetch("/api/history", { headers: getHeaders() });
            const currentBranch = histRes.headers.get("X-Current-Branch") || "main";

            // 1. Check if a summary already exists (supporting both editable textarea and historical rendered text)
            const summaryBox = notebookBar.parentElement?.querySelector(".summary-box");
            let existingSummary =
                summaryBox?.querySelector("textarea")?.value?.trim() ||
                summaryBox?.querySelector(".summary-content")?.textContent?.trim();

            let modalResult = null;


            // 2. If no summary exists, generate it first and prompt user for review/edit
            if (!existingSummary) {
                const loadingNotice = document.createElement("div");
                loadingNotice.className = "text-center my-3 text-xs text-amber-600 dark:text-amber-400 font-mono py-1";
                loadingNotice.innerText = "Generating summary before branching...";
                chatBox.appendChild(loadingNotice);
                chatBox.scrollTop = chatBox.scrollHeight;

                const customUrl = localStorage.getItem("custom_api_url") || "";
                const customKey = localStorage.getItem("custom_api_key") || "";

                const absRes = await fetch("/api/generate-abstract", {
                    method: "POST",
                    headers: getHeaders(),
                    body: JSON.stringify({
                        custom_url: customUrl,
                        custom_key: customKey,
                    }),
                });

                loadingNotice.remove();

                if (!absRes.ok) {
                    alert("Failed to generate summary; unable to fork side branch.");
                    return;
                }

                const data = await absRes.json();
                const generatedAbstract = data.abstract || data.DialogAbstract || "";

                //  Open unified modal (review/edit summary + select persona) 
                modalResult = await showForkModal({
                    initialSummary: generatedAbstract,
                    needSummaryEdit: true
                });

                if (!modalResult) return; // Abort if cancelled by user

                // Persist modified summary to backend if edited
                if (modalResult.finalSummary !== generatedAbstract) {
                    await fetch("/api/edit-abstract", {
                        method: "POST",
                        headers: getHeaders(),
                        body: JSON.stringify({ abstract: modalResult.finalSummary }),
                    });
                }
            } else {
                // Summary already exists; prompt for persona selection only 
                modalResult = await showForkModal({ needSummaryEdit: false });
                if (!modalResult) return;
            }

            // 3. Archive main branch (required before branching off main)
            if (currentBranch === "main") {
                const archRes = await fetch("/api/archive-main", {
                    method: "POST",
                    headers: getHeaders(),
                });

                if (!archRes.ok) {
                    const err = await archRes.json().catch(() => ({}));
                    alert(err.error || "Failed to archive main branch; unable to fork side branch.");
                    return;
                }
            }

            // 4. Fork side branch
            const res = await fetch("/api/fork-side", {
                method: "POST",
                headers: getHeaders(),
                body: JSON.stringify({ with_summary: withSummary }),
            });

            if (res.ok) {
                if (modalResult && modalResult.finalPromptId) {
                    await fetch("/api/switch-prompt", {
                        method: "POST",
                        headers: getHeaders(),
                        body: JSON.stringify({ id: modalResult.finalPromptId }),
                    });
                }

                updateBranchIndicator("side");

                if (typeof loadHistory === "function") {
                    await loadHistory();
                }

                setTimeout(() => {
                    const notice = document.createElement("div");
                    notice.className =
                        "flex items-center justify-center gap-1.5 text-center my-3 text-xs text-emerald-600 dark:text-emerald-400 font-mono bg-emerald-50 dark:bg-emerald-950/40 py-1.5 px-3 rounded-lg border border-emerald-200 dark:border-emerald-800/50";
                    notice.innerHTML = withSummary
                        ? `<span>${SIDE_BRANCH_HTML}</span> Switched to side branch (Inherited summary)`
                        : `<span>${SANDBOX_HTML}</span> Switched to isolated side branch (Clean context)`;

                    chatBox.appendChild(notice);
                    chatBox.scrollTo({ top: chatBox.scrollHeight, behavior: "smooth" });
                }, 60);
            }
        } catch (e) {
            console.error("Fork Side failed:", e);
            alert("Failed to fork side branch, please try again.");
        }
    };


    /**
     * Unified fork modal dialog (Flexoki Dark Theme):
     * - If needSummaryEdit is true, provides both summary review/editing and persona selection.
     * - If needSummaryEdit is false, provides persona selection only.
     */
    async function showForkModal({ initialSummary = "", needSummaryEdit = false }) {
        const res = await fetch("/api/prompts", { headers: getHeaders() });
        const data = res.ok ? await res.json() : { prompts: [], active: "" };
        const prompts = data.prompts || [];
        const activeId = data.active || (prompts[0]?.id || "");

        let selectedPromptId = activeId;
        const currentPromptObj = prompts.find(p => p.id === activeId) || prompts[0];

        return new Promise((resolve) => {
            const modal = document.createElement("div");
            modal.className = "fixed inset-0 z-[75] flex items-center justify-center bg-black/60 backdrop-blur-sm animate-fade-in";

            const summarySectionHtml = needSummaryEdit ? `
                <div class="flex flex-col gap-1.5">
                    <label class="text-[11px] font-mono text-[#878580]">Review & Edit Summary:</label>
                    <textarea id="modal-fork-summary-text" class="w-full h-32 p-3 text-xs rounded-xl border border-gray-200 dark:border-[#282726] bg-gray-50/50 dark:bg-[#100F0F] focus:outline-none focus:ring-1 focus:ring-[#879A39] text-gray-800 dark:text-[#CECDC3] resize-none font-sans leading-relaxed"></textarea>
                </div>
            ` : ``;

            const customOptionsHtml = prompts.map(p => `
                <div class="custom-option px-3 py-2 text-xs rounded-lg cursor-pointer flex items-center justify-between transition-colors hover:bg-black/5 dark:hover:bg-[#282726] ${p.id === activeId ? 'text-[#879A39] font-medium bg-[#879A39]/10' : 'text-gray-700 dark:text-[#CECDC3]'}" data-value="${p.id}">
                    <span>${p.name}</span>
                    <span class="check-icon text-[11px] ${p.id === activeId ? '' : 'hidden'}">✓</span>
                </div>
            `).join("");

            modal.innerHTML = `
            <div class="bg-white dark:bg-[#1C1B1A] w-full max-w-md p-6 rounded-2xl shadow-2xl mx-4 border border-gray-100 dark:border-[#282726] flex flex-col gap-4 transition-colors font-sans select-none">
                <div class="flex items-center gap-2 text-sm font-semibold text-gray-900 dark:text-[#CECDC3]">
                    <span class="text-emerald-600 dark:text-[#879A39]">${SIDE_BRANCH_HTML}</span>
                    <span>Fork to Side Branch</span>
                </div>

                ${summarySectionHtml}

                <div class="flex flex-col gap-1.5 relative">
                    <label class="text-[11px] font-mono text-gray-500 dark:text-[#878580]">Select Persona for Side Branch:</label>
                    
                    <button type="button" id="custom-select-trigger" class="w-full flex items-center justify-between bg-gray-50 dark:bg-[#100F0F] border border-gray-200 dark:border-[#282726] rounded-xl px-3 py-2 text-xs text-gray-800 dark:text-[#CECDC3] focus:outline-none focus:ring-1 focus:ring-[#879A39] cursor-pointer">
                        <span id="custom-select-label">${currentPromptObj ? currentPromptObj.name : 'Select Persona'}</span>
                        <svg class="w-3.5 h-3.5 text-gray-400 transition-transform duration-200" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7"></path>
                        </svg>
                    </button>

                    <div id="custom-select-menu" class="hidden absolute top-full mt-1.5 left-0 w-full bg-white dark:bg-[#1C1B1A] border border-gray-200 dark:border-[#282726] rounded-xl shadow-xl p-1 z-30 flex flex-col gap-0.5 max-h-48 overflow-y-auto animate-fade-in">
                        ${customOptionsHtml}
                    </div>
                </div>

                <div class="flex justify-end gap-3 mt-2">
                    <button class="modal-cancel px-4 py-2 text-xs font-medium rounded-xl hover:bg-gray-100 dark:hover:bg-white/5 text-gray-600 dark:text-[#878580] transition-colors">
                        Cancel
                    </button>
                    <button class="modal-confirm px-4 py-2 text-xs font-medium text-[#FFFCF0] bg-[#66800B] hover:bg-[#526609] dark:bg-[#879A39] dark:text-[#100F0F] dark:hover:bg-[#97AC44] active:scale-[0.98] rounded-xl transition-all shadow-xs font-medium">
                        Confirm & Fork
                    </button>
                </div>
            </div>
            `;

            document.body.appendChild(modal);

            const summaryTextarea = modal.querySelector("#modal-fork-summary-text");
            if (summaryTextarea) summaryTextarea.value = initialSummary;

            const triggerBtn = modal.querySelector("#custom-select-trigger");
            const menuEl = modal.querySelector("#custom-select-menu");
            const labelEl = modal.querySelector("#custom-select-label");
            const arrowEl = triggerBtn.querySelector("svg");

            triggerBtn.onclick = (e) => {
                e.stopPropagation();
                const isHidden = menuEl.classList.toggle("hidden");
                arrowEl.style.transform = isHidden ? "" : "rotate(180deg)";
            };

            modal.querySelectorAll(".custom-option").forEach(opt => {
                opt.onclick = (e) => {
                    e.stopPropagation();
                    selectedPromptId = opt.dataset.value;
                    labelEl.innerText = opt.querySelector("span").innerText;

                    modal.querySelectorAll(".custom-option").forEach(item => {
                        item.classList.remove("text-[#879A39]", "font-medium", "bg-[#879A39]/10");
                        item.classList.add("text-gray-700", "dark:text-[#CECDC3]");
                        item.querySelector(".check-icon")?.classList.add("hidden");
                    });

                    opt.classList.add("text-[#879A39]", "font-medium", "bg-[#879A39]/10");
                    opt.classList.remove("text-gray-700", "dark:text-[#CECDC3]");
                    opt.querySelector(".check-icon")?.classList.remove("hidden");

                    menuEl.classList.add("hidden");
                    arrowEl.style.transform = "";
                };
            });

            modal.onclick = (e) => {
                if (!triggerBtn.contains(e.target) && !menuEl.contains(e.target)) {
                    menuEl.classList.add("hidden");
                    arrowEl.style.transform = "";
                }
            };

            const cleanup = () => modal.remove();

            modal.querySelector(".modal-cancel").onclick = () => {
                cleanup();
                resolve(null);
            };

            modal.querySelector(".modal-confirm").onclick = () => {
                const finalSummary = summaryTextarea ? summaryTextarea.value.trim() : null;
                cleanup();
                resolve({ finalSummary, finalPromptId: selectedPromptId });
            };
        });
    }

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
                    <span>${SUMMARY_HTML}</span> Conversation Summary
                </div>
                <div id="summary-actions" class="flex items-center gap-2">
                    <!-- Save button injected here -->
                </div>
            </div>
            <div class="summary-content markdown-body text-xs opacity-90">processing...</div>
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
                btn.innerHTML = `<span class="text-amber-500 animate-spin">${LOADING_HTML}</span> waiting...`;

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
                    saveBtn.innerHTML = `<span>${LOADING_HTML}</span> Saving...`;
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
                btn.innerHTML = `<span class="text-amber-500 font-bold">${SUMMARY_HTML}</span> Summary`;
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
        if (!await showConfirm("Are you sure you want to discard the current side branch?")) return;

        try {
            switchBtn.innerHTML = `<span class="text-rose-500 animate-spin">${LOADING_HTML}</span> switching...`;

            const res = await fetch("/api/switch-side-to-main", {
                method: "POST",
                headers: getHeaders(),
            });

            if (res.ok) {
                updateBranchIndicator("main");
                SideTracker.discard();
                await loadHistory();

                switchBtn.innerHTML = `<span class="text-rose-500 font-bold">✓</span> Discarded`;
                switchBtn.disabled = true;

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
        btn.innerHTML = `<span class="text-purple-500 animate-spin">${LOADING_HTML}</span> Merging...`;
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
            btn.innerHTML = `<span class="text-amber-500 animate-pulse">${SUMMARY_HTML}</span> Summarizing first...`;

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
        SideTracker.commit();

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

// Download/Export chat (supports inline side-branch detours & merged archives)
async function downloadChat() {
    try {
        const res = await fetch("/api/history", { headers: getHeaders() });
        const currentBranch = res.headers.get("X-Current-Branch") || "main";
        const data = await res.json();

        if (!data || data.length === 0) {
            alert("No chat history available to export");
            return;
        }

        const now = new Date();
        const dateStr = now.toISOString().slice(0, 10);
        let content = `==================== Chat History (${dateStr}) ====================\n\n`;

        const isSideBranch = (branchName) => {
            if (!branchName) return false;
            return branchName.toLowerCase().startsWith("side");
        };

        const formatMessage = (m, defaultBranch) => {
            const branch = m.branch || defaultBranch;
            const branchLabel = isSideBranch(branch) ? "Side Branch" : "Main Branch";
            const roleLabel = m.role === "user" ? `[User [${branchLabel}]]` : `[AI [${branchLabel}]]`;

            let text = `${roleLabel}\n${(m.content || "").trim()}\n\n`;

            if (m.abstract && m.abstract.trim() !== "") {
                text += `[Summary]\n${m.abstract.trim()}\n\n`;
            }
            return text;
        };

        data.forEach((m, index) => {
            content += formatMessage(m, currentBranch);

            if (m.archives && Array.isArray(m.archives) && m.archives.length > 0) {
                m.archives.forEach((branchMsgs, bIdx) => {
                    content += `>>> [Archived Merged Side Branch #${bIdx + 1} Under Message #${index + 1}] <<<\n\n`;
                    branchMsgs.forEach((archivedMsg) => {
                        const role = archivedMsg.role === "user" ? "[User [Side Branch (Merged)]]" : "[AI [Side Branch (Merged)]]";
                        content += `${role}\n${(archivedMsg.content || "").trim()}\n\n`;
                    });
                    content += `>>> [End of Merged Side Branch #${bIdx + 1}] <<<\n\n`;
                });
            }

            content += "--------------------------------------------------\n\n";
        });

        const blob = new Blob([content], { type: "text/plain;charset=utf-8" });
        const url = URL.createObjectURL(blob);
        const a = document.createElement("a");
        a.href = url;
        a.download = `chat-${dateStr}.txt`;

        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);

        URL.revokeObjectURL(url);
    } catch (e) {
        console.error("Failed to export chat history:", e);
        alert("Export failed, please try again.");
    }
}

async function newChat() {
    if (await showConfirm("Clear all conversations?")) {
        // Abort in-flight request if currently generating
        if (chatAbortController) {
            chatAbortController.abort();
            chatAbortController = null;
        }

        // Reset button and loading states
        loading.classList.add("hidden");
        sendBtn.classList.remove("is-loading");
        sendBtn.innerHTML = SEND_ICON_HTML;

        try {
            await fetch("/api/new-chat", { headers: getHeaders() });
            updateBranchIndicator("main");
            chatBox.innerHTML = `
                <div class="message-row flex justify-start items-start gap-3 mb-8">
                    ${getAiAvatarHtml()}
                    <div class="ai-bubble p-4 rounded-2xl max-w-[85%] markdown-body">
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

const userAvatarInput = document.getElementById("upload-user-avatar");
const aiAvatarInput = document.getElementById("upload-ai-avatar");
const userPreview = document.getElementById("setting-user-preview");
const aiPreview = document.getElementById("setting-ai-preview");
const resetUserBtn = document.getElementById("reset-user-avatar");
const resetAiBtn = document.getElementById("reset-ai-avatar");

let tempUserAvatar = null;
let tempAiAvatar = null;

async function compressAvatar(file) {
    return new Promise((resolve, reject) => {
        const reader = new FileReader();
        reader.readAsDataURL(file);
        reader.onload = (e) => {
            const img = new Image();
            img.src = e.target.result;
            img.onload = () => {
                const canvas = document.createElement("canvas");
                const size = 128;
                canvas.width = size;
                canvas.height = size;
                const ctx = canvas.getContext("2d");
                const minSide = Math.min(img.width, img.height);
                const sx = (img.width - minSide) / 2;
                const sy = (img.height - minSide) / 2;
                ctx.drawImage(img, sx, sy, minSide, minSide, 0, 0, size, size);
                resolve(canvas.toDataURL("image/jpeg", 0.85));
            };
            img.onerror = reject;
        };
        reader.onerror = reject;
    });
}

function refreshAvatarPreviews() {
    const curUser = tempUserAvatar !== null ? tempUserAvatar : (localStorage.getItem("custom_user_avatar") || "");
    const curAi = tempAiAvatar !== null ? tempAiAvatar : (localStorage.getItem("custom_ai_avatar") || "");

    const defaultUserSvg = "data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' fill='%23666' viewBox='0 0 24 24'><path d='M12 12c2.21 0 4-1.79 4-4s-1.79-4-4-4-4 1.79-4 4 1.79 4 4 4zm0 2c-2.67 0-8 1.34-8 4v2h16v-2c0-2.66-5.33-4-8-4z'/></svg>";
    const defaultAiSvg = "data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' fill='%23666' viewBox='0 0 24 24'><path d='M12 12c2.21 0 4-1.79 4-4s-1.79-4-4-4-4 1.79-4 4 1.79 4 4 4zm0 2c-2.67 0-8 1.34-8 4v2h16v-2c0-2.66-5.33-4-8-4z'/></svg>";
    userPreview.src = curUser || defaultUserSvg;
    aiPreview.src = curAi || defaultAiSvg;
}

document.getElementById("settings-btn").addEventListener("click", () => {
    customUrlInput.value = localStorage.getItem("custom_api_url") || "";
    if (customEmbeddingUrlInput) {
        customEmbeddingUrlInput.value = localStorage.getItem("custom_embedding_url") || "";
    }
    customKeyInput.value = localStorage.getItem("custom_api_key") || "";

    tempUserAvatar = null;
    tempAiAvatar = null;
    refreshAvatarPreviews();
    modal.classList.remove("hidden");
});

userAvatarInput.addEventListener("change", async (e) => {
    const file = e.target.files[0];
    if (file) {
        tempUserAvatar = await compressAvatar(file);
        refreshAvatarPreviews();
    }
});

aiAvatarInput.addEventListener("change", async (e) => {
    const file = e.target.files[0];
    if (file) {
        tempAiAvatar = await compressAvatar(file);
        refreshAvatarPreviews();
    }
});

resetUserBtn.onclick = () => {
    tempUserAvatar = "";
    refreshAvatarPreviews();
};
resetAiBtn.onclick = () => {
    tempAiAvatar = "";
    refreshAvatarPreviews();
};

document.getElementById("save-settings").addEventListener("click", () => {
    localStorage.setItem("custom_api_url", customUrlInput.value.trim());
    if (customEmbeddingUrlInput) {
        localStorage.setItem("custom_embedding_url", customEmbeddingUrlInput.value.trim());
    }
    localStorage.setItem("custom_api_key", customKeyInput.value.trim());

    if (tempUserAvatar !== null) {
        if (tempUserAvatar === "") localStorage.removeItem("custom_user_avatar");
        else localStorage.setItem("custom_user_avatar", tempUserAvatar);
    }
    if (tempAiAvatar !== null) {
        if (tempAiAvatar === "") localStorage.removeItem("custom_ai_avatar");
        else localStorage.setItem("custom_ai_avatar", tempAiAvatar);
    }

    document.querySelectorAll(".user-avatar-node").forEach(el => {
        el.outerHTML = getUserAvatarHtml();
    });
    document.querySelectorAll(".ai-avatar-node").forEach(el => {
        el.outerHTML = getAiAvatarHtml();
    });

    modal.classList.add("hidden");
});


document.getElementById("close-settings").addEventListener("click", () => {
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
                statusDot.className = "w-2 h-2 rounded-full bg-[#66800B] dark:bg-[#879A39] transition-colors duration-300";
                statusDot.title = "Connected to llama.cpp successfully";
                return;
            }
        }
        throw new Error("Service status abnormal");
    } catch (err) {
        statusDot.className = "w-2 h-2 rounded-full bg-[#AF3029] dark:bg-[#D14D41] transition-colors duration-300 animate-pulse";
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
                tokenDisplay.classList.add("text-[#AF3029]", "dark:text-[#D14D41]");
            } else {
                tokenDisplay.classList.remove("text-[#AF3029]", "dark:text-[#D14D41]", "text-rose-500");
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
            let icon = "✨";
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

// open parser

if (openParserBtn) {
    openParserBtn.addEventListener("click", async () => {
        try {
            openParserBtn.classList.add("opacity-50", "pointer-events-none");

            const res = await fetch("/api/open-parser", {
                method: "POST",
                headers: getHeaders()
            });

            if (!res.ok) {
                throw new Error("Failed to trigger parser");
            }
        } catch (e) {
            console.error("Failed to launch parser:", e);
            alert("Could not launch parser.exe. Please ensure it exists in parser_py/");
        } finally {
            setTimeout(() => {
                openParserBtn.classList.remove("opacity-50", "pointer-events-none");
            }, 800);
        }
    });
}

// copy btn
const COPY_ICON_SVG = `<svg class="w-3.5 h-3.5 inline-block" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"></path></svg>`;
const CHECK_ICON_SVG = `<svg class="w-3.5 h-3.5 inline-block text-emerald-500" fill="none" stroke="currentColor" stroke-width="2.5" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7"></path></svg>`;

/**
 * copy buttons
 * @param {HTMLElement} container 
 */
function attachCodeCopyButtons(container = document) {
    container.querySelectorAll(".markdown-body pre").forEach((pre) => {

        if (pre.querySelector(".code-copy-btn")) return;

        const copyBtn = document.createElement("button");
        copyBtn.className = "code-copy-btn flex items-center gap-1 font-mono";
        copyBtn.innerHTML = `${COPY_ICON_SVG} <span>Copy</span>`;

        copyBtn.onclick = async () => {
            const codeEl = pre.querySelector("code");

            const textToCopy = codeEl ? codeEl.innerText : pre.innerText;

            try {
                await navigator.clipboard.writeText(textToCopy);
                copyBtn.innerHTML = `${CHECK_ICON_SVG} <span class="text-emerald-500">Copied!</span>`;
                setTimeout(() => {
                    copyBtn.innerHTML = `${COPY_ICON_SVG} <span>Copy</span>`;
                }, 1500);
            } catch (err) {
                console.error("copy error:", err);
            }
        };

        pre.appendChild(copyBtn);
    });
}

// ==================== default avator SVG ====================
const PERSON_SVG_PATH = `
<svg class="w-7 h-7" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24">
    <path stroke-linecap="round" stroke-linejoin="round" d="M15.75 6a3.75 3.75 0 11-7.5 0 3.75 3.75 0 017.5 0zM4.501 20.118a7.5 7.5 0 0114.998 0A17.933 17.933 0 0112 21.75c-2.676 0-5.216-.584-7.499-1.632z"/>
</svg>`;

const DEFAULT_AI_ICON = `
<div class="w-full h-full bg-gray-100 dark:bg-white/10 text-gray-600 dark:text-gray-300 flex items-center justify-center">
    ${PERSON_SVG_PATH}
</div>`;

const DEFAULT_USER_ICON = `
<div class="w-full h-full bg-gray-100 dark:bg-white/10 text-gray-600 dark:text-gray-300 flex items-center justify-center">
    ${PERSON_SVG_PATH}
</div>`;


function getUserAvatarHtml() {
    const custom = localStorage.getItem("custom_user_avatar");
    const inner = custom
        ? `<img src="${custom}" class="w-full h-full object-cover">`
        : DEFAULT_USER_ICON;
    return `<div class="user-avatar-node w-12 h-12 rounded-full border border-gray-200 dark:border-gray-700/80 flex items-center justify-center shrink-0 shadow-xs select-none overflow-hidden">${inner}</div>`;
}

function getAiAvatarHtml() {
    const custom = localStorage.getItem("custom_ai_avatar");
    const inner = custom
        ? `<img src="${custom}" class="w-full h-full object-cover">`
        : DEFAULT_AI_ICON;
    return `<div class="ai-avatar-node w-12 h-12 rounded-full border border-gray-200 dark:border-gray-700/80 flex items-center justify-center shrink-0 shadow-xs select-none overflow-hidden">${inner}</div>`;
}

// ==================== SIDE bubble track ====================
const SideTracker = {
    bubbles: [],

    isSide() {
        const indicator = document.getElementById("branch-indicator");
        return indicator && indicator.innerText.toLowerCase().includes("side");
    },

    track(bubbleEl) {
        if (bubbleEl && this.isSide()) {
            this.bubbles.push(bubbleEl);
        }
    },

    discard() {
        this.bubbles.forEach((row) => {
            if (row && row.classList) {
                row.classList.add("is-discarded");
                row.querySelectorAll(".notebook-bar").forEach(bar => bar.remove());
            }
        });
        this.bubbles = [];
    },

    commit() {
        this.bubbles = [];
    }
};


// ==================== EDIT LATEST AI MESSAGE ====================
const editLatestAiBtn = document.getElementById("edit-latest-ai-btn");
const editAiModal = document.getElementById("edit-ai-modal");
const editAiTextarea = document.getElementById("edit-ai-textarea");
const cancelEditAiBtn = document.getElementById("cancel-edit-ai-btn");
const saveEditAiBtn = document.getElementById("save-edit-ai-btn");

let targetAiBubble = null;

if (editLatestAiBtn) {
    editLatestAiBtn.addEventListener("click", () => {

        const allAiBubbles = Array.from(document.querySelectorAll("#chat-box .ai-bubble")).filter(
            el => !el.closest(".message-row.is-discarded")
        );

        if (allAiBubbles.length === 0) {
            alert("No editable AI response found.");
            return;
        }

        targetAiBubble = allAiBubbles[allAiBubbles.length - 1];

        editAiTextarea.value = targetAiBubble.dataset.raw !== undefined ? targetAiBubble.dataset.raw : targetAiBubble.innerText.trim();
        editAiModal.classList.remove("hidden");
        editAiTextarea.focus();
    });
}

if (cancelEditAiBtn) {
    cancelEditAiBtn.addEventListener("click", () => {
        editAiModal.classList.add("hidden");
        targetAiBubble = null;
    });
}

if (saveEditAiBtn) {
    saveEditAiBtn.addEventListener("click", async () => {
        if (!targetAiBubble) return;

        const newContent = editAiTextarea.value.trim();
        const origBtnText = saveEditAiBtn.innerText;
        saveEditAiBtn.innerText = "Saving...";
        saveEditAiBtn.disabled = true;

        try {
            const res = await fetch("/api/edit-latest-ai", {
                method: "POST",
                headers: getHeaders(),
                body: JSON.stringify({ content: newContent }),
            });

            if (!res.ok) {
                let errorMsg = "";
                try {
                    const errData = await res.json();
                    errorMsg = errData.error || errData.message;
                } catch (_) {
                    errorMsg = await res.text();
                }
                throw new Error(errorMsg || "Unable to edit response: This message is locked or has been archived.");
            }

            targetAiBubble.dataset.raw = newContent;
            targetAiBubble.innerHTML = safeMarkdownParse(newContent);

            targetAiBubble.querySelectorAll("pre code").forEach((el) => hljs.highlightElement(el));
            attachCodeCopyButtons(targetAiBubble);
            twemoji.parse(targetAiBubble, { folder: "svg", ext: ".svg" });

            editAiModal.classList.add("hidden");
            targetAiBubble = null;
        } catch (err) {
            console.error(err);
            editAiModal.classList.add("hidden");
            targetAiBubble = null;

            alert("Cannot save edit:\n" + err.message.trim());
        } finally {
            saveEditAiBtn.innerText = origBtnText;
            saveEditAiBtn.disabled = false;
        }
    });
}

// ==================== image view ====================
const imageViewerModal = document.getElementById("image-viewer-modal");
const imageViewerImg = document.getElementById("image-viewer-img");

if (imageViewerModal && imageViewerImg) {
    chatBox.addEventListener("click", (e) => {
        if (e.target.tagName === "IMG" && !e.target.classList.contains("emoji")) {
            imageViewerImg.src = e.target.src;
            imageViewerModal.classList.remove("hidden");
        }
    });

    imageViewerModal.addEventListener("click", () => {
        imageViewerModal.classList.add("hidden");
        imageViewerImg.src = "";
    });

    window.addEventListener("keydown", (e) => {
        if (e.key === "Escape" && !imageViewerModal.classList.contains("hidden")) {
            imageViewerModal.classList.add("hidden");
            imageViewerImg.src = "";
        }
    });
}

