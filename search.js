/**
 * search.js - 独立联网搜索模块
 */

const SEARCH_GATEWAY_URL = "http://192.168.190.128:8000"; // vm test

let currentSearchSources = [];
let isWebSearchEnabled = localStorage.getItem("enable_web_search") === "true";

// DOM 引用（延迟在 init 时获取，防止脚本加载时机问题）
let webSearchBtn = null;
let customSearchKeyInput = null;
let creditsBadge = null;
let creditsCount = null;

/**
 * 工具函数：生成本地时间字符串 YYYY-MM-DD HH:mm:ss（彻底避免 UTC 时区慢一天的问题）
 */
function getLocalDateTimeString(date = new Date()) {
    const pad = (n) => String(n).padStart(2, "0");
    return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}

/**
 * 工具函数：将新闻发布时间（如 ISO 或 RFC 格式）转换为可读的本地时间 YYYY-MM-DD HH:mm
 */
function formatPublishedDate(rawDate) {
    if (!rawDate) return "";
    const parsed = new Date(rawDate);
    if (isNaN(parsed.getTime())) return rawDate; // 如果后端返回非标准格式，直接原样保留
    const pad = (n) => String(n).padStart(2, "0");
    return `${parsed.getFullYear()}-${pad(parsed.getMonth() + 1)}-${pad(parsed.getDate())} ${pad(parsed.getHours())}:${pad(parsed.getMinutes())}`;
}

/**
 * 初始化联网搜索相关的按钮与事件绑定
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
 * 当前是否开启了联网搜索
 */
export function isWebSearchActive() {
    return isWebSearchEnabled;
}

/**
 * 清空 / 获取本轮搜索来源
 */
export function clearSearchSources() {
    currentSearchSources = [];
}

export function getSearchSources() {
    return currentSearchSources;
}

/**
 * 更新输入框旁边的地球图标状态
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
 * 设置页：打开设置弹窗时回填 Key 并查询余额
 */
export function loadSearchSettings() {
    const savedKey = localStorage.getItem("custom_search_key") || "";
    if (customSearchKeyInput) {
        customSearchKeyInput.value = savedKey;
    }
    fetchUserCredits(savedKey);
}

/**
 * 设置页：点击保存时写入 Key
 */
export function saveSearchSettings() {
    if (customSearchKeyInput) {
        localStorage.setItem("custom_search_key", customSearchKeyInput.value.trim());
    }
}

/**
 * 查询 Pro Key 剩余积分
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
 * 调用远程搜索网关（小模型提词 + Tavily + 扣点）
 * 返回 { sources, searchedAt }
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

    // 同步更新设置页的余额徽章
    if (creditsCount && data.remaining_credits !== undefined) {
        creditsCount.innerText = data.remaining_credits;
    }

    return {
        sources: data.sources || [],
        searchedAt: data.searched_at || ""
    };
}

/**
 * 将搜索结果格式化为注入大模型的 Prompt 上下文
 */
export function formatSearchContextForLLM(results, searchedAt = "") {
    if (!results || results.length === 0) return "";

    // 优先用后端返回的 searched_at，如果没有则用浏览器本地时间（非 UTC）
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
 * 一站式执行搜索：查 Key -> 请求网关 -> 缓存来源 -> 返回拼接好的上下文
 */
export async function performWebSearch(query, signal = undefined) {
    const searchKey = localStorage.getItem("custom_search_key") || "";
    const truncatedQuery = query.trim().slice(0, 500);
    const { sources, searchedAt } = await realWebSearch(truncatedQuery, searchKey, signal);
    currentSearchSources = sources;
    return formatSearchContextForLLM(sources, searchedAt);
}

/**
 * 将 HTML 中的 [1], [1, 2] 转换为可点击、带发布日期 Tooltip 的超链接角标卡片
 * （基于 DOM TreeWalker 安全替换，跳过代码块、已有链接及 KaTeX 公式）
 */
export function linkifyCitations(html, sources = currentSearchSources) {
    if (!sources || sources.length === 0) return html;

    // 1. 解析为内存中的 DOM 树（传入的 html 已经过 safeMarkdownParse 的 DOMPurify 清洗）
    const container = document.createElement("div");
    container.innerHTML = html;

    // 2. 只遍历纯文本节点，跳过 pre / code / a / script / style / .katex
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

    // 先收集所有待替换的文本节点，避免边遍历边修改 DOM 导致游标错乱
    const textNodes = [];
    let currentNode;
    while ((currentNode = walker.nextNode())) {
        textNodes.push(currentNode);
    }

    // 3. 逐个文本节点做安全替换
    textNodes.forEach((textNode) => {
        const text = textNode.nodeValue;
        const regex = /\[(\d+(?:\s*[,，、]\s*\d+)*)\]/g;
        const frag = document.createDocumentFragment();
        let lastIndex = 0;
        let match;

        while ((match = regex.exec(text)) !== null) {
            // 保留匹配前的普通文本
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

                // 严格校验协议：只放行 http:// 或 https://，彻底拦截 javascript: 等伪协议
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

                    // 使用 DOM 属性赋值，浏览器会自动转义特殊字符，无需手动处理引号/XSS
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
                // 如果序号都不在 sources 范围内（比如 [2026]），原样保留
                frag.appendChild(document.createTextNode(match[0]));
            }

            lastIndex = regex.lastIndex;
        }

        // 保留剩余文本
        if (lastIndex < text.length) {
            frag.appendChild(document.createTextNode(text.slice(lastIndex)));
        }

        textNode.parentNode.replaceChild(frag, textNode);
    });

    return container.innerHTML;
}

// 保留控制台调试方法
window.testSearchPipeline = async (keyword = "quantum computing") => {
    console.log("--- Starting Search Pipeline Test ---");
    const key = localStorage.getItem("custom_search_key") || "";
    const { sources, searchedAt } = await realWebSearch(keyword, key);
    console.log("1. Raw search API response:", { sources, searchedAt });
    const finalPrompt = formatSearchContextForLLM(sources, searchedAt);
    console.log("2. Final injected text for LLM:\n" + finalPrompt);
    console.log("--- Test completed ---");
};