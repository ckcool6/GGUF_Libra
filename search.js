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
 */
export async function realWebSearch(query, searchKey) {
    if (!searchKey) {
        throw new Error("Please configure your Pro Web Search Key in Settings first.");
    }

    const res = await fetch(`${SEARCH_GATEWAY_URL}/v1/search`, {
        method: "POST",
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
        throw new Error(err.detail || `Search gateway error (${res.status})`);
    }

    const data = await res.json();

    // 同步更新设置页的余额徽章
    if (creditsCount && data.remaining_credits !== undefined) {
        creditsCount.innerText = data.remaining_credits;
    }

    return data.sources;
}

/**
 * 将搜索结果格式化为注入大模型的 Prompt 上下文
 */
export function formatSearchContextForLLM(results) {
    if (!results || results.length === 0) return "";

    const today = new Date().toISOString().slice(0, 10);
    let contextStr = `\n\n[Web Search Results | Reference Date: ${today}]\n`;

    results.forEach((item, index) => {
        contextStr += `Source [${index + 1}]:\n`;
        contextStr += `Title: ${item.title}\n`;
        contextStr += `URL: ${item.url}\n`;
        contextStr += `Content: ${item.snippet}\n\n`;
    });

    contextStr += `Instructions: You have access to real-time search results above. Answer the user prompt accurately based on these sources if relevant. Cite sources individually using separate brackets like [1] or [1][2] (do NOT combine them like [1, 2]).\n`;

    return contextStr;
}

/**
 * 一站式执行搜索：查 Key -> 请求网关 -> 缓存来源 -> 返回拼接好的上下文
 */
export async function performWebSearch(query) {
    const searchKey = localStorage.getItem("custom_search_key") || "";
    const searchResults = await realWebSearch(query, searchKey);
    currentSearchSources = searchResults;
    return formatSearchContextForLLM(searchResults);
}

/**
 * 将 HTML 中的 [1], [2] 转换为可点击、带 Tooltip 提示的超链接角标卡片
 */
export function linkifyCitations(html, sources = currentSearchSources) {
    if (!sources || sources.length === 0) return html;

    // 匹配 [1] 以及 [1, 2]、[1,2,3]、[1、2] 等组合格式
    return html.replace(/\[(\d+(?:\s*[,，、]\s*\d+)*)\]/g, (match, group) => {
        const nums = group.split(/\s*[,，、]\s*/);
        let hasValidLink = false;

        const badges = nums.map((numStr) => {
            const index = parseInt(numStr, 10) - 1;
            const source = sources[index];

            if (source && source.url) {
                hasValidLink = true;
                const safeTitle = (source.title || "Reference source").replace(/"/g, '&quot;');
                return `<a href="${source.url}" target="_blank" rel="noopener noreferrer" 
                    class="citation-badge inline-flex items-center justify-center text-[10px] font-mono font-semibold text-emerald-700 dark:text-[#879A39] bg-emerald-100/70 dark:bg-[#879A39]/15 border border-emerald-300/80 dark:border-[#879A39]/30 rounded px-1.5 py-0.2 mx-0.5 align-super hover:underline hover:scale-105 active:scale-95 transition-all shadow-2xs select-none" 
                    title="${safeTitle}">[${numStr}]</a>`;
            }
            return `[${numStr}]`;
        });

        return hasValidLink ? badges.join("") : match;
    });
}

// 保留控制台调试方法
window.testSearchPipeline = async (keyword = "quantum computing") => {
    console.log("--- Starting Search Pipeline Test ---");
    const key = localStorage.getItem("custom_search_key") || "";
    const rawData = await realWebSearch(keyword, key);
    console.log("1. Raw search API response object:", rawData);
    const finalPrompt = formatSearchContextForLLM(rawData);
    console.log("2. Final injected text for LLM:\n" + finalPrompt);
    console.log("--- Test completed ---");
};