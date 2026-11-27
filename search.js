/**
 * Copyright 2026 Lu ZhiYuan
 * SPDX-License-Identifier: AGPL-3.0-only
 */

/**
 * search.js - Standalone Web Search Module
 */

const SEARCH_GATEWAY_URL = "http://192.168.190.128:8000"; // VM test gateway endpoint

let currentSearchSources = [];
let isWebSearchEnabled = localStorage.getItem("enable_web_search") === "true";

// DOM references (lazily resolved during init to prevent script execution timing issues)
let webSearchBtn = null;
let customSearchKeyInput = null;
let creditsBadge = null;
let creditsCount = null;

/**
 * Utility: Generate local time string formatted as YYYY-MM-DD HH:mm:ss
 * (Prevents UTC timezone day-offset discrepancies)
 */
function getLocalDateTimeString(date = new Date()) {
    const pad = (n) => String(n).padStart(2, "0");
    return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}

/**
 * Utility: Format news publication date (ISO/RFC) into readable local time YYYY-MM-DD HH:mm
 */
function formatPublishedDate(rawDate) {
    if (!rawDate) return "";
    const parsed = new Date(rawDate);
    if (isNaN(parsed.getTime())) return rawDate; // Fallback to raw string if non-standard format
    const pad = (n) => String(n).padStart(2, "0");
    return `${parsed.getFullYear()}-${pad(parsed.getMonth() + 1)}-${pad(parsed.getDate())} ${pad(parsed.getHours())}:${pad(parsed.getMinutes())}`;
}

/**
 * Initialize web search elements and bind user interaction events
 */
export function initWebSearch() {
    webSearchBtn = document.getElementById("web-search-btn");
    customSearchKeyInput = document.getElementById("custom-search-key");
    creditsBadge = document.getElementById("credits-badge");
    creditsCount = document.getElementById("credits-count");

    updateWebSearchBtnUI();

    if (webSearchBtn) {
        webSearchBtn.addEventListener("click", () => {
            isWebSearchEnabled = !isWebSearchEnabled;
            localStorage.setItem("enable_web_search", isWebSearchEnabled);
            updateWebSearchBtnUI();
        });
    }

    if (customSearchKeyInput) {
        customSearchKeyInput.addEventListener("blur", () => {
            fetchUserCredits(customSearchKeyInput.value.trim());
        });
    }
}

/**
 * Check if web search toggle is currently active
 */
export function isWebSearchActive() {
    return isWebSearchEnabled;
}

/**
 * Clear or retrieve current search sources
 */
export function clearSearchSources() {
    currentSearchSources = [];
}

export function getSearchSources() {
    return currentSearchSources;
}

/**
 * Update the web search icon UI state next to the input area
 */
function updateWebSearchBtnUI() {
    if (!webSearchBtn) return;
    if (isWebSearchEnabled) {
        webSearchBtn.classList.add(
            "text-emerald-600", "dark:text-[#879A39]",
            "bg-emerald-50", "dark:bg-[#879A39]/15"
        );
        webSearchBtn.classList.remove("text-gray-400", "dark:text-[#878580]");
        webSearchBtn.title = "Web Search: ON (Pro)";
        webSearchBtn.querySelector("svg")?.classList.add("scale-110");
    } else {
        webSearchBtn.classList.remove(
            "text-emerald-600", "dark:text-[#879A39]",
            "bg-emerald-50", "dark:bg-[#879A39]/15"
        );
        webSearchBtn.classList.add("text-gray-400", "dark:text-[#878580]");
        webSearchBtn.title = "Web Search: OFF";
        webSearchBtn.querySelector("svg")?.classList.remove("scale-110");
    }
}

/**
 * Settings modal: populate Key and refresh credit balance upon opening
 */
export function loadSearchSettings() {
    const savedKey = localStorage.getItem("custom_search_key") || "";
    if (customSearchKeyInput) {
        customSearchKeyInput.value = savedKey;
    }
    fetchUserCredits(savedKey);
}

/**
 * Settings modal: persist Key to storage upon saving
 */
export function saveSearchSettings() {
    if (customSearchKeyInput) {
        localStorage.setItem("custom_search_key", customSearchKeyInput.value.trim());
    }
}

/**
 * Fetch and update remaining credits for the Pro Search Key
 */
export async function fetchUserCredits(searchKey) {
    if (!searchKey || !creditsBadge || !creditsCount) {
        creditsBadge?.classList.add("hidden");
        return;
    }

    try {
        const res = await fetch(`${SEARCH_GATEWAY_URL}/v1/user/credits`, {
            headers: { "Authorization": `Bearer ${searchKey}` }
        });

        if (res.ok) {
            const data = await res.json();
            creditsCount.innerText = data.credits;
            creditsBadge.classList.remove("hidden");
        } else {
            creditsBadge.classList.add("hidden");
        }
    } catch (e) {
        console.warn("Failed to check credits:", e);
        creditsBadge?.classList.add("hidden");
    }
}

/**
 * Call remote search gateway (small model query optimization + Tavily + credit deduction)
 * Returns { sources, searchedAt }
 */
export async function realWebSearch(query, searchKey, signal = undefined) {
    if (!searchKey) {
        throw new Error("Please configure your Pro Web Search Key in Settings first.");
    }

    const res = await fetch(`${SEARCH_GATEWAY_URL}/v1/search`, {
        method: "POST",
        signal,
        headers: {
            "Content-Type": "application/json",
            "Authorization": `Bearer ${searchKey}`
        },
        body: JSON.stringify({ query: query })
    });

    if (res.status === 401) {
        throw new Error("Pro Search Key is invalid or expired. Please check settings.");
    }
    if (res.status === 402) {
        throw new Error("Credits exhausted! Please top up your account.");
    }
    if (!res.ok) {
        const err = await res.json().catch(() => ({}));
        const detailMsg = Array.isArray(err.detail)
            ? err.detail.map(e => e.msg || JSON.stringify(e)).join("; ")
            : err.detail;
        throw new Error(detailMsg || `Search gateway error (${res.status})`);
    }

    const data = await res.json();

    // Synchronize balance badge in the settings view
    if (creditsCount && data.remaining_credits !== undefined) {
        creditsCount.innerText = data.remaining_credits;
    }

    return {
        sources: data.sources || [],
        searchedAt: data.searched_at || ""
    };
}

/**
 * Format search results into a structured prompt context for LLM ingestion
 */
export function formatSearchContextForLLM(results, searchedAt = "") {
    if (!results || results.length === 0) return "";

    // Prefer searched_at from gateway; fallback to local browser time (non-UTC)
    let referenceTime = getLocalDateTimeString();
    if (searchedAt) {
        const parsed = new Date(searchedAt);
        referenceTime = isNaN(parsed.getTime()) ? searchedAt : getLocalDateTimeString(parsed);
    }
    let contextStr = `\n\n[Web Search Results | Current Time: ${referenceTime}]\n`;

    results.forEach((item, index) => {
        contextStr += `Source [${index + 1}]:\n`;
        contextStr += `Title: ${item.title}\n`;
        if (item.published_date) {
            contextStr += `Published Date: ${formatPublishedDate(item.published_date)}\n`;
        }
        contextStr += `URL: ${item.url}\n`;
        contextStr += `Content: ${item.snippet}\n\n`;
    });

    contextStr += `Instructions: You have access to real-time search results above. Answer the user prompt accurately based on these sources if relevant. Cite sources individually using separate brackets like [1] or [1][2] (do NOT combine them like [1, 2]).\n`;

    return contextStr;
}

/**
 * End-to-end search pipeline: resolve Key -> query gateway -> cache sources -> return formatted context
 */
export async function performWebSearch(query, signal = undefined) {
    const searchKey = localStorage.getItem("custom_search_key") || "";
    const truncatedQuery = query.trim().slice(0, 500);
    const { sources, searchedAt } = await realWebSearch(truncatedQuery, searchKey, signal);
    currentSearchSources = sources;
    return formatSearchContextForLLM(sources, searchedAt);
}

/**
 * Convert [1], [1, 2] in HTML into clickable citation badge cards with publication date tooltips
 * (Built on DOM TreeWalker for safe replacement, bypassing code blocks, links, and KaTeX formulas)
 */
export function linkifyCitations(html, sources = currentSearchSources) {
    if (!sources || sources.length === 0) return html;

    // 1. Parse into in-memory DOM (input HTML is pre-sanitized by DOMPurify in safeMarkdownParse)
    const container = document.createElement("div");
    container.innerHTML = html;

    // 2. Filter text nodes only, strictly rejecting pre / code / a / script / style / .katex
    const walker = document.createTreeWalker(container, NodeFilter.SHOW_TEXT, {
        acceptNode(node) {
            const parent = node.parentElement;
            if (!parent) return NodeFilter.FILTER_REJECT;

            if (parent.closest("pre, code, a, script, style, .katex")) {
                return NodeFilter.FILTER_REJECT;
            }

            return /\[\d+(?:\s*[,，、]\s*\d+)*\]/.test(node.nodeValue)
                ? NodeFilter.FILTER_ACCEPT
                : NodeFilter.FILTER_REJECT;
        }
    });

    // Collect candidate text nodes first to prevent cursor invalidation during active mutation
    const textNodes = [];
    let currentNode;
    while ((currentNode = walker.nextNode())) {
        textNodes.push(currentNode);
    }

    // 3. Perform safe replacements per text node
    textNodes.forEach((textNode) => {
        const text = textNode.nodeValue;
        const regex = /\[(\d+(?:\s*[,，、]\s*\d+)*)\]/g;
        const frag = document.createDocumentFragment();
        let lastIndex = 0;
        let match;

        while ((match = regex.exec(text)) !== null) {
            // Append plain text prior to current match
            if (match.index > lastIndex) {
                frag.appendChild(
                    document.createTextNode(text.slice(lastIndex, match.index))
                );
            }

            const nums = match[1].split(/\s*[,，、]\s*/);
            const tempFrag = document.createDocumentFragment();
            let hasValidLink = false;

            nums.forEach((numStr) => {
                const index = parseInt(numStr, 10) - 1;
                const source = sources[index];
                const rawUrl = (source?.url || "").trim();

                // Strict protocol validation: allow only http:// or https:// to block javascript: pseudo-protocols
                if (source && /^https?:\/\//i.test(rawUrl)) {
                    hasValidLink = true;

                    const a = document.createElement("a");
                    a.href = rawUrl;
                    a.target = "_blank";
                    a.rel = "noopener noreferrer";
                    a.className =
                        "citation-badge inline-flex items-center justify-center text-[10px] font-mono font-semibold text-emerald-700 dark:text-[#879A39] bg-emerald-100/70 dark:bg-[#879A39]/15 border border-emerald-300/80 dark:border-[#879A39]/30 rounded px-1.5 py-0.2 mx-0.5 align-super hover:underline hover:scale-105 active:scale-95 transition-all shadow-2xs select-none";

                    let tooltipText = source.title || "Reference source";
                    if (source.published_date) {
                        tooltipText += ` (${formatPublishedDate(source.published_date)})`;
                    }

                    // Native DOM assignment auto-escapes special characters to eliminate quotation/XSS issues
                    a.title = tooltipText;
                    a.textContent = `[${numStr}]`;

                    tempFrag.appendChild(a);
                } else {
                    tempFrag.appendChild(document.createTextNode(`[${numStr}]`));
                }
            });

            if (hasValidLink) {
                frag.appendChild(tempFrag);
            } else {
                // Keep original match if indices do not exist within sources (e.g., [2026])
                frag.appendChild(document.createTextNode(match[0]));
            }

            lastIndex = regex.lastIndex;
        }

        // Append trailing plain text
        if (lastIndex < text.length) {
            frag.appendChild(document.createTextNode(text.slice(lastIndex)));
        }

        textNode.parentNode.replaceChild(frag, textNode);
    });

    return container.innerHTML;
}

// Global console test helper
window.testSearchPipeline = async (keyword = "quantum computing") => {
    console.log("--- Starting Search Pipeline Test ---");
    const key = localStorage.getItem("custom_search_key") || "";
    const { sources, searchedAt } = await realWebSearch(keyword, key);
    console.log("1. Raw search API response:", { sources, searchedAt });
    const finalPrompt = formatSearchContextForLLM(sources, searchedAt);
    console.log("2. Final injected text for LLM:\n" + finalPrompt);
    console.log("--- Test completed ---");
};